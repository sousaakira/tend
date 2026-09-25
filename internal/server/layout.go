package server

import (
	"fmt"

	"github.com/auth-com-br/tend/internal/session"
)

// A layout is an arrangement somebody set up and would like back: three panes
// in a particular shape, each in its own directory, running the thing it runs.
// herdr exports and applies one (`layout.export`, `layout.apply`); this is
// that, over tend's own split tree rather than herdr's binary one.
//
// Exporting reads the tree; applying rebuilds it by splitting, which is the
// only way to make panes — there is no constructor for a shape, and the
// programs have to be started as panes are started everywhere else.

// LayoutSpec is a tab's arrangement: the tree, and what each leaf runs.
type LayoutSpec struct {
	Tree  session.LayoutSnapshot `json:"tree"`
	Panes map[uint64]PaneSpec    `json:"-"`
}

// ExportLayout describes a tab: its tree, and what each pane is.
func (s *Server) ExportLayout(id session.TabID) (session.LayoutSnapshot, []session.PaneSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return session.LayoutSnapshot{}, nil, ErrClosed
	}
	tree, ok := s.session.TabLayout(id)
	if !ok {
		return session.LayoutSnapshot{}, nil, fmt.Errorf("%w: %d", session.ErrNoSuchTab, id)
	}
	tab, _ := s.session.Tab(id)

	var panes []session.PaneSnapshot
	for _, paneID := range tab.Panes() {
		p, ok := tab.Pane(paneID)
		if !ok {
			continue
		}
		dir := p.Dir
		if rt, live := s.runtimes[paneID]; live {
			// Where the pane is now, not where it started: an arrangement is
			// worth having back in the directories it ended up in.
			if now := rt.pty.Cwd(); now != "" {
				dir = now
			}
		}
		panes = append(panes, session.PaneSnapshot{
			ID: uint64(p.ID), Title: p.Title, Named: p.Named,
			Command: p.Command, Dir: dir, Agent: p.Agent,
		})
	}
	return tree, panes, nil
}

// ApplyLayout builds an arrangement as a new tab in a space.
//
// The panes are started as the tree is rebuilt, so a layout that names a
// command nobody has takes the tab down with it rather than leaving half an
// arrangement: a half-applied layout is worse than none, because it looks like
// it worked.
func (s *Server) ApplyLayout(ws session.WorkspaceID, name string, tree session.LayoutSnapshot, specs map[uint64]PaneSpec, fallback PaneSpec) (session.TabID, error) {
	leaves := layoutLeaves(tree)
	if len(leaves) == 0 {
		return 0, fmt.Errorf("server: that layout has no panes in it")
	}

	first := specFor(specs, leaves[0], fallback)
	tab, root, err := s.NewTab(ws, name, first)
	if err != nil {
		return 0, err
	}

	// Each further leaf is a split of the pane before it, in the order the
	// tree lists them; the shape comes out right because the shares are
	// copied on afterwards.
	previous := root
	for _, leaf := range leaves[1:] {
		spec := specFor(specs, leaf, fallback)
		direction := session.Columns
		if tree.Dir == "rows" {
			direction = session.Rows
		}
		pane, err := s.SplitPane(previous, direction, spec)
		if err != nil {
			_ = s.CloseTab(tab)
			return 0, err
		}
		previous = pane
	}

	s.mu.Lock()
	err = s.session.SetLayoutSizes(tab, tree)
	s.mu.Unlock()
	if err != nil {
		// The shape is right and the shares are not, which is worth saying
		// and not worth throwing the tab away over.
		s.logf("layout: %v", err)
	}
	s.publish(Event{Kind: EventSessionChanged})
	return tab, nil
}

// layoutLeaves lists a tree's panes, left to right and top to bottom.
func layoutLeaves(n session.LayoutSnapshot) []uint64 {
	if len(n.Kids) == 0 {
		return []uint64{n.Pane}
	}
	var out []uint64
	for _, kid := range n.Kids {
		out = append(out, layoutLeaves(kid)...)
	}
	return out
}

// specFor is what a leaf should run: what the layout said, or the fallback.
func specFor(specs map[uint64]PaneSpec, leaf uint64, fallback PaneSpec) PaneSpec {
	if spec, ok := specs[leaf]; ok && len(spec.Command) > 0 {
		return spec
	}
	return fallback
}
