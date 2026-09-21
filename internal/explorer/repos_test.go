package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// projects is a folder that is not a repository, holding two that are and
// one plain directory: ~/src.
func projects(t *testing.T) (folder, api, web string) {
	t.Helper()
	folder = t.TempDir()
	api, web = filepath.Join(folder, "api"), filepath.Join(folder, "clients", "web")
	for _, dir := range []string{api, web} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "init", "-q", "-b", "main")
		write(t, dir, "readme.txt", "x\n")
		gitIn(t, dir, "add", "-A")
		gitIn(t, dir, "commit", "-q", "-m", "first")
	}
	write(t, folder, "notes/todo.txt", "plain\n")
	write(t, api, "readme.txt", "changed\n")
	write(t, web, "new.js", "x\n")
	return folder, api, web
}

// TestAFolderOfRepositoriesListsEachOnesChanges: opened on a folder of
// projects, the panel finds the repositories under it, lists each one's
// changes under its name and branch, marks them in the tree, and acts on
// the repository of the selection. If it regresses, the panel says "no
// repository" over a folder that has three.
func TestAFolderOfRepositoriesListsEachOnesChanges(t *testing.T) {
	withIdentity(t)
	folder, api, web := projects(t)
	m := New(folder, nil)
	if !m.multiRepo() || len(m.repos) != 2 {
		t.Fatalf("repos = %d", len(m.repos))
	}
	keys(m, "3")
	text := screen(m, 44, 16)
	for _, want := range []string{"▾ api ⎇ main", "▾ clients/web ⎇ main", "readme.txt", "new.js", "changes 2"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}

	// Stage and commit in web: the selection's repository, not api's.
	for i, r := range m.changeRows {
		if r.change.Path == "new.js" {
			m.cursor[ViewChanges] = i
		}
	}
	m.clamp()
	if m.git.Top != web {
		t.Fatalf("the active repository is %s, want web", m.git.Top)
	}
	keys(m, "s", "c")
	typeText(m, "add new.js")
	keys(m, "enter")
	if log := gitIn(t, web, "log", "--oneline", "-1"); !strings.Contains(log, "add new.js") {
		t.Errorf("web: %s (%q)", log, m.message)
	}
	if log := gitIn(t, api, "log", "--oneline", "-1"); strings.Contains(log, "add new.js") {
		t.Error("the commit went to api")
	}

	// The tree marks the repositories' changes where they are.
	keys(m, "1")
	text = screen(m, 44, 16)
	if !strings.Contains(text, "▸ api") || !strings.HasSuffix(lineWith(text, "▸ api"), "•") {
		t.Errorf("api holds a change:\n%s", text)
	}
	if strings.HasSuffix(lineWith(text, "▸ notes"), "•") {
		t.Errorf("notes is in no repository:\n%s", text)
	}
	if !strings.Contains(text, "/api") && !strings.Contains(text, filepath.Base(folder)) {
		t.Errorf("the bar names the folder:\n%s", text)
	}
}

func lineWith(text, part string) string {
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, part) {
			return strings.TrimRight(l, " ")
		}
	}
	return ""
}
