package ui

import (
	"bytes"
	"strconv"
	"strings"
)

// What the outer terminal says about itself, when asked: whether it is in a
// light or dark scheme (DEC mode 2031's report, `CSI ?997;1n` dark and
// `CSI ?997;2n` light), and what its background colour is (an OSC 11 reply).
// herdr reads both (`terminal_theme.rs`, `raw_input.rs`) to switch themes
// with the terminal.
//
// These arrive on the same input as the keyboard. Left there they would be
// typed into the focused pane as garbage, so they are taken out before the
// key machine sees the bytes.

// HostReport is one thing the terminal said.
type HostReport struct {
	// Explicit is a colour-scheme report, which is the terminal saying which
	// it is; otherwise Light was worked out from the background colour.
	Explicit bool
	Light    bool
	// Focus is a focus report (mode 1004): FocusIn or FocusOut, and then the
	// other fields mean nothing.
	Focus WindowFocus
}

// WindowFocus is what a focus report says about the terminal's window.
type WindowFocus uint8

const (
	// FocusNone means the report is not about focus.
	FocusNone WindowFocus = iota
	// FocusIn and FocusOut are `CSI I` and `CSI O`.
	FocusIn
	FocusOut
)

// hostReportHold bounds how much of a report split across reads is kept
// waiting for the rest. A real reply is a few dozen bytes.
const hostReportHold = 96

// HostReports takes the terminal's reports out of a chunk of input. pending
// is what an earlier call held back because a report had begun and not
// ended; the new pending is returned for the next call.
func HostReports(pending, data []byte) (rest []byte, reports []HostReport, hold []byte) {
	if len(pending) > 0 {
		data = append(append([]byte(nil), pending...), data...)
	}
	for i := 0; i < len(data); {
		if data[i] != 0x1b {
			rest = append(rest, data[i])
			i++
			continue
		}
		report, n, ok, partial := parseHostReport(data[i:])
		switch {
		case ok:
			if report != nil {
				reports = append(reports, *report)
			}
			i += n
		case partial && len(data)-i < hostReportHold:
			return rest, reports, append([]byte(nil), data[i:]...)
		default:
			rest = append(rest, data[i])
			i++
		}
	}
	return rest, reports, nil
}

// parseHostReport reads a report at the start of b. ok means one was read and
// is n bytes long (report is nil for one tend does not use, such as an OSC 10
// reply); partial means b is the beginning of one.
func parseHostReport(b []byte) (report *HostReport, n int, ok, partial bool) {
	const scheme = "\x1b[?997;"
	switch {
	case bytes.HasPrefix(b, []byte("\x1b[I")):
		return &HostReport{Focus: FocusIn}, 3, true, false
	case bytes.HasPrefix(b, []byte("\x1b[O")):
		return &HostReport{Focus: FocusOut}, 3, true, false
	case bytes.HasPrefix(b, []byte(scheme)):
		rest := b[len(scheme):]
		if len(rest) < 2 {
			return nil, 0, false, true
		}
		if rest[1] != 'n' {
			return nil, 0, false, false
		}
		switch rest[0] {
		case '1':
			return &HostReport{Explicit: true}, len(scheme) + 2, true, false
		case '2':
			return &HostReport{Explicit: true, Light: true}, len(scheme) + 2, true, false
		}
		return nil, len(scheme) + 2, true, false
	case len(b) < len(scheme) && bytes.HasPrefix([]byte(scheme), b) && len(b) >= 3:
		return nil, 0, false, true

	case bytes.HasPrefix(b, []byte("\x1b]10;")), bytes.HasPrefix(b, []byte("\x1b]11;")):
		end, termLen := oscEnd(b)
		if end < 0 {
			return nil, 0, false, true
		}
		body := string(b[2:end])
		n = end + termLen
		kind, value, _ := strings.Cut(body, ";")
		r, g, bl, parsed := parseHostColor(value)
		if kind != "11" || !parsed {
			return nil, n, true, false
		}
		return &HostReport{Light: lightBackground(r, g, bl)}, n, true, false
	case len(b) < 5 && (bytes.HasPrefix([]byte("\x1b]10;"), b) || bytes.HasPrefix([]byte("\x1b]11;"), b)) && len(b) >= 3:
		return nil, 0, false, true
	}
	return nil, 0, false, false
}

// oscEnd finds where an OSC ends: BEL or ST.
func oscEnd(b []byte) (end, termLen int) {
	for i := 2; i < len(b); i++ {
		switch {
		case b[i] == 0x07:
			return i, 1
		case b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\':
			return i, 2
		}
	}
	return -1, 0
}

// parseHostColor reads "rgb:RRRR/GGGG/BBBB" or "#RRGGBB", with one to four
// hex digits a component, as herdr does.
func parseHostColor(value string) (r, g, b uint8, ok bool) {
	var parts []string
	switch {
	case strings.HasPrefix(value, "rgb:"):
		parts = strings.Split(value[4:], "/")
	case strings.HasPrefix(value, "#"):
		hex := value[1:]
		digits := len(hex) / 3
		if digits < 1 || digits > 4 || len(hex) != digits*3 {
			return 0, 0, 0, false
		}
		parts = []string{hex[:digits], hex[digits : 2*digits], hex[2*digits:]}
	default:
		return 0, 0, 0, false
	}
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	var out [3]uint8
	for i, p := range parts {
		if len(p) < 1 || len(p) > 4 {
			return 0, 0, 0, false
		}
		v, err := strconv.ParseUint(p, 16, 32)
		if err != nil {
			return 0, 0, 0, false
		}
		max := uint64(1)<<(4*len(p)) - 1
		out[i] = uint8((v*255 + max/2) / max)
	}
	return out[0], out[1], out[2], true
}

// lightBackground is herdr's luminance rule (`inferred_appearance`).
func lightBackground(r, g, b uint8) bool {
	return uint32(r)*299+uint32(g)*587+uint32(b)*114 >= 128_000
}

// Queries and modes for the outer terminal, as herdr sends them.
const (
	// HostSchemeReports asks to be told when the scheme changes (mode 2031),
	// and HostSchemeReportsOff stops it.
	HostSchemeReports    = "\x1b[?2031h"
	HostSchemeReportsOff = "\x1b[?2031l"
	// HostSchemeQuery asks for the scheme now, and HostBackgroundQuery for the
	// background colour, which is all a terminal without 2031 can answer.
	HostSchemeQuery = "\x1b[?996n"
	// HostFocusReports asks the terminal to say when its window gains and
	// loses focus (mode 1004); HostFocusReportsOff stops it.
	HostFocusReports    = "\x1b[?1004h"
	HostFocusReportsOff = "\x1b[?1004l"
	HostBackgroundQuery = "\x1b]11;?\x1b\\"
)
