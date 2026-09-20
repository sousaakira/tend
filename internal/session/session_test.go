package session

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sousaakira/tend/internal/detect"
)

// check asserts the session's shape. Every test calls it after mutating,
// because the failures this structure invites are quiet: a stale index, a
// focus on a closed pane, split shares that no longer sum to one.
func check(t *testing.T, s *Session) {
	t.Helper()
	if err := s.CheckInvariants(); err != nil {
		t.Fatalf("invariants: %v", err)
	}
}

// fixture returns a session with one workspace and one tab holding one pane.
func fixture(t *testing.T) (*Session, *Workspace, *Tab, *Pane) {
	t.Helper()
	s := New()
	w := s.AddWorkspace("main")
	tab, pane, err := s.AddTab(w.ID, "shell", PaneSpec{Command: []string{"sh"}})
	if err != nil {
		t.Fatalf("AddTab: %v", err)
	}
	check(t, s)
	return s, w, tab, pane
}

func TestNewSessionIsEmpty(t *testing.T) {
	s := New()
	check(t, s)
	if s.ActiveWorkspace() != nil {
		t.Error("a new session has no workspace")
	}
	if s.ActiveTab() != nil || s.ActivePane() != nil {
		t.Error("a new session has no tab or pane")
	}
	if s.PaneCount() != 0 {
		t.Errorf("PaneCount = %d, want 0", s.PaneCount())
	}
}

func TestAddWorkspaceFocusesIt(t *testing.T) {
	s := New()
	a := s.AddWorkspace("one")
	check(t, s)
	if s.ActiveWorkspace() != a {
		t.Error("adding a workspace should focus it")
	}
	b := s.AddWorkspace("two")
	check(t, s)
	if s.ActiveWorkspace() != b {
		t.Error("the newest workspace should be focused")
	}
	if len(s.Workspaces()) != 2 {
		t.Errorf("got %d workspaces", len(s.Workspaces()))
	}
}

func TestAddTabCreatesOnePane(t *testing.T) {
	s, _, tab, pane := fixture(t)
	if got := tab.Panes(); len(got) != 1 || got[0] != pane.ID {
		t.Errorf("tab panes = %v, want [%d]", got, pane.ID)
	}
	if tab.ActivePane() != pane.ID {
		t.Errorf("active pane = %d, want %d", tab.ActivePane(), pane.ID)
	}
	if s.ActivePane() != pane {
		t.Error("the new pane should be focused")
	}
}

func TestAddTabRejectsUnknownWorkspace(t *testing.T) {
	s := New()
	_, _, err := s.AddTab(WorkspaceID(99), "x", PaneSpec{})
	if !errors.Is(err, ErrNoSuchWorkspace) {
		t.Errorf("err = %v, want ErrNoSuchWorkspace", err)
	}
	check(t, s)
}

// TestIdentifiersAreNeverReused: a stale id must fail to resolve rather than
// silently addressing whatever took its place.
func TestIdentifiersAreNeverReused(t *testing.T) {
	s, _, _, first := fixture(t)
	if err := s.ClosePane(first.ID); err != nil {
		t.Fatalf("ClosePane: %v", err)
	}
	check(t, s)

	w := s.ActiveWorkspace()
	_, second, err := s.AddTab(w.ID, "again", PaneSpec{})
	if err != nil {
		t.Fatal(err)
	}
	check(t, s)
	if second.ID == first.ID {
		t.Errorf("pane id %d was reused", second.ID)
	}
	if _, ok := s.Pane(first.ID); ok {
		t.Error("the closed pane should no longer resolve")
	}
}

// --- splitting -------------------------------------------------------------

func TestSplitPane(t *testing.T) {
	s, _, tab, first := fixture(t)

	second, err := s.SplitPane(first.ID, Columns, PaneSpec{})
	if err != nil {
		t.Fatalf("SplitPane: %v", err)
	}
	check(t, s)

	if got := tab.Panes(); len(got) != 2 {
		t.Fatalf("panes = %v, want 2", got)
	}
	if tab.ActivePane() != second.ID {
		t.Error("splitting should focus the new pane")
	}
	if got, _ := s.TabOf(second.ID); got != tab {
		t.Error("the new pane should belong to the same tab")
	}
}

