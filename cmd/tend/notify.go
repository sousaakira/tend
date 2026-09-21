package main

import (
	"io"
	"os"
	"time"

	"github.com/sousaakira/tend/internal/detect"
	"github.com/sousaakira/tend/internal/notify"
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/worktree"
)

// Being told that an agent needs you, which is the reason this program exists.
// The count on the status bar only reaches somebody already looking at tend;
// this reaches somebody reading something else.
//
// What is worth saying is herdr's rule (`app/actions.rs`,
// `notification_toast_for_state_change`): an agent that became blocked needs
// attention, and an agent that was working and went idle has finished.
// Anything else is a state change nobody asked to hear about. The pane being
// looked at says nothing, because being told what is on screen is noise.

// notifyCooldown is how long one pane is left alone after it was announced.
// An agent that flickers between states — and they do, while a tool runs —
// would otherwise raise a notification a second.
const notifyCooldown = 10 * time.Second

// paneNotice is what was last said about a pane.
type paneNotice struct {
	state detect.State
	at    time.Time
}

// announce decides whether a state change is worth telling the user about, and
// tells them. It runs on the event goroutine, so it does no round trips.
func (t *tui) announce(ev proto.Event) {
	if t.toasts == "off" && !t.sound.Enabled {
		return
	}
	state := detect.StateFromString(ev.State)
	if state != detect.StateBlocked && state != detect.StateIdle {
		return
	}

	t.mu.Lock()
	previous := t.notices[ev.Pane]
	// The pane in view is not announced — unless the window is behind
	// something else, when nobody is looking at it either (herdr's
	// active_tab_suppresses_notifications).
	focused := ev.Pane == t.focus && t.windowFocused
	kind, worth := worthAnnouncing(previous, state, time.Now())
	t.notices[ev.Pane] = paneNotice{state: state, at: time.Now()}

	var agent, where string
	for _, p := range t.snap.Panes {
		if p.ID == ev.Pane {
			agent = p.Agent
		}
	}
	if agent == "" {
		// The event is ahead of the session this client last read: an agent
		// whose first word is "I need you" would otherwise go unannounced.
		agent = ev.Agent
	}
	for _, w := range t.snap.Workspaces {
		for _, tab := range w.Tabs {
			for _, id := range tab.Panes {
				if id == ev.Pane {
					where = agentLabel(w, tab)
				}
			}
		}
	}
	notifyFocused := t.notifyFocused
	t.mu.Unlock()

	if agent == "" || !worth || (focused && !notifyFocused) {
		return
	}
	t.mu.Lock()
	t.lastNotice = ev.Pane // for open-notification
	t.mu.Unlock()
	switch kind {
	case announceBlocked:
		t.raise(agent+" needs attention", where, t.soundFor(agent, notify.SoundRequest))
	case announceFinished:
		t.raise(agent+" finished", where, t.soundFor(agent, notify.SoundDone))
	}
}

// What an announcement is about.
const (
	announceNothing = iota
	announceBlocked
	announceFinished
)

// worthAnnouncing is herdr's rule, and the cooldown tend adds to it: an agent
// that became blocked needs attention; one that was working and went idle has
// finished; anything else is a state change nobody asked to hear about.
//
// The cooldown is not herdr's. An agent flickers between working and idle
// while a tool runs — the screen says one thing between two redraws — and
// without it the same agent finishes a dozen times a minute.
func worthAnnouncing(previous paneNotice, state detect.State, now time.Time) (kind int, ok bool) {
	switch {
	case state == detect.StateBlocked && previous.state != detect.StateBlocked:
		kind = announceBlocked
	case state == detect.StateIdle && previous.state == detect.StateWorking:
		kind = announceFinished
	default:
		return announceNothing, false
	}
	if !previous.at.IsZero() && now.Sub(previous.at) < notifyCooldown && previous.state == state {
		return kind, false
	}
	return kind, true
}

// raise says it, by whichever means are turned on.
func (t *tui) raise(title, body string, sound notify.Sound) {
	message := title
	if body != "" {
		message += ": " + body
	}
	if t.toasts != "off" {
		t.setMessage(message, false)
	}
	if t.toasts == "system" {
		// On its own goroutine: raising one runs another program, and the
		// event goroutine is the one delivering pane output.
		go func() {
			if err := notify.System(title, body); err != nil {
				t.setMessage("notification: "+err.Error(), true)
			}
		}()
	}
	if t.toasts == "terminal" && t.notifier.Available() {
		if seq := t.notifier.Sequence(title, body); len(seq) > 0 {
			// Straight to the terminal rather than through the frame: it is
			// not drawing, it is asking the terminal for something, and the
			// next redraw must not repeat it.
			_, _ = os.Stdout.Write(seq)
		}
	}
	t.sound.Play(sound)
}

// soundFor is the sound an agent's change makes, or none when the settings
// mute that agent ([sound.agents], herdr's per-agent override).
func (t *tui) soundFor(agent string, sound notify.Sound) notify.Sound {
	t.mu.Lock()
	allowed := t.config.Sound.AllowsAgent(agent)
	t.mu.Unlock()
	if !allowed {
		return notify.SoundNone
	}
	return sound
}

// expandHome turns a leading ~ into the home directory, so a sound can be
// named the way it is written down.
func expandHome(path string) string { return worktree.ExpandHome(path) }

// bell rings the terminal's bell, which is the sound tend has when no file is
// configured.
func bell() { _, _ = io.WriteString(os.Stdout, "\a") }
