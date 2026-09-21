//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestACtrlClickOpensTheLinkUnderIt: a URL a pane printed, ctrl+clicked,
// is handed to the desktop's opener on this machine, trimmed as herdr
// trims it; a plain click is not. If it regresses, the links an agent
// prints have to be copied by hand.
func TestACtrlClickOpensTheLinkUnderIt(t *testing.T) {
	fake := t.TempDir()
	opened := filepath.Join(fake, "opened")
	if err := os.WriteFile(filepath.Join(fake, "xdg-open"), []byte("#!/bin/sh\nprintf '%s\\n' \"$1\" >> "+opened+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DISPLAY", ":99")
	a := startSession(t, 100, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "printf 'see https://example.com/a(b)c. now\\n'\n", "the link",
		func(s string) bool {
			return strings.Contains(s, "\nsee https://example.com") || strings.Contains(s, "│see https://example.com")
		})

	row := 0
	for i, line := range a.lines() {
		if strings.Contains(line, "│see https://") {
			row = i + 1
		}
	}
	line := a.lines()[row-1]
	col := len([]rune(line[:strings.Index(line, "example")])) + 1

	// A plain click is the program's, or a selection's.
	a.clickAt(t, col, row)
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(opened); err == nil {
		t.Fatal("a click without ctrl opened the link")
	}
	// ctrl+click: SGR button 0 with the ctrl bit, 16.
	a.send(t, "\x1b[<16;"+itoa(col)+";"+itoa(row)+"M\x1b[<16;"+itoa(col)+";"+itoa(row)+"m")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(opened); err == nil && strings.TrimSpace(string(data)) != "" {
			if got := strings.TrimSpace(string(data)); got != "https://example.com/a(b)c" {
				t.Errorf("opened %q", got)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("nothing was opened; the screen:\n%s", a.text())
}
