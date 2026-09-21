package server

import "github.com/sousaakira/tend/internal/session"

// A program can ask to be told when its terminal gains or loses focus (mode
// 1004): an editor dims its cursor, an agent stops animating, a shell reloads
// what changed while you were away. tend recorded the request and never sent
// anything, so every such program believed it had the focus for ever.
//
// Focus in tend is which pane the user is looking at, which is the client's
// business; the terminal that has to be told is the server's. So the client
// says which pane it focused, and the server tells the programs — the one that
// gained it and the one that lost it — but only those that asked.

// Focus sequences, as every terminal sends them.
const (
	focusGained = "\x1b[I"
	focusLost   = "\x1b[O"
)

// FocusPane tells panes that focus moved from one to another. Either may be
// zero: nothing had it, or nothing has it now.
func (s *Server) FocusPane(gained, lost session.PaneID) {
	if lost != 0 && lost != gained {
		s.sendFocus(lost, focusLost)
	}
	if gained != 0 {
		s.sendFocus(gained, focusGained)
	}
}

// sendFocus writes a focus report to a pane that asked for one.
func (s *Server) sendFocus(id session.PaneID, sequence string) {
	rt, err := s.runtime(id)
	if err != nil {
		return
	}
	rt.mu.Lock()
	wants := rt.screen.Modes().FocusEvents
	rt.mu.Unlock()
	if !wants {
		// Sending it anyway would put an escape sequence into the input of
		// every program that never asked, which is a stray "[I" in a shell.
		return
	}
	_ = s.writeTo(rt, []byte(sequence))
}

// WantsFocusEvents reports whether a pane's program asked to be told.
func (s *Server) WantsFocusEvents(id session.PaneID) bool {
	rt, err := s.runtime(id)
	if err != nil {
		return false
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.screen.Modes().FocusEvents
}
