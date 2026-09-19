package vt

import (
	"strings"
	"testing"
)

// assertSameScreen compares two screens cell by cell, including style and
// combining marks.
func assertSameScreen(t *testing.T, got, want *Screen) {
	t.Helper()

	gc, gr := got.Size()
	wc, wr := want.Size()
	if gc != wc || gr != wr {
		t.Fatalf("size = %dx%d, want %dx%d", gc, gr, wc, wr)
	}

	for y := 0; y < wr; y++ {
		gl, wl := got.Grid().Line(y), want.Grid().Line(y)
		for x := 0; x < wc; x++ {
			gcell, wcell := gl.Cell(x), wl.Cell(x)
			if gcell != wcell {
				t.Fatalf("cell (%d,%d) = %+v, want %+v\n got row: %q\nwant row: %q",
					x, y, gcell, wcell, gl.Text(), wl.Text())
			}
			if !sameRunes(gl.Combining(x), wl.Combining(x)) {
				t.Fatalf("combining marks at (%d,%d) = %v, want %v",
					x, y, gl.Combining(x), wl.Combining(x))
			}
		}
	}
}

func sameRunes(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRenderScreenRoundTrip is the property the wire depends on: what the
// server renders, a client's terminal reconstructs exactly.
//
// Everything about the encoder — writing trailing blanks instead of erasing,
// resetting before each style, emitting wide characters once — exists to make
// this hold, so it is checked against the kinds of content that break each of
// those choices.
func TestRenderScreenRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		cols  int
		rows  int
		input string
	}{
		{"empty", 20, 5, ""},
		{"plain text", 20, 3, "hello\r\nworld"},
		{"basic colours", 30, 3, "\x1b[31mred\x1b[0m \x1b[42mgreen bg\x1b[0m"},
		{"bright colours", 30, 2, "\x1b[91mbright\x1b[0m \x1b[102mon bright\x1b[0m"},
		{"256 colour", 30, 2, "\x1b[38;5;208morange\x1b[48;5;17m on navy\x1b[0m"},
		{"direct colour", 40, 2, "\x1b[38;2;10;20;30mrgb\x1b[48;2;200;100;50m bg\x1b[0m"},
		{"attributes", 40, 2, "\x1b[1mbold\x1b[3m italic\x1b[4m under\x1b[7m rev\x1b[0m"},
		{"underline styles", 40, 2, "\x1b[4:3mcurly\x1b[0m \x1b[4:2mdouble\x1b[0m \x1b[21mdbl\x1b[0m"},
		{"wide characters", 20, 3, "世界 wide\r\n世"},
		{"combining marks", 20, 2, "éx à"},
		{"trailing background", 20, 2, "\x1b[41m\x1b[2Kred line"},
		{"styled blanks mid row", 20, 2, "a\x1b[44m   \x1b[0mb"},
		{"braille spinner", 20, 2, "⠁ working⠉"},
		{"full width line", 8, 2, "abcdefgh"},
		{"scrolled content", 10, 3, "one\r\ntwo\r\nthree\r\nfour\r\nfive"},
		{"cursor moved", 20, 4, "abc\x1b[3;5Hdef"},
		{"hidden cursor", 20, 2, "x\x1b[?25l"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			origin := NewScreen(c.cols, c.rows, 100)
			origin.Write([]byte(c.input))

			replica := NewScreen(c.cols, c.rows, 100)
			replica.Write(RenderScreen(origin))

			assertSameScreen(t, replica, origin)

			if got, want := replica.Cursor(), origin.Cursor(); got.X != want.X || got.Y != want.Y {
				t.Errorf("cursor = (%d,%d), want (%d,%d)", got.X, got.Y, want.X, want.Y)
			}
			if got, want := replica.Modes().CursorVisible, origin.Modes().CursorVisible; got != want {
				t.Errorf("cursor visible = %v, want %v", got, want)
			}
		})
	}
}

// TestRenderScreenIsIdempotent: rendering a reconstruction must produce the
// same bytes, or the encoder is losing or inventing state on each hop.
func TestRenderScreenIsIdempotent(t *testing.T) {
	origin := NewScreen(24, 4, 50)
	origin.Write([]byte("\x1b[1;31mbold red\x1b[0m\r\nplain\r\n\x1b[44mblue bg"))

	first := RenderScreen(origin)

	replica := NewScreen(24, 4, 50)
	replica.Write(first)
	second := RenderScreen(replica)

	if string(first) != string(second) {
		t.Errorf("a re-render differs:\n first: %q\nsecond: %q", first, second)
	}
}

// TestRenderScreenDoesNotScroll: writing the last cell of the last row can
// push the screen up. The encoder must land on exactly the screen it started
// from, including its history.
func TestRenderScreenDoesNotScroll(t *testing.T) {
	origin := NewScreen(6, 3, 100)
	origin.Write([]byte("aaaaaa\r\nbbbbbb\r\ncccccc"))

	replica := NewScreen(6, 3, 100)
	replica.Write(RenderScreen(origin))

	assertSameScreen(t, replica, origin)
	if n := replica.MainGrid().HistoryLen(); n != 0 {
		t.Errorf("rendering scrolled %d lines into history", n)
	}
}

func TestRenderScreenUnchangedRunsCostNothing(t *testing.T) {
	// A screen of one style should emit one style sequence, not one per cell.
	screen := NewScreen(40, 2, 0)
	screen.Write([]byte("\x1b[31m" + strings.Repeat("x", 40)))

	out := string(RenderScreen(screen))
	if got := strings.Count(out, "\x1b[0;31m"); got != 1 {
		t.Errorf("emitted the red style %d times, want 1:\n%q", got, out)
	}
}

