package ui

import "github.com/sousaakira/tend/internal/vt"

// The agent list is the one view that answers "which one stopped?" without
// looking at every pane. It is grouped by workspace and tab because that is
// where an agent actually is, and reaching it means going there.

// SidebarKind is what a row describes.
type SidebarKind uint8

const (
	SidebarWorkspace SidebarKind = iota
	SidebarTab
	SidebarPane
)

// SidebarRow is one line of the list.
type SidebarRow struct {
	Kind   SidebarKind
	Label  string
	Detail string

	// Pane, Tab and Workspace say what a click on this row selects. Every row
	// carries the ones that apply to it, so clicking a heading goes there
	// rather than doing nothing — a line that looks clickable and is not is
	// worse than one that is not drawn.
	Pane      uint64
	Tab       uint64
	Workspace uint64

	State   string
	Running bool

	// Active marks what is currently being shown; Selected marks the
	// navigation cursor. They are separate because moving the cursor must not
	// move the view — a user looking down the list has not left the pane they
	// are working in.
	Active   bool
	Selected bool
}

// SidebarWidth is how many columns the list takes when shown. Wide enough for
// an agent name and its state, narrow enough that it costs a pane little.
const SidebarWidth = 26

// drawSidebar draws the list down the left edge.
func drawSidebar(dst *vt.Grid, f Frame, theme Theme) {
	if !f.Sidebar || len(f.SidebarRows) == 0 {
		return
	}
	top := TabRows(len(f.Tabs))
	bottom := dst.Rows() - StatusRows
	width := min(SidebarWidth, dst.Cols())

	for y := top; y < bottom; y++ {
		row := dst.Line(y)
		if row == nil {
			continue
		}
		for x := 0; x < width; x++ {
			row.SetCell(x, vt.Cell{R: ' ', Style: theme.Sidebar, Width: 1})
		}
		// A rule down the right edge, so the list reads as its own column
		// rather than as text spilling into the first pane.
		row.SetCell(width-1, vt.Cell{R: '│', Style: theme.Border, Width: 1})
	}

	limit := width - 1
	y := top
	for _, r := range f.SidebarRows {
		if y >= bottom {
			break
		}
		drawSidebarRow(dst, r, y, limit, theme)
		y++
	}
}

func drawSidebarRow(dst *vt.Grid, r SidebarRow, y, limit int, theme Theme) {
	switch r.Kind {
	case SidebarWorkspace:
		style := theme.SidebarGroup
		if r.Active {
			style = theme.SidebarGroupActive
		}
		writeString(dst, 0, y, truncate(" "+r.Label, limit), style, limit)

	case SidebarTab:
		style := theme.Sidebar
		if r.Active {
			style = theme.SidebarActive
		}
		writeString(dst, 0, y, truncate("  "+r.Label, limit), style, limit)

	case SidebarPane:
		// The cursor is drawn as a marker in the margin rather than as a
		// highlight, so it does not compete with the marker that says which
		// pane is actually focused.
		cursor := " "
		if r.Selected {
			cursor = "›"
		}
		style := theme.Sidebar
		if r.Active {
			style = theme.SidebarActive
		}

		x := writeString(dst, 0, y, cursor+"   ", style, limit)
		marker := stateMarker(r.State, r.Running)
		label := truncate(r.Label, limit-x-len([]rune(marker))-1)
		x = writeString(dst, x, y, label+" ", style, limit)
		writeString(dst, x, y, marker, theme.StateStyle(r.State, r.Running), limit)
	}
}

// SidebarRowAt returns the list row under a point, and whether there is one.
func SidebarRowAt(f Frame, x, y int) (SidebarRow, bool) {
	if !f.Sidebar || x >= SidebarWidth {
		return SidebarRow{}, false
	}
	index := y - TabRows(len(f.Tabs))
	if index < 0 || index >= len(f.SidebarRows) {
		return SidebarRow{}, false
	}
	return f.SidebarRows[index], true
}
