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

// TestSpinnerFramesAreStrippedFromTheTerminalTitle holds herdr's cases: a
// window bar showing Claude's spinner turns over ten times a second, and one
// that eats "★ production" has lost somebody's name for a window.
func TestSpinnerFramesAreStrippedFromTheTerminalTitle(t *testing.T) {
	for _, title := range []string{"⠋ task", "✳ task", "  ⠙   task  ", "✢ task", "✻ task", "◐ task", "◒ task"} {
		if got := strippedTerminalTitle(title); got != "task" {
			t.Errorf("strippedTerminalTitle(%q) = %q, want %q", title, got, "task")
		}
	}
	for title, want := range map[string]string{
		"⠋ ⠙ task":      "⠙ task", // one frame, not every one
		"★ production":  "★ production",
		"✨ task":        "✨ task",
		"task ⠋ detail": "task ⠋ detail",
		"⠋task":         "⠋task", // not followed by a space: part of the word
		" ⠋ 修复🙂标题 ":     "修复🙂标题",
		"⠋   ":          "",
	} {
		if got := strippedTerminalTitle(title); got != want {
			t.Errorf("strippedTerminalTitle(%q) = %q, want %q", title, got, want)
		}
	}
}

// TestTheWindowIsNamedAfterTheSession: the window tend runs in carries the
// session's title, a script can say something else in it, and detaching
// gives the window back what it had. If it regresses, every tend window in a
// window manager's bar is called whatever the shell last said, and a script
// cannot mark the window it is working in.
func TestTheWindowIsNamedAfterTheSession(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	configPath := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(configPath,
		[]byte("[ui]\nwindow_title = \"{hostname} | {tab} | tend\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := buildBinary(t)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh", "TEND_CONFIG="+configPath)

	p, err := pty.Start(bin, []string{"attach", "-s", "titled"}, pty.Options{
		Size: pty.Size{Cols: 100, Rows: 14}, Env: env,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(100, 14, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "titled") })

	host, _ := os.Hostname()
	want := "\x1b]0;" + host + " | tab 1 | tend\x07"
	a.waitForScreen(t, "the window title", func(string) bool {
		return strings.Contains(a.raw(), want)
	})
	if !strings.Contains(a.raw(), pushTitle) {
		t.Error("the window's own title was not saved before tend replaced it")
	}

	title := func(args ...string) {
		t.Helper()
		cmd := exec.Command(bin, append([]string{"terminal", "title"}, args...)...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tend terminal title %v: %v\n%s", args, err, out)
		}
	}
	title("set", "deploying", "-s", "titled")
	a.waitForScreen(t, "the script's title", func(string) bool {
		return strings.Contains(a.raw(), "\x1b]0;deploying\x07")
	})

	// Cleared, the template comes back.
	before := strings.Count(a.raw(), want)
	title("clear", "-s", "titled")
	a.waitForScreen(t, "the template again", func(string) bool {
		return strings.Count(a.raw(), want) > before
	})

	a.send(t, "\x02d")
	if err := a.pty.Wait(); err != nil {
		t.Logf("client exit: %v", err)
	}
	if raw := a.raw(); !strings.Contains(raw[strings.LastIndex(raw, want):], popTitle) {
		t.Error("detaching did not give the window its title back")
	}
}