// --- painter ---------------------------------------------------------------

func TestPainterFirstFrameIsFull(t *testing.T) {
	g := NewGrid(10, 3, 0)
	fill(g.Line(0), "hello")

	p := NewPainter()
	out := p.Paint(g, 0, 0, true)

	replica := NewScreen(10, 3, 0)
	replica.Write(out)
	if got := replica.Grid().Line(0).Text(); got != "hello" {
		t.Errorf("row 0 = %q, want %q", got, "hello")
	}
}

// TestPainterSkipsUnchangedRows is why the painter keeps history: most frames
// change a handful of rows, and resending the rest is slow and flickery.
func TestPainterSkipsUnchangedRows(t *testing.T) {
	g := NewGrid(20, 10, 0)
	for y := 0; y < 10; y++ {
		fill(g.Line(y), "row content here")
	}

	p := NewPainter()
	firstFrame := p.Paint(g, 0, 0, true)

	// Nothing changed: the frame should carry no row content at all.
	idle := p.Paint(g, 0, 0, true)
	if len(idle) >= len(firstFrame) {
		t.Errorf("an unchanged frame is %d bytes against a full frame's %d",
			len(idle), len(firstFrame))
	}
	if strings.Contains(string(idle), "row content") {
		t.Errorf("an unchanged frame redrew a row: %q", idle)
	}

	// Change one row; only that row should appear.
	fill(g.Line(4), "changed")
	partial := p.Paint(g, 0, 0, true)
	if !strings.Contains(string(partial), "changed") {
		t.Error("the changed row was not drawn")
	}
	if strings.Contains(string(partial), "row content") {
		t.Errorf("an unchanged row was redrawn: %q", partial)
	}
}

// TestPainterConvergesOnTheGrid: whatever the painter chooses to send, the
// terminal must end up showing the grid. Skipping a row that actually changed
// would show up here and nowhere else.
func TestPainterConvergesOnTheGrid(t *testing.T) {
	g := NewGrid(16, 5, 0)
	terminal := NewScreen(16, 5, 0)
	p := NewPainter()

	frames := []func(){
		func() { fill(g.Line(0), "first frame") },
		func() { fill(g.Line(2), "second") },
		func() { g.Line(0).clearRange(0, 16, DefaultStyle) },
		func() { fill(g.Line(4), "last row") },
		func() {
			g.Line(2).SetCell(0, Cell{R: 'X', Style: Style{FG: IndexedColor(1)}, Width: 1})
		},
	}
	for i, change := range frames {
		change()
		terminal.Write(p.Paint(g, 0, 0, true))

		for y := 0; y < 5; y++ {
			want := g.Line(y).Text()
			got := terminal.Grid().Line(y).Text()
			if got != want {
				t.Fatalf("frame %d row %d = %q, want %q", i, y, got, want)
			}
		}
	}
}

func TestPainterInvalidateForcesAFullFrame(t *testing.T) {
	g := NewGrid(20, 4, 0)
	fill(g.Line(1), "content")

	p := NewPainter()
	p.Paint(g, 0, 0, true)
	if idle := p.Paint(g, 0, 0, true); strings.Contains(string(idle), "content") {
		t.Fatal("the second frame should have skipped the row")
	}

	p.Invalidate()
	if forced := p.Paint(g, 0, 0, true); !strings.Contains(string(forced), "content") {
		t.Error("Invalidate should force a full repaint")
	}
}

// TestPainterRepaintsFullyAfterAResize: a grid of a different shape shares no
// rows with the last frame, so none of them may be skipped.
func TestPainterRepaintsFullyAfterAResize(t *testing.T) {
	small := NewGrid(10, 3, 0)
	fill(small.Line(0), "abc")

	p := NewPainter()
	p.Paint(small, 0, 0, true)

	large := NewGrid(20, 6, 0)
	fill(large.Line(0), "abc")
	out := p.Paint(large, 0, 0, true)

	if !strings.Contains(string(out), "abc") {
		t.Error("a resized grid should repaint in full")
	}
}

func TestPainterHidesTheCursorWhileDrawing(t *testing.T) {
	g := NewGrid(10, 2, 0)
	out := string(NewPainter().Paint(g, 3, 1, true))

	if !strings.HasPrefix(out, "\x1b[?25l") {
		t.Errorf("a frame should start by hiding the cursor: %q", out)
	}
	if !strings.HasSuffix(out, "\x1b[?25h") {
		t.Errorf("a frame should end by showing it again: %q", out)
	}
}

func TestPainterLeavesTheCursorHidden(t *testing.T) {
	g := NewGrid(10, 2, 0)
	out := string(NewPainter().Paint(g, 0, 0, false))
	if strings.HasSuffix(out, "\x1b[?25h") {
		t.Error("the cursor should stay hidden when asked")
	}
}

func BenchmarkRenderScreen(b *testing.B) {
	s := NewScreen(120, 40, 0)
	s.Write([]byte(strings.Repeat("\x1b[1;32mstatus\x1b[0m some output line here\r\n", 40)))
	b.ReportAllocs()
	for b.Loop() {
		_ = RenderScreen(s)
	}
}

func BenchmarkPainterIdleFrame(b *testing.B) {
	g := NewGrid(200, 60, 0)
	for y := 0; y < 60; y++ {
		fill(g.Line(y), strings.Repeat("x", 120))
	}
	p := NewPainter()
	p.Paint(g, 0, 0, true)

	b.ReportAllocs()
	for b.Loop() {
		_ = p.Paint(g, 0, 0, true)
	}
}
