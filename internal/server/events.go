package server

import (
	"sync"

	"github.com/sousaakira/tend/internal/detect"
	"github.com/sousaakira/tend/internal/session"
)

// EventKind is what happened.
type EventKind uint8

const (
	// EventPaneOpened reports a new pane and its process.
	EventPaneOpened EventKind = iota
	// EventPaneOutput reports that a pane's screen changed. It carries no
	// data: a subscriber redraws from the current screen rather than
	// replaying a stream, so a missed one costs nothing.
	EventPaneOutput
	// EventPaneState reports that a pane's record changed: the agent state
	// detected in it, or whether its program has asked for the mouse. Both
	// are things a client can only learn by re-reading the session, so one
	// event saying "look again" serves them.
	EventPaneState
	// EventPaneExited reports that a pane's process ended. The pane record
	// remains until it is closed.
	EventPaneExited
	// EventPaneClosed reports that a pane was removed from the session.
	EventPaneClosed
)

func (k EventKind) String() string {
	switch k {
	case EventPaneOpened:
		return "pane-opened"
	case EventPaneOutput:
		return "pane-output"
	case EventPaneState:
		return "pane-state"
	case EventPaneExited:
		return "pane-exited"
	case EventPaneClosed:
		return "pane-closed"
	default:
		return "unknown"
	}
}

// Event is one thing that happened to a pane.
type Event struct {
	Kind EventKind
	Pane session.PaneID

	// State and Rule are set on EventPaneState.
	State detect.State
	Rule  string

	// Err is set on EventPaneExited when the process failed.
	Err string
}

// Subscription is a stream of events.
//
// The channel is buffered and the server never blocks on it. A subscriber that
// stops reading loses events rather than stalling the runtime: a stuck client
// must not be able to freeze the agents it is watching. Dropped counts what was
// lost, so a client can tell "nothing happened" from "I fell behind" and
// resynchronise from a fresh snapshot.
type Subscription struct {
	C <-chan Event

	hub     *eventHub
	ch      chan Event
	mu      sync.Mutex
	dropped uint64
	closed  bool
}

// Dropped returns how many events this subscription has lost so far.
func (s *Subscription) Dropped() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

// Close unsubscribes. It is safe to call more than once.
func (s *Subscription) Close() {
	s.hub.remove(s)
}

type eventHub struct {
	mu   sync.Mutex
	subs map[*Subscription]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subs: make(map[*Subscription]struct{})}
}

// subscribe returns a new subscription with the given buffer.
func (h *eventHub) subscribe(buffer int) *Subscription {
	if buffer < 1 {
		buffer = 1
	}
	ch := make(chan Event, buffer)
	sub := &Subscription{C: ch, hub: h, ch: ch}

	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

func (h *eventHub) remove(sub *Subscription) {
	h.mu.Lock()
	_, present := h.subs[sub]
	delete(h.subs, sub)
	h.mu.Unlock()

	if !present {
		return
	}
	sub.mu.Lock()
	if !sub.closed {
		sub.closed = true
		close(sub.ch)
	}
	sub.mu.Unlock()
}

// publish delivers to every subscriber without blocking on any of them.
func (h *eventHub) publish(ev Event) {
	h.mu.Lock()
	subs := make([]*Subscription, 0, len(h.subs))
	for sub := range h.subs {
		subs = append(subs, sub)
	}
	h.mu.Unlock()

	for _, sub := range subs {
		sub.mu.Lock()
		if sub.closed {
			sub.mu.Unlock()
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			sub.dropped++
		}
		sub.mu.Unlock()
	}
}

// closeAll ends every subscription, which is how shutdown tells clients to
// stop waiting for events that will never come.
func (h *eventHub) closeAll() {
	h.mu.Lock()
	subs := make([]*Subscription, 0, len(h.subs))
	for sub := range h.subs {
		subs = append(subs, sub)
	}
	h.subs = make(map[*Subscription]struct{})
	h.mu.Unlock()

	for _, sub := range subs {
		sub.mu.Lock()
		if !sub.closed {
			sub.closed = true
			close(sub.ch)
		}
		sub.mu.Unlock()
	}
}
