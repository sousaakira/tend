// Package vt implements tend's terminal emulator core.
//
// Parser is the escape-sequence state machine. It follows the DEC
// ANSI-compatible machine documented by Paul Williams
// (https://vt100.net/emu/dec_ansi_parser), with three deliberate extensions:
//
//   - UTF-8 is decoded in the ground state, incrementally, so a rune may be
//     split across Parse calls.
//   - CSI sub-parameters separated by ':' are preserved, which SGR needs for
//     direct colors written as 38:2::R:G:B.
//   - APC payloads are captured rather than discarded, so kitty-graphics
//     support can be layered on later without touching the machine.
//
// Two behaviours differ from xterm on purpose:
//
//   - 8-bit C1 controls (0x80-0x9F) are never honoured. In a UTF-8 terminal
//     those bytes are continuation bytes, and honouring them corrupts any
//     non-ASCII text. Sequences must use the 7-bit ESC forms.
//   - A string sequence whose payload exceeds MaxStringLen is dropped rather
//     than truncated, so a handler never acts on half a title.
//
// Parse does not allocate on the hot path: parameter and intermediate storage
// is fixed-size and reused across dispatches. Handlers must therefore treat
// every slice and *Params they receive as valid only for the duration of the
// call.
package vt

import "unicode/utf8"

// Limits on a single escape sequence. Anything beyond these is still consumed,
// but the resulting dispatch is flagged with ignore=true.
const (
	// MaxParams is the number of CSI/DCS parameters retained, counting
	// colon-separated sub-parameters individually.
	MaxParams = 32
	// MaxIntermediates is the number of intermediate and private-marker bytes
	// retained for one sequence.
	MaxIntermediates = 2
	// DefaultMaxStringLen bounds an OSC, DCS or APC payload.
	DefaultMaxStringLen = 1 << 16

	// paramMax matches xterm's per-parameter clamp.
	paramMax = 65535
)

// Handler receives the events a Parser produces. Implementations that only
// care about some events should embed NopHandler.
type Handler interface {
	// Print reports one printable rune. Invalid input is reported as
	// utf8.RuneError.
	Print(r rune)
	// Execute reports a C0 control byte.
	Execute(b byte)
	// ESCDispatch reports a complete escape sequence with no CSI/string body.
	ESCDispatch(intermediates []byte, final byte, ignore bool)
	// CSIDispatch reports a complete control sequence.
	CSIDispatch(params *Params, intermediates []byte, final byte, ignore bool)
	// OSCDispatch reports an operating-system command. params is the payload
	// split on ';'; it always has at least one element.
	OSCDispatch(params [][]byte, bellTerminated bool)
	// DCSHook begins a device-control string. DCSPut then streams its body and
	// DCSUnhook closes it.
	DCSHook(params *Params, intermediates []byte, final byte, ignore bool)
	DCSPut(b byte)
	DCSUnhook()
	// APCDispatch reports an application program command payload.
	APCDispatch(data []byte)
}

// NopHandler implements Handler with no-ops, for embedding.
type NopHandler struct{}

func (NopHandler) Print(rune)                              {}
func (NopHandler) Execute(byte)                            {}
func (NopHandler) ESCDispatch([]byte, byte, bool)          {}
func (NopHandler) CSIDispatch(*Params, []byte, byte, bool) {}
func (NopHandler) OSCDispatch([][]byte, bool)              {}
func (NopHandler) DCSHook(*Params, []byte, byte, bool)     {}
func (NopHandler) DCSPut(byte)                             {}
func (NopHandler) DCSUnhook()                              {}
func (NopHandler) APCDispatch([]byte)                      {}

var _ Handler = NopHandler{}

// Params holds the parameters of a CSI or DCS sequence.
//
// A parameter is "set" when the sequence spelled out digits for it. CSI ; 5 m
// carries two parameters, of which only the second is set. A parameter is a
// "sub" when it was joined to the one before it with ':' rather than ';'.
type Params struct {
	vals [MaxParams]int32
	set  [MaxParams]bool
	subs [MaxParams]bool
	n    int
	full bool
}

