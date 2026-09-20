package server

import (
	"errors"
	"time"

	"github.com/sousaakira/tend/internal/agent"
	"github.com/sousaakira/tend/internal/session"
)

// ErrBadAgent means a report named no agent.
var ErrBadAgent = errors.New("server: a report must name its agent")

// knownAgent reports whether a label is an agent detection can recognise,
// which is what lets a report disagree with the screen.
func (s *Server) knownAgent(label string) bool {
	_, ok := s.catalog.Lookup(label)
	return ok
}

// ReportAgent takes a hook's account of what an agent is doing. It reports
// whether the account was believed: one that is out of order, or about an agent
// other than the one in the pane, is dropped without being an error — the hook
// did nothing wrong, it was only late or in the wrong place.
func (s *Server) ReportAgent(id session.PaneID, r agent.Report) (bool, error) {
	if r.Agent = agent.NormalizeLabel(r.Agent); r.Agent == "" {
		return false, ErrBadAgent
	}
	now := time.Now()
	return s.hooked(id, func(a *agent.Arbiter) bool { return a.Report(r, now) })
}

// ReportAgentSession takes a hook's word on which conversation an agent is in.
func (s *Server) ReportAgentSession(id session.PaneID, source, label string, ref agent.SessionRef, seq *uint64) (bool, error) {
	if label = agent.NormalizeLabel(label); label == "" {
		return false, ErrBadAgent
	}
	return s.hooked(id, func(a *agent.Arbiter) bool { return a.ReportSession(source, label, ref, seq) })
}

// ReleaseAgent is a hook saying its agent has left the pane.
func (s *Server) ReleaseAgent(id session.PaneID, source, label string, seq *uint64) (bool, error) {
	if label = agent.NormalizeLabel(label); label == "" {
		return false, ErrBadAgent
	}
	return s.hooked(id, func(a *agent.Arbiter) bool {
		released, _ := a.Release(source, label, seq)
		return released
	})
}

// ClearAgentAuthority drops whatever a hook last said, leaving the screen to
// speak for the pane. An empty source clears whoever holds it.
func (s *Server) ClearAgentAuthority(id session.PaneID, source string, seq *uint64) (bool, error) {
	return s.hooked(id, func(a *agent.Arbiter) bool { return a.Clear(source, seq) })
}

// AgentSession is the conversation known for a pane, if a hook has named one.
func (s *Server) AgentSession(id session.PaneID) (agent.PersistedSession, bool, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return agent.PersistedSession{}, false, err
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	p, ok := rt.arbiter.Session()
	return p, ok, nil
}

// hooked applies fn to a pane's arbiter and passes on whatever that changed.
func (s *Server) hooked(id session.PaneID, fn func(*agent.Arbiter) bool) (bool, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return false, err
	}
	accepted, eff, rule, changed := rt.hooked(fn)
	if !changed {
		return accepted, nil
	}

	s.mu.Lock()
	if _, live := s.runtimes[id]; live {
		_ = s.session.SetPaneState(id, eff.Agent, eff.State)
	}
	s.mu.Unlock()
	s.events.publish(Event{Kind: EventPaneState, Pane: id, State: eff.State, Rule: rule})
	return accepted, nil
}
