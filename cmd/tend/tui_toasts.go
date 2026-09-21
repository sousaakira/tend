package main

import (
	"time"

	"github.com/sousaakira/tend/internal/ui"
)

// Notifications on tend's own screen are herdr's cards
// (`client/shell/notification_policy.rs`): one shown at a time, for as long
// as its kind deserves, the rest waiting in a short queue; a newer word
// about a pane replaces an older one still waiting; a click on the card
// goes to its pane.

// maxQueuedToasts is herdr's MAX_QUEUED_NOTIFICATIONS: past it the oldest
// waiting is dropped, since a backlog of stale news helps nobody.
const maxQueuedToasts = 8

// toastDuration is herdr's notification_duration.
func toastDuration(kind string) time.Duration {
	switch kind {
	case ui.ToastAttention:
		return 8 * time.Second
	default:
		return 5 * time.Second
	}
}

type toastEntry struct {
	toast    ui.Toast
	pane     uint64
	machine  string
	deadline time.Time
}

// pushToast shows a card about the machine shown, or queues it.
func (t *tui) pushToast(kind, title, body string, pane uint64) {
	t.mu.Lock()
	machine := t.shownMachineLocked()
	t.mu.Unlock()
	t.pushToastOn(machine, kind, title, body, pane)
}

// pushToastOn shows a card, or queues it behind the one shown. A pane is
// known by its machine and its number together: two machines each have a
// pane 1.
func (t *tui) pushToastOn(machine, kind, title, body string, pane uint64) {
	t.mu.Lock()
	defer func() {
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}()
	now := time.Now()
	if pane != 0 {
		kept := t.toastQueue[:0]
		for _, q := range t.toastQueue {
			if q.pane != pane || q.machine != machine {
				kept = append(kept, q)
			}
		}
		t.toastQueue = kept
		if t.toast != nil && t.toast.pane == pane && t.toast.machine == machine {
			t.toast = nil
		}
	}
	entry := toastEntry{toast: ui.Toast{Kind: kind, Title: title, Body: body}, pane: pane, machine: machine}
	if t.toast == nil {
		t.promoteToastLocked(now)
	}
	if t.toast == nil {
		entry.deadline = now.Add(toastDuration(kind))
		t.toast = &entry
		return
	}
	if len(t.toastQueue) == maxQueuedToasts {
		t.toastQueue = t.toastQueue[1:]
	}
	t.toastQueue = append(t.toastQueue, entry)
}

// promoteToastLocked shows the next waiting card, if any.
func (t *tui) promoteToastLocked(now time.Time) {
	t.toast = nil
	if len(t.toastQueue) == 0 {
		return
	}
	next := t.toastQueue[0]
	t.toastQueue = t.toastQueue[1:]
	next.deadline = now.Add(toastDuration(next.toast.Kind))
	t.toast = &next
}

// expireToasts retires the card shown once its time is up.
func (t *tui) expireToasts() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now := time.Now(); t.toast != nil && now.After(t.toast.deadline) {
		t.promoteToastLocked(now)
		t.dirty = true
	}
}

// toastFrameLocked is the card as the frame draws it.
func (t *tui) toastFrameLocked() *ui.Toast {
	if t.toast == nil {
		return nil
	}
	card := t.toast.toast
	card.Position = t.config.Notify.Position
	return &card
}

// clickToast answers a press on the card: it goes to the pane the card is
// about, and the card goes. It reports whether the press was on one.
func (t *tui) clickToast(x, y int) (bool, error) {
	t.mu.Lock()
	card := t.toastFrameLocked()
	if card == nil {
		t.mu.Unlock()
		return false, nil
	}
	r := ui.ToastRect(*card, t.cols, t.rows)
	if x < r.X || x >= r.X+r.Cols || y < r.Y || y >= r.Y+r.Rows {
		t.mu.Unlock()
		return false, nil
	}
	pane, machine := t.toast.pane, t.toast.machine
	t.promoteToastLocked(time.Now())
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	return true, t.goToNotice(machine, pane)
}

// goToNotice shows the pane a notice was about, on whichever machine it is.
func (t *tui) goToNotice(machine string, pane uint64) error {
	if pane == 0 {
		return nil
	}
	t.mu.Lock()
	shown := t.shownMachineLocked()
	t.mu.Unlock()
	if machine != shown {
		return t.switchMachine(machine, 0, pane)
	}
	return t.jumpToPane(pane)
}