// TestSplitTwiceSameDirectionIsFlat: splitting the same way twice should give
// three panes in a row, not nested halves. Nesting would make the third pane a
// quarter of the screen instead of a third, and deepen the tree for nothing.
func TestSplitTwiceSameDirectionIsFlat(t *testing.T) {
	s, _, tab, first := fixture(t)

	second, _ := s.SplitPane(first.ID, Columns, PaneSpec{})
	check(t, s)
	_, err := s.SplitPane(second.ID, Columns, PaneSpec{})
	if err != nil {
		t.Fatal(err)
	}
	check(t, s)

	if tab.root.isLeaf() || len(tab.root.kids) != 3 {
		t.Fatalf("root has %d children, want a flat split of 3", len(tab.root.kids))
	}

	rects := tab.Layout(Rect{W: 90, H: 30})
	if len(rects) != 3 {
		t.Fatalf("got %d rects", len(rects))
	}
	// The first pane kept half, the other two split the rest.
	widths := []int{rects[0].Rect.W, rects[1].Rect.W, rects[2].Rect.W}
	if widths[0] != 45 {
		t.Errorf("widths = %v, want the original pane to keep half", widths)
	}
	if widths[0]+widths[1]+widths[2] != 90 {
		t.Errorf("widths = %v, want them to sum to 90", widths)
	}
}

func TestSplitMixedDirectionsNests(t *testing.T) {
	s, _, tab, first := fixture(t)
	second, _ := s.SplitPane(first.ID, Columns, PaneSpec{})
	check(t, s)
	_, err := s.SplitPane(second.ID, Rows, PaneSpec{})
	if err != nil {
		t.Fatal(err)
	}
	check(t, s)

	if len(tab.root.kids) != 2 {
		t.Fatalf("root has %d children, want 2", len(tab.root.kids))
	}
	if tab.root.dir != Columns {
		t.Errorf("root direction = %v, want columns", tab.root.dir)
	}
	if tab.root.kids[1].isLeaf() {
		t.Error("the second child should be a nested row split")
	}
}

func TestSplitRejectsUnknownPane(t *testing.T) {
	s, _, _, _ := fixture(t)
	before := s.PaneCount()
	_, err := s.SplitPane(PaneID(999), Columns, PaneSpec{})
	if !errors.Is(err, ErrNoSuchPane) {
		t.Errorf("err = %v, want ErrNoSuchPane", err)
	}
	if s.PaneCount() != before {
		t.Error("a failed split must not leave a pane behind")
	}
	check(t, s)
}

// --- closing ---------------------------------------------------------------

func TestClosePaneCollapsesTheSplit(t *testing.T) {
	s, _, tab, first := fixture(t)
	second, _ := s.SplitPane(first.ID, Columns, PaneSpec{})
	check(t, s)

	if err := s.ClosePane(second.ID); err != nil {
		t.Fatalf("ClosePane: %v", err)
	}
	check(t, s)

	if !tab.root.isLeaf() {
		t.Error("a split with one pane left should collapse to that pane")
	}
	if tab.ActivePane() != first.ID {
		t.Errorf("active pane = %d, want %d", tab.ActivePane(), first.ID)
	}
}

// TestClosePaneKeepsSiblingProportions: closing a pane should hand its space
// to its siblings in proportion, not reshuffle everyone.
func TestClosePaneKeepsSiblingProportions(t *testing.T) {
	s, _, tab, a := fixture(t)
	b, _ := s.SplitPane(a.ID, Columns, PaneSpec{})
	c, _ := s.SplitPane(b.ID, Columns, PaneSpec{})
	check(t, s)
	// a=1/2, b=1/4, c=1/4

	if err := s.ClosePane(c.ID); err != nil {
		t.Fatal(err)
	}
	check(t, s)

	rects := tab.Layout(Rect{W: 120, H: 30})
	if len(rects) != 2 {
		t.Fatalf("got %d rects", len(rects))
	}
	// a and b kept their 2:1 ratio.
	if rects[0].Rect.W != 80 || rects[1].Rect.W != 40 {
		t.Errorf("widths = %d,%d, want 80,40 (the 2:1 ratio preserved)",
			rects[0].Rect.W, rects[1].Rect.W)
	}
}

