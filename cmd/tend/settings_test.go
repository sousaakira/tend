//go:build unix

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sousaakira/tend/internal/update"
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

	// The second row is the sidebar (the theme comes first, as in herdr), and
	// changing it turns it off in front of the user.
	a.send(t, "j")
	a.waitForScreen(t, "the cursor on the sidebar row", func(s string) bool {
		return strings.Contains(s, "▸ sidebar")
	})
	a.send(t, "l")
	// The row saying off, not only "spaces" gone: the settings panel itself
	// covers that word, and then the wait is over before the file is written.
	a.waitForScreen(t, "the sidebar to go", func(s string) bool {
		return strings.Contains(s, "sidebar             on [off]")
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

// TestPickingAThemeInTheSettingsScreen: the theme row writes the name into
// the file and the session is drawn in it straight away. If it regresses, the
// row either changes nothing on screen or leaves a file the next start reads
// differently from what was shown.
func TestPickingAThemeInTheSettingsScreen(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(configPath, []byte("[ui]\nsidebar = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withConfig(t, configPath)

	a := startSessionIn(t, 100, 30, t.TempDir())
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02s")
	a.waitForScreen(t, "the theme row", func(s string) bool {
		return strings.Contains(s, "▸ theme") && strings.Contains(s, "[terminal colours]")
	})
	// One step along is the first named theme, catppuccin.
	a.send(t, "l")
	a.waitForScreen(t, "catppuccin chosen", func(s string) bool {
		return strings.Contains(s, "[catppuccin]")
	})

	written, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "[ui.theme]\nname = \"catppuccin\"") {
		t.Errorf("the theme was not written to the file:\n%s", written)
	}

	// The focused frame is drawn in catppuccin's accent, 137 180 250.
	a.send(t, "q")
	a.waitForScreen(t, "the accent on screen", func(string) bool {
		return strings.Contains(a.raw(), "2;137;180;250")
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

// TestUpdatingFromAPublishedManifest is the updater end to end with the real
// binary: a manifest served over HTTP, a download checked against it, and the
// binary replaced. If it regresses, an update either does nothing or installs
// bytes nobody checked.
func TestUpdatingFromAPublishedManifest(t *testing.T) {
	published := []byte("#!/bin/sh\necho i-am-the-new-tend\n")
	sum := sha256.Sum256(published)

	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/tend", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(published) })
	mux.HandleFunc("/latest.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"version":"published-build","notes":"what changed","assets":{%q:%q},"sha256":{%q:%q}}`,
			update.Platform(), base+"/tend", update.Platform(), hex.EncodeToString(sum[:]))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL

	configPath := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(configPath, []byte(
		"[update]\nchannel = \"stable\"\nmanifest = \""+srv.URL+"/latest.json\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A copy of the binary, so the test replaces its own and not the one the
	// suite is running from.
	built := buildBinary(t)
	source, err := os.ReadFile(built)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "tend")
	if err := os.WriteFile(bin, source, 0o755); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(), "TEND_CONFIG="+configPath)
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	out, err := run("update", "-check")
	if err != nil {
		t.Fatalf("update -check: %v\n%s", err, out)
	}
	if !strings.Contains(out, "published-build") || !strings.Contains(out, "what changed") {
		t.Errorf("the check does not say what is published: %s", out)
	}
	if after, _ := os.ReadFile(bin); string(after) != string(source) {
		t.Error("a check installed something")
	}

	if out, err := run("update"); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	after, err := os.ReadFile(bin)
	if err != nil || string(after) != string(published) {
		t.Fatalf("after updating, the binary is %d bytes, %v", len(after), err)
	}
	// And nothing was left beside it.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("the directory holds %d files after an update", len(entries))
	}
}

// TestTheThemeFollowsTheTerminal: with auto_switch the client asks the
// terminal what it looks like, turns to the light theme when the terminal
// says it went light, and keeps the report out of the pane. If it regresses,
// the theme stays dark on a light desktop, or the report is typed into the
// shell.
func TestTheThemeFollowsTheTerminal(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(configPath, []byte("[ui.theme]\nauto_switch = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withConfig(t, configPath)

	a := startSessionIn(t, 100, 16, t.TempDir())
	a.waitForScreen(t, "the question to the terminal", func(string) bool {
		return strings.Contains(a.raw(), "\x1b[?2031h") && strings.Contains(a.raw(), "\x1b]11;?")
	})
	// Dark until told otherwise: catppuccin's accent, 137 180 250.
	a.waitForScreen(t, "the dark theme", func(string) bool {
		return strings.Contains(a.raw(), "2;137;180;250")
	})

	a.send(t, "\x1b[?997;2n")
	// Latte's accent is 30 102 245.
	a.waitForScreen(t, "the light theme", func(string) bool {
		return strings.Contains(a.raw(), "2;30;102;245")
	})
	a.sendUntil(t, "printf 'STILL-CLEAN\\n'\n", "the shell", func(s string) bool {
		return strings.Contains(s, "STILL-CLEAN")
	})
	if strings.Contains(a.text(), "997") {
		t.Errorf("the report reached the pane:\n%s", a.text())
	}

	a.send(t, "\x02d")
	a.waitForScreen(t, "the reports turned off", func(string) bool {
		return strings.Contains(a.raw(), "\x1b[?2031l")
	})
}
