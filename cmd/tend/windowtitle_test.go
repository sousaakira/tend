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

// TestTheTabBarShowsWhatTheSettingsAskFor: entries from ui.tab_bar_right
// reach the right end of the bar through the real server, including a
// command's output and ZOOM only while this client is zoomed. If it
// regresses, the bar stays empty with nothing to say why.
func TestTheTabBarShowsWhatTheSettingsAskFor(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(configPath, []byte(`[ui]
tab_bar_right = [
  { type = "zoom" },
  { type = "text", text = "prod" },
  { type = "command", command = "echo from-a-command" },
]
tab_bar_right_separator = " / "
`), 0o600); err != nil {
		t.Fatal(err)
	}
	// The real binary, so the server is the daemon a user gets, configured
	// from the file the way it is on start.
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	bin := buildBinary(t)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh", "TEND_CONFIG="+configPath)
	p, err := pty.Start(bin, []string{"attach", "-s", "barred"}, pty.Options{
		Size: pty.Size{Cols: 100, Rows: 16}, Env: env,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(100, 16, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "barred") })

	a.waitForScreen(t, "the status entries", func(s string) bool {
		first, _, _ := strings.Cut(s, "\n")
		return strings.HasSuffix(strings.TrimRight(first, " "), "prod / from-a-command")
	})
	if strings.Contains(a.text(), "ZOOM") {
		t.Error("ZOOM shown with nothing zoomed")
	}

	// A second pane, so there is something to zoom away from.
	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool { return strings.Count(s, "┌") >= 2 })
	a.send(t, "\x02z")
	a.waitForScreen(t, "ZOOM in the bar", func(s string) bool {
		first, _, _ := strings.Cut(s, "\n")
		return strings.Contains(first, "ZOOM / prod / from-a-command")
	})
}

// TestATabBarAtTheBottom: with tab_bar_position = "bottom" the panes take
// the top row, the bar sits above the status line, and its "+" still makes a
// tab. If it regresses, the panes are laid out over the bar or a click on it
// goes to a pane.
func TestATabBarAtTheBottom(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(configPath, []byte("[ui]\nsidebar = false\ntab_bar_position = \"bottom\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withConfig(t, configPath)

	const rows = 14
	a := startSessionIn(t, 80, rows, t.TempDir())
	a.waitForScreen(t, "a pane at the top and the bar at the bottom", func(s string) bool {
		lines := strings.Split(s, "\n")
		return len(lines) >= rows && strings.Contains(lines[0], "┌") &&
			strings.Contains(lines[rows-2], "tab 1")
	})

	bar := a.lines()[rows-2]
	plus := strings.LastIndex(bar, "+")
	if plus < 0 {
		t.Fatalf("no new-tab button on the bar: %q", bar)
	}
	// Mouse reports count from one.
	a.clickAt(t, len([]rune(bar[:plus]))+1, rows-2+1)
	a.waitForScreen(t, "a second tab", func(s string) bool {
		lines := strings.Split(s, "\n")
		return len(lines) >= rows && strings.Contains(lines[rows-2], "2")
	})
}

// TestATabIsDraggedToAnotherPlace is herdr's tab drag: pressed on the bar,
// dragged over another tab and let go, it takes that tab's place. If it
// regresses, the only way to reorder tabs is the socket.
func TestATabIsDraggedToAnotherPlace(t *testing.T) {
	a := startSession(t, 100, 14)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	for range 2 {
		before := strings.Count(a.lines()[0], "tab ")
		a.send(t, "\x02c")
		a.waitForScreen(t, "another tab", func(string) bool { return strings.Count(a.lines()[0], "tab ") > before })
	}
	bar := a.lines()[0]
	from := columnOfString(bar, "tab 3")
	to := columnOfString(bar, "tab 1")
	if from < 0 || to < 0 {
		t.Fatalf("tab bar = %q", bar)
	}
	a.dragFromTo(t, 0, from+2, 1, to+2, 1)
	a.waitForScreen(t, "tab 3 first", func(string) bool {
		bar := a.lines()[0]
		return strings.Index(bar, "tab 3") >= 0 && strings.Index(bar, "tab 3") < strings.Index(bar, "tab 1")
	})
}

// TestASpaceIsDraggedToAnotherPlace: pressed in the sidebar, dragged onto
// another space and let go, a space takes its place, as a tab does on the
// bar. If it regresses, spaces can only be reordered over the socket.
func TestASpaceIsDraggedToAnotherPlace(t *testing.T) {
	a := startSession(t, 100, 18)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.send(t, "\x02N")
	a.waitForScreen(t, "a second space", func(string) bool { return strings.Contains(a.sidebarText(), "space 2") })

	from := a.lineContaining(t, "space 2")
	to := a.lineContaining(t, "main")
	a.dragFromTo(t, 0, 6, from, 6, to)
	a.waitForScreen(t, "space 2 above main", func(string) bool {
		side := a.sidebarText()
		return strings.Index(side, "space 2") >= 0 && strings.Index(side, "space 2") < strings.Index(side, "main")
	})
}
