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

// The sidebar is two lists, not one scrolling column of both.
//
// They answer different questions — where else could I be, and which agent
// needs me — and one of them is usually the reason the program is open. Sharing
// a single scroll meant the spaces pushed the agents off the bottom, so the
// list that matters most was the one you could not see. They get a region each
// now, with a divider the user can move, and each scrolls on its own.

// SidebarSection is one of the two lists.
type SidebarSection struct {
	Rows []SidebarRow
	// Scroll is how many entries are scrolled off the top of this list. It is
	// clamped where it is drawn, so a client need not track what fits.
	Scroll int
}

// SidebarHeight is how many lines the sidebar has, divider included.
func SidebarHeight(rows int) int { return max(rows-StatusRows, 0) }

// sidebarMinSection is the fewest lines a section keeps when the divider is
// dragged at it. One line is not a list, it is a heading with nothing under it.
const sidebarMinSection = 3

// SidebarSplitAt is the line the divider sits on: the spaces list is above it
// and the agents list below.
//
// A split of zero means "decide for me", which is what a client that has never
// been dragged passes. The answer is as much as the spaces need and no more
// than half, so a session with two spaces does not spend half the column on
// them and a session with twenty does not bury the agents.
func SidebarSplitAt(f Frame, rows int) int {
	height := SidebarHeight(rows)
	if height < 2*sidebarMinSection+1 {
		// No room to divide. The spaces take what there is; the agents list
		// is the one that can be reached by other means.
		return height
	}

	at := f.SidebarSplit
	if at <= 0 {
		at = min(sectionHeight(f.Spaces), height/2)
	}
	return min(max(at, sidebarMinSection), height-sidebarMinSection-1)
}

// sectionHeight is how many lines a section's entries would take in full.
func sectionHeight(s SidebarSection) int {
	total := 0
	for _, r := range s.Rows {
		total += r.height()
	}
	return total
}

// pinnedRows is how many rows at the top of a section stay put while the rest
// scrolls under them.
//
// It is the heading, when there is one. A list whose title scrolls away leaves
// the reader looking at names with nothing saying what they are names of, and
// the toggle that lives in the heading would be unreachable at the same time.
func pinnedRows(s SidebarSection) int {
	if len(s.Rows) > 0 && s.Rows[0].Kind == SidebarHeading {
		return 1
	}
	return 0
}

// scrollable splits a section into the part that stays and the part that moves.
func scrollable(s SidebarSection, height int) (pinned []SidebarRow, rest []SidebarRow, room int) {
	at := pinnedRows(s)
	pinned = s.Rows[:at]
	for _, r := range pinned {
		height -= r.height()
	}
	return pinned, s.Rows[at:], max(height, 0)
}

// sidebarRegions returns the lines each list is drawn in, and the divider row.
// A divider of -1 means there is no room for one.
func sidebarRegions(f Frame, rows int) (spaces, agents Rect, divider int) {
	height := SidebarHeight(rows)
	at := SidebarSplitAt(f, rows)
	if at >= height {
		return Rect{Y: 0, Rows: height}, Rect{}, -1
	}
	return Rect{Y: 0, Rows: at},
		Rect{Y: at + 1, Rows: height - at - 1},
		at
}

// SidebarMaxScroll is the furthest a section can be scrolled and still fill
// its region: scrolling past that would leave a gap at the bottom and nothing
// new at the top.
//
// It counts in entries rather than lines. A two-line entry is one thing, and
// scrolling half of one off the top would put a branch under a heading it does
// not belong to.
func SidebarMaxScroll(s SidebarSection, height int) int {
	_, rest, room := scrollable(s, height)
	total := 0
	for _, r := range rest {
		total += r.height()
	}
	if total <= room {
		return 0
	}
	at := len(rest)
	fits := 0
	for at > 0 {
		next := fits + rest[at-1].height()
		if next > room {
			break
		}
		fits = next
		at--
	}
	return at
}

// sidebarScroll is the offset actually used, which is the one asked for
// clamped to what there is to scroll.
func sidebarScroll(s SidebarSection, height int) int {
	return min(max(s.Scroll, 0), SidebarMaxScroll(s, height))
}

