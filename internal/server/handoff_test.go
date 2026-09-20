package server

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/session"
)

// echoer is a program that proves who it is: it prints its pid when it starts
// and again with every line it is sent. The same pid after a handoff is the
// same process, which a restored pane — a new shell — could never show.
const echoer = `echo "born:$$"; while read line; do echo "said:$line:$$"; done`

var bornPid = regexp.MustCompile(`born:(\d+)`)

func bornAs(t *testing.T, s *Server, pane session.PaneID) string {
	t.Helper()
	var pid string
	waitFor(t, "the program to say its pid", func() bool {
		text, _ := s.ScreenText(pane)
		if m := bornPid.FindStringSubmatch(text); m != nil {
			pid = m[1]
			return true
		}
		return false
	})
	return pid
}

func handoffConfig() Config {
	return Config{
		DetectInterval: 10 * time.Millisecond,
		AdoptInterval:  20 * time.Millisecond,
		DefaultSize:    pty.Size{Cols: 80, Rows: 24},
	}
}

// TestHandoffKeepsTheProgramRunning is the reason the feature exists. If it
// regresses, replacing the server goes back to costing the user every agent
// that was mid-task.
func TestHandoffKeepsTheProgramRunning(t *testing.T) {
	old := newServer(t)
	_, pane := openTab(t, old, echoer)
	pid := bornAs(t, old, pane)

	h, err := old.BeginHandoff()
	if err != nil {
		t.Fatalf("BeginHandoff: %v", err)
	}
	if len(h.Files) != 1 || len(h.Manifest.Panes) != 1 {
		t.Fatalf("handed over %d terminals for %d panes, want 1 and 1", len(h.Files), len(h.Manifest.Panes))
	}
	files := h.Files
	h.Files = nil // the replacement's now

	acked := false
	next, err := NewFromHandoff(handoffConfig(), h.Manifest, files, func() error {
		acked = true
		return nil
	})
	if err != nil {
		t.Fatalf("NewFromHandoff: %v", err)
	}
	t.Cleanup(func() { _ = next.Close() })
	if !acked {
		t.Fatal("the replacement never said it was ready")
	}
	if err := old.CommitHandoff(h); err != nil {
		t.Fatalf("CommitHandoff: %v", err)
	}

	// What was on the screen came across.
	if text, _ := next.ScreenText(pane); !strings.Contains(text, "born:"+pid) {
		t.Errorf("the replacement's screen is %q, want what the old one showed", text)
	}

	// And the process behind it is the one that was there before.
	if err := next.Write(pane, []byte("hello\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitFor(t, "the same process to answer", func() bool {
		text, _ := next.ScreenText(pane)
		return strings.Contains(text, "said:hello:"+pid)
	})
}

// TestAbortedHandoffLosesNothing covers the replacement failing to start. If
// this regresses, a failed upgrade eats whatever the panes printed while it
// was being tried, or leaves them frozen.
func TestAbortedHandoffLosesNothing(t *testing.T) {
	s := newServer(t)
	_, pane := openTab(t, s, echoer)
	pid := bornAs(t, s, pane)

	h, err := s.BeginHandoff()
	if err != nil {
		t.Fatalf("BeginHandoff: %v", err)
	}
	// Sent while nobody is reading. The answer sits in the kernel.
	if err := s.Write(pane, []byte("during\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, _, err := s.NewTab(1, "late", shell("sleep 5")); !errors.Is(err, ErrHandingOff) {
		t.Errorf("opening a pane during a handoff: err = %v, want ErrHandingOff", err)
	}
	s.AbortHandoff(h)

	waitFor(t, "what was said during the attempt", func() bool {
		text, _ := s.ScreenText(pane)
		return strings.Contains(text, "said:during:"+pid)
	})

	// A second attempt must work: the first left nothing behind.
	h, err = s.BeginHandoff()
	if err != nil {
		t.Fatalf("second BeginHandoff: %v", err)
	}
	s.AbortHandoff(h)
	if err := s.Write(pane, []byte("after\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitFor(t, "the pane to carry on", func() bool {
		text, _ := s.ScreenText(pane)
		return strings.Contains(text, "said:after:"+pid)
	})
}

// TestHandoffWithoutAReplacementIsRefused: a server that was never told how to
// start another must say so, not stop its readers and sit there.
func TestHandoffWithoutAReplacementIsRefused(t *testing.T) {
	s := newServer(t)
	if _, err := s.Replace(); !errors.Is(err, ErrHandoffUnavailable) {
		t.Errorf("err = %v, want ErrHandoffUnavailable", err)
	}
}
