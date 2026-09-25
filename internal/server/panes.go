package server

import (
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/session"
)

// Small questions a script or a plugin asks about a pane's place: who is next
// to it, which edges of the tab it touches, what is running in it. herdr
// answers them (`pane.neighbor`, `pane.edges`, `pane.process_info`); a plugin
// that docks a panel on the edge of a tab needs the first two, and one that
// knows what is running needs the third.
//
// Geometry is asked of the layout at a nominal size. What is next to what does
// not depend on how big the window is, and the server does not know how big
// any client's window is — that is theirs.

// nominalArea is the size a layout is asked about when no client is saying.
var nominalArea = session.Rect{W: 120, H: 40}

// Neighbor is the pane on one side of another, or zero at the tab's edge.
func (s *Server) Neighbor(id session.PaneID, side session.Side) (session.PaneID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.session.TabOf(id)
	if !ok {
		return 0, session.ErrNoSuchPane
	}
	other, found := session.Neighbor(t.Layout(nominalArea), id, side)
	if !found {
		return 0, nil
	}
	return other, nil
}

// Edges says which sides of its tab a pane touches.
func (s *Server) Edges(id session.PaneID) (map[string]bool, error) {
	out := map[string]bool{}
	for side, name := range map[session.Side]string{
		session.Left: "left", session.Right: "right", session.Up: "up", session.Down: "down",
	} {
		neighbor, err := s.Neighbor(id, side)
		if err != nil {
			return nil, err
		}
		out[name] = neighbor == 0
	}
	return out, nil
}

// ProcessInfo is what is running in a pane: its shell, and the process group
// in charge of its terminal now.
func (s *Server) ProcessInfo(id session.PaneID) (pty.ProcessInfo, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return pty.ProcessInfo{}, err
	}
	// Outside every lock: it reads /proc, and nothing that touches the
	// filesystem belongs under the lock every pane operation needs.
	return rt.pty.Processes(), nil
}
