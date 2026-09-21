package ui

import (
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/sousaakira/tend/internal/vt"
)

// The navigator is herdr's (prefix+g, `client/shell/aggregate_navigation.rs`
// for the rows, `overlays.rs` for the popup): every space, its tabs and
// their panes in one list over the screen, with each pane's agent state, a
// search, and a filter by state. It is what to reach for when the thing
// wanted is known by name or by state and not by where it is.
//
// The rows are built here from a plain description of the session, so the
// rules — what a query matches, what a filter keeps, what is open — are
// tested without a client, and drawing and hit-testing share one geometry.

// NavTarget is what a row goes to: a space, a tab, or a pane — or, with
// saved machines, a machine (Host), herdr's ClientNavigatorTarget::Machine.
// Machine is empty for the machine shown and names any other, whose numbers
// are its own.
type NavTarget struct {
	Workspace, Tab, Pane uint64
	Machine              string
	Host                 bool
}

// NavSource is the session as the navigator needs it.
type NavSource struct {
	Workspaces []NavWorkspace
	// Focused is the pane the client is on, marked ◆ and selected first.
	Focused uint64
	// Machines, when set, is every machine with its spaces, in the
	// sidebar's order, and Workspaces is not read: the list is herdr's
	// federated one, a row for each machine and its spaces beneath.
	Machines []NavMachine
}

// NavMachine is one machine in the navigator.
type NavMachine struct {
	// ID is what a machine row's target carries; the shown machine's own
	// rows carry an empty Machine.
	ID, Label string
	// Signal is its reachability as the sidebar shows it, empty for this one.
	Signal     string
	Shown      bool
	Stale      bool
	Workspaces []NavWorkspace
	// Expanded is which of its spaces are open, for a machine not shown;
	// the shown one's are the expanded map BuildNavigatorRows is given.
	Expanded map[uint64]bool
}

// NavWorkspace is a space and its tabs.
type NavWorkspace struct {
	ID     uint64
	Label  string
	Branch string
	Tabs   []NavTab
}

// NavTab is a tab and its panes.
type NavTab struct {
	ID    uint64
	Label string
	Panes []NavPane
}

// NavPane is one pane: what to call it, where it is, and its agent's state
// ("working", "blocked", "done", "idle", or "unknown" for no agent).
type NavPane struct {
	ID    uint64
	Label string
	Meta  string
	State string
}

// NavFilter keeps only one state. Empty keeps everything.
type NavFilter string

// NavigatorRow is one line of the list.
type NavigatorRow struct {
	Depth int
	Label string
	Meta  string
	// State is set on pane rows only.
	State    string
	Current  bool
	Expanded bool
	Target   NavTarget
	// Stale dims a row whose machine cannot be reached; Signal is drawn
	// against the edge of a machine's row.
	Stale  bool
	Signal string
}

// Navigator is the popup's state, as the client keeps it and the frame
// carries it.
type Navigator struct {
	Rows      []NavigatorRow
	Query     string
	Searching bool
	Filter    NavFilter
	// Selected is the row the cursor is on; a target no longer listed puts
	// it on the first row, as herdr's does.
	Selected NavTarget
	Scroll   int
	Symbols  bool
}

// statePriority is herdr's status_priority, for what a tab or a space is
// "doing": the most urgent of its panes.
func statePriority(state string) int {
	switch state {
	case "blocked":
		return 4
	case "done":
		return 3
	case "working":
		return 2
	case "idle":
		return 1
	}
	return 0
}

// BuildNavigatorRows is herdr's navigator_rows. Every space is listed; its
// tabs and panes when it is open, or whenever a query or a filter is on, so
// what matches is never hidden inside a closed space. A query matches a pane
// by its name or its directory, a tab by its name, a space by its name or
// branch, and with machines everything on a machine whose name it matches; a
// filter keeps panes in that state, and the tabs and spaces whose most urgent
// pane is in it. A parent of anything kept is kept, so a match is always
// shown where it lives.
func BuildNavigatorRows(src NavSource, query string, filter NavFilter, expanded map[uint64]bool) []NavigatorRow {
	q := strings.ToLower(strings.TrimSpace(query))
	if len(src.Machines) == 0 {
		return navSpaces(src.Workspaces, "", src.Focused, 0, false, false, q, filter, expanded)
	}
	filtering := filter != "" || q != ""
	var rows []NavigatorRow
	for _, m := range src.Machines {
		machineMatches := q != "" && strings.Contains(strings.ToLower(m.Label), q)
		id, focused, open := m.ID, uint64(0), m.Expanded
		if m.Shown {
			id, focused, open = "", src.Focused, expanded
		}
		spaces := navSpaces(m.Workspaces, id, focused, 1, machineMatches, m.Stale, q, filter, open)
		if filtering && !machineMatches && len(spaces) == 0 {
			continue
		}
		rows = append(rows, NavigatorRow{
			Label: m.Label, Signal: m.Signal, Stale: m.Stale, Expanded: true,
			Target: NavTarget{Machine: m.ID, Host: true},
		})
		rows = append(rows, spaces...)
	}
	return rows
}

