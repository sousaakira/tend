package server

import (
	"errors"
	"sync"
	"time"

	"github.com/sousaakira/tend/internal/agent"
	"github.com/sousaakira/tend/internal/session"
)

// Values a script reports about a space rather than a pane — a jj status, a
// deploy state — for the sidebar's space rows to show as $name: herdr's
// workspace.report_metadata. Reported and ordered like a pane's (per source,
// with a sequence, for a while or until replaced), so the same arbiter does
// the bookkeeping, one per space, holding only metadata.
//
// Not persisted: a value reported about a space is true while its reporter
// is running, and a restart is a fresh start for both.

type workspaceMeta struct {
	mu      sync.Mutex
	spaces  map[session.WorkspaceID]*agent.Arbiter
	expires map[session.WorkspaceID]*time.Timer
}

// ReportWorkspaceMetadata records values about a space and tells clients
// when something changed. It reports whether anything did.
func (s *Server) ReportWorkspaceMetadata(id session.WorkspaceID, r agent.MetadataReport) (bool, error) {
	s.mu.Lock()
	_, ok := s.session.Workspace(id)
	s.mu.Unlock()
	if !ok {
		return false, session.ErrNoSuchWorkspace
	}
	if r.Source == "" {
		return false, errors.New("server: a metadata report needs a source")
	}
	m := &s.workspaceMeta
	m.mu.Lock()
	if m.spaces == nil {
		m.spaces = map[session.WorkspaceID]*agent.Arbiter{}
		m.expires = map[session.WorkspaceID]*time.Timer{}
	}
	a := m.spaces[id]
	if a == nil {
		a = agent.NewArbiter(nil)
		m.spaces[id] = a
	}
	changed := a.ReportMetadata(r, time.Now())
	if changed && r.TTL > 0 {
		// A value said for a while disappears when the while is over,
		// whether or not anything else happens to redraw the sidebar.
		if t := m.expires[id]; t != nil {
			t.Stop()
		}
		m.expires[id] = time.AfterFunc(r.TTL+10*time.Millisecond, func() {
			m.mu.Lock()
			gone := a.ExpireMetadata(time.Now())
			m.mu.Unlock()
			if gone {
				s.publish(Event{Kind: EventSessionChanged})
			}
		})
	}
	m.mu.Unlock()
	if changed {
		s.publish(Event{Kind: EventSessionChanged})
	}
	return changed, nil
}

// workspaceTokens are the values reported about a space, in key order.
func (s *Server) workspaceTokens(id session.WorkspaceID) []agent.Token {
	m := &s.workspaceMeta
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.spaces[id]
	if a == nil {
		return nil
	}
	return a.Presentation(time.Now()).Tokens
}
