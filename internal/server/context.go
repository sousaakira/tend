package server

import (
	"fmt"
	"time"

	"github.com/auth-com-br/tend/internal/capture"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/session"
	"github.com/auth-com-br/tend/internal/vt"
)

// The context buffer: what tools captured, kept here until it is sent to an
// agent (internal/capture says what an item is). It is the server's because
// the server is between every tool and every agent — a browser, the files
// panel, a script put things in over the socket, and any agent can be handed
// them — and it lives as long as the server does: it is a clipboard of
// sorts, not a record, and a restart starts it empty.

// contextMax is how many items the buffer keeps; adding past it lets the
// oldest go.
const contextMax = 50

// contextState is the buffer, under s.mu.
type contextState struct {
	items []proto.ContextItem
	next  uint64
}

// AddContext puts an item in the buffer and returns it as kept, with its id.
func (s *Server) AddContext(it proto.ContextItem) (proto.ContextItem, error) {
	if err := capture.Check(it); err != nil {
		return proto.ContextItem{}, err
	}
	s.mu.Lock()
	s.captured.next++
	it.ID = s.captured.next
	if it.Created == 0 {
		it.Created = time.Now().Unix()
	}
	s.captured.items = append(s.captured.items, it)
	if over := len(s.captured.items) - contextMax; over > 0 {
		s.captured.items = append([]proto.ContextItem(nil), s.captured.items[over:]...)
	}
	s.mu.Unlock()
	s.publish(Event{Kind: EventContextChanged})
	return it, nil
}

// ContextItems is the buffer, oldest first.
func (s *Server) ContextItems() []proto.ContextItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]proto.ContextItem{}, s.captured.items...)
}

// RemoveContext takes items out of the buffer, or all of them for none.
func (s *Server) RemoveContext(ids []uint64) {
	s.mu.Lock()
	if len(ids) == 0 {
		s.captured.items = nil
	} else {
		gone := map[uint64]bool{}
		for _, id := range ids {
			gone[id] = true
		}
		kept := s.captured.items[:0:0]
		for _, it := range s.captured.items {
			if !gone[it.ID] {
				kept = append(kept, it)
			}
		}
		s.captured.items = kept
	}
	s.mu.Unlock()
	s.publish(Event{Kind: EventContextChanged})
}

// SendContext types items (none: all) into a pane, formatted for an agent
// to read, and submits nothing: the user reads it over, adds a question,
// and sends it themselves.
func (s *Server) SendContext(pane session.PaneID, ids []uint64) error {
	return s.SendContextWith(pane, ids, "")
}

// SendContextWith is SendContext led by a message for the agent.
func (s *Server) SendContextWith(pane session.PaneID, ids []uint64, message string) error {
	items := s.ContextItems()
	if len(ids) > 0 {
		want := map[uint64]bool{}
		for _, id := range ids {
			want[id] = true
		}
		picked := items[:0:0]
		for _, it := range items {
			if want[it.ID] {
				picked = append(picked, it)
			}
		}
		items = picked
	}
	if len(items) == 0 {
		return fmt.Errorf("context: nothing to send")
	}
	return s.typeUnsubmitted(pane, capture.FormatWith(message, items))
}

// typeUnsubmitted types text captured elsewhere — a web page's, much of it —
// into a pane for the user to read over, and makes sure none of it acts.
//
// Two things could. A line break typed into a program that is not taking a
// paste is Enter: into a shell, each line of a page's text ran as a command,
// which is how this was found. And an escape in the text could end a
// bracketed paste early and let what follows act as keys. So control
// characters are dropped, and a program that has not asked for bracketed
// paste — a shell, not an agent — gets the text on one line, its breaks
// shown as ⏎ and none of them pressed.
func (s *Server) typeUnsubmitted(pane session.PaneID, text string) error {
	rt, err := s.runtime(pane)
	if err != nil {
		return err
	}
	modes := rt.modes()
	return s.writeTo(rt, vt.EncodeText(capture.Inert(text, modes.BracketedPaste), modes))
}
