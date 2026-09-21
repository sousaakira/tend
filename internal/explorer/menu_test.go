package explorer

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func typeText(m *Model, text string) {
	for _, r := range text {
		m.Key(Key{Name: string(r), Rune: r})
	}
}

func selectNode(t *testing.T, m *Model, rel string) *Node {
	t.Helper()
	screen(m, 40, 16)
	for i, n := range m.fileRows {
		if n.Rel == rel {
			m.cursor[ViewFiles] = i
			return n
		}
	}
	t.Fatalf("%s is not listed", rel)
	return nil
}

func menuLabels(items []menuItem) string {
	var out []string
	for _, it := range items {
		out = append(out, it.label)
	}
	return strings.Join(out, "|")
}

// TestTheMenuOffersWhatTheTargetTakes is herdr-sidebar's menu_entries: a
// file can be opened with the system's app, anything in a repository
// staged, and empty space only takes creating and the folder actions.
func TestTheMenuOffersWhatTheTargetTakes(t *testing.T) {
	file, dir := &Node{Name: "a.go"}, &Node{Name: "src", Dir: true}
	if got := menuLabels(menuEntries(file, true)); !strings.Contains(got, "Open with default app|Stage changes|File history|Copy path") {
		t.Errorf("file: %s", got)
	}
	if got := menuLabels(menuEntries(dir, false)); strings.Contains(got, "Open with default app") || strings.Contains(got, "Stage") {
		t.Errorf("folder outside git: %s", got)
	}
	if got := menuLabels(menuEntries(nil, true)); got != "New file…|New folder…|Reveal in file manager|Change folder…" {
		t.Errorf("empty space: %s", got)
	}
}

func chooseMenu(t *testing.T, m *Model, label string) {
	t.Helper()
	for i, it := range m.menu.items {
		if it.label == label {
			m.menu.cursor = i
			keys(m, "enter")
			return
		}
	}
	t.Fatalf("no %q in %s", label, menuLabels(m.menu.items))
}

// TestTheMenuMakesRenamesAndDeletes: a new file lands in the folder
// chosen and is shown, a rename moves it, and a delete needs "yes" typed —
// a folder takes everything under it with it. If it regresses, the panel
// looks and cannot touch, or deletes on a stray key.
func TestTheMenuMakesRenamesAndDeletes(t *testing.T) {
	dir := repo(t)
	m := New(dir, nil)
	selectNode(t, m, "src")
	keys(m, "m")
	chooseMenu(t, m, "New file…")
	if text := screen(m, 40, 16); !strings.Contains(text, "new file in src") {
		t.Errorf("the question:\n%s", text)
	}
	typeText(m, "fresh.go")
	keys(m, "enter")
	if _, err := os.Stat(filepath.Join(dir, "src", "fresh.go")); err != nil {
		t.Fatalf("not made: %v (%q)", err, m.message)
	}
	if n := m.selectedNode(); n == nil || n.Rel != "src/fresh.go" {
		t.Errorf("the new file is shown and selected: %+v", n)
	}

	keys(m, "m")
	chooseMenu(t, m, "Rename…")
	keys(m, "ctrl+u")
	typeText(m, "renamed.go")
	keys(m, "enter")
	if _, err := os.Stat(filepath.Join(dir, "src", "renamed.go")); err != nil {
		t.Fatalf("not renamed: %v (%q)", err, m.message)
	}

	selectNode(t, m, "src")
	keys(m, "m")
	chooseMenu(t, m, "Delete")
	typeText(m, "no")
	keys(m, "enter")
	if _, err := os.Stat(filepath.Join(dir, "src")); err != nil {
		t.Fatal("deleted without a yes")
	}
	selectNode(t, m, "src")
	keys(m, "m")
	chooseMenu(t, m, "Delete")
	typeText(m, "yes")
	keys(m, "enter")
	if _, err := os.Stat(filepath.Join(dir, "src")); !os.IsNotExist(err) {
		t.Errorf("src should be gone: %v (%q)", err, m.message)
	}

	// A name, not a path.
	keys(m, "m")
	chooseMenu(t, m, "New folder…")
	typeText(m, "../escape")
	keys(m, "enter")
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape")); err == nil {
		t.Error("a path was taken for a name")
	}
}

// TestTheTreeStagesAFolderAndCopiesPaths: s stages everything under a
// folder; the copy entries send the path to the clipboard as OSC 52. If it
// regresses, staging from the tree means going to the changes one by one.
func TestTheTreeStagesAFolderAndCopiesPaths(t *testing.T) {
	dir := repo(t)
	write(t, dir, "src/more.go", "package src\n")
	m := New(dir, nil)
	selectNode(t, m, "src")
	keys(m, "s")
	staged := gitIn(t, dir, "diff", "--cached", "--name-only")
	if !strings.Contains(staged, "src/changed.go") || !strings.Contains(staged, "src/more.go") || strings.Contains(staged, "brandnew.md") {
		t.Errorf("staged: %q", staged)
	}

	var clip bytes.Buffer
	old := clipboardOut
	clipboardOut = &clip
	t.Cleanup(func() { clipboardOut = old })
	selectNode(t, m, "kept.txt")
	keys(m, "m")
	chooseMenu(t, m, "Copy relative path")
	if want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("kept.txt")) + "\x07"; clip.String() != want {
		t.Errorf("clipboard sequence %q, want %q", clip.String(), want)
	}
}

// TestChangingFolderByHandHoldsAgainstFollowing: a folder typed in is
// shown, and the pane beside the panel does not take it back until that
// pane moves. If it regresses, the folder chosen is gone two seconds later.
func TestChangingFolderByHandHoldsAgainstFollowing(t *testing.T) {
	first, second := repo(t), repo(t)
	n := &fakeNeighbours{siblings: []Sibling{{Pane: "p_2", Cwd: first}}}
	m := New(first, nil)
	m.FollowPanes(n)
	m.Follow()
	m.openMenu(nil)
	chooseMenu(t, m, "Change folder…")
	keys(m, "ctrl+u")
	typeText(m, second)
	keys(m, "enter")
	if m.tree.Root != second {
		t.Fatalf("root = %s", m.tree.Root)
	}
	m.Follow()
	if m.tree.Root != second {
		t.Errorf("following took the chosen folder away")
	}
	n.siblings[0].Cwd = first + "/src"
	m.Follow()
	if m.tree.Root != first {
		t.Errorf("the pane beside moved; the panel should follow again")
	}
}

// TestARightClickOpensTheMenuOnTheEntry: the mouse reaches the menu too.
func TestARightClickOpensTheMenuOnTheEntry(t *testing.T) {
	dir := repo(t)
	m := New(dir, nil)
	screen(m, 40, 16)
	for i, n := range m.fileRows {
		if n.Name == "kept.txt" {
			m.Mouse(Mouse{X: 4, Y: 2 + i, Button: 2, Press: true})
		}
	}
	if m.mode != modeMenu || m.menu.target == nil || m.menu.target.Name != "kept.txt" {
		t.Fatalf("mode %v target %+v", m.mode, m.menu.target)
	}
	if text := screen(m, 40, 16); !strings.Contains(text, "Rename…") {
		t.Errorf("the menu:\n%s", text)
	}
}
