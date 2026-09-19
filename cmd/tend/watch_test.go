package main

import (
	"bytes"
	"io"
	"os"
	"testing"
)

// TestOutputsNeverIncludesNil guards a bug that reads as the watched process
// dying the instant it starts.
//
// openCapture used to return a nil *os.File. Assigned to an io.Writer field it
// became an interface that is not nil but holds a nil pointer, so the nil check
// passed, the writer joined the MultiWriter, and its first write returned
// "invalid argument" — aborting the copy after the first chunk of output.
func TestOutputsNeverIncludesNil(t *testing.T) {
	var screen bytes.Buffer

	cases := map[string]watchOptions{
		"zero value": {},
		"mirroring":  {mirror: true},
		"explicit nil file": func() watchOptions {
			// The exact shape that caused the bug: a nil *os.File widened to
			// an interface.
			capture, err := openCapture("")
			if err != nil {
				t.Fatal(err)
			}
			return watchOptions{capture: capture}
		}(),
	}

	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			dst := opts.outputs(&screen)
			if len(dst) == 0 {
				t.Fatal("outputs returned nothing; the screen must always be written to")
			}
			for i, w := range dst {
				if w == nil {
					t.Fatalf("writer %d is nil", i)
				}
				if f, ok := w.(*os.File); ok && f == nil {
					t.Fatalf("writer %d is a nil *os.File in an interface", i)
				}
			}
			// The real guard: writing must succeed.
			if _, err := io.MultiWriter(dst...).Write([]byte("x")); err != nil {
				t.Fatalf("writing to the outputs failed: %v", err)
			}
		})
	}
}

func TestOutputsIncludesCapture(t *testing.T) {
	var screen, capture bytes.Buffer
	opts := watchOptions{capture: &capture}

	if _, err := io.MultiWriter(opts.outputs(&screen)...).Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if screen.String() != "hello" {
		t.Errorf("screen got %q", screen.String())
	}
	if capture.String() != "hello" {
		t.Errorf("capture got %q", capture.String())
	}
}

func TestOpenCaptureReturnsGenuineNil(t *testing.T) {
	w, err := openCapture("")
	if err != nil {
		t.Fatal(err)
	}
	if w != nil {
		t.Errorf("openCapture(\"\") = %#v, want a nil interface", w)
	}
}

func TestOpenCaptureWritesToFile(t *testing.T) {
	path := t.TempDir() + "/capture.raw"
	w, err := openCapture(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "data" {
		t.Errorf("file = %q, want %q", got, "data")
	}
}

// TestResolveAgentFromCommandName covers `tend watch -- claude` needing no
// flag, which is the whole point of the fallback.
func TestResolveAgentFromCommandName(t *testing.T) {
	m, err := resolveAgent("", "/usr/local/bin/claude")
	if err != nil {
		t.Fatalf("resolveAgent: %v", err)
	}
	if m.ID != "claude" {
		t.Errorf("manifest = %q, want claude", m.ID)
	}

	// An explicit agent wins over the command name.
	m, err = resolveAgent("codex", "/usr/local/bin/claude")
	if err != nil {
		t.Fatalf("resolveAgent: %v", err)
	}
	if m.ID != "codex" {
		t.Errorf("manifest = %q, want codex", m.ID)
	}
}

func TestResolveAgentErrors(t *testing.T) {
	if _, err := resolveAgent("no-such-agent", "sh"); err == nil {
		t.Error("an unknown -agent should fail")
	}
	if _, err := resolveAgent("", "/bin/sh"); err == nil {
		t.Error("a command with no matching manifest should fail")
	}
}
