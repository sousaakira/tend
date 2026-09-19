package vt

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// recorder renders every event as a short string so a whole byte stream can be
// asserted as one readable sequence.
type recorder struct {
	events []string
	dcs    strings.Builder
}

func (r *recorder) Print(c rune) {
	r.events = append(r.events, fmt.Sprintf("print(%q)", c))
}

func (r *recorder) Execute(b byte) {
	r.events = append(r.events, fmt.Sprintf("exec(%02X)", b))
}

func (r *recorder) ESCDispatch(inter []byte, final byte, ignore bool) {
	r.events = append(r.events, fmt.Sprintf("esc(%s,%c%s)", string(inter), final, flag(ignore)))
}

func (r *recorder) CSIDispatch(ps *Params, inter []byte, final byte, ignore bool) {
	r.events = append(r.events, fmt.Sprintf("csi(%s,%s,%c%s)", fmtParams(ps), string(inter), final, flag(ignore)))
}

func (r *recorder) OSCDispatch(params [][]byte, bell bool) {
	parts := make([]string, len(params))
	for i, p := range params {
		parts[i] = string(p)
	}
	term := "st"
	if bell {
		term = "bel"
	}
	r.events = append(r.events, fmt.Sprintf("osc(%s|%s)", strings.Join(parts, "/"), term))
}

func (r *recorder) DCSHook(ps *Params, inter []byte, final byte, ignore bool) {
	r.dcs.Reset()
	r.events = append(r.events, fmt.Sprintf("hook(%s,%s,%c%s)", fmtParams(ps), string(inter), final, flag(ignore)))
}

func (r *recorder) DCSPut(b byte) { r.dcs.WriteByte(b) }

func (r *recorder) DCSUnhook() {
	r.events = append(r.events, fmt.Sprintf("unhook(%s)", r.dcs.String()))
}

func (r *recorder) APCDispatch(data []byte) {
	r.events = append(r.events, fmt.Sprintf("apc(%s)", string(data)))
}

func flag(ignore bool) string {
	if ignore {
		return ",IGN"
	}
	return ""
}

// fmtParams renders parameters as "1;2:3", marking unset ones with '-', so
// tests can distinguish CSI ; 5 m from CSI 0 ; 5 m.
func fmtParams(ps *Params) string {
	var sb strings.Builder
	for i := 0; i < ps.Len(); i++ {
		if i > 0 {
			if ps.IsSub(i) {
				sb.WriteByte(':')
			} else {
				sb.WriteByte(';')
			}
		}
		if !ps.IsSet(i) {
			sb.WriteByte('-')
			continue
		}
		fmt.Fprintf(&sb, "%d", ps.Get(i))
	}
	return sb.String()
}

// run feeds input to a fresh parser as a single write.
func run(t *testing.T, input string) []string {
	t.Helper()
	var r recorder
	var p Parser
	p.Parse([]byte(input), &r)
	return r.events
}

func expect(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event count = %d, want %d\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestPrintASCII(t *testing.T) {
	expect(t, run(t, "hi"), `print('h')`, `print('i')`)
}

func TestExecuteC0(t *testing.T) {
	expect(t, run(t, "a\r\n\x07b"),
		`print('a')`, `exec(0D)`, `exec(0A)`, `exec(07)`, `print('b')`)
}

func TestDELIsIgnored(t *testing.T) {
	expect(t, run(t, "a\x7fb"), `print('a')`, `print('b')`)
}

func TestUTF8(t *testing.T) {
	// Braille U+2801 and the half-circle U+25D0 both appear in agent spinners,
	// which detection matches on, so they are worth pinning explicitly.
	expect(t, run(t, "⠁◐é"), `print('⠁')`, `print('◐')`, `print('é')`)
}

func TestUTF8SplitAcrossWrites(t *testing.T) {
	var r recorder
	var p Parser
	buf := []byte("⠁") // 3 bytes
	for _, b := range buf {
		p.Parse([]byte{b}, &r)
	}
	expect(t, r.events, `print('⠁')`)
}

func TestUTF8Invalid(t *testing.T) {
	cases := map[string]string{
		"lone continuation": "\x80",
		"overlong":          "\xe0\x80\x80",
		"surrogate":         "\xed\xa0\x80",
		"out of range lead": "\xf5",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			got := run(t, input)
			if len(got) == 0 {
				t.Fatal("expected at least one event")
			}
			want := fmt.Sprintf("print(%q)", utf8.RuneError)
			if got[0] != want {
				t.Errorf("first event = %s, want %s", got[0], want)
			}
		})
	}
}