// Len returns the number of parameters.
func (p *Params) Len() int { return p.n }

// Get returns the parameter at i, or 0 when i is out of range.
func (p *Params) Get(i int) int32 {
	if i < 0 || i >= p.n {
		return 0
	}
	return p.vals[i]
}

// GetOr returns the parameter at i, substituting def when that parameter is
// absent or zero. ECMA-48 treats an omitted or zero parameter as the default
// for cursor movement and similar commands, so this is the common accessor.
// SGR, where zero is meaningful, should use Get.
func (p *Params) GetOr(i int, def int32) int32 {
	if v := p.Get(i); v != 0 {
		return v
	}
	return def
}

// IsSet reports whether the sequence spelled out digits for parameter i.
func (p *Params) IsSet(i int) bool {
	if i < 0 || i >= p.n {
		return false
	}
	return p.set[i]
}

// IsSub reports whether parameter i was joined to parameter i-1 with ':'.
func (p *Params) IsSub(i int) bool {
	if i < 0 || i >= p.n {
		return false
	}
	return p.subs[i]
}

// GroupEnd returns the exclusive end index of the colon-joined group starting
// at i, so SGR can walk groups with:
//
//	for i := 0; i < ps.Len(); i = ps.GroupEnd(i) { ... }
func (p *Params) GroupEnd(i int) int {
	j := i + 1
	for j < p.n && p.subs[j] {
		j++
	}
	return j
}

func (p *Params) reset() {
	p.n = 0
	p.full = false
}

// state is a node in the Williams machine.
type state uint8

const (
	stateGround state = iota
	stateEscape
	stateEscapeIntermediate
	stateCSIEntry
	stateCSIParam
	stateCSIIntermediate
	stateCSIIgnore
	stateDCSEntry
	stateDCSParam
	stateDCSIntermediate
	stateDCSPassthrough
	stateDCSIgnore
	stateString       // OSC, APC, SOS or PM payload
	stateStringEscape // saw ESC inside a string; ST if '\' follows
)

// strKind records which string sequence is being collected, since they share
// states.
type strKind uint8

const (
	kindNone strKind = iota
	kindOSC
	kindAPC
	kindIgnored // SOS and PM: consumed, never reported
	kindDCS
)

// Parser is an escape-sequence state machine. The zero value is ready to use
// and applies DefaultMaxStringLen. A Parser is not safe for concurrent use.
type Parser struct {
	// MaxStringLen bounds OSC/DCS/APC payloads. Zero means DefaultMaxStringLen.
	// Change it only before the first Parse call.
	MaxStringLen int

	state state

	params       Params
	curParam     int32
	curParamSet  bool
	paramStarted bool
	pendingSub   bool

	inter  [MaxIntermediates]byte
	interN int
	ignore bool

	str         []byte
	strKind     strKind
	strOverflow bool
	oscParams   [][]byte

	utf8Buf  [4]byte
	utf8N    int
	utf8Need int
}

// Reset returns the parser to the ground state, discarding any sequence or
// partial rune in progress. Use it when the byte stream is known to have been
// interrupted, such as after a reconnect.
func (p *Parser) Reset() {
	p.state = stateGround
	p.clear()
	p.str = p.str[:0]
	p.strKind = kindNone
	p.strOverflow = false
	p.utf8N = 0
	p.utf8Need = 0
}

// Parse feeds data to the machine, dispatching to h. Sequences and runes may
// be split across calls.
func (p *Parser) Parse(data []byte, h Handler) {
	for _, b := range data {
		p.step(b, h)
	}
}

func (p *Parser) maxStr() int {
	if p.MaxStringLen > 0 {
		return p.MaxStringLen
	}
	return DefaultMaxStringLen
}

func (p *Parser) clear() {
	p.params.reset()
	p.curParam = 0
	p.curParamSet = false
	p.paramStarted = false
	p.pendingSub = false
	p.interN = 0
	p.ignore = false
}

