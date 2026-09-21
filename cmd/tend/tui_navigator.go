package main

import (
	"strconv"
	"unicode/utf8"

	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/ui"
)

// The navigator is herdr's (prefix+g): a popup over the screen listing every
// space, its tabs and their panes, with each pane's agent state, a search by
// name or directory, and a filter by state. The rules live in ui (rows,
// drawing, what a click is on); this is the state one client keeps while it
// is up and what its keys and clicks do. It is this client's alone: what is
// being searched for is nobody else's business.

// navigatorState is what the popup remembers between frames.
type navigatorState struct {
	query     string
	searching bool
	filter    ui.NavFilter
	selected  ui.NavTarget
	scroll    int
	expanded  map[uint64]bool
}

// navigatorPage is how far ctrl+d and ctrl+u move, herdr's eight rows.
const navigatorPage = 8

func (t *tui) navigatorOpen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.navigator != nil
}

// openNavigator puts the popup up with every space open and the cursor on
// the pane in view, as herdr's does.
func (t *tui) openNavigator() {
	t.mu.Lock()
	n := &navigatorState{expanded: map[uint64]bool{}}
	for _, w := range t.snap.Workspaces {
		n.expanded[w.ID] = true
	}
	t.navigator = n
	for _, row := range t.navigatorRowsLocked() {
		if row.Current {
			n.selected = row.Target
		}
	}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) closeNavigator() {
	t.mu.Lock()
	had := t.navigator != nil
	t.navigator = nil
	t.dirty = true
	t.mu.Unlock()
	if had {
		t.wakeUp()
	}
}

// navigatorSourceLocked is the session as the navigator lists it. A pane is
// called by the name the user gave it, then what its agent is called, then
// its title, then its place, as herdr names them.
func (t *tui) navigatorSourceLocked() ui.NavSource {
	panes := make(map[uint64]proto.PaneInfo, len(t.snap.Panes))
	for _, p := range t.snap.Panes {
		panes[p.ID] = p
	}
	src := ui.NavSource{Focused: t.focus}
	for _, w := range t.snap.Workspaces {
		nw := ui.NavWorkspace{ID: w.ID, Label: orDash(w.Name), Branch: w.Branch}
		for i, tab := range w.Tabs {
			label := tab.Name
			if label == "" {
				label = "tab " + strconv.Itoa(i+1)
			}
			nt := ui.NavTab{ID: tab.ID, Label: label}
			for j, id := range tab.Panes {
				p := panes[id]
				nt.Panes = append(nt.Panes, ui.NavPane{
					ID: id, Label: navigatorPaneLabel(p, j), Meta: p.Dir, State: navState(p),
				})
			}
			nw.Tabs = append(nw.Tabs, nt)
		}
		src.Workspaces = append(src.Workspaces, nw)
	}
	return src
}

func navigatorPaneLabel(p proto.PaneInfo, index int) string {
	switch {
	case p.Named && p.Title != "":
		return p.Title
	case p.Display != "":
		return p.Display
	case p.Agent != "":
		return p.Agent
	case p.Title != "":
		return p.Title
	}
	return "pane " + strconv.Itoa(index+1)
}

// navState is a pane's state as herdr's filter names it: an agent
// that finished unseen is "done", and a pane with no agent, or whose
// program has ended, is "unknown".
func navState(p proto.PaneInfo) string {
	if p.Agent == "" || !p.Running {
		return "unknown"
	}
	if p.Done {
		return "done"
	}
	switch p.State {
	case "working", "blocked", "idle":
		return p.State
	}
	return "unknown"
}

func (t *tui) navigatorRowsLocked() []ui.NavigatorRow {
	n := t.navigator
	if n == nil {
		return nil
	}
	return ui.BuildNavigatorRows(t.navigatorSourceLocked(), n.query, n.filter, n.expanded)
}

// navigatorFrameLocked is the popup as the frame draws it.
func (t *tui) navigatorFrameLocked() *ui.Navigator {
	n := t.navigator
	if n == nil {
		return nil
	}
	return &ui.Navigator{
		Rows: t.navigatorRowsLocked(), Query: n.query, Searching: n.searching,
		Filter: n.filter, Selected: n.selected, Scroll: n.scroll,
		Symbols: t.config.UI.StatusIndicators == "symbols",
	}
}

// moveNavigator moves the cursor by delta rows, stopping at the ends.
func (t *tui) moveNavigator(delta int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := t.navigator
	if n == nil {
		return
	}
	frame := t.navigatorFrameLocked()
	if len(frame.Rows) == 0 {
		n.selected = ui.NavTarget{}
		return
	}
	at := min(max(frame.SelectedIndex()+delta, 0), len(frame.Rows)-1)
	n.selected = frame.Rows[at].Target
	t.dirty = true
}

// acceptNavigator goes where the cursor is, and puts the popup away.
func (t *tui) acceptNavigator() error {
	t.mu.Lock()
	frame := t.navigatorFrameLocked()
	t.mu.Unlock()
	if frame == nil || len(frame.Rows) == 0 {
		return nil
	}
	target := frame.Rows[frame.SelectedIndex()].Target
	t.closeNavigator()
	switch {
	case target.Pane != 0:
		return t.jumpToPane(target.Pane)
	case target.Tab != 0:
		return t.showTab(target.Tab)
	case target.Workspace != 0:
		return t.showWorkspace(target.Workspace)
	}
	return nil
}