func TestCloseLastPaneClosesTheTab(t *testing.T) {
	s, w, tab, pane := fixture(t)
	if err := s.ClosePane(pane.ID); err != nil {
		t.Fatal(err)
	}
	check(t, s)

	if len(w.Tabs()) != 0 {
		t.Errorf("workspace has %d tabs, want 0", len(w.Tabs()))
	}
	if _, ok := s.Tab(tab.ID); ok {
		t.Error("the tab should be gone")
	}
	// The workspace outlives its contents: the user named and arranged it.
	if s.ActiveWorkspace() != w {
		t.Error("the workspace should remain")
	}
}

func TestClosePaneRefocuses(t *testing.T) {
	s, _, tab, a := fixture(t)
	b, _ := s.SplitPane(a.ID, Columns, PaneSpec{})
	check(t, s)
	if tab.ActivePane() != b.ID {
		t.Fatal("expected the new pane to be focused")
	}
	if err := s.ClosePane(b.ID); err != nil {
		t.Fatal(err)
	}
	check(t, s)
	if tab.ActivePane() != a.ID {
		t.Errorf("active pane = %d, want %d", tab.ActivePane(), a.ID)
	}
}

func TestCloseTabClosesItsPanes(t *testing.T) {
	s, _, tab, a := fixture(t)
	b, _ := s.SplitPane(a.ID, Rows, PaneSpec{})
	check(t, s)

	closed, err := s.CloseTab(tab.ID)
	if err != nil {
		t.Fatal(err)
	}
	check(t, s)

	if len(closed) != 2 {
		t.Errorf("CloseTab reported %v, want both panes", closed)
	}
	for _, id := range []PaneID{a.ID, b.ID} {
		if _, ok := s.Pane(id); ok {
			t.Errorf("pane %d should be gone", id)
		}
	}
	if s.PaneCount() != 0 {
		t.Errorf("PaneCount = %d, want 0", s.PaneCount())
	}
}

func TestClosePaneRejectsUnknown(t *testing.T) {
	s, _, _, _ := fixture(t)
	if err := s.ClosePane(PaneID(999)); !errors.Is(err, ErrNoSuchPane) {
		t.Errorf("err = %v, want ErrNoSuchPane", err)
	}
	check(t, s)
}

// --- focus -----------------------------------------------------------------

func TestFocusPaneFocusesItsTabAndWorkspace(t *testing.T) {
	s := New()
	w1 := s.AddWorkspace("one")
	_, p1, _ := s.AddTab(w1.ID, "a", PaneSpec{})
	w2 := s.AddWorkspace("two")
	_, p2, _ := s.AddTab(w2.ID, "b", PaneSpec{})
	check(t, s)

	if s.ActivePane() != p2 {
		t.Fatal("expected the second workspace to be focused")
	}
	if err := s.FocusPane(p1.ID); err != nil {
		t.Fatal(err)
	}
	check(t, s)
	if s.ActiveWorkspace() != w1 {
		t.Error("focusing a pane should focus its workspace")
	}
	if s.ActivePane() != p1 {
		t.Error("the pane should be active")
	}
}

func TestMoveFocus(t *testing.T) {
	s, _, tab, left := fixture(t)
	right, _ := s.SplitPane(left.ID, Columns, PaneSpec{})
	check(t, s)

	area := Rect{W: 80, H: 24}
	if tab.ActivePane() != right.ID {
		t.Fatal("expected the new pane to be focused")
	}
	if !s.MoveFocus(Left, area) {
		t.Fatal("moving left should find the original pane")
	}
	if tab.ActivePane() != left.ID {
		t.Errorf("active = %d, want %d", tab.ActivePane(), left.ID)
	}
	if !s.MoveFocus(Right, area) {
		t.Fatal("moving right should find the other pane")
	}
	if tab.ActivePane() != right.ID {
		t.Errorf("active = %d, want %d", tab.ActivePane(), right.ID)
	}
	// There is nothing beyond the edge.
	if s.MoveFocus(Right, area) {
		t.Error("moving past the edge should not move focus")
	}
	check(t, s)
}

