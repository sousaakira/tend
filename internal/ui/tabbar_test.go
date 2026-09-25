package ui

import (
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// TestTabsAreBlocksWithTheBarBetween: each tab is a block of its name and
// four columns, eight at least, the name in its middle, with a column of the
// bar between two blocks and before the new-tab button, as herdr lays them
// out. If it regresses, the tabs run together into one band and nobody can
// tell where one ends.
func TestTabsAreBlocksWithTheBarBetween(t *testing.T) {
	f := Frame{Sidebar: true, Tabs: []Tab{{ID: 1, Name: "a", Active: true}, {ID: 2, Name: "kept.txt"}}}
	segs := TabSegments(f, 100)
	if len(segs) != 3 || !segs[2].New {
		t.Fatalf("two tabs and the button: %+v", segs)
	}
	if w := segs[0].End - segs[0].Start; w != minTabWidth {
		t.Errorf("a short name still makes an eight-column tab: %d", w)
	}
	if w := segs[1].End - segs[1].Start; w != len("kept.txt")+4 {
		t.Errorf("a name and four columns: %d", w)
	}
	if segs[1].Start != segs[0].End+1 || segs[2].Start != segs[1].End+1 {
		t.Errorf("a column between each: %+v", segs)
	}
	g := vt.NewGrid(100, 10, 0)
	Draw(g, f, DefaultTheme())
	row := []rune(gridText(g)[TabBarRow(f, 10)])
	if got := string(row[segs[1].Start:segs[1].End]); got != "  kept.txt  " {
		t.Errorf("the name in the middle of its block: %q", got)
	}
	if id, _, ok := TabAt(f, segs[0].End, TabBarRow(f, 10), 100, 10); ok {
		t.Errorf("the gap between two tabs is neither: %d", id)
	}
}