// toggleNavigatorSpace opens or closes the space under the cursor.
func (t *tui) toggleNavigatorSpace() {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := t.navigator
	if n == nil {
		return
	}
	frame := t.navigatorFrameLocked()
	if len(frame.Rows) == 0 {
		return
	}
	row := frame.Rows[frame.SelectedIndex()]
	if row.Target.Pane != 0 || row.Target.Tab != 0 {
		return
	}
	n.expanded[row.Target.Workspace] = !n.expanded[row.Target.Workspace]
	n.scroll = 0
	t.dirty = true
}

// navigatorKeys is herdr's navigator keys: j/k and the arrows move, / searches,
// b w i d keep only blocked, working, idle or done panes and a shows all
// again, space opens a space, enter goes, esc leaves the search and then the
// popup.
func (t *tui) navigatorKeys(data []byte) error {
	for _, key := range splitKeys(data) {
		t.mu.Lock()
		n := t.navigator
		if n == nil {
			t.mu.Unlock()
			return nil
		}
		searching := n.searching
		t.dirty = true
		t.mu.Unlock()

		switch key {
		case "\x1b", "\x03":
			if searching {
				t.mu.Lock()
				n.searching = false
				t.mu.Unlock()
			} else {
				t.closeNavigator()
			}
			continue
		case "\r", "\n":
			if err := t.acceptNavigator(); err != nil {
				return err
			}
			continue
		}

		if searching {
			switch key {
			case "\x1b[A", "\x10":
				t.moveNavigator(-1)
			case "\x1b[B", "\x0e":
				t.moveNavigator(1)
			default:
				t.mu.Lock()
				switch key {
				case "\x15":
					n.query = ""
				case "\x7f", "\x08":
					if n.query != "" {
						_, size := utf8.DecodeLastRuneInString(n.query)
						n.query = n.query[:len(n.query)-size]
					}
				default:
					// Printable text, a byte at a time: a character of more
					// than one byte arrives as its bytes, and joined back
					// together they are the character again.
					if len(key) == 1 && (key[0] >= 0x20 && key[0] != 0x7f || key[0] >= 0x80) {
						n.query += key
					} else {
						t.mu.Unlock()
						continue
					}
				}
				n.filter, n.selected = "", ui.NavTarget{}
				t.mu.Unlock()
			}
			continue
		}

		switch key {
		case "j", "\x1b[B":
			t.moveNavigator(1)
		case "k", "\x1b[A":
			t.moveNavigator(-1)
		case "\x04":
			t.moveNavigator(navigatorPage)
		case "\x15":
			t.moveNavigator(-navigatorPage)
		case "\x1b[H", "\x1b[1~":
			t.mu.Lock()
			n.selected, n.scroll = ui.NavTarget{}, 0
			t.mu.Unlock()
		case "G", "\x1b[F", "\x1b[4~":
			t.mu.Lock()
			if rows := t.navigatorRowsLocked(); len(rows) > 0 {
				n.selected = rows[len(rows)-1].Target
			}
			t.mu.Unlock()
		case "/":
			t.mu.Lock()
			n.searching, n.filter = true, ""
			t.mu.Unlock()
		case "\x7f", "\x08":
			t.mu.Lock()
			if n.filter != "" {
				n.filter, n.selected = "", ui.NavTarget{}
			}
			t.mu.Unlock()
		case "b", "w", "i", "d":
			t.mu.Lock()
			n.query = ""
			n.filter = map[string]ui.NavFilter{"b": "blocked", "w": "working", "i": "idle", "d": "done"}[key]
			n.selected = ui.NavTarget{}
			t.mu.Unlock()
		case "a":
			t.mu.Lock()
			n.query, n.filter, n.selected = "", "", ui.NavTarget{}
			t.mu.Unlock()
		case " ":
			t.toggleNavigatorSpace()
		}
	}
	t.wakeUp()
	return nil
}

// navigatorMouse answers the mouse while the popup is up, and reports
// whether it was the popup's to answer: all of it is, since the popup is
// over everything.
func (t *tui) navigatorMouse(ev ui.MouseEvent) (bool, error) {
	t.mu.Lock()
	frame := t.navigatorFrameLocked()
	cols, rows := t.cols, t.rows
	n := t.navigator
	t.mu.Unlock()
	if frame == nil {
		return false, nil
	}
	hit := ui.NavigatorAt(*frame, cols, rows, ev.X, ev.Y)
	defer t.wakeUp()
	switch ev.Kind {
	case ui.MouseWheelUp:
		t.moveNavigator(-3)
	case ui.MouseWheelDown:
		t.moveNavigator(3)
	case ui.MouseMove:
		if hit.Row >= 0 {
			t.mu.Lock()
			n.selected = frame.Rows[hit.Row].Target
			t.dirty = true
			t.mu.Unlock()
		}
	case ui.MousePress:
		switch {
		case !hit.Inside:
			t.closeNavigator()
		case hit.Search:
			t.mu.Lock()
			n.searching, n.filter = true, ""
			t.dirty = true
			t.mu.Unlock()
		case hit.Row >= 0:
			t.mu.Lock()
			n.selected = frame.Rows[hit.Row].Target
			t.dirty = true
			t.mu.Unlock()
			if hit.Caret {
				t.toggleNavigatorSpace()
				return true, nil
			}
			return true, t.acceptNavigator()
		}
	}
	return true, nil
}