// navSpaces is one machine's spaces, their tabs and panes, depth levels in.
// machineMatches is a query matching the machine's name, which keeps
// everything on it that the filter keeps.
func navSpaces(workspaces []NavWorkspace, machine string, focused uint64, depth int, machineMatches, stale bool,
	q string, filter NavFilter, expanded map[uint64]bool) []NavigatorRow {
	text := func(v string) bool { return q == "" || machineMatches || strings.Contains(strings.ToLower(v), q) }
	keep := func(state string) bool { return filter == "" || state == string(filter) }
	filtering := filter != "" || q != ""

	var rows []NavigatorRow
	for _, w := range workspaces {
		var children []NavigatorRow
		wsState := ""
		for _, tab := range w.Tabs {
			var panes []NavigatorRow
			tabState := ""
			for _, p := range tab.Panes {
				if statePriority(p.State) > statePriority(tabState) {
					tabState = p.State
				}
				if !filtering || keep(p.State) && (text(p.Label) || text(p.Meta)) {
					panes = append(panes, NavigatorRow{
						Depth: depth + 2, Label: p.Label, Meta: p.Meta, State: p.State, Stale: stale,
						Current: p.ID == focused,
						Target:  NavTarget{Workspace: w.ID, Tab: tab.ID, Pane: p.ID, Machine: machine},
					})
				}
			}
			if statePriority(tabState) > statePriority(wsState) {
				wsState = tabState
			}
			if !filtering || keep(tabState) && text(tab.Label) || len(panes) > 0 {
				children = append(children, NavigatorRow{
					Depth: depth + 1, Label: tab.Label, Meta: strconv.Itoa(len(tab.Panes)) + " panes", Stale: stale,
					Target: NavTarget{Workspace: w.ID, Tab: tab.ID, Machine: machine},
				})
				children = append(children, panes...)
			}
		}
		matches := keep(wsState) && (text(w.Label) || text(w.Branch))
		if filtering && !matches && len(children) == 0 {
			continue
		}
		open := expanded[w.ID]
		rows = append(rows, NavigatorRow{
			Depth: depth, Label: w.Label, Meta: w.Branch, Expanded: open, Stale: stale,
			Target: NavTarget{Workspace: w.ID, Machine: machine},
		})
		if open || filtering {
			rows = append(rows, children...)
		}
	}
	return rows
}

// IsSpace reports whether a row is a space's, the kind that opens and closes.
func (r NavigatorRow) IsSpace() bool {
	return !r.Target.Host && r.Target.Tab == 0 && r.Target.Pane == 0
}

// SelectedIndex is the row the cursor is on.
func (n Navigator) SelectedIndex() int {
	for i, r := range n.Rows {
		if r.Target == n.Selected {
			return i
		}
	}
	return 0
}

// NavigatorRect is where the popup goes: most of the screen, with herdr's
// margins.
func NavigatorRect(cols, rows int) Rect {
	mx := max(cols/16, 2)
	my := max(rows/10, 1)
	return Rect{X: mx, Y: my, Cols: max(cols-2*mx, 4), Rows: max(rows-2*my, 4)}
}

// navigatorBody is the rows' area inside the popup: under the search line
// and its rule, over the detail line and the hints.
func navigatorBody(cols, rows int) Rect {
	r := NavigatorRect(cols, rows)
	return Rect{X: r.X + 1, Y: r.Y + 3, Cols: r.Cols - 2, Rows: max(r.Rows-6, 0)}
}