// TestMoveFocusPrefersTheOverlappingNeighbour: with a tall pane beside two
// stacked ones, moving right must land on the one actually beside the cursor.
func TestMoveFocusPrefersTheOverlappingNeighbour(t *testing.T) {
	s, _, tab, left := fixture(t)
	topRight, _ := s.SplitPane(left.ID, Columns, PaneSpec{})
	bottomRight, _ := s.SplitPane(topRight.ID, Rows, PaneSpec{})
	check(t, s)

	area := Rect{W: 80, H: 24}
	rects := tab.Layout(area)
	if len(rects) != 3 {
		t.Fatalf("got %d rects", len(rects))
	}

	// From the left pane, whose full height overlaps both, the nearer edge
	// wins; both are equally near, so the larger overlap decides. They are
	// equal too, so this pins the first in layout order — the top one.
	if err := s.FocusPane(left.ID); err != nil {
		t.Fatal(err)
	}
	if !s.MoveFocus(Right, area) {
		t.Fatal("expected a neighbour to the right")
	}
	if got := tab.ActivePane(); got != topRight.ID {
		t.Errorf("active = %d, want the top-right pane %d", got, topRight.ID)
	}

	// From the bottom-right pane, left must reach the tall pane.
	if err := s.FocusPane(bottomRight.ID); err != nil {
		t.Fatal(err)
	}
	if !s.MoveFocus(Left, area) {
		t.Fatal("expected a neighbour to the left")
	}
	if got := tab.ActivePane(); got != left.ID {
		t.Errorf("active = %d, want %d", got, left.ID)
	}
	check(t, s)
}

func TestMoveFocusWithNoTab(t *testing.T) {
	s := New()
	if s.MoveFocus(Left, Rect{W: 10, H: 10}) {
		t.Error("an empty session has nothing to focus")
	}
}

// --- pane state ------------------------------------------------------------

func TestSetPaneState(t *testing.T) {
	s, _, _, p := fixture(t)
	if p.State != detect.StateUnknown {
		t.Errorf("a new pane starts at %v, want unknown", p.State)
	}
	if err := s.SetPaneState(p.ID, "claude", detect.StateWorking); err != nil {
		t.Fatal(err)
	}
	check(t, s)

	got, _ := s.Pane(p.ID)
	if got.Agent != "claude" || got.State != detect.StateWorking {
		t.Errorf("pane = %+v, want claude/working", got)
	}
	if err := s.SetPaneState(PaneID(999), "x", detect.StateIdle); !errors.Is(err, ErrNoSuchPane) {
		t.Errorf("err = %v, want ErrNoSuchPane", err)
	}
}

// --- invariants ------------------------------------------------------------

// TestInvariantsCatchADriftedIndex proves the check earns its place: a
// hand-corrupted index must be reported, not tolerated.
func TestInvariantsCatchADriftedIndex(t *testing.T) {
	s, _, tab, p := fixture(t)

	delete(s.index, p.ID)
	if err := s.CheckInvariants(); err == nil {
		t.Error("a missing index entry should be caught")
	}

	s.index[p.ID] = tab
	if err := s.CheckInvariants(); err != nil {
		t.Fatalf("restoring the index should fix it: %v", err)
	}

	s.index[PaneID(999)] = tab
	if err := s.CheckInvariants(); err == nil {
		t.Error("an index entry for a pane in no tab should be caught")
	}
}

func TestInvariantsCatchBadFocus(t *testing.T) {
	s, _, tab, _ := fixture(t)
	tab.active = PaneID(999)
	if err := s.CheckInvariants(); err == nil {
		t.Error("focus on a pane the tab does not hold should be caught")
	}
}

