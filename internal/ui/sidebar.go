package ui

import (
	"strings"

	"github.com/sousaakira/tend/internal/vt"
)

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
	// SidebarSpaceGroup is a heading over spaces kept together, which folds.
	SidebarSpaceGroup
	// SidebarAction is a row that does something rather than going somewhere.
	SidebarAction
	// SidebarBlank is a spacer.
	SidebarBlank
)

// Action names carried on an action row.
const (
	ActionNewSpace      = "new-space"
	ActionOpenMenu      = "open-menu"
	ActionToggleGrouped = "toggle-grouped"
	// ActionToggleGroup folds or unfolds the group named by the row.
	ActionToggleGroup = "toggle-group"
)

// SidebarRow is one entry. A two-line entry is one row: the detail is drawn
// beneath the name, so selecting and hit-testing count entries rather than
// lines.
type SidebarRow struct {
	Kind   SidebarKind
	Label  string
	Detail string
	// Trailing is drawn against the right edge, such as the "grouped" toggle
	// or the "menu" button. TrailingAction is what clicking it does, which is
	// not what clicking the rest of the row does.
	Trailing       string
	TrailingAction string

	// Pane, Tab and Workspace say where selecting this row goes. Action names
	// what it does instead.
	Pane      uint64
	Tab       uint64
	Workspace uint64
	Action    string

	// Group is the group a row belongs to, or names when it is the heading
	// over one. Depth indents a row under its heading.
	Group  string
	Depth  int
	Folded bool

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

// TrailingStart is the column where a row's trailing label begins, or -1 when
// it has none or there is no room for it.
//
// Drawing and hit-testing both ask this, so a button cannot be drawn in one
// place and clicked in another.
func TrailingStart(r SidebarRow) int {
	if r.Trailing == "" {
		return -1
	}
	at := SidebarWidth - 2 - len([]rune(r.Trailing))
	if at <= 1 {
		return -1
	}
	return at
}

// SidebarHeight is how many lines the list has to work with.
func SidebarHeight(rows int) int { return max(rows-StatusRows, 0) }

// SidebarMaxScroll is the furthest the list can be scrolled and still fill the
// space: scrolling past that would leave a gap at the bottom and nothing new
// at the top.
//
// It counts in entries rather than lines. A two-line entry is one thing, and
// scrolling half of one off the top would put a branch under a heading it does
// not belong to.
func SidebarMaxScroll(f Frame, rows int) int {
	height := SidebarHeight(rows)
	total := 0
	for _, r := range f.SidebarRows {
		total += r.height()
	}
	if total <= height {
		return 0
	}
	at := len(f.SidebarRows)
	fits := 0
	for at > 0 {
		next := fits + f.SidebarRows[at-1].height()
		if next > height {
			break
		}
		fits = next
		at--
	}
	return at
}

// sidebarScroll is the offset actually used, which is the one asked for
// clamped to what there is to scroll.
func sidebarScroll(f Frame, rows int) int {
	return min(max(f.SidebarScroll, 0), SidebarMaxScroll(f, rows))
}

// drawSidebar draws the lists down the left edge.
func drawSidebar(dst *vt.Grid, f Frame, theme Theme) {
	if !f.Sidebar {
		return
	}
	// The sidebar runs from the very top: it is not inside the tab bar's
	// space, the tab bar is inside its own.
	top := 0
	bottom := SidebarHeight(dst.Rows())
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
	from := sidebarScroll(f, dst.Rows())
	y := top
	last := from
	for _, r := range f.SidebarRows[min(from, len(f.SidebarRows)):] {
		if y+r.height() > bottom {
			break
		}
		drawSidebarRow(dst, r, y, limit, theme)
		y += r.height()
		last++
	}

	// Say which way there is more. A list that silently ends is one the user
	// believes they have seen all of, which is how a waiting agent goes
	// unnoticed below the fold.
	if from > 0 {
		writeString(dst, limit-1, top, "↑", theme.SidebarGroup, width)
	}
	if last < len(f.SidebarRows) {
		writeString(dst, limit-1, bottom-1, "↓", theme.SidebarGroup, width)
	}
}

func drawSidebarRow(dst *vt.Grid, r SidebarRow, y, limit int, theme Theme) {
	switch r.Kind {
	case SidebarBlank:
		return

	case SidebarHeading:
		writeString(dst, 1, y, truncate(r.Label, limit-1), theme.SidebarGroup, limit)
		drawTrailing(dst, r, y, limit, theme)

	case SidebarGroup:
		writeString(dst, 3, y, truncate(r.Label, limit-3), theme.SidebarGroup, limit)

	case SidebarSpaceGroup:
		// The triangle points at what it will do: down when the group is open
		// and its spaces are below, right when it is shut and they are not.
		marker := "▼ "
		if r.Folded {
			marker = "▶ "
		}
		style := theme.SidebarGroup
		if r.Active {
			style = theme.SidebarGroupActive
		}
		x := writeString(dst, 1, y, marker, style, limit)
		writeString(dst, x, y, truncate(r.Label, limit-x), style, limit)
		drawTrailing(dst, r, y, limit, theme)

	case SidebarAction:
		style := theme.SidebarGroup
		if r.Selected {
			style = theme.SidebarActive
		}
		writeString(dst, 1, y, truncate(r.Label, limit-1), style, limit)
		drawTrailing(dst, r, y, limit, theme)

	case SidebarSpace, SidebarAgent:
		drawSidebarEntry(dst, r, y, limit, theme)
	}
}

// drawTrailing puts a row's button against the right edge. A toggle belongs
// at the edge of what it toggles, not trailing the words that name it.
func drawTrailing(dst *vt.Grid, r SidebarRow, y, limit int, theme Theme) {
	at := TrailingStart(r)
	if at < 0 {
		return
	}
	style := theme.SidebarGroup
	if r.Active {
		style = theme.SidebarGroupActive
	}
	writeString(dst, at, y, r.Trailing, style, limit)
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
	mark := theme.StateStyle(r.State, r.Running)
	if r.Active {
		// The current entry is a band rather than a word that changed weight:
		// scanning twenty rows, weight is not enough to find one.
		style = theme.SidebarSelected
		mark = style
		fill(dst, y, 0, limit, style)
	}

	x := writeString(dst, 0, y, cursor, style, limit)
	x = writeString(dst, x, y, indent(r.Depth), style, limit)
	x = writeString(dst, x, y, stateCircle(r), mark, limit)
	writeString(dst, x, y, truncate(r.Label, limit-x), style, limit)

	if r.Detail != "" {
		detail := theme.SidebarDetail
		if r.Active {
			detail = style
			fill(dst, y+1, 0, limit, style)
		}
		// Lined up under the name rather than under the marker: the detail
		// belongs to the name, and a column of branches that steps in and out
		// with the tree is harder to read down than one that does not.
		at := 3 + 2*r.Depth
		writeString(dst, at, y+1, truncate(r.Detail, limit-at), detail, limit)
	}
}

// indent is the space a row sits in under its heading.
func indent(depth int) string {
	if depth <= 0 {
		return ""
	}
	return strings.Repeat("  ", depth)
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
func SidebarRowAt(f Frame, x, y, rows int) (SidebarRow, bool) {
	if x >= SidebarColumns(f, SidebarWidth) {
		return SidebarRow{}, false
	}
	at := 0
	from := sidebarScroll(f, rows)
	for _, r := range f.SidebarRows[min(from, len(f.SidebarRows)):] {
		height := r.height()
		if y >= at && y < at+height {
			// A trailing button is its own target. Without this the "menu"
			// beside "new" would create a space, which is the one thing
			// somebody reaching for a menu did not ask for.
			if start := TrailingStart(r); r.TrailingAction != "" && start >= 0 && x >= start && y == at {
				r.Action = r.TrailingAction
			}
			return r, true
		}
		at += height
	}
	return SidebarRow{}, false
}

// SidebarRevealScroll is the smallest offset that brings an entry into view,
// given where the list is scrolled now.
//
// Smallest on purpose: jumping to another space should move the list only as
// far as it must, so the entries around the one being left stay where the eye
// last saw them.
func SidebarRevealScroll(f Frame, rows, index int) int {
	if index < 0 || index >= len(f.SidebarRows) {
		return sidebarScroll(f, rows)
	}
	at := sidebarScroll(f, rows)
	if index < at {
		return index
	}

	height := SidebarHeight(rows)
	for {
		used := 0
		for i := at; i <= index; i++ {
			used += f.SidebarRows[i].height()
		}
		if used <= height || at >= index {
			return at
		}
		at++
	}
}

// SidebarActiveRow is the entry the list should keep in view: the navigation
// cursor when it is up, and otherwise the space being looked at.
func SidebarActiveRow(f Frame) int {
	fallback := -1
	for i, r := range f.SidebarRows {
		if r.Selected {
			return i
		}
		if fallback < 0 && r.Kind == SidebarSpace && r.Active {
			fallback = i
		}
	}
	return fallback
}
