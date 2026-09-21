package agent

import (
	"testing"

	"github.com/sousaakira/tend/internal/detect"
	"github.com/sousaakira/tend/internal/vt"
)

func screenWith(t *testing.T, cols, rows int, input string) *vt.Screen {
	t.Helper()
	s := vt.NewScreen(cols, rows, 100)
	if _, err := s.Write([]byte(input)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return s
}

// TestScreenTextTrimsTrailingBlankRows: a terminal is mostly empty below the
// cursor. Leaving those rows in would push every line-counted region past the
// content it was written to find.
func TestScreenTextTrimsTrailingBlankRows(t *testing.T) {
	s := screenWith(t, 20, 10, "first\r\nsecond\r\n")
	if got, want := ScreenText(s), "first\nsecond"; got != want {
		t.Errorf("ScreenText = %q, want %q", got, want)
	}
}

// TestScreenTextKeepsInteriorBlankRows: blank lines inside the output are
// content — a rule may match on the shape of a block.
func TestScreenTextKeepsInteriorBlankRows(t *testing.T) {
	s := screenWith(t, 20, 10, "a\r\n\r\nb\r\n")
	if got, want := ScreenText(s), "a\n\nb"; got != want {
		t.Errorf("ScreenText = %q, want %q", got, want)
	}
}

func TestScreenTextOnEmptyScreen(t *testing.T) {
	s := screenWith(t, 20, 5, "")
	if got := ScreenText(s); got != "" {
		t.Errorf("ScreenText = %q, want empty", got)
	}
}

func TestSnapshotCarriesTitleAndProgress(t *testing.T) {
	s := screenWith(t, 20, 5, "work\r\n\x1b]0;my-title\x07\x1b]9;4;1;-1\x1b\\")
	in := Snapshot(s)

	if in.Screen != "work" {
		t.Errorf("Screen = %q, want %q", in.Screen, "work")
	}
	if in.OSCTitle != "my-title" {
		t.Errorf("OSCTitle = %q", in.OSCTitle)
	}
	// Progress drops its command number: rules match "4;1;-1", not "9;4;1;-1".
	if in.OSCProgress != "4;1;-1" {
		t.Errorf("OSCProgress = %q, want %q", in.OSCProgress, "4;1;-1")
	}
}

// TestSnapshotReadsTheAlternateScreen: a full-screen agent draws there, and
// detection must follow it rather than reading the main screen underneath.
func TestSnapshotReadsTheAlternateScreen(t *testing.T) {
	s := screenWith(t, 20, 5, "main text\r\n\x1b[?1049h\x1b[Halt text")
	if got := ScreenText(s); got != "alt text" {
		t.Errorf("ScreenText = %q, want the alternate screen", got)
	}
}

func testManifest(t *testing.T, src string) *detect.Manifest {
	t.Helper()
	m, err := detect.ParseManifest([]byte(src))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	return m
}

func TestDetectorReportsOnlyChanges(t *testing.T) {
	m := testManifest(t, `
id = "demo"
[[rules]]
id = "busy"
state = "working"
priority = 100
contains = ["busy"]
[[rules]]
id = "done"
state = "idle"
priority = 100
contains = ["done"]
`)
	d := NewDetector(m)
	if d.Agent() != "demo" {
		t.Errorf("Agent = %q", d.Agent())
	}

	s := screenWith(t, 20, 5, "busy")
	if res, changed := d.Update(s); !changed || res.State != detect.StateWorking {
		t.Fatalf("first update: %+v changed=%v, want working", res, changed)
	}
	// The same screen again is not a change.
	if _, changed := d.Update(s); changed {
		t.Error("an unchanged state should not be reported again")
	}

	s2 := screenWith(t, 20, 5, "done")
	if res, changed := d.Update(s2); !changed || res.State != detect.StateIdle {
		t.Fatalf("second update: %+v changed=%v, want idle", res, changed)
	}
	if d.State() != detect.StateIdle || d.Rule() != "done" {
		t.Errorf("state = %v rule = %q", d.State(), d.Rule())
	}
}

// TestDetectorReportsTheFirstObservation: the first observation is a change
// even when no rule matched, so a caller learns the starting state rather
// than assuming. With no rule matching, a known agent is idle, as in herdr.
func TestDetectorReportsTheFirstObservation(t *testing.T) {
	m := testManifest(t, `
id = "demo"
[[rules]]
id = "busy"
state = "working"
contains = ["busy"]
`)
	d := NewDetector(m)
	res, changed := d.Update(screenWith(t, 20, 5, "nothing here"))
	if !changed {
		t.Error("the first observation should be reported")
	}
	if res.State != detect.StateIdle {
		t.Errorf("state = %v, want idle", res.State)
	}
}

// TestDetectorHonoursSkipStateUpdate: an ambiguous screen must leave the
// previous conclusion standing rather than replace it with a guess.
func TestDetectorHonoursSkipStateUpdate(t *testing.T) {
	m := testManifest(t, `
id = "demo"
[[rules]]
id = "busy"
state = "working"
priority = 100
contains = ["busy"]
[[rules]]
id = "ambiguous"
state = "unknown"
priority = 200
skip_state_update = true
contains = ["scrollback"]
`)
	d := NewDetector(m)

	if _, changed := d.Update(screenWith(t, 20, 5, "busy")); !changed {
		t.Fatal("expected the working state")
	}
	if d.State() != detect.StateWorking {
		t.Fatalf("state = %v, want working", d.State())
	}

	res, changed := d.Update(screenWith(t, 20, 5, "scrollback view"))
	if changed {
		t.Error("a skip_state_update rule must not report a change")
	}
	if !res.SkipStateUpdate {
		t.Error("the result should still carry the flag")
	}
	if d.State() != detect.StateWorking {
		t.Errorf("state = %v, want the previous working state to stand", d.State())
	}
}

func TestDetectorWithoutManifest(t *testing.T) {
	d := NewDetector(nil)
	if d.Agent() != "" {
		t.Errorf("Agent = %q, want empty", d.Agent())
	}
	if _, changed := d.Update(screenWith(t, 10, 3, "x")); changed {
		t.Error("a detector with no manifest should report nothing")
	}
}