func (p *Parser) collect(b byte) {
	if p.interN >= MaxIntermediates {
		p.ignore = true
		return
	}
	p.inter[p.interN] = b
	p.interN++
}

// digit accumulates one decimal digit into the parameter being built.
func (p *Parser) digit(b byte) {
	p.paramStarted = true
	p.curParamSet = true
	v := p.curParam*10 + int32(b-'0')
	if v > paramMax {
		v = paramMax
	}
	p.curParam = v
}

// separator closes the current parameter. sub records whether the *next*
// parameter continues this one with ':'.
func (p *Parser) separator(sub bool) {
	p.paramStarted = true
	p.pushParam()
	p.pendingSub = sub
}

func (p *Parser) pushParam() {
	if p.params.n >= MaxParams {
		p.params.full = true
		p.ignore = true
		p.curParam = 0
		p.curParamSet = false
		return
	}
	i := p.params.n
	p.params.vals[i] = p.curParam
	p.params.set[i] = p.curParamSet
	p.params.subs[i] = p.pendingSub
	p.params.n++
	p.curParam = 0
	p.curParamSet = false
}

// finishParams flushes the trailing parameter just before a dispatch.
func (p *Parser) finishParams() {
	if p.paramStarted {
		p.pushParam()
		p.paramStarted = false
	}
}

func (p *Parser) strStart(k strKind) {
	p.str = p.str[:0]
	p.strKind = k
	p.strOverflow = false
}

func (p *Parser) strPut(b byte) {
	if len(p.str) >= p.maxStr() {
		p.strOverflow = true
		return
	}
	p.str = append(p.str, b)
}

// isC0Exec reports whether b is a C0 control that executes in-place, i.e.
// every C0 except CAN, SUB and ESC, which drive the machine itself.
func isC0Exec(b byte) bool {
	return b < 0x20 && b != 0x18 && b != 0x1A && b != 0x1B
}