// drawSidebar draws the two lists down the left edge.
func drawSidebar(dst *vt.Grid, f Frame, theme Theme) {
	if !f.Sidebar {
		return
	}
	// The sidebar runs from the very top: it is not inside the tab bar's
	// space, the tab bar is inside its own.
	bottom := SidebarHeight(dst.Rows())
	width := SidebarColumns(f, dst.Cols())

	for y := 0; y < bottom; y++ {
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

	spaces, agents, divider := sidebarRegions(f, dst.Rows())
	drawSection(dst, f.Spaces, spaces, width, theme)
	if divider >= 0 {
		drawSidebarDivider(dst, divider, width, theme)
		drawSection(dst, f.Agents, agents, width, theme)
	}
}

// drawSidebarDivider draws the line between the lists.
//
// It is drawn as something to grab rather than as a rule: the handle in the
// middle is the only thing saying the line can be moved, and a plain rule
// would read as decoration.
func drawSidebarDivider(dst *vt.Grid, y, width int, theme Theme) {
	row := dst.Line(y)
	if row == nil {
		return
	}
	for x := 0; x < width-1; x++ {
		row.SetCell(x, vt.Cell{R: '─', Style: theme.Border, Width: 1})
	}
	if handle := (width - 1 - 4) / 2; handle > 0 {
		writeString(dst, handle, y, "────", theme.SidebarGroupActive, width-1)
	}
}

// drawSection draws one list inside its region.
func drawSection(dst *vt.Grid, s SidebarSection, region Rect, width int, theme Theme) {
	if region.Rows <= 0 {
		return
	}
	limit := width - 1
	pinned, rest, _ := scrollable(s, region.Rows)
	from := sidebarScroll(s, region.Rows)

	y := region.Y
	for _, r := range pinned {
		drawSidebarRow(dst, r, y, limit, theme)
		y += r.height()
	}

	head := y
	last := from
	for _, r := range rest[min(from, len(rest)):] {
		if y+r.height() > region.Y+region.Rows {
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
		writeString(dst, limit-1, head, "↑", theme.SidebarGroup, width)
	}
	if last < len(rest) {
		writeString(dst, limit-1, region.Y+region.Rows-1, "↓", theme.SidebarGroup, width)
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

// SidebarPlace names which of the two lists a point is in.
type SidebarPlace uint8

const (
	// SidebarNowhere is outside the sidebar.
	SidebarNowhere SidebarPlace = iota
	// SidebarSpacesList and SidebarAgentsList are the two regions.
	SidebarSpacesList
	SidebarAgentsList
	// SidebarDivider is the line between them, which can be dragged.
	SidebarDivider
)

// SidebarPlaceAt says what is under a point.
//
// Drawing and hit-testing share sidebarRegions, so a click cannot land
// somewhere other than what it looks like it is on.
func SidebarPlaceAt(f Frame, x, y, rows int) SidebarPlace {
	if !f.Sidebar || x >= SidebarColumns(f, SidebarWidth) || y >= SidebarHeight(rows) {
		return SidebarNowhere
	}
	spaces, agents, divider := sidebarRegions(f, rows)
	switch {
	case y == divider:
		return SidebarDivider
	case y >= spaces.Y && y < spaces.Y+spaces.Rows:
		return SidebarSpacesList
	case agents.Rows > 0 && y >= agents.Y && y < agents.Y+agents.Rows:
		return SidebarAgentsList
	}
	return SidebarNowhere
}

// SidebarRowAt returns the row under a point, and whether there is one.
//
// Rows are found by walking the same heights the drawing uses, so a two-line
// entry is one target: clicking a branch selects the space it belongs to,
// which is what it looks like it should do.
func SidebarRowAt(f Frame, x, y, rows int) (SidebarRow, bool) {
	spaces, agents, _ := sidebarRegions(f, rows)
	switch SidebarPlaceAt(f, x, y, rows) {
	case SidebarSpacesList:
		return rowInSection(f.Spaces, spaces, x, y)
	case SidebarAgentsList:
		return rowInSection(f.Agents, agents, x, y)
	}
	return SidebarRow{}, false
}

func rowInSection(s SidebarSection, region Rect, x, y int) (SidebarRow, bool) {
	pinned, rest, _ := scrollable(s, region.Rows)
	from := sidebarScroll(s, region.Rows)

	at := region.Y
	walk := append(append([]SidebarRow{}, pinned...), rest[min(from, len(rest)):]...)
	for _, r := range walk {
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
// given where a section is scrolled now.
//
// Smallest on purpose: jumping to another space should move the list only as
// far as it must, so the entries around the one being left stay where the eye
// last saw them.
func SidebarRevealScroll(s SidebarSection, height, index int) int {
	_, rest, room := scrollable(s, height)
	index -= pinnedRows(s)
	if index < 0 || index >= len(rest) {
		// A pinned row is always in view, so nothing needs to move for it.
		return sidebarScroll(s, height)
	}

	at := sidebarScroll(s, height)
	if index < at {
		return index
	}
	for {
		used := 0
		for i := at; i <= index; i++ {
			used += rest[i].height()
		}
		if used <= room || at >= index {
			return at
		}
		at++
	}
}

// SidebarActiveRow is the entry a section should keep in view: the navigation
// cursor when it is up, and otherwise whatever is current.
func SidebarActiveRow(s SidebarSection) int {
	fallback := -1
	for i, r := range s.Rows {
		if r.Selected {
			return i
		}
		if fallback < 0 && r.Active && (r.Kind == SidebarSpace || r.Kind == SidebarAgent) {
			fallback = i
		}
	}
	return fallback
}

// SidebarRegions is where each list is drawn, for a client that needs to know
// how tall a section is before it can scroll or reveal inside it.
func SidebarRegions(f Frame, rows int) (spaces, agents Rect) {
	spaces, agents, _ = sidebarRegions(f, rows)
	return spaces, agents
}