func TestInvariantsCatchBadSplitSizes(t *testing.T) {
	s, _, tab, a := fixture(t)
	if _, err := s.SplitPane(a.ID, Columns, PaneSpec{}); err != nil {
		t.Fatal(err)
	}
	check(t, s)

	tab.root.sizes[0] = 0.9 // no longer sums to 1
	if err := s.CheckInvariants(); err == nil {
		t.Error("split sizes that do not sum to 1 should be caught")
	}
}

// TestInvariantsSurviveHeavyEditing runs a long sequence of splits and closes
// and checks the shape after every step. Layout trees fail by drifting, not by
// crashing, so the value is in the volume of steps rather than any one of them.
func TestInvariantsSurviveHeavyEditing(t *testing.T) {
	s := New()
	w := s.AddWorkspace("main")
	_, root, err := s.AddTab(w.ID, "work", PaneSpec{})
	if err != nil {
		t.Fatal(err)
	}
	check(t, s)

	live := []PaneID{root.ID}
	dirs := []Direction{Columns, Rows, Rows, Columns}

	for i := 0; i < 40; i++ {
		target := live[i%len(live)]
		p, err := s.SplitPane(target, dirs[i%len(dirs)], PaneSpec{})
		if err != nil {
			t.Fatalf("split %d: %v", i, err)
		}
		live = append(live, p.ID)
		check(t, s)

		// Close one every third step, keeping at least one alive.
		if i%3 == 2 && len(live) > 2 {
			victim := live[1]
			if err := s.ClosePane(victim); err != nil {
				t.Fatalf("close %d: %v", i, err)
			}
			live = append(live[:1], live[2:]...)
			check(t, s)
		}
	}

	// The geometry must still tile the area exactly.
	tab := s.ActiveTab()
	rects := tab.Layout(Rect{W: 200, H: 60})
	if len(rects) != len(tab.Panes()) {
		t.Fatalf("laid out %d rects for %d panes", len(rects), len(tab.Panes()))
	}
	assertTiles(t, rects, Rect{W: 200, H: 60})
}

// --- adjusting dividers ----------------------------------------------------

func TestAdjustSplitMovesTheDivider(t *testing.T) {
	s, _, tab, left := fixture(t)
	if _, err := s.SplitPane(left.ID, Columns, PaneSpec{}); err != nil {
		t.Fatal(err)
	}
	check(t, s)

	area := Rect{W: 100, H: 20}
	if got := tab.Layout(area); got[0].Rect.W != 50 {
		t.Fatalf("started at %d columns, want 50", got[0].Rect.W)
	}

	// Growing the left pane rightwards takes from its neighbour.
	if err := s.AdjustSplit(left.ID, Right, 10, area); err != nil {
		t.Fatal(err)
	}
	check(t, s)

	rects := tab.Layout(area)
	if rects[0].Rect.W != 60 || rects[1].Rect.W != 40 {
		t.Errorf("widths = %d,%d, want 60,40", rects[0].Rect.W, rects[1].Rect.W)
	}
	// Whatever moves, the panes must still tile exactly.
	assertTiles(t, rects, area)
}

func TestAdjustSplitShrinks(t *testing.T) {
	s, _, tab, left := fixture(t)
	right, _ := s.SplitPane(left.ID, Columns, PaneSpec{})
	check(t, s)

	area := Rect{W: 100, H: 20}
	// The right pane growing leftwards is the same divider, moved the other way.
	if err := s.AdjustSplit(right.ID, Left, 20, area); err != nil {
		t.Fatal(err)
	}
	check(t, s)

	rects := tab.Layout(area)
	if rects[0].Rect.W != 30 || rects[1].Rect.W != 70 {
		t.Errorf("widths = %d,%d, want 30,70", rects[0].Rect.W, rects[1].Rect.W)
	}
	assertTiles(t, rects, area)
}

