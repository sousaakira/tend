package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTheSettingsScreenChangesTheFileAndTheSession: a settings screen that
// only changed the running client would leave the file saying something else,
// and the next start would undo what the user just did.
func TestTheSettingsScreenChangesTheFileAndTheSession(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(configPath, []byte("# mine\n[ui]\nsidebar = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withConfig(t, configPath)

	a := startSessionIn(t, 100, 18, t.TempDir())
	a.waitForScreen(t, "the sidebar", func(s string) bool { return strings.Contains(s, "spaces") })

	a.send(t, "\x02s")
	a.waitForScreen(t, "the settings screen", func(s string) bool {
		return strings.Contains(s, "settings") && strings.Contains(s, "sidebar")
	})

	// The first row is the sidebar, and changing it turns it off in front of
	// the user.
	a.send(t, "l")
	a.waitForScreen(t, "the sidebar to go", func(s string) bool {
		return !strings.Contains(s, "spaces")
	})

	// And the file says so, with what the user wrote still in it.
	written, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "sidebar = false") {
		t.Errorf("the file was not changed:\n%s", written)
	}
	if !strings.Contains(string(written), "# mine") {
		t.Errorf("the edit lost the user's own lines:\n%s", written)
	}

	// q closes the screen and leaves the pane usable.
	a.send(t, "q")
	a.waitForScreen(t, "the screen to close", func(s string) bool {
		return !strings.Contains(s, "j k  move")
	})
	a.sendUntil(t, "printf 'STILL-WORKS\\n'\n", "the pane", func(s string) bool {
		return strings.Contains(s, "STILL-WORKS")
	})
}

// TestRebindingAKeyInTheSettingsFile: the keys are tmux's where herdr's
// differ, and somebody whose hands know one or the other should be able to
// say so rather than relearn.
func TestRebindingAKeyInTheSettingsFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(configPath, []byte("[keys.bind]\ndetach = \"q\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withConfig(t, configPath)

	// Wide enough for the help's two columns, so the last rows are not cut.
	a := startSessionIn(t, 130, 20, t.TempDir())
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// The help shows the key that is in effect.
	a.send(t, "\x02?")
	a.waitForScreen(t, "the help with the rebound key", func(s string) bool {
		return strings.Contains(s, "d q")
	})
	a.send(t, "\x1b")

	// And the key detaches, which ends the client.
	a.send(t, "\x02q")
	exited := make(chan error, 1)
	go func() { exited <- a.pty.Wait() }()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the rebound key did not detach")
	}
}