// navigatorScroll is the first row shown, keeping the selection in view.
func navigatorScroll(n Navigator, body int) int {
	selected := n.SelectedIndex()
	most := max(len(n.Rows)-body, 0)
	scroll := max(n.Scroll, selected-max(body-1, 0))
	return max(min(scroll, selected, most), 0)
}

// NavigatorHit is what a point on the popup is.
type NavigatorHit struct {
	Inside bool
	Search bool
	// Row is the index of the row under the point, or -1; Caret is whether
	// the point is on a space's ▸, which opens it rather than going to it.
	Row   int
	Caret bool
}

// NavigatorAt says what is at a point, from the same geometry the popup is
// drawn with.
func NavigatorAt(n Navigator, cols, rows, x, y int) NavigatorHit {
	r := NavigatorRect(cols, rows)
	hit := NavigatorHit{Row: -1}
	if x < r.X || x >= r.X+r.Cols || y < r.Y || y >= r.Y+r.Rows {
		return hit
	}
	hit.Inside = true
	if y == r.Y+1 {
		hit.Search = true
		return hit
	}
	body := navigatorBody(cols, rows)
	if y >= body.Y && y < body.Y+body.Rows {
		i := navigatorScroll(n, body.Rows) + y - body.Y
		if i < len(n.Rows) {
			hit.Row = i
			hit.Caret = n.Rows[i].IsSpace() && x <= body.X+3+2*n.Rows[i].Depth
		}
	}
	return hit
}

// NavigatorCursor is where the terminal's cursor goes while a query is
// being typed.
func NavigatorCursor(n Navigator, cols, rows int) (int, int, bool) {
	if !n.Searching {
		return 0, 0, false
	}
	r := NavigatorRect(cols, rows)
	return r.X + 1 + 3 + runewidth.StringWidth(n.Query), r.Y + 1, true
}

// followingSiblings is herdr's navigator_following_siblings: whether each
// row has a later sibling at its depth before its parent's next sibling,
// which decides ├── from └──.
func followingSiblings(rows []NavigatorRow) []bool {
	following := make([]bool, len(rows))
	var depths []int
	for i := len(rows) - 1; i >= 0; i-- {
		d := rows[i].Depth
		for len(depths) > 0 && depths[len(depths)-1] > d {
			depths = depths[:len(depths)-1]
		}
		following[i] = len(depths) > 0 && depths[len(depths)-1] == d
		if !following[i] {
			depths = append(depths, d)
		}
	}
	return following
}

func stateStyle(state string, theme Theme) vt.Style {
	switch state {
	case "working":
		return theme.Working
	case "blocked":
		return theme.Blocked
	case "done":
		return theme.Done
	case "idle":
		return theme.Idle
	}
	return theme.Unknown
}

