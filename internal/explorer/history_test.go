package explorer

import (
	"strings"
	"testing"
)

// TestHistoryShowsCommitsAndOneFilesCommits: L lists the commits, enter
// shows one with its diff and q comes back to the list; a file's history
// lists only the commits that touched it, each shown as that file's diff.
// If it regresses, what an agent committed cannot be read from the panel.
func TestHistoryShowsCommitsAndOneFilesCommits(t *testing.T) {
	withIdentity(t)
	dir := repo(t)
	gitIn(t, dir, "add", "src/changed.go")
	gitIn(t, dir, "commit", "-q", "-m", "set x")
	write(t, dir, "kept.txt", "one\ntwo\n")
	gitIn(t, dir, "commit", "-q", "-am", "grow kept")

	m := New(dir, nil)
	keys(m, "L")
	text := screen(m, 44, 14)
	if !strings.Contains(text, "commits") || !strings.Contains(text, "grow kept") || !strings.Contains(text, "set x") || !strings.Contains(text, "first") {
		t.Fatalf("the log:\n%s", text)
	}
	keys(m, "j", "enter") // "set x"
	shown := strings.Join(m.viewer.lines, "\n")
	if !strings.Contains(screen(m, 44, 14), "set x") || !strings.Contains(shown, "+var x = 1") {
		t.Errorf("the commit shown:\n%s", shown)
	}
	keys(m, "q")
	if m.mode != modeHistory {
		t.Errorf("q goes back to the list, not to %v", m.mode)
	}
	keys(m, "esc")

	selectNode(t, m, "kept.txt")
	keys(m, "m")
	chooseMenu(t, m, "File history")
	text = screen(m, 44, 14)
	if !strings.Contains(text, "history of kept.txt") || !strings.Contains(text, "grow kept") || strings.Contains(text, "set x") {
		t.Errorf("kept.txt's history:\n%s", text)
	}
}

// TestStashesAreListedAppliedAndDroppedOnlyWithAYes: tab goes to the
// stashes; a applies one; d asks for yes before it throws one away. If it
// regresses, shelved work is out of reach, or lost to a key.
func TestStashesAreListedAppliedAndDroppedOnlyWithAYes(t *testing.T) {
	withIdentity(t)
	dir := repo(t)
	gitIn(t, dir, "stash", "push", "-q", "-m", "half done")
	m := New(dir, nil)
	keys(m, "L", "tab")
	text := screen(m, 44, 14)
	if !strings.Contains(text, "stash@{0}") || !strings.Contains(text, "half done") {
		t.Fatalf("stashes:\n%s", text)
	}
	keys(m, "d")
	typeText(m, "no")
	keys(m, "enter")
	if list := gitIn(t, dir, "stash", "list"); !strings.Contains(list, "half done") {
		t.Fatal("dropped without a yes")
	}
	keys(m, "L", "tab", "a")
	if diff := gitIn(t, dir, "diff", "--name-only"); !strings.Contains(diff, "src/changed.go") {
		t.Errorf("apply should bring the change back: %q (%q)", diff, m.message)
	}
	keys(m, "L", "tab", "d")
	typeText(m, "yes")
	keys(m, "enter")
	if list := gitIn(t, dir, "stash", "list"); strings.Contains(list, "half done") {
		t.Errorf("a yes drops it: %s", list)
	}
}

// TestTagsAreListed: the third kind, newest first.
func TestTagsAreListed(t *testing.T) {
	withIdentity(t)
	dir := repo(t)
	gitIn(t, dir, "tag", "-a", "v1.0", "-m", "the first release")
	m := New(dir, nil)
	keys(m, "L", "tab", "tab")
	if text := screen(m, 44, 14); !strings.Contains(text, "v1.0") || !strings.Contains(text, "the first release") {
		t.Errorf("tags:\n%s", text)
	}
}