func (p *Parser) step(b byte, h Handler) {
	switch p.state {
	case stateGround:
		p.ground(b, h)

	case stateEscape:
		switch {
		case isC0Exec(b):
			h.Execute(b)
		case b == 0x18 || b == 0x1A:
			h.Execute(b)
			p.state = stateGround
		case b == 0x1B:
			p.clear()
		case b == 0x7F:
			// ignored
		case b >= 0x20 && b <= 0x2F:
			p.collect(b)
			p.state = stateEscapeIntermediate
		case b == 'P':
			p.clear()
			p.state = stateDCSEntry
		case b == '[':
			p.clear()
			p.state = stateCSIEntry
		case b == ']':
			p.strStart(kindOSC)
			p.state = stateString
		case b == '_':
			p.strStart(kindAPC)
			p.state = stateString
		case b == 'X' || b == '^':
			p.strStart(kindIgnored)
			p.state = stateString
		default: // 0x30-0x7E
			p.finishParams()
			h.ESCDispatch(p.inter[:p.interN], b, p.ignore)
			p.state = stateGround
		}

	case stateEscapeIntermediate:
		switch {
		case isC0Exec(b):
			h.Execute(b)
		case b == 0x18 || b == 0x1A:
			h.Execute(b)
			p.state = stateGround
		case b == 0x1B:
			p.clear()
			p.state = stateEscape
		case b == 0x7F:
			// ignored
		case b >= 0x20 && b <= 0x2F:
			p.collect(b)
		default: // 0x30-0x7E
			h.ESCDispatch(p.inter[:p.interN], b, p.ignore)
			p.state = stateGround
		}

	case stateCSIEntry, stateCSIParam:
		switch {
		case isC0Exec(b):
			h.Execute(b)
		case b == 0x18 || b == 0x1A:
			h.Execute(b)
			p.state = stateGround
		case b == 0x1B:
			p.clear()
			p.state = stateEscape
		case b == 0x7F:
			// ignored
		case b >= '0' && b <= '9':
			p.digit(b)
			p.state = stateCSIParam
		case b == ';':
			p.separator(false)
			p.state = stateCSIParam
		case b == ':':
			p.separator(true)
			p.state = stateCSIParam
		case b >= 0x3C && b <= 0x3F:
			// Private markers are only valid before any parameter.
			if p.state == stateCSIEntry {
				p.collect(b)
				p.state = stateCSIParam
			} else {
				p.state = stateCSIIgnore
			}
		case b >= 0x20 && b <= 0x2F:
			p.collect(b)
			p.state = stateCSIIntermediate
		default: // 0x40-0x7E
			p.finishParams()
			h.CSIDispatch(&p.params, p.inter[:p.interN], b, p.ignore)
			p.state = stateGround
		}

	case stateCSIIntermediate:
		switch {
		case isC0Exec(b):
			h.Execute(b)
		case b == 0x18 || b == 0x1A:
			h.Execute(b)
			p.state = stateGround
		case b == 0x1B:
			p.clear()
			p.state = stateEscape
		case b == 0x7F:
			// ignored
		case b >= 0x20 && b <= 0x2F:
			p.collect(b)
		case b >= 0x30 && b <= 0x3F:
			p.state = stateCSIIgnore
		default: // 0x40-0x7E
			p.finishParams()
			h.CSIDispatch(&p.params, p.inter[:p.interN], b, p.ignore)
			p.state = stateGround
		}

	case stateCSIIgnore:
		switch {
		case isC0Exec(b):
			h.Execute(b)
		case b == 0x18 || b == 0x1A:
			h.Execute(b)
			p.state = stateGround
		case b == 0x1B:
			p.clear()
			p.state = stateEscape
		case b >= 0x40 && b <= 0x7E:
			// Consumed without dispatch.
			p.state = stateGround
		}

	case stateDCSEntry, stateDCSParam:
		switch {
		case b == 0x18 || b == 0x1A:
			h.Execute(b)
			p.state = stateGround
		case b == 0x1B:
			p.clear()
			p.state = stateEscape
		case b < 0x20 || b == 0x7F:
			// ignored: C0 has no meaning before a DCS body
		case b >= '0' && b <= '9':
			p.digit(b)
			p.state = stateDCSParam
		case b == ';':
			p.separator(false)
			p.state = stateDCSParam
		case b == ':':
			p.separator(true)
			p.state = stateDCSParam
		case b >= 0x3C && b <= 0x3F:
			if p.state == stateDCSEntry {
				p.collect(b)
				p.state = stateDCSParam
			} else {
				p.state = stateDCSIgnore
			}
		case b >= 0x20 && b <= 0x2F:
			p.collect(b)
			p.state = stateDCSIntermediate
		default: // 0x40-0x7E
			p.finishParams()
			h.DCSHook(&p.params, p.inter[:p.interN], b, p.ignore)
			p.strKind = kindDCS
			p.state = stateDCSPassthrough
		}

	case stateDCSIntermediate:
		switch {
		case b == 0x18 || b == 0x1A:
			h.Execute(b)
			p.state = stateGround
		case b == 0x1B:
			p.clear()
			p.state = stateEscape
		case b < 0x20 || b == 0x7F:
			// ignored
		case b >= 0x20 && b <= 0x2F:
			p.collect(b)
		case b >= 0x30 && b <= 0x3F:
			p.state = stateDCSIgnore
		default: // 0x40-0x7E
			p.finishParams()
			h.DCSHook(&p.params, p.inter[:p.interN], b, p.ignore)
			p.strKind = kindDCS
			p.state = stateDCSPassthrough
		}

	case stateDCSIgnore:
		switch {
		case b == 0x18 || b == 0x1A:
			h.Execute(b)
			p.state = stateGround
		case b == 0x1B:
			p.clear()
			p.state = stateEscape
		}

	case stateDCSPassthrough:
		switch {
		case b == 0x18 || b == 0x1A:
			h.DCSUnhook()
			h.Execute(b)
			p.strKind = kindNone
			p.state = stateGround
		case b == 0x1B:
			p.state = stateStringEscape
		case b == 0x7F:
			// ignored
		default:
			h.DCSPut(b)
		}

	case stateString:
		switch {
		case b == 0x07 && p.strKind == kindOSC:
			p.dispatchString(h, true)
			p.state = stateGround
		case b == 0x18 || b == 0x1A:
			// Cancelled: the payload is discarded, not reported.
			h.Execute(b)
			p.strKind = kindNone
			p.state = stateGround
		case b == 0x1B:
			p.state = stateStringEscape
		case b < 0x20:
			// C0 inside a string payload is dropped.
		default:
			p.strPut(b)
		}

	case stateStringEscape:
		if b == '\\' { // ST
			if p.strKind == kindDCS {
				h.DCSUnhook()
				p.strKind = kindNone
			} else {
				p.dispatchString(h, false)
			}
			p.state = stateGround
			return
		}
		// Not ST: the string is abandoned and this byte begins a new escape
		// sequence, which is how a terminal recovers from a truncated payload.
		if p.strKind == kindDCS {
			h.DCSUnhook()
		}
		p.strKind = kindNone
		p.clear()
		p.state = stateEscape
		p.step(b, h)
	}
}

