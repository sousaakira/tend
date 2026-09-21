package server

import (
	"github.com/sousaakira/tend/internal/detect"
	"github.com/sousaakira/tend/internal/session"
)

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

// FocusPane tells panes that focus moved from one to another, and everything
// listening that it did. Either may be zero: nothing had it, or nothing has it
// now.
//
// The tab and the space it is in are announced as well, when they changed,
// because that is what a plugin decorating a tab is waiting for — and only
// when they changed, or every keystroke that moves focus inside one tab would
// announce the tab again.
func (s *Server) FocusPane(gained, lost session.PaneID) {
	if lost != 0 && lost != gained {
		s.sendFocus(lost, focusLost)
	}
	if gained == 0 {
		return
	}
	s.sendFocus(gained, focusGained)

	tab, workspace := s.placeOf(gained)
	s.mu.Lock()
	tabChanged := tab != 0 && tab != s.focusedTab
	wsChanged := workspace != 0 && workspace != s.focusedWorkspace
	paneChanged := gained != s.focusedPane
	s.focusedPane, s.focusedTab, s.focusedWorkspace = gained, tab, workspace
	// Looking at a tab is seeing what finished in it (herdr's switch_tab).
	seen := tab != 0 && s.session.MarkTabSeen(tab)
	s.mu.Unlock()

	if seen {
		s.publish(Event{Kind: EventPaneState, Pane: gained})
	}

	if wsChanged {
		s.publish(Event{Kind: EventWorkspaceFocused, Workspace: workspace})
	}
	if tabChanged {
		s.publish(Event{Kind: EventTabFocused, Tab: tab, Workspace: workspace})
	}
	if paneChanged {
		s.publish(Event{Kind: EventPaneFocused, Pane: gained, Tab: tab, Workspace: workspace})
	}
}

// placeOf is the tab and space a pane is in.
func (s *Server) placeOf(id session.PaneID) (session.TabID, session.WorkspaceID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.session.Workspaces() {
		for _, t := range w.Tabs() {
			if _, ok := t.Pane(id); ok {
				return t.ID, w.ID
			}
		}
	}
	return 0, 0
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

// WindowFocus records whether the client's terminal window has focus. While
// it does not, nothing is being looked at — an agent that finishes in the
// tab on screen finished unseen — and when it comes back, the tab on screen
// has been seen, as herdr acknowledges the active surface on focus.
func (s *Server) WindowFocus(focused bool) {
	s.mu.Lock()
	s.windowUnfocused = !focused
	seen := focused && s.focusedTab != 0 && s.session.MarkTabSeen(s.focusedTab)
	pane := s.focusedPane
	s.mu.Unlock()
	if seen {
		s.publish(Event{Kind: EventPaneState, Pane: pane})
	}
}

// watchedTabLocked is the tab somebody is looking at, or none while the
// window is behind something. The caller holds the lock.
func (s *Server) watchedTabLocked() session.TabID {
	if s.windowUnfocused {
		return 0
	}
	return s.focusedTab
}

// setPaneStateLocked records a pane's agent and state and reports whether an
// agent was newly recognised in it, which is herdr's pane.agent_detected.
// The caller holds the lock.
func (s *Server) setPaneStateLocked(id session.PaneID, agentLabel string, state detect.State) bool {
	previous := ""
	if p, ok := s.session.Pane(id); ok {
		previous = p.Agent
	}
	_ = s.session.SetPaneStateWatched(id, agentLabel, state, s.watchedTabLocked())
	return agentLabel != "" && agentLabel != previous
}
