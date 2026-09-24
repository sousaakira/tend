package explorer

import (
	"encoding/json"
	"errors"
)

// The panel is a program in a pane, and a program keeps running the binary
// it was started from. After an install — `make install`, `tend update` — a
// panel opened before it went on drawing the old one until somebody closed
// it and opened it again (prefix+f twice), and the new look never showed on
// its own. So the panel watches its binary (the caller says how, through
// WatchBinary) and, when a new one is in place, starts it in its own stead:
// the same pane, the same terminal, and what was being looked at carried
// across as a State, so the swap reads as a redraw.
//
// herdr has nothing to copy here: its sidebar is a plugin it installs and
// restarts itself. This is tend's, for tend's own panel.

// ErrUpgrade is what Run returns when the binary on disk changed and the
// panel should start it in its place. The terminal is put back first.
var ErrUpgrade = errors.New("explorer: a new build is installed")

// State is what the panel carries into the build that replaces it.
type State struct {
	Root     string   `json:"root"`
	View     View     `json:"view"`
	Hidden   bool     `json:"hidden"`
	Expanded []string `json:"expanded,omitempty"`
	// Cursor is the entry selected in the tree, by its path from the root.
	Cursor string `json:"cursor,omitempty"`
}

// WatchBinary gives the panel a way to ask whether a new build of it is
// installed, which it does on the refresh timer.
func (m *Model) WatchBinary(changed func() bool) { m.binaryChanged = changed }

// upgradeDue is whether to hand over now: a new build is in place, and the
// panel is only showing something — not in the middle of typing a commit
// message, a name, an answer, nor with a job running, whose result the new
// build would never hear of.
func (m *Model) upgradeDue() bool {
	if m.binaryChanged == nil || m.mode != modeList || m.jobRunning || m.job != nil {
		return false
	}
	return m.binaryChanged()
}

// State is what is being looked at, for the build that replaces this one.
func (m *Model) State() State {
	s := State{Root: m.tree.Root, View: m.view, Hidden: m.tree.Hidden}
	for _, n := range m.fileRows {
		if n.Dir && n.Expanded {
			s.Expanded = append(s.Expanded, n.Rel)
		}
	}
	if n := m.selectedNode(); n != nil {
		s.Cursor = n.Rel
	}
	return s
}

// Restore puts a carried State back: the folders open, the view, the
// selection. What no longer exists is skipped rather than failed on — the
// new build may start a moment after a folder was removed.
func (m *Model) Restore(s State) {
	m.tree.Hidden = s.Hidden
	for _, rel := range s.Expanded {
		if n := m.tree.Reveal(rel, m.git); n != nil && n.Dir && !n.Expanded {
			m.tree.Toggle(n, m.git)
		}
	}
	m.layout()
	for i, n := range m.fileRows {
		if n.Rel == s.Cursor {
			m.cursor[ViewFiles] = i
		}
	}
	if s.View >= ViewFiles && s.View <= ViewChanges {
		m.view = s.View
	}
	m.clamp()
}

// EncodeState and DecodeState carry a State through the environment.
func EncodeState(s State) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func DecodeState(raw string) (State, bool) {
	var s State
	if raw == "" || json.Unmarshal([]byte(raw), &s) != nil || s.Root == "" {
		return State{}, false
	}
	return s, true
}
