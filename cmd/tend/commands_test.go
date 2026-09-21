//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAKeyRunsTheUsersCommand is [[keys.command]] end to end: a pane command
// has the screen while it runs and gives the view back when it ends, and a
// shell command runs in the background from the focused pane. If it
// regresses, the key does nothing, or leaves a dead pane where the view was.
func TestAKeyRunsTheUsersCommand(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	configPath := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(configPath, []byte(`[[keys.command]]
key = "prefix+Y"
type = "pane"
command = "printf 'IN-THE-COMMAND\n'; read _"

[[keys.command]]
key = "U"
command = "echo done > `+marker+`"
description = "mark it"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	withConfig(t, configPath)

	a := startSession(t, 100, 18)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "printf 'BEFORE-THE-COMMAND\\n'\n", "the shell's output", func(s string) bool {
		return strings.Count(s, "BEFORE-THE-COMMAND") >= 2
	})

	a.send(t, "\x02Y")
	a.waitForScreen(t, "the command alone on screen", func(s string) bool {
		return strings.Contains(s, "IN-THE-COMMAND") && !strings.Contains(s, "BEFORE-THE-COMMAND")
	})
	a.send(t, "\n")
	a.waitForScreen(t, "the view it came from", func(s string) bool {
		return !strings.Contains(s, "IN-THE-COMMAND") && strings.Count(s, "BEFORE-THE-COMMAND") >= 2 &&
			strings.Count(s, "┌") == 1
	})

	a.send(t, "\x02U")
	a.waitForScreen(t, "word that it ran", func(s string) bool { return strings.Contains(s, "ran mark it") })
	waitForFileContent(t, marker, "done")
}

// waitForFileContent waits, bounded, for a file to say something.
func waitForFileContent(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never said %q", path, want)
}

// TestAPopupCommandFloatsOverTheTabAndHasTheKeys is herdr's popup: a
// command of type popup runs in a box over the tab, of the size asked for,
// with the panes still there under it; what is typed goes to it; when its
// program ends it goes, and the keys are the shell's again. If it
// regresses, a popup command takes a pane of the layout, or leaves the
// keyboard with nowhere to go.
func TestAPopupCommandFloatsOverTheTabAndHasTheKeys(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(configPath, []byte(`[[keys.command]]
key = "prefix+Y"
type = "popup"
width = 40
height = "50%"
command = "printf 'IN-THE-POPUP\n'; read x; printf 'got-%s\n' \"$x\"; sleep 1"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	withConfig(t, configPath)

	a := startSession(t, 100, 24)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "printf 'BEFORE-THE-POPUP\\n'\n", "the shell's output", func(s string) bool {
		return strings.Count(s, "BEFORE-THE-POPUP") >= 2
	})

	a.send(t, "\x02Y")
	a.waitForScreen(t, "the popup over the pane", func(s string) bool {
		return strings.Contains(s, "IN-THE-POPUP") && strings.Contains(s, "BEFORE-THE-POPUP")
	})
	if strings.Contains(a.text(), "exited") {
		t.Errorf("a running popup is not exited:\n%s", a.text())
	}
	// 40 columns wide, framed, drawn inside the pane that is still there.
	top := []rune(a.lines()[a.lineContaining(t, "┌ 2 ")-1])
	start := -1
	for i, r := range top {
		if r == '┌' && i > 30 {
			start = i
			break
		}
	}
	if start < 0 || start+39 >= len(top) || top[start+39] != '┐' {
		t.Errorf("the popup should be a 40-wide box:\n%s", a.text())
	}

	a.send(t, "hello\r")
	a.waitForScreen(t, "the popup to answer", func(s string) bool { return strings.Contains(s, "got-hello") })
	a.waitForScreen(t, "the popup to go", func(s string) bool {
		return !strings.Contains(s, "IN-THE-POPUP") && strings.Count(s, "┌") == 1
	})
	a.sendUntil(t, "printf 'AFTER-THE-POPUP\\n'\n", "the shell to have the keys again", func(s string) bool {
		return strings.Count(s, "AFTER-THE-POPUP") >= 2
	})
}