// TestAdjustSplitWalksOutwards: the divider that moves belongs to the nearest
// enclosing split with something on that side, so the keys mean "this edge of
// this pane" rather than "some divider above it".
func TestAdjustSplitWalksOutwards(t *testing.T) {
	s, _, tab, left := fixture(t)
	right, _ := s.SplitPane(left.ID, Columns, PaneSpec{})
	bottomRight, _ := s.SplitPane(right.ID, Rows, PaneSpec{})
	check(t, s)

	area := Rect{W: 100, H: 20}
	// The bottom-right pane has no pane to its left inside its own row split,
	// so moving left has to reach the column split one level out.
	if err := s.AdjustSplit(bottomRight.ID, Left, 10, area); err != nil {
		t.Fatal(err)
	}
	check(t, s)

	rects := tab.Layout(area)
	if rects[0].Rect.W != 40 {
		t.Errorf("the left pane is %d columns, want 40", rects[0].Rect.W)
	}
	assertTiles(t, rects, area)
}

// TestAdjustSplitCannotSqueezeAPaneAway: a pane that can be dragged to nothing
// is a pane that can be lost by accident.
func TestAdjustSplitCannotSqueezeAPaneAway(t *testing.T) {
	s, _, tab, left := fixture(t)
	if _, err := s.SplitPane(left.ID, Columns, PaneSpec{}); err != nil {
		t.Fatal(err)
	}
	area := Rect{W: 100, H: 20}

	for i := 0; i < 20; i++ {
		if err := s.AdjustSplit(left.ID, Right, 20, area); err != nil {
			t.Fatal(err)
		}
		check(t, s)
	}
	rects := tab.Layout(area)
	if rects[1].Rect.W < 1 {
		t.Errorf("the neighbour was squeezed to %d columns", rects[1].Rect.W)
	}
	assertTiles(t, rects, area)
}

// TestAdjustSplitAtTheEdgeDoesNothing: a key press at the edge of the screen
// should do nothing rather than fail.
func TestAdjustSplitAtTheEdgeDoesNothing(t *testing.T) {
	s, _, tab, only := fixture(t)
	area := Rect{W: 80, H: 20}

	if err := s.AdjustSplit(only.ID, Left, 10, area); err != nil {
		t.Errorf("a lone pane should be a no-op, not an error: %v", err)
	}
	check(t, s)
	if got := tab.Layout(area); got[0].Rect.W != 80 {
		t.Errorf("width = %d, want the whole area", got[0].Rect.W)
	}
}

func TestAdjustSplitRejectsAnUnknownPane(t *testing.T) {
	s, _, _, _ := fixture(t)
	if err := s.AdjustSplit(PaneID(999), Left, 5, Rect{W: 80, H: 20}); !errors.Is(err, ErrNoSuchPane) {
		t.Errorf("err = %v, want ErrNoSuchPane", err)
	}
}

