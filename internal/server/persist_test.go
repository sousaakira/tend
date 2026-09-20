package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/session"
)

func persistentServer(t *testing.T, stateFile string) *Server {
	t.Helper()
	s, err := New(Config{
		StateFile:      stateFile,
		DetectInterval: 10 * time.Millisecond,
		DefaultSize:    pty.Size{Cols: 80, Rows: 24},
		ShutdownGrace:  time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// paneIDs lists every pane in a session, in layout order.
func paneIDs(s *Server) []session.PaneID {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []session.PaneID
	for _, w := range s.session.Workspaces() {
		for _, tab := range w.Tabs() {
			out = append(out, tab.Panes()...)
		}
	}
	return out
}

// TestServerComesBackToTheSameSession is the reason for all of this: stopping
// a server used to take every space, tab and split with it, and an empty
// screen was what came back.
func TestServerComesBackToTheSameSession(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state", "work.json")
	deep := filepath.Join(t.TempDir(), "deep", "down")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	first := persistentServer(t, state)
	ws, err := first.NewWorkspaceIn("alpha", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := first.GroupWorkspace(ws, "clients"); err != nil {
		t.Fatal(err)
	}
	_, left, err := first.NewTab(ws, "main", PaneSpec{Command: []string{"/bin/sh"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.SplitPane(left, session.Columns, PaneSpec{Command: []string{"/bin/sh"}}); err != nil {
		t.Fatal(err)
	}

	// Say something, and go somewhere: both have to be there afterwards.
	if err := first.Write(left, []byte("echo SAID-BEFORE; cd "+deep+"\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the output", func() bool {
		text, _ := first.ScreenText(left)
		return strings.Contains(text, "SAID-BEFORE")
	})
	waitFor(t, "the shell to move", func() bool {
		first.mu.Lock()
		rt := first.runtimes[left]
		first.mu.Unlock()
		return rt != nil && rt.pty.Cwd() == deep
	})
	before := paneIDs(first)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := persistentServer(t, state)
	after := paneIDs(second)
	if len(after) != 2 || after[0] != before[0] || after[1] != before[1] {
		t.Fatalf("panes = %v, want %v back in the same order", after, before)
	}

	second.mu.Lock()
	w := second.session.Workspaces()[0]
	name, group, tabs := w.Name, w.Group, len(w.Tabs())
	pane, _ := second.session.Pane(left)
	second.mu.Unlock()
	if name != "alpha" || group != "clients" || tabs != 1 {
		t.Errorf("space = %q in %q with %d tabs", name, group, tabs)
	}
	// Where the work was, not where the shell was opened.
	if pane == nil || pane.Dir != deep {
		t.Errorf("pane dir = %v, want %s", pane, deep)
	}

	// What was said is above the new prompt.
	waitFor(t, "the past to be there", func() bool {
		text, _ := second.ScreenText(left)
		return strings.Contains(text, "SAID-BEFORE")
	})
	// And the pane is alive, in the directory it was left in.
	if err := second.Write(left, []byte("echo NOW-IN:$(pwd)\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the new shell to answer", func() bool {
		text, _ := second.ScreenText(left)
		return strings.Contains(text, "NOW-IN:"+deep)
	})
}

// TestClosingEverythingForgetsTheSession: a session the user closed on purpose
// must not come back.
func TestClosingEverythingForgetsTheSession(t *testing.T) {
	state := filepath.Join(t.TempDir(), "work.json")
	s := persistentServer(t, state)
	ws, _ := s.NewWorkspace("alpha")
	if _, _, err := s.NewTab(ws, "main", PaneSpec{Command: []string{"/bin/sh"}}); err != nil {
		t.Fatal(err)
	}
	s.saveStructure()
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("the session should have been written: %v", err)
	}

	if err := s.CloseWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	s.saveStructure()
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Errorf("an emptied session should leave no file, stat = %v", err)
	}
}

// TestDamagedStateIsIgnoredNotFatal: refusing to start over a bad file turns a
// damaged file into a server nobody can run.
func TestDamagedStateIsIgnoredNotFatal(t *testing.T) {
	state := filepath.Join(t.TempDir(), "work.json")
	for name, body := range map[string]string{
		"truncated":  `{"version":1,"workspaces":[{"id":1,"tabs":[{"id":1,"lay`,
		"newer":      `{"version":99,"workspaces":[]}`,
		"ghost pane": `{"version":1,"workspaces":[{"id":1,"tabs":[{"id":1,"active":5,"layout":{"pane":5},"panes":[{"id":4,"command":["/bin/sh"]}]}]}]}`,
		"not json":   `hello`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(state, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			s := persistentServer(t, state)
			if n := len(paneIDs(s)); n != 0 {
				t.Errorf("a file that cannot be trusted restored %d panes", n)
			}
			// And the server is usable.
			ws, err := s.NewWorkspace("fresh")
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.NewTab(ws, "t", PaneSpec{Command: []string{"/bin/sh"}}); err != nil {
				t.Errorf("the server should still work: %v", err)
			}
		})
	}
}

// TestRestoreSurvivesAMissingDirectory: a deleted worktree or an unmounted disk
// is no reason to lose the pane.
func TestRestoreSurvivesAMissingDirectory(t *testing.T) {
	state := filepath.Join(t.TempDir(), "work.json")
	gone := filepath.Join(t.TempDir(), "was-here")
	body := `{"version":1,"workspaces":[{"id":1,"name":"a","tabs":[{"id":1,"active":1,` +
		`"layout":{"pane":1},"panes":[{"id":1,"command":["/bin/sh"],"dir":"` + gone + `"}]}]}],` +
		`"next_pane":1,"next_tab":1,"next_workspace":1}`
	if err := os.WriteFile(state, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	s := persistentServer(t, state)
	ids := paneIDs(s)
	if len(ids) != 1 {
		t.Fatalf("panes = %v, want the one pane back", ids)
	}
	if err := s.Write(ids[0], []byte("echo ALIVE\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pane to be alive", func() bool {
		text, _ := s.ScreenText(ids[0])
		return strings.Contains(text, "ALIVE")
	})
}

// TestStateFilesArePrivate: they hold what was on the user's terminals.
func TestStateFilesArePrivate(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state", "work.json")
	s := persistentServer(t, state)
	ws, _ := s.NewWorkspace("alpha")
	_, pane, err := s.NewTab(ws, "main", PaneSpec{Command: []string{"/bin/sh"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Write(pane, []byte("echo a-secret\n"))
	waitFor(t, "output", func() bool {
		text, _ := s.ScreenText(pane)
		return strings.Contains(text, "a-secret")
	})
	s.saveStructure()
	s.saveHistory()

	for _, path := range []string{state, historyFile(state)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s is %o, want 600", filepath.Base(path), perm)
		}
	}
	if info, _ := os.Stat(filepath.Dir(state)); info.Mode().Perm() != 0o700 {
		t.Errorf("the state directory is %o, want 700", info.Mode().Perm())
	}
}
