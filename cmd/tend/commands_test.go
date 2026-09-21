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
