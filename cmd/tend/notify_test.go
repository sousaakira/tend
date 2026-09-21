package main

import (
	"strings"

	"github.com/sousaakira/tend/internal/detect"
	"testing"
	"time"
)

// TestAnAgentThatStopsIsAnnounced is why tend exists: the agent that needs you
// is usually not the one on screen. If it regresses, the only sign is a number
// on the status bar that nobody is looking at.
func TestAnAgentThatStopsIsAnnounced(t *testing.T) {
	a := startSession(t, 100, 16)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// A second pane, so the agent is not the one focused: what is on screen is
	// deliberately not announced.
	prog := fakeAgentBin(t, "claude", `printf 'esc to interrupt\n'; sleep 1; printf 'Do you want to proceed?\n  1. Yes\n'; cat`)
	a.sendUntil(t, prog+"\n", "the agent working", func(s string) bool {
		return strings.Contains(s, "esc to interrupt")
	})
	a.send(t, "\x02|")
	a.waitForScreen(t, "the second pane", func(s string) bool { return strings.Count(s, "┌") == 2 })

	a.waitForScreen(t, "the notice that it needs answering", func(s string) bool {
		return strings.Contains(s, "claude needs attention")
	})
}

// TestOnlyStoppingIsWorthSaying covers herdr's rule and the cooldown tend
// adds: an agent flickers between working and idle while a tool runs, and a
// notification a second is worse than none.
func TestOnlyStoppingIsWorthSaying(t *testing.T) {
	t0 := time.Now()
	for _, c := range []struct {
		name     string
		previous paneNotice
		state    detect.State
		at       time.Time
		kind     int
		want     bool
	}{
		{"blocked from working", paneNotice{state: detect.StateWorking, at: t0}, detect.StateBlocked, t0, announceBlocked, true},
		{"finished", paneNotice{state: detect.StateWorking, at: t0}, detect.StateIdle, t0, announceFinished, true},
		{"idle that was never working", paneNotice{state: detect.StateIdle, at: t0}, detect.StateIdle, t0, announceNothing, false},
		{"still working", paneNotice{state: detect.StateWorking, at: t0}, detect.StateWorking, t0, announceNothing, false},
		{"blocked again, too soon", paneNotice{state: detect.StateBlocked, at: t0}, detect.StateBlocked, t0.Add(time.Second), announceNothing, false},
		{"blocked again, much later", paneNotice{state: detect.StateWorking, at: t0}, detect.StateBlocked, t0.Add(time.Hour), announceBlocked, true},
	} {
		kind, ok := worthAnnouncing(c.previous, c.state, c.at)
		if ok != c.want || (c.want && kind != c.kind) {
			t.Errorf("%s: worthAnnouncing = %d, %v; want %d, %v", c.name, kind, ok, c.kind, c.want)
		}
	}
}
