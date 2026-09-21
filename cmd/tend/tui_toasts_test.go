package main

import (
	"testing"
	"time"

	"github.com/sousaakira/tend/internal/ui"
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