// drawNavigator paints the popup.
func drawNavigator(dst *vt.Grid, n Navigator, theme Theme) {
	cols, rows := dst.Cols(), dst.Rows()
	r := NavigatorRect(cols, rows)
	if r.Cols < 10 || r.Rows < 7 {
		return
	}
	for y := r.Y; y < r.Y+r.Rows; y++ {
		fill(dst, y, r.X, r.X+r.Cols, theme.Menu)
	}
	drawBox(dst, r, theme.BorderFocused)
	writeString(dst, r.X+2, r.Y, " navigate ", theme.BorderFocused, r.X+r.Cols-1)

	inner := Rect{X: r.X + 1, Y: r.Y + 1, Cols: r.Cols - 2, Rows: r.Rows - 2}
	limit := inner.X + inner.Cols
	dim := theme.Menu
	dim.Attrs |= vt.AttrDim

	search := " / search panes"
	switch {
	case n.Searching || n.Query != "":
		search = " / " + n.Query
	case n.Filter != "":
		search = " / " + string(n.Filter)
	}
	searchStyle := dim
	if n.Searching {
		searchStyle = theme.Menu
	}
	writeString(dst, inner.X, inner.Y, truncate(search, inner.Cols), searchStyle, limit)
	panes := 0
	for _, row := range n.Rows {
		if row.Target.Pane != 0 {
			panes++
		}
	}
	count := strconv.Itoa(panes) + " panes "
	writeString(dst, limit-runewidth.StringWidth(count), inner.Y, count, dim, limit)
	writeString(dst, inner.X, inner.Y+1, strings.Repeat("─", inner.Cols), dim, limit)

	body := navigatorBody(cols, rows)
	selected := n.SelectedIndex()
	scroll := navigatorScroll(n, body.Rows)
	following := followingSiblings(n.Rows)
	federated := len(n.Rows) > 0 && n.Rows[0].Target.Host
	var ancestors []bool
	for i, row := range n.Rows {
		if i >= scroll+body.Rows {
			break
		}
		if len(ancestors) > row.Depth {
			ancestors = ancestors[:row.Depth]
		}
		ancestors = append(ancestors, following[i])
		if i < scroll {
			continue
		}
		y := body.Y + i - scroll
		style := theme.Menu
		if i == selected {
			style = theme.MenuSelected
		} else if row.Stale || !row.Current && !row.IsSpace() && !row.Target.Host {
			style = dim
		}
		fill(dst, y, body.X, body.X+body.Cols, style)

		var tree string
		switch {
		case row.Target.Host:
			tree = "▾"
		case row.IsSpace():
			tree = strings.Repeat("  ", row.Depth) + "▸"
			if row.Expanded {
				tree = strings.Repeat("  ", row.Depth) + "▾"
			}
		default:
			// Under a machine, the branches start below its spaces, as
			// herdr's federated tree does.
			from := 1
			if federated {
				tree, from = "    ", 2
			}
			for d := from; d < row.Depth && d < len(ancestors); d++ {
				if ancestors[d] {
					tree += "│  "
				} else {
					tree += "   "
				}
			}
			if following[i] {
				tree += "├──"
			} else {
				tree += "└──"
			}
		}
		current := ""
		if row.Current {
			current = "◆ "
		}
		prefix := " " + tree + " " + current
		x := writeString(dst, body.X, y, prefix, style, body.X+body.Cols)
		if row.Target.Pane != 0 {
			icon := StatusIcon(row.State, true, n.Symbols)
			iconStyle := stateStyle(row.State, theme)
			if i == selected {
				iconStyle = style
			} else {
				iconStyle.BG = theme.Menu.BG
			}
			x = writeString(dst, x, y, icon, iconStyle, body.X+body.Cols)
			x = writeString(dst, x, y, " ", style, body.X+body.Cols)
		}
		room := body.X + body.Cols - x
		label := truncate(row.Label, room)
		x = writeString(dst, x, y, label, style, body.X+body.Cols)
		if row.Signal != "" {
			signal := machineSignalStyle(machineStateOf(row.Signal), theme)
			if i == selected {
				signal = style
			} else {
				signal.BG = theme.Menu.BG
			}
			writeString(dst, body.X+body.Cols-runewidth.StringWidth(row.Signal)-1, y, row.Signal, signal, body.X+body.Cols)
		} else if row.Meta != "" {
			if space := body.X + body.Cols - x - 2; space > 3 {
				meta := truncateLeft(row.Meta, space)
				writeString(dst, body.X+body.Cols-runewidth.StringWidth(meta)-1, y, meta, style, body.X+body.Cols)
			}
		}
	}
	if len(n.Rows) == 0 {
		writeString(dst, body.X+2, body.Y, "nothing matches", dim, limit)
	}

	if selected < len(n.Rows) {
		row := n.Rows[selected]
		detail := " " + row.Label
		if row.Meta != "" {
			detail += " · " + row.Meta
		}
		writeString(dst, inner.X, inner.Y+inner.Rows-2, truncate(detail, inner.Cols), dim, limit)
	}
	hint := " move j/k · expand space · filter a/b/w/i/d · search / · open enter · close esc"
	if n.Searching {
		hint = " search type · move ↑↓/ctrl+n/p · open enter · back esc"
	}
	writeString(dst, inner.X, inner.Y+inner.Rows-1, truncate(hint, inner.Cols), dim, limit)
}

// truncateLeft keeps the end of text, which for a path is the part that
// says which directory it is.
func truncateLeft(text string, cols int) string {
	if runewidth.StringWidth(text) <= cols {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 && runewidth.StringWidth(string(runes))+1 > cols {
		runes = runes[1:]
	}
	return "…" + string(runes)
}

// machineStateOf reads a machine's state back from its signal, for its
// colour.
func machineStateOf(signal string) string {
	for _, state := range []string{MachineConnecting, MachineReconnecting, MachineAttention, MachineDisabled} {
		if strings.HasSuffix(signal, state) {
			return state
		}
	}
	if signal == MachineSignal(MachineOnline) {
		return MachineOnline
	}
	return ""
}
