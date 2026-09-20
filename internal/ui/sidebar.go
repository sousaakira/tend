package ui

import "github.com/sousaakira/tend/internal/vt"

// The sidebar answers two questions without leaving the pane you are in:
// where else could I be, and which agent needs me.
//
// It is two lists, not one. Spaces are places — a repository, a branch, a
// piece of work — and they exist whether or not anything is running in them.
// Agents are what is running, and the one that stopped is rarely in the space
// being looked at, which is why the second list is not merely the first
// expanded.
//
// Both are drawn as a two-line entry: a name, and beneath it dimmed, the thing
// that tells two entries with the same name apart. For a space that is its
// branch; for an agent, which agent it is.

// SidebarKind is what a row describes.
type SidebarKind uint8

const (
	// SidebarHeading is a section title, such as "spaces".
	SidebarHeading SidebarKind = iota
	// SidebarSpace is a workspace, with its branch beneath.
	SidebarSpace
	// SidebarAgent is a running agent, with its kind beneath.
	SidebarAgent
	// SidebarGroup is a tab heading, shown when the agent list is grouped.
	SidebarGroup
	// SidebarAction is a row that does something rather than going somewhere.
	SidebarAction
	// SidebarBlank is a spacer.
	SidebarBlank
)

// Action names carried on an action row.
const (
	ActionNewSpace      = "new-space"
	ActionToggleGrouped = "toggle-grouped"
)

// SidebarRow is one entry. A two-line entry is one row: the detail is drawn
// beneath the name, so selecting and hit-testing count entries rather than
// lines.
type SidebarRow struct {
	Kind   SidebarKind
	Label  string
	Detail string
	// Trailing is drawn against the right edge, such as the "grouped" toggle.
	Trailing string

	// Pane, Tab and Workspace say where selecting this row goes. Action names
	// what it does instead.
	Pane      uint64
	Tab       uint64
	Workspace uint64
	Action    string

	State   string
	Running bool

	// Active marks what is currently shown; Selected marks the navigation
	// cursor. They are separate because moving the cursor must not move the
	// view — reading down the list has not left the pane being worked in.
	Active   bool
	Selected bool
}

// height is how many screen lines the row occupies.
func (r SidebarRow) height() int {
	if r.Detail != "" {
		return 2
	}
	return 1
}

// SidebarWidth is how many columns the sidebar takes. Wide enough for a
// repository name and a state marker, narrow enough to cost a pane little.
const SidebarWidth = 26

// SidebarColumns is how many columns the sidebar occupies in a frame, which is
// none when it is hidden.
//
// It is the left edge of everything else on screen, which is why the tab bar
// and the sidebar both measure from it rather than each deciding for itself.
func SidebarColumns(f Frame, cols int) int {
	if !f.Sidebar {
		return 0
	}
	return min(SidebarWidth, cols)
}

// drawSidebar draws the lists down the left edge.
func drawSidebar(dst *vt.Grid, f Frame, theme Theme) {
	if !f.Sidebar {
		return
	}
	// The sidebar runs from the very top: it is not inside the tab bar's
	// space, the tab bar is inside its own.
	top := 0
	bottom := dst.Rows() - StatusRows
	width := SidebarColumns(f, dst.Cols())

	for y := top; y < bottom; y++ {
		row := dst.Line(y)
		if row == nil {
			continue
		}
		for x := 0; x < width; x++ {
			row.SetCell(x, vt.Cell{R: ' ', Style: theme.Sidebar, Width: 1})
		}
		// A rule down the right edge, so the sidebar reads as its own column
		// rather than as text spilling into the first pane.
		row.SetCell(width-1, vt.Cell{R: '│', Style: theme.Border, Width: 1})
	}

	limit := width - 1
	y := top
	for _, r := range f.SidebarRows {
		if y+r.height() > bottom {
			break
		}
		drawSidebarRow(dst, r, y, limit, theme)
		y += r.height()
	}
}

func drawSidebarRow(dst *vt.Grid, r SidebarRow, y, limit int, theme Theme) {
	switch r.Kind {
	case SidebarBlank:
		return

	case SidebarHeading:
		writeString(dst, 1, y, truncate(r.Label, limit-1), theme.SidebarGroup, limit)
		if r.Trailing != "" {
			// Right-aligned: a toggle belongs at the edge of what it toggles,
			// not trailing the words that name it.
			style := theme.SidebarGroup
			if r.Active {
				style = theme.SidebarGroupActive
			}
			if at := limit - 1 - len([]rune(r.Trailing)); at > 1 {
				writeString(dst, at, y, r.Trailing, style, limit)
			}
		}

	case SidebarGroup:
		writeString(dst, 3, y, truncate(r.Label, limit-3), theme.SidebarGroup, limit)

	case SidebarAction:
		style := theme.SidebarGroup
		if r.Selected {
			style = theme.SidebarActive
		}
		writeString(dst, 1, y, truncate(r.Label, limit-1), style, limit)

	case SidebarSpace, SidebarAgent:
		drawSidebarEntry(dst, r, y, limit, theme)
	}
}

// drawSidebarEntry draws a name with its marker, and its detail beneath.
func drawSidebarEntry(dst *vt.Grid, r SidebarRow, y, limit int, theme Theme) {
	// The cursor is a marker in the margin rather than a highlight, so it does
	// not compete with the marker saying which entry is current.
	cursor := " "
	if r.Selected {
		cursor = "›"
	}
	style := theme.Sidebar
	if r.Active {
		style = theme.SidebarActive
	}

	x := writeString(dst, 0, y, cursor, style, limit)
	x = writeString(dst, x, y, stateCircle(r), theme.StateStyle(r.State, r.Running), limit)
	writeString(dst, x, y, truncate(r.Label, limit-x), style, limit)

	if r.Detail != "" {
		writeString(dst, 3, y+1, truncate(r.Detail, limit-3), theme.SidebarDetail, limit)
	}
}

// stateCircle is the mark beside an entry: filled for the one in view, hollow
// otherwise, coloured by what the agent is doing.
func stateCircle(r SidebarRow) string {
	if r.Active {
		return " ● "
	}
	return " ○ "
}

// SidebarRowAt returns the row under a point, and whether there is one.
//
// Rows are found by walking the same heights the drawing uses, so a two-line
// entry is one target: clicking a branch selects the space it belongs to,
// which is what it looks like it should do.
func SidebarRowAt(f Frame, x, y int) (SidebarRow, bool) {
	if x >= SidebarColumns(f, SidebarWidth) {
		return SidebarRow{}, false
	}
	at := 0
	for _, r := range f.SidebarRows {
		height := r.height()
		if y >= at && y < at+height {
			return r, true
		}
		at += height
	}
	return SidebarRow{}, false
}
