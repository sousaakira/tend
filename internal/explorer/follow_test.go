package explorer

import (
	"strings"
	"testing"
)

// These are herdr-sidebar's own cases for its CwdFollower, ported with it.

// TestTheFollowerPrefersTheFocusedPaneThenTheLowestID: with no pane focused
// the choice is the lowest id, not whatever order the panes were listed in,
// and it is said once; a focused pane wins. If it regresses, the panel
// jumps between projects as the list reorders.
func TestTheFollowerPrefersTheFocusedPaneThenTheLowestID(t *testing.T) {
	var f follower
	unordered := []Sibling{{Pane: "p_9", Cwd: "/nine"}, {Pane: "p_1", Cwd: "/one"}}
	if got := f.next(unordered); got != "/one" {
		t.Errorf("first = %q, want /one", got)
	}
	if got := f.next(unordered); got != "" {
		t.Errorf("unchanged = %q, want nothing", got)
	}
	focused := []Sibling{{Pane: "p_9", Cwd: "/nine", Focused: true}, {Pane: "p_1", Cwd: "/one"}}
	if got := f.next(focused); got != "/nine" {
		t.Errorf("focused = %q, want /nine", got)
	}
	// Focus back on the panel: the last one followed holds.
	if got := f.next(unordered); got != "" {
		t.Errorf("with nothing focused the one followed holds, got %q", got)
	}
	moved := []Sibling{{Pane: "p_9", Cwd: "/nine/deeper"}, {Pane: "p_1", Cwd: "/one"}}
	if got := f.next(moved); got != "/nine/deeper" {
		t.Errorf("the followed pane moved: %q", got)
	}
}

// TestTheFollowerIgnoresPanesWithNoLiveDirectory: a pane whose program's
// directory cannot be read is not followed.
func TestTheFollowerIgnoresPanesWithNoLiveDirectory(t *testing.T) {
	var f follower
	if got := f.next([]Sibling{{Pane: "p_1"}}); got != "" {
		t.Errorf("got %q", got)
	}
}

// TestAManualFolderWinsUntilAnObservedPaneMoves is herdr-sidebar's rule for
// a folder the user chose: focus, reordering and a new pane do not take it
// away, a known pane moving does.
func TestAManualFolderWinsUntilAnObservedPaneMoves(t *testing.T) {
	var f follower
	f.next([]Sibling{{Pane: "p_1", Cwd: "/one"}, {Pane: "p_2", Cwd: "/two"}})
	f.manual = true
	focusOnly := []Sibling{{Pane: "p_4", Cwd: "/new"}, {Pane: "p_2", Cwd: "/two", Focused: true}, {Pane: "p_1", Cwd: "/one"}}
	if got := f.next(focusOnly); got != "" {
		t.Errorf("focus alone overrode a chosen folder: %q", got)
	}
	moved := []Sibling{{Pane: "p_1", Cwd: "/one"}, {Pane: "p_2", Cwd: "/two/next", Focused: true}}
	if got := f.next(moved); got != "/two/next" {
		t.Errorf("a known pane moved: %q", got)
	}
}

type fakeNeighbours struct{ siblings []Sibling }

func (f *fakeNeighbours) Siblings() ([]Sibling, error) { return f.siblings, nil }

// TestThePanelMovesToTheProjectThePaneBesideItMovesTo: a pane that cd's
// into another repository takes the panel with it; moving within the same
// repository changes nothing; and the pane the panel opened beside is the
// one followed even when another has a lower id. If it regresses, the panel
// keeps showing yesterday's project after the agent beside it moved on.
func TestThePanelMovesToTheProjectThePaneBesideItMovesTo(t *testing.T) {
	first, second := repo(t), repo(t)
	n := &fakeNeighbours{siblings: []Sibling{
		{Pane: "p_1", Cwd: second},         // lower id, another project
		{Pane: "p_2", Cwd: first + "/src"}, // where the panel was opened
	}}
	m := New(first, nil)
	m.FollowPanes(n)
	m.Follow()
	if m.tree.Root != first {
		t.Fatalf("the panel left the project it opened in for %s", m.tree.Root)
	}
	n.siblings[1].Cwd = second + "/src"
	m.Follow()
	if m.tree.Root != second {
		t.Fatalf("root = %s, want %s", m.tree.Root, second)
	}
	if !strings.Contains(m.message, "following") {
		t.Errorf("message = %q", m.message)
	}
	n.siblings[1].Cwd = second
	m.Follow()
	if m.tree.Root != second {
		t.Errorf("moving within the repository moved the panel to %s", m.tree.Root)
	}
}