// TestCloseWorkspace removes a space and everything in it.
func TestCloseWorkspace(t *testing.T) {
	s := New()
	keep := s.AddWorkspace("keep")
	drop := s.AddWorkspace("drop")
	_, first, err := s.AddTab(drop.ID, "a", PaneSpec{})
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := s.AddTab(drop.ID, "b", PaneSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddTab(keep.ID, "c", PaneSpec{}); err != nil {
		t.Fatal(err)
	}

	closed, err := s.CloseWorkspace(drop.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(closed) != 2 {
		t.Errorf("closed %v, want both panes", closed)
	}
	for _, id := range []PaneID{first.ID, second.ID} {
		if _, ok := s.Pane(id); ok {
			t.Errorf("pane %d should be gone from the index", id)
		}
	}
	if len(s.Workspaces()) != 1 || s.Workspaces()[0].ID != keep.ID {
		t.Errorf("workspaces = %v, want only the kept one", s.Workspaces())
	}
	if s.ActiveWorkspace() == nil {
		t.Error("something should be active while a workspace remains")
	}
	if err := s.CheckInvariants(); err != nil {
		t.Error(err)
	}

	if _, err := s.CloseWorkspace(drop.ID); err == nil {
		t.Error("closing it twice should fail")
	}

	// The last one can go too: a session with none is a real state, and the
	// client makes one when it finds nothing.
	if _, err := s.CloseWorkspace(keep.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.Workspaces()) != 0 || s.ActiveWorkspace() != nil {
		t.Error("the session should be empty")
	}
	if err := s.CheckInvariants(); err != nil {
		t.Error(err)
	}
}

// TestGroupWorkspace: a group is only the set of workspaces naming it, so
// creating one is naming it and the last member leaving removes it.
func TestGroupWorkspace(t *testing.T) {
	s := New()
	a := s.AddWorkspace("a")
	b := s.AddWorkspace("b")

	if a.Group != "" {
		t.Error("a workspace should start in no group")
	}
	for _, w := range []*Workspace{a, b} {
		if err := s.GroupWorkspace(w.ID, "clients"); err != nil {
			t.Fatal(err)
		}
	}
	if a.Group != "clients" || b.Group != "clients" {
		t.Errorf("groups = %q %q", a.Group, b.Group)
	}

	// Leaving is naming nothing.
	if err := s.GroupWorkspace(a.ID, ""); err != nil {
		t.Fatal(err)
	}
	if a.Group != "" {
		t.Errorf("a.Group = %q, want none", a.Group)
	}

	if err := s.GroupWorkspace(WorkspaceID(999), "x"); err == nil {
		t.Error("grouping a workspace that does not exist should fail")
	}
	if err := s.CheckInvariants(); err != nil {
		t.Error(err)
	}
}

// TestAheadBehind: the arrows only mean anything against a branch that
// follows another one.
func TestAheadBehind(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	clone := filepath.Join(dir, "clone")

	git := func(at string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = at
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	git(dir, "init", "-q", origin)
	git(origin, "commit", "-q", "--allow-empty", "-m", "one")
	git(dir, "clone", "-q", origin, clone)

	// In step with its upstream: nothing to report.
	if got := AheadBehind(clone); !got.Empty() {
		t.Errorf("a fresh clone = %+v, want nothing", got)
	}

	git(clone, "commit", "-q", "--allow-empty", "-m", "two")
	git(clone, "commit", "-q", "--allow-empty", "-m", "three")
	if got := AheadBehind(clone); got.Ahead != 2 || got.Behind != 0 {
		t.Errorf("two commits ahead = %+v", got)
	}

	// A branch with no upstream has nothing to be ahead or behind of, which
	// is not an error.
	git(clone, "checkout", "-q", "-b", "solo")
	if got := AheadBehind(clone); !got.Empty() {
		t.Errorf("a branch with no upstream = %+v, want nothing", got)
	}

	// Neither is a directory that is not a repository at all.
	if got := AheadBehind(t.TempDir()); !got.Empty() {
		t.Errorf("not a repository = %+v", got)
	}
	if got := AheadBehind(""); !got.Empty() {
		t.Errorf("no directory = %+v", got)
	}
}

// buildBusySession is a session with some of everything in it: two spaces, one
// in a group, tabs, and a tab divided both ways.
func buildBusySession(t *testing.T) *Session {
	t.Helper()
	s := New()
	a := s.AddWorkspaceIn("alpha", "/work/alpha")
	_, first, err := s.AddTab(a.ID, "main", PaneSpec{Command: []string{"zsh"}, Dir: "/work/alpha"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.SplitPane(first.ID, Columns, PaneSpec{Command: []string{"zsh"}, Dir: "/work/alpha/src"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SplitPane(second.ID, Rows, PaneSpec{Command: []string{"claude"}, Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddTab(a.ID, "logs", PaneSpec{Command: []string{"tail", "-f", "x"}}); err != nil {
		t.Fatal(err)
	}

	b := s.AddWorkspaceIn("beta", "/work/beta")
	if err := s.GroupWorkspace(b.ID, "clients"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddTab(b.ID, "tab 1", PaneSpec{Command: []string{"zsh"}}); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestSnapshotRoundTrip: what is written down has to come back as the same
// session, or a restart quietly rearranges somebody's work.
func TestSnapshotRoundTrip(t *testing.T) {
	s := buildBusySession(t)
	before := s.Snapshot(nil)

	restored, err := Restore(before)
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
	after := restored.Snapshot(nil)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("the session changed across a save and a restore:\nbefore %+v\nafter  %+v", before, after)
	}

	// The shape of a divided tab survives, not only its membership.
	tab := restored.Workspaces()[0].Tabs()[0]
	if got := len(tab.Panes()); got != 3 {
		t.Errorf("first tab has %d panes, want 3", got)
	}
	if restored.Workspaces()[1].Group != "clients" {
		t.Error("the group should survive")
	}
}

// TestSnapshotRecordsWhereAPaneIsNow: somebody who has spent an hour three
// directories down wants to come back there, not to where the shell opened.
func TestSnapshotRecordsWhereAPaneIsNow(t *testing.T) {
	s := buildBusySession(t)
	first := s.Workspaces()[0].Tabs()[0].Panes()[0]

	snap := s.Snapshot(map[PaneID]string{first: "/work/alpha/deep/down"})
	if got := snap.Workspaces[0].Tabs[0].Panes[0].Dir; got != "/work/alpha/deep/down" {
		t.Errorf("dir = %q, want where the pane is now", got)
	}
	// A pane nobody could ask keeps the directory it started in.
	if got := snap.Workspaces[0].Tabs[0].Panes[1].Dir; got != "/work/alpha/src" {
		t.Errorf("dir = %q, want where it started", got)
	}
}

// TestRestoreHandsOutFreshIdentifiers: an identifier repeated after a restore
// would point a client's stale reference at somebody else's pane.
func TestRestoreHandsOutFreshIdentifiers(t *testing.T) {
	snap := buildBusySession(t).Snapshot(nil)
	// Counters lower than what is in the file, as an edited or older file
	// might have.
	snap.NextPane, snap.NextTab, snap.NextWorkspace = 0, 0, 0

	s, err := Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[PaneID]bool)
	for _, w := range s.Workspaces() {
		for _, tab := range w.Tabs() {
			for _, id := range tab.Panes() {
				seen[id] = true
			}
		}
	}
	_, fresh, err := s.AddTab(s.Workspaces()[0].ID, "new", PaneSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if seen[fresh.ID] {
		t.Errorf("pane %d was handed out twice", fresh.ID)
	}
}

// TestRestoreRefusesWhatItCannotTrust: a file is outside input. A session that
// violates its own invariants fails later, somewhere unrelated, in a way that
// points nowhere near the file.
func TestRestoreRefusesWhatItCannotTrust(t *testing.T) {
	good := func() Snapshot { return buildBusySession(t).Snapshot(nil) }

	newer := good()
	newer.Version = SnapshotVersion + 1
	if _, err := Restore(newer); !errors.Is(err, ErrSnapshotVersion) {
		t.Errorf("a newer file = %v, want ErrSnapshotVersion", err)
	}

	ghost := good()
	ghost.Workspaces[0].Tabs[0].Layout.Kids[0].Pane = 9999
	if _, err := Restore(ghost); err == nil {
		t.Error("a layout naming a pane the tab does not have should be refused")
	}

	lopsided := good()
	lopsided.Workspaces[0].Tabs[0].Layout.Sizes = []float64{1}
	if _, err := Restore(lopsided); err == nil {
		t.Error("sizes that do not match the children should be refused")
	}

	twice := good()
	dup := twice.Workspaces[0].Tabs[0].Panes[0]
	twice.Workspaces[1].Tabs[0].Panes = append(twice.Workspaces[1].Tabs[0].Panes, dup)
	if _, err := Restore(twice); err == nil {
		t.Error("the same pane in two tabs should be refused")
	}

	// An empty session is a real one and restores as itself.
	empty, err := Restore(New().Snapshot(nil))
	if err != nil || len(empty.Workspaces()) != 0 {
		t.Errorf("an empty session = %v, %v", empty, err)
	}
}
