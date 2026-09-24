//go:build unix

package main

import (
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
			strings.Contains(s, "● update ready") && strings.Contains(s, "tend update -handoff") && strings.Contains(s, "NEW")
	})

	a.send(t, "\x1b")
	a.waitForScreen(t, "the panel closed", func(s string) bool { return !strings.Contains(s, " esc close ") })
}
