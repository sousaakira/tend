//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sousaakira/tend/internal/server"
)

// TestAReleaseFoundIsSaidAndItsNotesOpenFromTheMenu: when the server's
// check finds a release (herdr's fake one here, TEND_FAKE_UPDATE_VERSION),
// the status bar says "update ready", the sidebar's button reads "● menu",
// the menu it opens offers "update ready", and that opens the notes panel,
// which esc closes. If it regresses, a release is found and nobody is told,
// or its notes cannot be reached.
func TestAReleaseFoundIsSaidAndItsNotesOpenFromTheMenu(t *testing.T) {
	// The update the notes offer runs the tend on the PATH: here one that
	// only says it was run, and not the one installed on the machine the
	// tests run on, which would update itself from the real releases.
	fake := t.TempDir()
	if err := os.WriteFile(filepath.Join(fake, "tend"), []byte("#!/bin/sh\necho \"fake-update $*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	buildBinary(t)
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TEND_BIN_PATH", "")
	a := startSessionConfigured(t, 120, 36, server.Config{
		Build: version, FakeUpdate: "v9.9.9",
		NotesPath: filepath.Join(t.TempDir(), "release-notes.json"),
	})
	a.waitForScreen(t, "update ready on the status bar, and the dot on the menu button", func(s string) bool {
		lines := strings.Split(s, "\n")
		return strings.Contains(lines[len(lines)-1], "update ready") && strings.Contains(s, "● menu")
	})

	row, col := -1, -1
	for i, line := range a.lines() {
		if c := columnOfString(line, "● menu"); c >= 0 {
			row, col = i, c
		}
	}
	a.clickAt(t, col+3, row+1)
	a.waitForScreen(t, "the global menu", func(s string) bool {
		return strings.Contains(s, "reload config") && strings.Contains(s, "update ready ●")
	})
	for i, line := range a.lines() {
		if c := columnOfString(line, "update ready ●"); c >= 0 {
			row, col = i, c
		}
	}
	a.clickAt(t, col+2, row+1)
	a.waitForScreen(t, "the notes panel", func(s string) bool {
		return strings.Contains(s, "v9.9.9") && strings.Contains(s, " esc close ") &&
			strings.Contains(s, "● update ready") && strings.Contains(s, "press u in the release notes") && strings.Contains(s, "NEW")
	})

	if !strings.Contains(strings.Join(a.lines(), "\n"), " u update now ") {
		t.Fatal("the notes of a newer release offer to update")
	}
	a.send(t, "u")
	a.waitForScreen(t, "the update, run in a tab of its own", func(s string) bool {
		return strings.Contains(s, "fake-update update -handoff") && strings.Contains(s, "update") && !strings.Contains(s, " esc close ")
	})
}