// ground handles the ground state, including incremental UTF-8 decoding.
func (p *Parser) ground(b byte, h Handler) {
	if p.utf8N > 0 {
		if b >= 0x80 && b < 0xC0 {
			p.utf8Buf[p.utf8N] = b
			p.utf8N++
			if p.utf8N == p.utf8Need {
				// DecodeRune rejects overlong forms and surrogates for us.
				r, _ := utf8.DecodeRune(p.utf8Buf[:p.utf8N])
				h.Print(r)
				p.utf8N = 0
			}
			return
		}
		// The sequence was truncated. Report the damage, then handle b
		// normally so a control byte is not swallowed.
		h.Print(utf8.RuneError)
		p.utf8N = 0
	}

	switch {
	case b == 0x1B:
		p.clear()
		p.state = stateEscape
	case b == 0x18 || b == 0x1A:
		h.Execute(b)
	case b < 0x20:
		h.Execute(b)
	case b == 0x7F:
		// DEL is ignored.
	case b < 0x80:
		h.Print(rune(b))
	default:
		p.utf8Start(b, h)
	}
}

func (p *Parser) utf8Start(b byte, h Handler) {
	var need int
	switch {
	case b >= 0xC2 && b <= 0xDF:
		need = 2
	case b >= 0xE0 && b <= 0xEF:
		need = 3
	case b >= 0xF0 && b <= 0xF4:
		need = 4
	default:
		// 0x80-0xC1 and 0xF5-0xFF can never start a valid rune. This is also
		// where 8-bit C1 controls land, which tend deliberately does not honour.
		h.Print(utf8.RuneError)
		return
	}
	p.utf8Buf[0] = b
	p.utf8N = 1
	p.utf8Need = need
}

// dispatchString reports a completed OSC or APC payload. Over-long payloads are
// dropped so a handler never sees a truncated title or command.
func (p *Parser) dispatchString(h Handler, bell bool) {
	kind := p.strKind
	p.strKind = kindNone
	if p.strOverflow {
		p.strOverflow = false
		return
	}
	switch kind {
	case kindOSC:
		h.OSCDispatch(p.splitOSC(), bell)
	case kindAPC:
		h.APCDispatch(p.str)
	}
}

// splitOSC splits the payload on ';' into a reused slice, so a dispatch costs
// no allocation once the slice has grown to its working size.
func (p *Parser) splitOSC() [][]byte {
	p.oscParams = p.oscParams[:0]
	start := 0
	for i := 0; i < len(p.str); i++ {
		if p.str[i] == ';' {
			p.oscParams = append(p.oscParams, p.str[start:i])
			start = i + 1
		}
	}
	p.oscParams = append(p.oscParams, p.str[start:])
	return p.oscParams
}
