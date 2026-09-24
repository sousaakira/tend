package server

import (
	"fmt"
	"time"

	"github.com/sousaakira/tend/internal/capture"
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/session"
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
	return s.SendText(pane, capture.Format(items))
}
