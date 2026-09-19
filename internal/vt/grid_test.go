package vt

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"
)

// TestCellSize pins the cell footprint. Cells are allocated per column × row ×
// scrollback × pane, so growth here is multiplicative and must be a decision,
// not an accident.
func TestCellSize(t *testing.T) {
	if got, max := unsafe.Sizeof(Cell{}), uintptr(24); got > max {
		t.Errorf("sizeof(Cell) = %d, want <= %d", got, max)
	}
}

func TestColorDefault(t *testing.T) {
	var c Color
	if !c.IsDefault() {
		t.Error("zero Color should be the default colour")
	}
	if c != DefaultColor {
		t.Error("zero Color should equal DefaultColor")
	}
}

func TestColorIndexed(t *testing.T) {
	for _, idx := range []uint8{0, 1, 15, 200, 255} {
		c := IndexedColor(idx)
		if !c.IsIndexed() {
			t.Fatalf("IndexedColor(%d) not indexed", idx)
		}
		if c.IsDefault() || c.IsRGB() {
			t.Errorf("IndexedColor(%d) has the wrong kind", idx)
		}
		if got := c.Index(); got != idx {
			t.Errorf("Index() = %d, want %d", got, idx)
		}
	}
}

func TestColorRGB(t *testing.T) {
	c := RGBColor(1, 2, 3)
	if !c.IsRGB() {
		t.Fatal("RGBColor is not RGB")
	}
	r, g, b := c.RGB()
	if r != 1 || g != 2 || b != 3 {
		t.Errorf("RGB() = %d,%d,%d, want 1,2,3", r, g, b)
	}
	// Black must stay distinguishable from the default colour.
	black := RGBColor(0, 0, 0)
	if black.IsDefault() {
		t.Error("RGB black must not read as the default colour")
	}
}

func TestColorAccessorsOnWrongKind(t *testing.T) {
	if got := RGBColor(9, 9, 9).Index(); got != 0 {
		t.Errorf("Index() on an RGB colour = %d, want 0", got)
	}
	r, g, b := IndexedColor(5).RGB()
	if r|g|b != 0 {
		t.Errorf("RGB() on an indexed colour = %d,%d,%d, want zeros", r, g, b)
	}
}

func TestStyleHas(t *testing.T) {
	s := Style{Attrs: AttrBold | AttrItalic}
	if !s.Has(AttrBold) || !s.Has(AttrItalic) {
		t.Error("Has should report set attributes")
	}
	if !s.Has(AttrBold | AttrItalic) {
		t.Error("Has should accept a combined mask")
	}
	if s.Has(AttrBold | AttrBlink) {
		t.Error("Has should require every bit in the mask")
	}
	if s.IsDefault() {
		t.Error("a styled Style is not the default")
	}
}

// fill writes s into the row starting at column 0.
func fill(r *Row, s string) {
	x := 0
	for _, ch := range s {
		if x >= r.Len() {
			break
		}
		r.SetCell(x, Cell{R: ch, Width: 1})
		x++
	}
}

func gridText(g *Grid) []string {
	out := make([]string, g.Rows())
	for y := 0; y < g.Rows(); y++ {
		out[y] = g.Line(y).Text()
	}
	return out
}

func historyText(g *Grid) []string {
	out := make([]string, g.HistoryLen())
	for i := 0; i < g.HistoryLen(); i++ {
		out[i] = g.HistoryLine(i).Text()
	}
	return out
}

func eq(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d lines %q, want %d %q", label, len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s line %d = %q, want %q", label, i, got[i], want[i])
		}
	}
}

func TestNewGridIsBlank(t *testing.T) {
	g := NewGrid(10, 3, 0)
	if g.Cols() != 10 || g.Rows() != 3 {
		t.Fatalf("dimensions = %dx%d, want 10x3", g.Cols(), g.Rows())
	}
	eq(t, "viewport", gridText(g), []string{"", "", ""})
	if g.HistoryLen() != 0 {
		t.Errorf("HistoryLen = %d, want 0", g.HistoryLen())
	}
}