func TestUTF8TruncatedThenControl(t *testing.T) {
	// A control byte arriving mid-rune must still be executed, not swallowed.
	expect(t, run(t, "\xe2\x80\r"),
		fmt.Sprintf("print(%q)", utf8.RuneError), `exec(0D)`)
}

func TestC1BytesAreNotControls(t *testing.T) {
	// 0x9B is CSI in 8-bit form. tend treats it as invalid UTF-8 instead, so
	// that a byte inside a multibyte rune can never be read as a control.
	expect(t, run(t, "\x9b1m"),
		fmt.Sprintf("print(%q)", utf8.RuneError), `print('1')`, `print('m')`)
}

func TestCSIBasic(t *testing.T) {
	expect(t, run(t, "\x1b[H"), `csi(,,H)`)
	expect(t, run(t, "\x1b[2J"), `csi(2,,J)`)
	expect(t, run(t, "\x1b[1;31m"), `csi(1;31,,m)`)
}

func TestCSIUnsetParams(t *testing.T) {
	// CSI ; 5 m carries two parameters; only the second was written out.
	expect(t, run(t, "\x1b[;5m"), `csi(-;5,,m)`)
	expect(t, run(t, "\x1b[0;5m"), `csi(0;5,,m)`)
}

func TestCSIPrivateMarker(t *testing.T) {
	expect(t, run(t, "\x1b[?1049h"), `csi(1049,?,h)`)
	expect(t, run(t, "\x1b[?25l"), `csi(25,?,l)`)
}

func TestCSIIntermediate(t *testing.T) {
	expect(t, run(t, "\x1b[4 q"), `csi(4, ,q)`)
}

func TestCSISubParams(t *testing.T) {
	// Direct color written with colons, which is what makes sub-parameters
	// worth keeping at all.
	expect(t, run(t, "\x1b[38:2::255:0:0m"), `csi(38:2:-:255:0:0,,m)`)
}

func TestCSIParamClamp(t *testing.T) {
	var r recorder
	var p Parser
	p.Parse([]byte("\x1b[99999999m"), &r)
	expect(t, r.events, fmt.Sprintf("csi(%d,,m)", paramMax))
}

func TestCSIParamOverflowSetsIgnore(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("\x1b[")
	for i := 0; i < MaxParams+5; i++ {
		if i > 0 {
			sb.WriteByte(';')
		}
		sb.WriteByte('1')
	}
	sb.WriteByte('m')
	got := run(t, sb.String())
	if len(got) != 1 {
		t.Fatalf("got %v, want one dispatch", got)
	}
	if !strings.Contains(got[0], "IGN") {
		t.Errorf("overflowing dispatch = %s, want ignore flag", got[0])
	}
}

func TestCSIPrivateMarkerAfterParamIsIgnored(t *testing.T) {
	// The private marker is only legal before parameters; the whole sequence
	// is consumed without dispatch.
	expect(t, run(t, "\x1b[1?mX"), `print('X')`)
}

func TestCSIAbortedByCAN(t *testing.T) {
	expect(t, run(t, "\x1b[12\x18m"), `exec(18)`, `print('m')`)
}

