package explorer

import (
	"strings"
	"testing"
)

// TestAPanelsStateCrossesToTheNewBuild: the folders open, the selection and
// the view survive being carried through the environment into a new
// panel; a panel in the middle of typing does not hand over. If it
// regresses, an install closes every folder the user had open, or a commit
// message half typed is lost to it.
func TestAPanelsStateCrossesToTheNewBuild(t *testing.T) {
	dir := repo(t)
	m := New(dir, nil)
	screen(m, 40, 16)
	for i, n := range m.fileRows {
		if n.Name == "src" {
			m.cursor[ViewFiles] = i
			m.tree.Toggle(n, m.git)
		}
	}
	m.layout()
	for i, n := range m.fileRows {
		if n.Name == "changed.go" {
			m.cursor[ViewFiles] = i
		}
	}
	m.switchView(ViewChanges)
	raw := EncodeState(m.State())

	s, ok := DecodeState(raw)
	if !ok {
		t.Fatalf("decoding %s", raw)
	}
	n := New(s.Root, nil)
	n.Restore(s)
	if n.view != ViewChanges {
		t.Errorf("the view: %d", n.view)
	}
	n.switchView(ViewFiles)
	text := screen(n, 40, 16)
	if !strings.Contains(text, "▾ src") || !strings.Contains(text, "changed.go") {
		t.Errorf("src stays open:\n%s", text)
	}
	if sel := n.selectedNode(); sel == nil || sel.Name != "changed.go" {
		t.Errorf("the selection: %+v", sel)
	}

	calls := 0
	n.WatchBinary(func() bool { calls++; return true })
	n.mode = modeCommit
	if n.upgradeDue() || calls != 0 {
		t.Error("no hand-over while a commit message is being typed")
	}
	n.mode = modeList
	if !n.upgradeDue() {
		t.Error("a hand-over when the panel is only showing")
	}
	if _, ok := DecodeState(""); ok {
		t.Error("no state, nothing to restore")
	}
}