func TestGridLineOutOfRange(t *testing.T) {
	g := NewGrid(4, 2, 0)
	if g.Line(-1) != nil || g.Line(2) != nil {
		t.Error("Line should return nil out of range")
	}
	if g.HistoryLine(0) != nil {
		t.Error("HistoryLine should return nil with no history")
	}
}

func TestRowText(t *testing.T) {
	g := NewGrid(10, 1, 0)
	r := g.Line(0)
	fill(r, "hi")
	if got := r.Text(); got != "hi" {
		t.Errorf("Text = %q, want %q (trailing blanks must be trimmed)", got, "hi")
	}
}

func TestRowTextSkipsWideContinuation(t *testing.T) {
	g := NewGrid(6, 1, 0)
	r := g.Line(0)
	r.SetCell(0, Cell{R: '世', Width: 2})
	r.SetCell(1, Cell{R: 0, Width: 0}) // continuation half
	r.SetCell(2, Cell{R: 'x', Width: 1})
	if got := r.Text(); got != "世x" {
		t.Errorf("Text = %q, want %q", got, "世x")
	}
}

func TestRowCombining(t *testing.T) {
	g := NewGrid(4, 1, 0)
	r := g.Line(0)
	r.SetCell(0, Cell{R: 'e', Width: 1})
	r.AddCombining(0, 0x0301) // combining acute
	if got := r.Text(); got != "é" {
		t.Errorf("Text = %q, want %q", got, "é")
	}
	// Overwriting a cell drops its marks.
	r.SetCell(0, Cell{R: 'a', Width: 1})
	if got := r.Text(); got != "a" {
		t.Errorf("Text after overwrite = %q, want %q", got, "a")
	}
}

func TestScrollUpNoHistory(t *testing.T) {
	g := NewGrid(8, 4, 0)
	for y, s := range []string{"a", "b", "c", "d"} {
		fill(g.Line(y), s)
	}
	g.ScrollUp(0, 3, 1, DefaultStyle, false)
	eq(t, "viewport", gridText(g), []string{"b", "c", "d", ""})
	if g.HistoryLen() != 0 {
		t.Errorf("HistoryLen = %d, want 0 when history is disabled", g.HistoryLen())
	}
}

func TestScrollUpToHistory(t *testing.T) {
	g := NewGrid(8, 3, 10)
	for y, s := range []string{"a", "b", "c"} {
		fill(g.Line(y), s)
	}
	g.ScrollUp(0, 2, 2, DefaultStyle, true)
	eq(t, "viewport", gridText(g), []string{"c", "", ""})
	eq(t, "history", historyText(g), []string{"a", "b"})
}

func TestScrollUpWholeRegion(t *testing.T) {
	g := NewGrid(8, 3, 10)
	for y, s := range []string{"a", "b", "c"} {
		fill(g.Line(y), s)
	}
	// Scrolling by more than the region height clamps and clears it.
	g.ScrollUp(0, 2, 99, DefaultStyle, true)
	eq(t, "viewport", gridText(g), []string{"", "", ""})
	eq(t, "history", historyText(g), []string{"a", "b", "c"})
}

func TestScrollUpInRegion(t *testing.T) {
	g := NewGrid(8, 5, 10)
	for y, s := range []string{"a", "b", "c", "d", "e"} {
		fill(g.Line(y), s)
	}
	// A region that does not start at row 0 never feeds history.
	g.ScrollUp(1, 3, 1, DefaultStyle, false)
	eq(t, "viewport", gridText(g), []string{"a", "c", "d", "", "e"})
	if g.HistoryLen() != 0 {
		t.Errorf("HistoryLen = %d, want 0 for a scroll inside a region", g.HistoryLen())
	}
}

func TestScrollDown(t *testing.T) {
	g := NewGrid(8, 4, 10)
	for y, s := range []string{"a", "b", "c", "d"} {
		fill(g.Line(y), s)
	}
	g.ScrollDown(0, 3, 1, DefaultStyle)
	eq(t, "viewport", gridText(g), []string{"", "a", "b", "c"})
	if g.HistoryLen() != 0 {
		t.Error("ScrollDown must never write to history")
	}
}

