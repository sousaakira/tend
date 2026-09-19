package session

import (
	"testing"
)

// assertTiles proves the panes cover the area exactly: every cell belongs to
// one pane, none to two, none to none.
//
// This is checked cell by cell rather than by summing widths because the
// failure that matters is a one-cell seam produced by rounding, which a sum
// hides and a user sees immediately as a gap between panes.
func assertTiles(t *testing.T, rects []PaneRect, area Rect) {
	t.Helper()

	owner := make(map[[2]int]PaneID, area.W*area.H)
	for _, r := range rects {
		if r.Rect.W < 0 || r.Rect.H < 0 {
			t.Fatalf("pane %d has a negative size: %+v", r.Pane, r.Rect)
		}
		for y := r.Rect.Y; y < r.Rect.Y+r.Rect.H; y++ {
			for x := r.Rect.X; x < r.Rect.X+r.Rect.W; x++ {
				cell := [2]int{x, y}
				if prev, dup := owner[cell]; dup {
					t.Fatalf("cell (%d,%d) is covered by panes %d and %d", x, y, prev, r.Pane)
				}
				owner[cell] = r.Pane
			}
		}
	}

	for y := area.Y; y < area.Y+area.H; y++ {
		for x := area.X; x < area.X+area.W; x++ {
			if _, ok := owner[[2]int{x, y}]; !ok {
				t.Fatalf("cell (%d,%d) is covered by no pane", x, y)
			}
		}
	}
	if len(owner) != area.W*area.H {
		t.Fatalf("panes cover %d cells, the area has %d", len(owner), area.W*area.H)
	}
}

func TestLayoutSinglePaneFillsTheArea(t *testing.T) {
	s, _, tab, pane := fixture(t)
	_ = s
	area := Rect{X: 2, Y: 3, W: 40, H: 12}
	rects := tab.Layout(area)

	if len(rects) != 1 {
		t.Fatalf("got %d rects", len(rects))
	}
	if rects[0].Pane != pane.ID || rects[0].Rect != area {
		t.Errorf("rect = %+v, want the whole area %+v", rects[0], area)
	}
	assertTiles(t, rects, area)
}

func TestLayoutColumns(t *testing.T) {
	s, _, tab, a := fixture(t)
	if _, err := s.SplitPane(a.ID, Columns, PaneSpec{}); err != nil {
		t.Fatal(err)
	}
	area := Rect{W: 80, H: 24}
	rects := tab.Layout(area)

	if len(rects) != 2 {
		t.Fatalf("got %d rects", len(rects))
	}
	if rects[0].Rect.W != 40 || rects[1].Rect.W != 40 {
		t.Errorf("widths = %d,%d, want 40,40", rects[0].Rect.W, rects[1].Rect.W)
	}
	if rects[0].Rect.H != 24 || rects[1].Rect.H != 24 {
		t.Error("columns should keep the full height")
	}
	assertTiles(t, rects, area)
}

func TestLayoutRows(t *testing.T) {
	s, _, tab, a := fixture(t)
	if _, err := s.SplitPane(a.ID, Rows, PaneSpec{}); err != nil {
		t.Fatal(err)
	}
	area := Rect{W: 80, H: 24}
	rects := tab.Layout(area)

	if rects[0].Rect.H != 12 || rects[1].Rect.H != 12 {
		t.Errorf("heights = %d,%d, want 12,12", rects[0].Rect.H, rects[1].Rect.H)
	}
	if rects[0].Rect.Y != 0 || rects[1].Rect.Y != 12 {
		t.Errorf("tops = %d,%d, want 0,12", rects[0].Rect.Y, rects[1].Rect.Y)
	}
	assertTiles(t, rects, area)
}

