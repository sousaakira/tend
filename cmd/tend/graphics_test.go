//go:build unix

package main

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/sousaakira/tend/internal/proto"
)

// TestOnlyTerminalsThatDrawImagesAreSentThem: a terminal that does not speak
// the protocol prints the escape sequence as text, which lands in the middle
// of a pane and stays there.
func TestOnlyTerminalsThatDrawImagesAreSentThem(t *testing.T) {
	for _, c := range []struct {
		env  map[string]string
		want bool
	}{
		{map[string]string{"KITTY_WINDOW_ID": "1"}, true},
		{map[string]string{"TERM_PROGRAM": "ghostty"}, true},
		{map[string]string{"TERM": "xterm-kitty"}, true},
		{map[string]string{"TERM": "wezterm"}, true},
		{map[string]string{"TERM": "xterm-256color"}, false},
		{map[string]string{"TERM": "screen"}, false},
		{map[string]string{"KITTY_WINDOW_ID": "1", "TEND_GRAPHICS": "off"}, false},
	} {
		env := func(k string) string { return c.env[k] }
		if got := graphicsSupported(env); got != c.want {
			t.Errorf("graphicsSupported(%v) = %v, want %v", c.env, got, c.want)
		}
	}
}

// TestAnImageIsTransmittedInChunksAndPlacedWhereItGoes: the sequences are what
// the outer terminal acts on, and a wrong one either draws nothing or draws
// over the wrong pane.
func TestAnImageIsTransmittedInChunksAndPlacedWhereItGoes(t *testing.T) {
	var out strings.Builder
	writeImage(&out, 990001, proto.GraphicsImage{
		ID: 1, Format: 100, Data: []byte(strings.Repeat("x", 4096)),
	})
	sent := out.String()
	if strings.Count(sent, "\x1b_G") < 2 {
		t.Errorf("a 4KB image went in one sequence; it has to be chunked:\n%q", sent[:80])
	}
	if !strings.Contains(sent, "a=t,t=d,q=2,i=990001,f=100") {
		t.Errorf("the transmission does not say what it is: %q", sent[:80])
	}
	if !strings.Contains(sent, ",m=1;") || !strings.HasSuffix(strings.TrimSuffix(sent, "\x1b\\"), strings.TrimSuffix(lastChunk(sent), "\x1b\\")) {
		t.Errorf("the chunks are not marked as continuing: %q", sent[:120])
	}

	out.Reset()
	writePlacement(&out, placedAt{x: 10, y: 4}, 990001, proto.GraphicsPlacement{Cols: 8, Rows: 3, Z: 2})
	placed := out.String()
	// One-based, because that is what the terminal counts in.
	if !strings.Contains(placed, "\x1b[5;11H") {
		t.Errorf("the cursor was not moved to the pane's cell: %q", placed)
	}
	if !strings.Contains(placed, "a=p,q=2,i=990001,p=1,C=1,c=8,r=3,z=2") {
		t.Errorf("the placement is wrong: %q", placed)
	}
	// Saved and restored, or the next paint draws from wherever this left it.
	if !strings.HasPrefix(placed, "\x1b7") || !strings.HasSuffix(placed, "\x1b8") {
		t.Errorf("the cursor was not put back: %q", placed)
	}
}

func lastChunk(s string) string {
	i := strings.LastIndex(s, "\x1b_G")
	return s[i:]
}

// TestAnImageDrawnInAPaneReachesTheTerminal is the whole path: a program in a
// pane sends an image, the server keeps it, and the client puts it on the
// terminal it is drawing to. If it regresses, an image in a pane is invisible
// and nothing says why.
func TestAnImageDrawnInAPaneReachesTheTerminal(t *testing.T) {
	// The harness's terminal is told it can draw images; the real one this
	// runs in may not be.
	a := startSessionEnv(t, 80, 14, "TERM=xterm-kitty")
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// A pane program that transmits a tiny image and places it.
	data := base64.StdEncoding.EncodeToString([]byte("PRETEND-PNG-BYTES"))
	a.sendUntil(t, "printf 'MARK\\n'; printf '\\033_Ga=T,i=5,f=100,c=4,r=2;"+data+"\\033\\\\'\n",
		"the mark the image was placed after", func(s string) bool {
			return strings.Contains(s, "MARK")
		})

	// What the client wrote to its terminal is what the test reads, so the
	// image's bytes turn up in the stream the harness is parsing.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(a.raw(), data) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("the image never reached the terminal; the client wrote:\n%q", tail(a.raw(), 400))
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
