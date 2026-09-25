package server

import (
	"errors"
	"fmt"

	"github.com/auth-com-br/tend/internal/session"
)

// Agent names: herdr's agent.rename and agent.start's name
// (`app/agents.rs`). Two panes running claude are both "claude"; a name is
// how a script says which one — `tend agent prompt reviewer "…"` — and it is
// kept with the pane, across a restart and a handoff, since the session's
// snapshot carries it.

// ErrBadAgentName and its siblings are what naming can refuse.
var (
	ErrBadAgentName   = errors.New("server: an agent name is a lowercase letter, then up to 31 lowercase letters, digits, - or _")
	ErrDuplicateName  = errors.New("server: another agent already has that name")
	ErrNotAnAgentPane = errors.New("server: no agent is running in that pane")
)

// ValidAgentName is herdr's rule for a name.
func ValidAgentName(name string) bool {
	if name == "" || len(name) > 32 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, r := range name[1:] {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !ok {
			return false
		}
	}
	return true
}

// RenameAgent names the agent in a pane, or with "" takes its name away.
// Only a pane with an agent in it can be renamed, as in herdr; NameAgent is
// for a pane about to start one.
func (s *Server) RenameAgent(id session.PaneID, name string) error {
	return s.nameAgent(id, name, true)
}

// NameAgent names a pane's agent before it has been detected, for
// agent.start, which names what it is about to launch.
func (s *Server) NameAgent(id session.PaneID, name string) error {
	return s.nameAgent(id, name, false)
}

func (s *Server) nameAgent(id session.PaneID, name string, mustBeAgent bool) error {
	if name != "" && !ValidAgentName(name) {
		return ErrBadAgentName
	}
	s.mu.Lock()
	p, ok := s.session.Pane(id)
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("%w: %d", session.ErrNoSuchPane, id)
	}
	if mustBeAgent && p.Agent == "" {
		s.mu.Unlock()
		return ErrNotAnAgentPane
	}
	if name != "" {
		for _, w := range s.session.Workspaces() {
			for _, t := range w.Tabs() {
				for _, other := range t.Panes() {
					if q, ok := t.Pane(other); ok && other != id && q.AgentName == name {
						s.mu.Unlock()
						return fmt.Errorf("%w: pane %d is %q", ErrDuplicateName, other, name)
					}
				}
			}
		}
	}
	p.AgentName = name
	s.mu.Unlock()
	s.publish(Event{Kind: EventSessionChanged})
	return nil
}

// agentNamed is the pane whose agent has a name, if one does.
func (s *Server) agentNamed(name string) (session.PaneID, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.session.Workspaces() {
		for _, t := range w.Tabs() {
			for _, id := range t.Panes() {
				if p, ok := t.Pane(id); ok && p.AgentName != "" && p.AgentName == name {
					return id, true
				}
			}
		}
	}
	return 0, false
}

// AgentName is what a pane's agent is called, or "".
func (s *Server) AgentName(id session.PaneID) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.session.Pane(id); ok {
		return p.AgentName
	}
	return ""
}
