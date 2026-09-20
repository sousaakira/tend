package main

import (
	"strings"
	"sync"
	"testing"
)

// clipboardOf records what the TUI tells the terminal to copy, which is the
// one route a test can watch: the local tools write to a clipboard the test
// has no business reading, and OSC 52 goes through the terminal it owns.
func clipboardOf(a *attached) func() string {
	var (
		mu   sync.Mutex
		last string
	)
	a.mu.Lock()
	a.screen.OnClipboard = func(text []byte) {
		mu.Lock()
		last = string(text)
		mu.Unlock()
	}
	a.mu.Unlock()
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return last
	}
}

// TestCopyModeSelectsWithTheKeyboard is copy mode end to end: into the
// history with prefix+[, back to a line, a word selected with vi motions, and
// what reaches the clipboard is that word. If it regresses, text that has
// scrolled off can only be copied with the mouse — and inside an agent that
// holds the mouse, not at all.
func TestCopyModeSelectsWithTheKeyboard(t *testing.T) {
	a := startSession(t, 80, 14)
	copied := clipboardOf(a)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "for i in $(seq 1 30); do echo row-$i; done; echo 'alpha bravo-charlie delta'\n")
	a.waitForScreen(t, "the output", func(s string) bool {
		return strings.Contains(s, "alpha bravo-charlie delta")
	})

	a.send(t, "\x02[")
	a.waitForScreen(t, "copy mode", func(s string) bool { return strings.Contains(s, "copy ") })

	// Searching backwards from the bottom finds the nearest "alpha" above,
	// which is the output line rather than the command that printed it.
	a.send(t, "?alpha\r")
	a.waitForScreen(t, "the search result", func(s string) bool { return strings.Contains(s, "found") })

	// w onto "bravo", v to start there, E to the end of the big word — which
	// runs to the space, hyphen and all — and y to copy it.
	a.send(t, "w")
	a.send(t, "v")
	a.send(t, "E")
	a.send(t, "y")
	a.waitForScreen(t, "the copy", func(s string) bool { return strings.Contains(s, "copied") })

	if got := copied(); got != "bravo-charlie" {
		t.Errorf("copied %q, want %q", got, "bravo-charlie")
	}
	// And copying left copy mode, back to the live pane.
	if strings.Contains(a.text(), "copy ") {
		t.Error("copy mode is still up after copying")
	}
}

// TestCopyModeYanksALineWithNothingSelected: y on its own takes the line under
// the cursor, as tmux does. The alternative — copying nothing — looks like a
// failure to anyone who pressed y.
func TestCopyModeYanksALineWithNothingSelected(t *testing.T) {
	a := startSession(t, 80, 14)
	copied := clipboardOf(a)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "echo the-whole-line-here\n")
	a.waitForScreen(t, "the output", func(s string) bool {
		return strings.Count(s, "the-whole-line-here") >= 2
	})
	a.send(t, "\x02[")
	a.waitForScreen(t, "copy mode", func(s string) bool { return strings.Contains(s, "copy ") })
	a.send(t, "?the-whole\r")
	a.waitForScreen(t, "the search result", func(s string) bool { return strings.Contains(s, "found") })
	a.send(t, "y")
	a.waitForScreen(t, "the copy", func(s string) bool { return strings.Contains(s, "copied") })

	if got := strings.TrimSpace(copied()); got != "the-whole-line-here" {
		t.Errorf("copied %q, want the line under the cursor", got)
	}
}
