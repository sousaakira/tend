//go:build unix

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/vt"
)

// gitProject is a repository with a committed file, a changed one and a new
// one, which is enough for every part of the panel to have something to say.
func gitProject(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	write := func(name, text string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("kept.txt", "one\n")
	write("src/changed.go", "package src\n")
	run("add", "-A")
	run("commit", "-q", "-m", "first")
	write("src/changed.go", "package src\n\nvar x = 1\n")
	write("brandnew.md", "# new\n")
	return dir
}

// TestTheFilesPanelDocksOnTheLeftAndShowsTheProject: prefix+f opens the
// explorer along the left of the tab, in the directory the pane is in, with
// git's news beside each file; pressed in the panel it closes it again. If
// it regresses, the key that shows the project shows nothing, or a panel
// that cannot be put away.
func TestTheFilesPanelDocksOnTheLeftAndShowsTheProject(t *testing.T) {
	project := gitProject(t)
	// The test's server runs in this process and names no binary for its
	// panes (TEND_BIN_PATH), so the panel finds tend on the PATH: this build.
	t.Setenv("PATH", filepath.Dir(buildBinary(t))+string(os.PathListSeparator)+os.Getenv("PATH"))
	a := startSession(t, 120, 30)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "cd "+project+" && echo in-project\n", "the shell in the project",
		func(s string) bool {
			return strings.Contains(s, "\nin-project") || strings.Contains(s, "│in-project")
		})

	a.send(t, "\x02f")
	a.waitForScreen(t, "the panel", func(s string) bool {
		return strings.Contains(s, "files │ search │ changes") && strings.Contains(s, "brandnew.md") && strings.Contains(s, "kept.txt")
	})
	// Docked on the left: the panel's header is left of the shell's pane.
	header := a.lineContaining(t, "files │ search │ changes")
	line := []rune(a.lines()[header-1])
	at := strings.Index(string(line), "files │ search │ changes")
	if at < 0 || at > 60 {
		t.Errorf("the panel should be on the left of the tab:\n%s", a.text())
	}
	// The branch, and the letters git gives the new file and the changed
	// directory.
	if !strings.Contains(a.text(), "⎇ main") {
		t.Errorf("the branch should be shown:\n%s", a.text())
	}
	if !strings.Contains(a.lines()[a.lineContaining(t, "brandnew.md")-1], "U") {
		t.Errorf("a new file is marked U:\n%s", a.text())
	}

	// A click reaches the panel through the client, which forwards the
	// mouse to a pane that asked for it: one on a folder opens it.
	row := a.lineContaining(t, "▸ src")
	col := len([]rune(a.lines()[row-1][:strings.Index(a.lines()[row-1], "▸ src")]))
	a.clickAt(t, col+3, row)
	a.waitForScreen(t, "src opened by a click", func(s string) bool { return strings.Contains(s, "changed.go") })

	// The changes view lists what changed.
	a.send(t, "3")
	a.waitForScreen(t, "the changes", func(s string) bool {
		return strings.Contains(s, "CHANGES 2") && strings.Contains(s, "changed.go")
	})

	// prefix+f in the panel puts it away.
	a.send(t, "\x02f")
	a.waitForScreen(t, "the panel to close", func(s string) bool { return !strings.Contains(s, "files │ search │ changes") })
}