func TestScrollDownInRegion(t *testing.T) {
	g := NewGrid(8, 5, 0)
	for y, s := range []string{"a", "b", "c", "d", "e"} {
		fill(g.Line(y), s)
	}
	g.ScrollDown(1, 3, 1, DefaultStyle)
	eq(t, "viewport", gridText(g), []string{"a", "", "b", "c", "e"})
}

func TestScrollNoOp(t *testing.T) {
	g := NewGrid(8, 3, 5)
	fill(g.Line(0), "a")
	before := gridText(g)
	g.ScrollUp(0, 2, 0, DefaultStyle, true)
	g.ScrollUp(2, 1, 1, DefaultStyle, true) // inverted region
	g.ScrollDown(0, 2, -1, DefaultStyle)
	eq(t, "viewport", gridText(g), before)
}

func TestHistoryRingEvictsOldest(t *testing.T) {
	g := NewGrid(8, 1, 3)
	for i := 0; i < 6; i++ {
		fill(g.Line(0), fmt.Sprintf("l%d", i))
		g.ScrollUp(0, 0, 1, DefaultStyle, true)
	}
	if g.HistoryLen() != 3 {
		t.Fatalf("HistoryLen = %d, want 3", g.HistoryLen())
	}
	// The three most recent survive, oldest first.
	eq(t, "history", historyText(g), []string{"l3", "l4", "l5"})
}

// TestHistoryRingRecyclesStorage is the reason the ring exists: once full,
// scrolling forever must not allocate forever.
func TestHistoryRingRecyclesStorage(t *testing.T) {
	g := NewGrid(80, 24, 64)
	warm := func(n int) {
		for i := 0; i < n; i++ {
			g.ScrollUp(0, 23, 1, DefaultStyle, true)
		}
	}
	warm(200) // fill the ring past capacity
	allocs := testingAllocs(func() { warm(500) })
	if allocs != 0 {
		t.Errorf("scrolling a full ring allocated %v times per op, want 0", allocs)
	}
}

func testingAllocs(f func()) float64 {
	return testing.AllocsPerRun(20, f)
}

func TestClearHistory(t *testing.T) {
	g := NewGrid(8, 1, 4)
	fill(g.Line(0), "x")
	g.ScrollUp(0, 0, 1, DefaultStyle, true)
	if g.HistoryLen() != 1 {
		t.Fatal("expected one history line")
	}
	g.ClearHistory()
	if g.HistoryLen() != 0 {
		t.Errorf("HistoryLen after ClearHistory = %d, want 0", g.HistoryLen())
	}
	if g.HistoryLine(0) != nil {
		t.Error("HistoryLine should be nil after ClearHistory")
	}
}

func TestClearKeepsHistory(t *testing.T) {
	g := NewGrid(8, 2, 4)
	fill(g.Line(0), "old")
	g.ScrollUp(0, 1, 1, DefaultStyle, true)
	fill(g.Line(0), "live")
	g.Clear(DefaultStyle)
	eq(t, "viewport", gridText(g), []string{"", ""})
	eq(t, "history", historyText(g), []string{"old"})
}

func TestEraseKeepsStyle(t *testing.T) {
	// Erasing with a background colour must leave that colour behind, which is
	// how a full-width coloured bar is drawn.
	styled := Style{BG: IndexedColor(4)}
	g := NewGrid(4, 1, 0)
	fill(g.Line(0), "abcd")
	g.Clear(styled)
	for x := 0; x < 4; x++ {
		if got := g.Line(0).Cell(x).Style.BG; got != styled.BG {
			t.Fatalf("cell %d background = %v, want %v", x, got, styled.BG)
		}
	}
}

func TestResizeWider(t *testing.T) {
	g := NewGrid(4, 2, 0)
	fill(g.Line(0), "abcd")
	g.Resize(6, 2, DefaultStyle)
	if g.Cols() != 6 {
		t.Fatalf("Cols = %d, want 6", g.Cols())
	}
	if got := g.Line(0).Text(); got != "abcd" {
		t.Errorf("row = %q, want %q", got, "abcd")
	}
	if g.Line(0).Len() != 6 {
		t.Errorf("row length = %d, want 6", g.Line(0).Len())
	}
}