// TestLayoutOddSizesLeaveNoSeam: an area that does not divide evenly must
// still be covered completely. The remainder goes to the last pane rather than
// being lost.
func TestLayoutOddSizesLeaveNoSeam(t *testing.T) {
	for _, width := range []int{1, 2, 3, 5, 7, 11, 13, 41, 99, 101} {
		s, _, tab, a := fixture(t)
		b, err := s.SplitPane(a.ID, Columns, PaneSpec{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.SplitPane(b.ID, Columns, PaneSpec{}); err != nil {
			t.Fatal(err)
		}

		area := Rect{W: width, H: 5}
		assertTiles(t, tab.Layout(area), area)
	}
}

func TestLayoutNested(t *testing.T) {
	s, _, tab, a := fixture(t)
	b, _ := s.SplitPane(a.ID, Columns, PaneSpec{})
	if _, err := s.SplitPane(b.ID, Rows, PaneSpec{}); err != nil {
		t.Fatal(err)
	}

	area := Rect{W: 100, H: 40}
	rects := tab.Layout(area)
	if len(rects) != 3 {
		t.Fatalf("got %d rects", len(rects))
	}
	assertTiles(t, rects, area)

	// The left column keeps the full height; the right one is split.
	if rects[0].Rect.H != 40 {
		t.Errorf("left pane height = %d, want 40", rects[0].Rect.H)
	}
	if rects[1].Rect.H != 20 || rects[2].Rect.H != 20 {
		t.Errorf("right heights = %d,%d, want 20,20", rects[1].Rect.H, rects[2].Rect.H)
	}
}

// TestLayoutTinyAreaStaysValid: a pane may end up with no cells, but never
// with a negative size, and the rects must still tile whatever area there is.
func TestLayoutTinyAreaStaysValid(t *testing.T) {
	s, _, tab, a := fixture(t)
	b, _ := s.SplitPane(a.ID, Columns, PaneSpec{})
	if _, err := s.SplitPane(b.ID, Columns, PaneSpec{}); err != nil {
		t.Fatal(err)
	}

	for _, area := range []Rect{{W: 0, H: 0}, {W: 1, H: 1}, {W: 2, H: 1}} {
		rects := tab.Layout(area)
		for _, r := range rects {
			if r.Rect.W < 0 || r.Rect.H < 0 {
				t.Fatalf("area %+v produced a negative rect %+v", area, r.Rect)
			}
		}
		assertTiles(t, rects, area)
	}
}

func TestLayoutPreservesOrder(t *testing.T) {
	s, _, tab, a := fixture(t)
	b, _ := s.SplitPane(a.ID, Columns, PaneSpec{})
	c, _ := s.SplitPane(b.ID, Columns, PaneSpec{})

	// Panes come back left to right, which is the order a and its splits sit in.
	want := []PaneID{a.ID, b.ID, c.ID}
	got := tab.Panes()
	if len(got) != len(want) {
		t.Fatalf("panes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("panes = %v, want %v", got, want)
			break
		}
	}

	rects := tab.Layout(Rect{W: 90, H: 10})
	for i := 1; i < len(rects); i++ {
		if rects[i].Rect.X < rects[i-1].Rect.X {
			t.Errorf("rect %d starts left of rect %d", i, i-1)
		}
	}
}

// --- neighbours ------------------------------------------------------------

func TestNeighborNoCandidate(t *testing.T) {
	rects := []PaneRect{{Pane: 1, Rect: Rect{W: 10, H: 10}}}
	for _, side := range []Side{Left, Right, Up, Down} {
		if _, ok := Neighbor(rects, 1, side); ok {
			t.Errorf("a lone pane has no neighbour %v", side)
		}
	}
}

func TestNeighborUnknownPane(t *testing.T) {
	rects := []PaneRect{{Pane: 1, Rect: Rect{W: 10, H: 10}}}
	if _, ok := Neighbor(rects, 99, Left); ok {
		t.Error("an unknown pane has no neighbour")
	}
}

// TestNeighborIgnoresNonOverlapping: a pane diagonally placed is not a
// neighbour, however near it is.
func TestNeighborIgnoresNonOverlapping(t *testing.T) {
	rects := []PaneRect{
		{Pane: 1, Rect: Rect{X: 0, Y: 0, W: 10, H: 10}},
		{Pane: 2, Rect: Rect{X: 10, Y: 10, W: 10, H: 10}}, // diagonal
	}
	if _, ok := Neighbor(rects, 1, Right); ok {
		t.Error("a diagonal pane is not a neighbour to the right")
	}
	if _, ok := Neighbor(rects, 1, Down); ok {
		t.Error("a diagonal pane is not a neighbour below")
	}
}

func TestNeighborPicksTheNearest(t *testing.T) {
	rects := []PaneRect{
		{Pane: 1, Rect: Rect{X: 0, Y: 0, W: 10, H: 10}},
		{Pane: 2, Rect: Rect{X: 10, Y: 0, W: 10, H: 10}}, // adjacent
		{Pane: 3, Rect: Rect{X: 20, Y: 0, W: 10, H: 10}}, // further
	}
	got, ok := Neighbor(rects, 1, Right)
	if !ok || got != 2 {
		t.Errorf("neighbour = %d (ok=%v), want the adjacent pane 2", got, ok)
	}
}

func TestNeighborPrefersTheLargerOverlap(t *testing.T) {
	// Two panes at the same distance to the right; the taller overlap wins.
	rects := []PaneRect{
		{Pane: 1, Rect: Rect{X: 0, Y: 0, W: 10, H: 10}},
		{Pane: 2, Rect: Rect{X: 10, Y: 8, W: 10, H: 4}}, // overlaps rows 8-9
		{Pane: 3, Rect: Rect{X: 10, Y: 0, W: 10, H: 8}}, // overlaps rows 0-7
	}
	got, ok := Neighbor(rects, 1, Right)
	if !ok || got != 3 {
		t.Errorf("neighbour = %d (ok=%v), want pane 3 with the larger overlap", got, ok)
	}
}

func TestNeighborAllSides(t *testing.T) {
	// A cross: centre pane with one neighbour on each side.
	rects := []PaneRect{
		{Pane: 1, Rect: Rect{X: 10, Y: 10, W: 10, H: 10}}, // centre
		{Pane: 2, Rect: Rect{X: 0, Y: 10, W: 10, H: 10}},  // left
		{Pane: 3, Rect: Rect{X: 20, Y: 10, W: 10, H: 10}}, // right
		{Pane: 4, Rect: Rect{X: 10, Y: 0, W: 10, H: 10}},  // above
		{Pane: 5, Rect: Rect{X: 10, Y: 20, W: 10, H: 10}}, // below
	}
	cases := map[Side]PaneID{Left: 2, Right: 3, Up: 4, Down: 5}
	for side, want := range cases {
		got, ok := Neighbor(rects, 1, side)
		if !ok || got != want {
			t.Errorf("neighbour %v = %d (ok=%v), want %d", side, got, ok, want)
		}
	}
}

// --- small types -----------------------------------------------------------

func TestRectEmpty(t *testing.T) {
	cases := map[Rect]bool{
		{W: 10, H: 10}: false,
		{}:             true,
		{W: 10}:        true,
		{H: 10}:        true,
		{W: -1, H: 10}: true,
	}
	for r, want := range cases {
		if got := r.Empty(); got != want {
			t.Errorf("%+v.Empty() = %v, want %v", r, got, want)
		}
	}
}

func TestStringers(t *testing.T) {
	if Columns.String() != "columns" || Rows.String() != "rows" {
		t.Error("direction names are wrong")
	}
	sides := map[Side]string{Left: "left", Right: "right", Up: "up", Down: "down"}
	for side, want := range sides {
		if got := side.String(); got != want {
			t.Errorf("Side %d = %q, want %q", side, got, want)
		}
	}
}

func TestSpan(t *testing.T) {
	cases := []struct {
		a, aLen, b, bLen, want int
	}{
		{0, 10, 0, 10, 10},   // identical
		{0, 10, 5, 10, 5},    // half
		{0, 10, 10, 10, 0},   // touching
		{0, 10, 20, 10, -10}, // apart
		{0, 10, 2, 3, 3},     // contained
	}
	for _, c := range cases {
		if got := span(c.a, c.aLen, c.b, c.bLen); got != c.want {
			t.Errorf("span(%d,%d,%d,%d) = %d, want %d",
				c.a, c.aLen, c.b, c.bLen, got, c.want)
		}
	}
}

func BenchmarkLayout(b *testing.B) {
	s := New()
	w := s.AddWorkspace("bench")
	_, root, err := s.AddTab(w.ID, "t", PaneSpec{})
	if err != nil {
		b.Fatal(err)
	}
	target := root.ID
	dirs := []Direction{Columns, Rows}
	for i := 0; i < 15; i++ {
		p, err := s.SplitPane(target, dirs[i%2], PaneSpec{})
		if err != nil {
			b.Fatal(err)
		}
		if i%3 == 0 {
			target = p.ID
		}
	}

	tab := s.ActiveTab()
	area := Rect{W: 200, H: 60}
	b.ReportAllocs()
	for b.Loop() {
		_ = tab.Layout(area)
	}
}
