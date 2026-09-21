//go:build unix

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/sousaakira/tend/internal/pty"
)

// TestResizingWhileDrawingDoesNotCrashTheClient: the window manager sends
// SIGWINCH whenever a window moves between monitors or a tiling manager
// rearranges, which is while the client is drawing. The resize used to
// invalidate the painter from its own goroutine, and the painter is the
// drawing goroutine's — a redraw that landed in between could be left
// painting from a frame that had just been taken away.
func TestResizingWhileDrawingDoesNotCrashTheClient(t *testing.T) {
	// The client watches its own goroutines for this one: the detector in
	// this process cannot see into the one it starts.
	raceBinary = true
	t.Cleanup(func() { raceBinary = false })

	a := startSession(t, 100, 24)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// Output while resizing, so the client is redrawing throughout.
	a.send(t, "while true; do printf 'line %s\\n' $RANDOM; done\n")

	for i := 0; i < 40; i++ {
		cols := uint16(90 + i%20)
		rows := uint16(20 + i%6)
		if err := a.pty.Resize(pty.Size{Cols: cols, Rows: rows}); err != nil {
			t.Fatalf("resize: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Still drawing, which is the whole assertion: a crashed client draws
	// nothing more.
	a.send(t, "\x03")
	a.sendUntil(t, "printf 'STILL-ALIVE\\n'\n", "the client to still be drawing", func(s string) bool {
		return strings.Contains(s, "STILL-ALIVE")
	})
	if raw := a.raw(); strings.Contains(raw, "DATA RACE") || strings.Contains(raw, "panic:") {
		// Print the report itself rather than the tail of the drawing.
		at := strings.Index(raw, "DATA RACE")
		if at < 0 {
			at = strings.Index(raw, "panic:")
		}
		end := min(at+2500, len(raw))
		t.Errorf("the client reported trouble while being resized:\n%s", raw[max(at-200, 0):end])
	}
}