func TestResizeNarrowerTruncates(t *testing.T) {
	// Without reflow, narrowing drops the overflow. This is a known limitation,
	// pinned so that adding reflow later is a visible change.
	g := NewGrid(6, 1, 0)
	fill(g.Line(0), "abcdef")
	g.Resize(3, 1, DefaultStyle)
	if got := g.Line(0).Text(); got != "abc" {
		t.Errorf("row = %q, want %q", got, "abc")
	}
}

func TestResizeTallerAddsBlankRowsAtBottom(t *testing.T) {
	g := NewGrid(4, 2, 0)
	fill(g.Line(0), "a")
	fill(g.Line(1), "b")
	g.Resize(4, 4, DefaultStyle)
	eq(t, "viewport", gridText(g), []string{"a", "b", "", ""})
}

func TestResizeShorterDropsTrailingBlanksFirst(t *testing.T) {
	// A mostly empty screen should lose its blank tail, not its content.
	g := NewGrid(4, 4, 10)
	fill(g.Line(0), "a")
	fill(g.Line(1), "b")
	g.Resize(4, 2, DefaultStyle)
	eq(t, "viewport", gridText(g), []string{"a", "b"})
	if g.HistoryLen() != 0 {
		t.Errorf("HistoryLen = %d, want 0 — blank rows should not enter history", g.HistoryLen())
	}
}

func TestResizeShorterKeepsBottomAndScrollsTopToHistory(t *testing.T) {
	// A full screen must keep its bottom, where the prompt lives.
	g := NewGrid(4, 4, 10)
	for y, s := range []string{"a", "b", "c", "d"} {
		fill(g.Line(y), s)
	}
	g.Resize(4, 2, DefaultStyle)
	eq(t, "viewport", gridText(g), []string{"c", "d"})
	eq(t, "history", historyText(g), []string{"a", "b"})
}

func TestResizeIsNoOpForSameSize(t *testing.T) {
	g := NewGrid(4, 2, 4)
	fill(g.Line(0), "ab")
	g.Resize(4, 2, DefaultStyle)
	eq(t, "viewport", gridText(g), []string{"ab", ""})
}

func TestResizeClampsToAtLeastOne(t *testing.T) {
	g := NewGrid(4, 4, 0)
	g.Resize(0, 0, DefaultStyle)
	if g.Cols() != 1 || g.Rows() != 1 {
		t.Errorf("dimensions = %dx%d, want 1x1", g.Cols(), g.Rows())
	}
}

func TestResizeAlsoResizesHistory(t *testing.T) {
	g := NewGrid(6, 1, 4)
	fill(g.Line(0), "abcdef")
	g.ScrollUp(0, 0, 1, DefaultStyle, true)
	g.Resize(10, 1, DefaultStyle)
	if got := g.HistoryLine(0).Len(); got != 10 {
		t.Errorf("history row length = %d, want 10", got)
	}
}

func TestWrappedFlag(t *testing.T) {
	g := NewGrid(4, 2, 0)
	r := g.Line(0)
	if r.Wrapped() {
		t.Error("a fresh row is not wrapped")
	}
	r.SetWrapped(true)
	if !r.Wrapped() {
		t.Error("SetWrapped did not take")
	}
	// Recycling a row must clear it, or reflow would later join unrelated lines.
	g.ScrollUp(0, 1, 1, DefaultStyle, false)
	if g.Line(1).Wrapped() {
		t.Error("a recycled row must not stay wrapped")
	}
}

func BenchmarkScrollUpWithHistory(b *testing.B) {
	g := NewGrid(200, 50, 1000)
	for i := 0; i < 1100; i++ {
		g.ScrollUp(0, 49, 1, DefaultStyle, true)
	}
	b.ReportAllocs()
	for b.Loop() {
		g.ScrollUp(0, 49, 1, DefaultStyle, true)
	}
}

func BenchmarkRowText(b *testing.B) {
	g := NewGrid(200, 1, 0)
	fill(g.Line(0), strings.Repeat("x", 120))
	r := g.Line(0)
	b.ReportAllocs()
	for b.Loop() {
		_ = r.Text()
	}
}