func TestCSIInterruptedByESC(t *testing.T) {
	// A fresh ESC discards the sequence in progress.
	expect(t, run(t, "\x1b[12\x1b[3m"), `csi(3,,m)`)
}

func TestESCDispatch(t *testing.T) {
	expect(t, run(t, "\x1bM"), `esc(,M)`)
	expect(t, run(t, "\x1b(B"), `esc((,B)`)
	expect(t, run(t, "\x1b#8"), `esc(#,8)`)
}

func TestOSCBellTerminated(t *testing.T) {
	expect(t, run(t, "\x1b]0;title\x07"), `osc(0/title|bel)`)
}

func TestOSCSTTerminated(t *testing.T) {
	expect(t, run(t, "\x1b]0;title\x1b\\"), `osc(0/title|st)`)
}

func TestOSCEmptyPayload(t *testing.T) {
	expect(t, run(t, "\x1b]\x07"), `osc(|bel)`)
}

func TestOSCSplitAcrossWrites(t *testing.T) {
	var r recorder
	var p Parser
	for _, chunk := range []string{"\x1b]0;my", " ti", "tle\x07"} {
		p.Parse([]byte(chunk), &r)
	}
	expect(t, r.events, `osc(0/my title|bel)`)
}

func TestOSCUnicodePayload(t *testing.T) {
	// Agent titles carry spinner runes; the payload must survive byte-for-byte.
	expect(t, run(t, "\x1b]0;⠁ working\x07"), `osc(0/⠁ working|bel)`)
}

func TestOSCAbandonedByNonSTEscape(t *testing.T) {
	// ESC that is not followed by '\' abandons the string and starts a new
	// sequence, which is how a terminal recovers from a truncated payload.
	expect(t, run(t, "\x1b]0;broken\x1b[1m"), `csi(1,,m)`)
}

func TestOSCOverflowIsDropped(t *testing.T) {
	var r recorder
	p := Parser{MaxStringLen: 8}
	p.Parse([]byte("\x1b]0;"+strings.Repeat("x", 64)+"\x07"), &r)
	if len(r.events) != 0 {
		t.Errorf("got %v, want no dispatch for an over-long payload", r.events)
	}
	// The parser must still be usable afterwards.
	p.Parse([]byte("\x1b[1m"), &r)
	expect(t, r.events, `csi(1,,m)`)
}

func TestDCS(t *testing.T) {
	expect(t, run(t, "\x1bP1$rbody\x1b\\"), `hook(1,$,r)`, `unhook(body)`)
}

func TestDCSAbortedByCAN(t *testing.T) {
	expect(t, run(t, "\x1bPq abc\x18"), `hook(,,q)`, `unhook( abc)`, `exec(18)`)
}

func TestAPC(t *testing.T) {
	// kitty graphics uses APC; capturing it now keeps that door open.
	expect(t, run(t, "\x1b_Gf=100,a=T;payload\x1b\\"), `apc(Gf=100,a=T;payload)`)
}

func TestSOSAndPMAreConsumedSilently(t *testing.T) {
	expect(t, run(t, "\x1bXjunk\x1b\\A"), `print('A')`)
	expect(t, run(t, "\x1b^junk\x1b\\A"), `print('A')`)
}

func TestResetClearsPartialState(t *testing.T) {
	var r recorder
	var p Parser
	p.Parse([]byte("\x1b[12;"), &r)
	p.Reset()
	p.Parse([]byte("\x1b[3m"), &r)
	expect(t, r.events, `csi(3,,m)`)
}

