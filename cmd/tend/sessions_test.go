//go:build unix

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/vt"
)

// TestTheSessionsListFindsResumesAndDeletes: prefix+S lists the Claude Code
// conversations on the machine by their titles, typing narrows the list,
// enter resumes the one chosen in a new tab in its directory with the
// agent's resume flag, and ctrl+d, answered with enter, deletes the one
// chosen from the disk. The claude here is a script that prints what it was
// given — the list and the tab are under test, not Claude Code. If it
// regresses, the toolbar's Sessions shows nothing, finds nothing, or
// resumes or deletes the wrong conversation.
func TestTheSessionsListFindsResumesAndDeletes(t *testing.T) {
	claudeDir := t.TempDir()
	project := t.TempDir()
	folder := filepath.Join(claudeDir, "projects", "-work")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	const keep, drop = "11111111-2222-3333-4444-555555555555", "66666666-7777-8888-9999-aaaaaaaaaaaa"
	write := func(id, title string) string {
		path := filepath.Join(folder, id+".jsonl")
		body := `{"type":"user","cwd":"` + project + `","message":{"role":"user","content":"hello"}}` + "\n" +
			`{"type":"ai-title","aiTitle":"` + title + `"}` + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write(keep, "Checkout redesign")
	dropped := write(drop, "Old experiment")

	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\necho \"claude was given: $*\"\necho \"in: $(basename \"$(pwd)\")\"\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh",
		"CLAUDE_CONFIG_DIR="+claudeDir, "PATH="+bin+":"+os.Getenv("PATH"))
	tend := buildBinary(t)
	p, err := pty.Start(tend, []string{"attach", "-s", "sessions"}, pty.Options{Size: pty.Size{Cols: 120, Rows: 36}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(120, 36, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "sessions") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02S")
	a.waitForScreen(t, "both sessions", func(s string) bool {
		return strings.Contains(s, "AGENT SESSIONS") && strings.Contains(s, "Checkout redesign") && strings.Contains(s, "Old experiment")
	})
	a.send(t, "old exp")
	a.waitForScreen(t, "the search narrowing it", func(s string) bool {
		return strings.Contains(s, "Old experiment") && !strings.Contains(s, "Checkout redesign") && strings.Contains(s, "1 of 2")
	})
	a.send(t, "\x04")
	a.waitForScreen(t, "the question", func(s string) bool { return strings.Contains(s, "delete 1 session(s) for good?") })
	a.send(t, "\r")
	a.waitForScreen(t, "it deleted", func(s string) bool { return strings.Contains(s, "deleted 1") })
	if _, err := os.Stat(dropped); !os.IsNotExist(err) {
		t.Fatalf("the file is still there: %v", err)
	}

	a.send(t, "\x1b") // out of the search
	a.waitForScreen(t, "the one left", func(s string) bool {
		return strings.Contains(s, "Checkout redesign") && strings.Contains(s, "1 of 1")
	})
	a.send(t, "\r")
	a.waitForScreen(t, "it resumed in a new tab, in its directory", func(s string) bool {
		return !strings.Contains(s, "AGENT SESSIONS") && strings.Contains(s, "claude was given: --resume "+keep) && strings.Contains(s, "in: "+filepath.Base(project))
	})
}
