package session

import "errors"

// Moving things around without making them again: two panes trade places,
// a tab or a space moves along its row. None of this touches a process — a
// pane that moves keeps running, its program never learns it went anywhere —
// which is what makes it different from closing one and opening another.
//
// The rules are herdr's (`layout.rs` swap_panes, `workspace.rs` move_tab,
// `app/actions.rs` move_workspace): a swap exchanges the two leaves and leaves
// the split shape and its ratios alone, and a move is "remove, then insert at
// this index", with the focused one staying focused wherever it ends up.

// ErrNoMove means the move asked for is not one: the same pane twice, panes in
// two different tabs, an index off the end.
var ErrNoMove = errors.New("session: nothing to move")

// SwapPanes exchanges two panes' places in their tab.
//
// Only within one tab: a pane's size and neighbours come from the tree it is
// in, and trading between two trees would leave each pane sized for a place
// in a layout it is no longer part of.
func (s *Session) SwapPanes(a, b PaneID) error {
	if a == b {
		return ErrNoMove
	}
	ta, ok := s.index[a]
	if !ok {
		return ErrNoSuchPane
	}
	tb, ok := s.index[b]
	if !ok {
		return ErrNoSuchPane
	}
	if ta != tb {
		return ErrNoMove
	}
	swapLeaves(ta.root, a, b)
	return nil
}

// swapLeaves renames every leaf that is a to b and every one that is b to a.
func swapLeaves(n *node, a, b PaneID) {
	if n == nil {
		return
	}
	if len(n.kids) == 0 {
		switch n.pane {
		case a:
			n.pane = b
		case b:
			n.pane = a
		}
		return
	}
	for _, kid := range n.kids {
		swapLeaves(kid, a, b)
	}
}

// SwapPaneToward exchanges a pane with its neighbour on one side, as laid out
// in area. It reports the neighbour, and ErrNoMove when there is none.
func (s *Session) SwapPaneToward(id PaneID, side Side, area Rect) (PaneID, error) {
	t, ok := s.index[id]
	if !ok {
		return 0, ErrNoSuchPane
	}
	other, found := Neighbor(t.Layout(area), id, side)
	if !found {
		return 0, ErrNoMove
	}
	if err := s.SwapPanes(id, other); err != nil {
		return 0, err
	}
	return other, nil
}

// MoveTab puts a tab at index in its space's row, counting as the row is
// after the tab has been taken out of it.
func (s *Session) MoveTab(id TabID, index int) error {
	for _, w := range s.workspaces {
		for from, t := range w.tabs {
			if t.ID != id {
				continue
			}
			if index < 0 || index >= len(w.tabs) {
				return ErrNoMove
			}
			if index == from {
				return ErrNoMove
			}
			var active TabID
			if w.active >= 0 && w.active < len(w.tabs) {
				active = w.tabs[w.active].ID
			}
			w.tabs = moveItem(w.tabs, from, index)
			for i, tab := range w.tabs {
				if tab.ID == active {
					w.active = i
				}
			}
			return nil
		}
	}
	return ErrNoSuchTab
}

// MoveWorkspace puts a space at index in the session's list.
func (s *Session) MoveWorkspace(id WorkspaceID, index int) error {
	for from, w := range s.workspaces {
		if w.ID != id {
			continue
		}
		if index < 0 || index >= len(s.workspaces) || index == from {
			return ErrNoMove
		}
		var active WorkspaceID
		if s.active >= 0 && s.active < len(s.workspaces) {
			active = s.workspaces[s.active].ID
		}
		s.workspaces = moveItem(s.workspaces, from, index)
		for i, ws := range s.workspaces {
			if ws.ID == active {
				s.active = i
			}
		}
		return nil
	}
	return ErrNoSuchWorkspace
}

// TabIndex is where a tab is in its space's row.
func (s *Session) TabIndex(id TabID) (int, bool) {
	for _, w := range s.workspaces {
		for i, t := range w.tabs {
			if t.ID == id {
				return i, true
			}
		}
	}
	return 0, false
}

// WorkspaceIndex is where a space is in the session's list.
func (s *Session) WorkspaceIndex(id WorkspaceID) (int, bool) {
	for i, w := range s.workspaces {
		if w.ID == id {
			return i, true
		}
	}
	return 0, false
}

// moveItem takes the item at from out and puts it back at to.
func moveItem[T any](items []T, from, to int) []T {
	item := items[from]
	rest := append(items[:from:from], items[from+1:]...)
	out := make([]T, 0, len(items))
	out = append(out, rest[:to]...)
	out = append(out, item)
	return append(out, rest[to:]...)
}