func TestParamsAccessors(t *testing.T) {
	var r recorder
	var p Parser
	var got *Params
	h := &paramCapture{recorder: &r, out: &got}
	p.Parse([]byte("\x1b[;0;7m"), h)

	if got.Len() != 3 {
		t.Fatalf("Len = %d, want 3", got.Len())
	}
	if got.IsSet(0) {
		t.Error("param 0 should be unset")
	}
	if !got.IsSet(1) {
		t.Error("param 1 should be set")
	}
	// Both an omitted and an explicit zero take the default.
	if v := got.GetOr(0, 1); v != 1 {
		t.Errorf("GetOr(0,1) = %d, want 1", v)
	}
	if v := got.GetOr(1, 1); v != 1 {
		t.Errorf("GetOr(1,1) = %d, want 1", v)
	}
	if v := got.GetOr(2, 1); v != 7 {
		t.Errorf("GetOr(2,1) = %d, want 7", v)
	}
	// Out of range reads are safe.
	if v := got.Get(99); v != 0 {
		t.Errorf("Get(99) = %d, want 0", v)
	}
}

func TestParamsGroupEnd(t *testing.T) {
	var r recorder
	var p Parser
	var got *Params
	h := &paramCapture{recorder: &r, out: &got}
	p.Parse([]byte("\x1b[1;38:2::0:0:0;4m"), h)

	var groups [][]int32
	for i := 0; i < got.Len(); i = got.GroupEnd(i) {
		var g []int32
		for j := i; j < got.GroupEnd(i); j++ {
			g = append(g, got.Get(j))
		}
		groups = append(groups, g)
	}
	if len(groups) != 3 {
		t.Fatalf("got %d groups (%v), want 3", len(groups), groups)
	}
	if len(groups[1]) != 6 {
		t.Errorf("colour group = %v, want 6 entries", groups[1])
	}
	if groups[2][0] != 4 {
		t.Errorf("third group = %v, want [4]", groups[2])
	}
}

// paramCapture keeps the Params pointer alive for accessor assertions. This is
// only safe because nothing is parsed after the dispatch.
type paramCapture struct {
	*recorder
	out **Params
}

func (h *paramCapture) CSIDispatch(ps *Params, inter []byte, final byte, ignore bool) {
	*h.out = ps
	h.recorder.CSIDispatch(ps, inter, final, ignore)
}

// TestParseIsChunkIndependent feeds a mixed stream one byte at a time and
// asserts it produces exactly what a single write does. Splitting is the most
// likely source of state-machine bugs, since PTY reads land on arbitrary
// boundaries.
func TestParseIsChunkIndependent(t *testing.T) {
	input := "hello\x1b[1;31mred\x1b[0m\x1b]0;⠁ title\x07\x1b[?1049h" +
		"\x1bP1$rok\x1b\\\x1b_Gpayload\x1b\\⠁◐\r\n\x1b[38:2::1:2:3m"

	var whole recorder
	var p1 Parser
	p1.Parse([]byte(input), &whole)

	var split recorder
	var p2 Parser
	for i := 0; i < len(input); i++ {
		p2.Parse([]byte(input[i:i+1]), &split)
	}

	if len(whole.events) != len(split.events) {
		t.Fatalf("byte-at-a-time produced %d events, single write %d\n split: %v\n whole: %v",
			len(split.events), len(whole.events), split.events, whole.events)
	}
	for i := range whole.events {
		if whole.events[i] != split.events[i] {
			t.Errorf("event %d: split=%s whole=%s", i, split.events[i], whole.events[i])
		}
	}
}

func BenchmarkParsePlainText(b *testing.B) {
	data := []byte(strings.Repeat("the quick brown fox jumps over the lazy dog\r\n", 64))
	var h NopHandler
	var p Parser
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		p.Parse(data, h)
	}
}

func BenchmarkParseSGRHeavy(b *testing.B) {
	data := []byte(strings.Repeat("\x1b[1;38:2::200:100:50mx\x1b[0m", 128))
	var h NopHandler
	var p Parser
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		p.Parse(data, h)
	}
}

func BenchmarkParseOSCTitle(b *testing.B) {
	data := []byte(strings.Repeat("\x1b]0;⠁ agent working\x07", 128))
	var h NopHandler
	var p Parser
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		p.Parse(data, h)
	}
}