// TestAFileChosenInThePanelOpensInTheEditorInATab is the panel in a real
// session — the server a client starts, its automation socket, the tend it
// names to its panes: a file found with / and enter opens in $EDITOR in a
// tab of its own. If it regresses, the panel shows files that cannot be
// opened from it.
func TestAFileChosenInThePanelOpensInTheEditorInATab(t *testing.T) {
	project := gitProject(t)
	if err := os.WriteFile(filepath.Join(project, "note.txt"), []byte("hello-from-note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runtimeDir, err := os.MkdirTemp("", "tf")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runtimeDir) })
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(),
		"TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh",
		"TEND_CONFIG="+filepath.Join(t.TempDir(), "absent.toml"),
		// An editor that shows the file and stays, so the tab is there to
		// be seen.
		`EDITOR=sh -c 'cat "$0"; sleep 60'`, "VISUAL=",
	)

	bin := buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", "panel"}, pty.Options{
		Size: pty.Size{Cols: 120, Rows: 30},
		Env:  env,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(120, 30, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stopSession(t, "panel")
	})
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "cd "+project+" && echo in-project\n", "the shell in the project",
		func(s string) bool {
			return strings.Contains(s, "\nin-project") || strings.Contains(s, "│in-project")
		})

	a.send(t, "\x02f")
	a.waitForScreen(t, "the panel", func(s string) bool { return strings.Contains(s, "note.txt") })
	a.send(t, "/note")
	a.waitForScreen(t, "the search", func(s string) bool { return strings.Contains(s, "› note") })
	a.send(t, "\r")
	a.waitForScreen(t, "the editor in a tab of its own", func(s string) bool {
		return strings.Contains(s, "hello-from-note") && strings.Contains(a.lines()[0], "note.txt")
	})
}

// TestTheFilesPanelOpensFromThePaneMenu: the panel is on a pane's
// right-click menu as well as on prefix+f. If it regresses, someone driving
// tend with the mouse has no way to find it.
func TestTheFilesPanelOpensFromThePaneMenu(t *testing.T) {
	t.Setenv("PATH", filepath.Dir(buildBinary(t))+string(os.PathListSeparator)+os.Getenv("PATH"))
	a := startSession(t, 120, 30)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.openMenuOn(t, 70, 12, "files panel")
	a.clickAt(t, 72, a.lineContaining(t, "files panel"))
	a.waitForScreen(t, "the panel", func(s string) bool { return strings.Contains(s, "files │ search │ changes") })
}

// TestTextFoundInThePanelOpensTheEditorOnItsLine: ctrl+f in the panel
// searches the project's files for text, and enter on a result opens the
// editor there — vi is given +line. If it regresses, the panel finds where
// something is written and then opens the file at its top.
func TestTextFoundInThePanelOpensTheEditorOnItsLine(t *testing.T) {
	project := gitProject(t)
	if err := os.WriteFile(filepath.Join(project, "notes.txt"), []byte("one\ntwo\nfind-this-line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A vi that says what it was asked to open, and stays.
	fake := t.TempDir()
	if err := os.WriteFile(filepath.Join(fake, "vi"), []byte("#!/bin/sh\necho \"EDITING $*\"\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runtimeDir, err := os.MkdirTemp("", "tf")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(runtimeDir) })
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(),
		"TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh",
		"TEND_CONFIG="+filepath.Join(t.TempDir(), "absent.toml"),
		"PATH="+fake+string(os.PathListSeparator)+os.Getenv("PATH"),
		"EDITOR=vi", "VISUAL=",
	)
	bin := buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", "grep"}, pty.Options{Size: pty.Size{Cols: 130, Rows: 30}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(130, 30, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stopSession(t, "grep")
	})
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "cd "+project+" && echo in-project\n", "the shell in the project",
		func(s string) bool {
			return strings.Contains(s, "\nin-project") || strings.Contains(s, "│in-project")
		})
	a.send(t, "\x02f")
	a.waitForScreen(t, "the panel", func(s string) bool { return strings.Contains(s, "notes.txt") })

	a.send(t, "\x06find-this")
	a.waitForScreen(t, "the result", func(s string) bool {
		return strings.Contains(s, "1 in 1 files") && strings.Contains(s, "3 find-this-line")
	})
	a.send(t, "\r\x1b[B\r")
	a.waitForScreen(t, "vi on line 3", func(s string) bool {
		return strings.Contains(s, "EDITING +3 "+filepath.Join(project, "notes.txt"))
	})
}
