package main

import (
	"strings"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// TestToastsQueueAndReplaceAsHerdrs: one card at a time; the rest wait, at
// most eight, the oldest dropped; newer news of a pane replaces what was
// waiting or shown about it; a card needing attention lasts longer. If it
// regresses, a burst of agents finishing shows only the last, or a stale
// "needs attention" sits on screen after the agent was answered.
func TestToastsQueueAndReplaceAsHerdrs(t *testing.T) {
	tu := &tui{}
	tu.pushToast(ui.ToastAttention, "a needs attention", "", 1)
	tu.pushToast(ui.ToastFinished, "b finished", "", 2)
	if tu.toast == nil || tu.toast.toast.Title != "a needs attention" || len(tu.toastQueue) != 1 {
		t.Fatalf("shown %+v, queued %d", tu.toast, len(tu.toastQueue))
	}
	if d := time.Until(tu.toast.deadline); d < 7*time.Second || d > 8*time.Second {
		t.Errorf("attention lasts 8s, got %v", d)
	}
	// Pane 1 again: the card shown about it goes, the next is promoted, and
	// the new one waits behind it.
	tu.pushToast(ui.ToastFinished, "a finished", "", 1)
	if tu.toast.toast.Title != "b finished" || len(tu.toastQueue) != 1 || tu.toastQueue[0].toast.Title != "a finished" {
		t.Errorf("shown %q, queued %+v", tu.toast.toast.Title, tu.toastQueue)
	}
	for i := 0; i < 10; i++ {
		tu.pushToast(ui.ToastCustom, "say", "", 0)
	}
	if len(tu.toastQueue) != maxQueuedToasts {
		t.Errorf("queue holds %d, want %d", len(tu.toastQueue), maxQueuedToasts)
	}
	tu.toast.deadline = time.Now().Add(-time.Second)
	tu.expireToasts()
	if tu.toast == nil || tu.toast.toast.Title == "b finished" {
		t.Errorf("an expired card should give way to the next: %+v", tu.toast)
	}
}

// TestNewsIsSaidOnlyIfStillTrueWhenItsMomentComes is herdr's notification
// policy: an agent's news is held for the delay, then said only if its pane
// is still blocked (for attention) or done (for finished); a finished pane
// still showing working is looked at again within a second; a script's
// word goes at once. If it regresses, an agent answered a moment after it
// asked still pops up a "needs attention" card.
func TestNewsIsSaidOnlyIfStillTrueWhenItsMomentComes(t *testing.T) {
	tu := &tui{toasts: "tend", notifyDelay: 30 * time.Millisecond}
	tu.snap.Panes = []proto.PaneInfo{{ID: 1, State: "blocked"}, {ID: 2, State: "blocked"}, {ID: 3, State: "working"}}

	tu.raise(ui.ToastAttention, "one needs attention", "", 1, 0)
	tu.raise(ui.ToastAttention, "two needs attention", "", 2, 0)
	tu.raise(ui.ToastFinished, "three finished", "", 3, 0)
	if tu.toast != nil {
		t.Fatal("an agent's news was said before its delay")
	}
	tu.raise(ui.ToastCustom, "the build finished", "", 0, 0)
	if tu.toast == nil || tu.toast.toast.Title != "the build finished" {
		t.Fatalf("a script's word goes at once: %+v", tu.toast)
	}

	// Two was answered in the meantime; three finished for real.
	tu.mu.Lock()
	tu.snap.Panes[1].State = "working"
	tu.snap.Panes[2].State, tu.snap.Panes[2].Done = "idle", true
	tu.mu.Unlock()
	time.Sleep(40 * time.Millisecond)
	tu.deliverDue()

	var said []string
	if tu.toast != nil {
		said = append(said, tu.toast.toast.Title)
	}
	for _, q := range tu.toastQueue {
		said = append(said, q.toast.Title)
	}
	got := strings.Join(said, "|")
	if got != "the build finished|one needs attention|three finished" {
		t.Errorf("said %q", got)
	}
	if len(tu.pendingNotices) != 0 {
		t.Errorf("%d notices still held", len(tu.pendingNotices))
	}
}
