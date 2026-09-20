// Package ui draws a tend session.
//
// Drawing is pure: it takes a description of what to show and fills a grid of
// cells. Nothing here reads a socket, a terminal or a clock, so a whole
// screenful can be asserted in a test without any of those existing.
//
// The client owns this. A pane's contents arrive as its own terminal screen,
// and compositing them into one grid is presentation — which is why it lives
// on this side of the socket rather than in the server.
package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/sousaakira/tend/internal/config"
	"github.com/sousaakira/tend/internal/vt"
)

// Theme is every colour the interface uses, in one place so a change is one
// edit rather than a search.
type Theme struct {
	Border        vt.Style
	BorderFocused vt.Style
	Title         vt.Style
	TitleFocused  vt.Style

	Status      vt.Style
	StatusKey   vt.Style
	StatusAlert vt.Style

	Overlay      vt.Style
	OverlayTitle vt.Style

	Sidebar            vt.Style
	SidebarActive      vt.Style
	SidebarSelected    vt.Style
	SidebarDetail      vt.Style
	SidebarGroup       vt.Style
	SidebarGroupActive vt.Style

	Working vt.Style
	Blocked vt.Style
	Idle    vt.Style
	Unknown vt.Style
	Exited  vt.Style
}

// DefaultTheme uses the terminal's own palette rather than fixed colours, so
// tend looks like the rest of the user's terminal instead of fighting it.
func DefaultTheme() Theme {
	dim := vt.Style{Attrs: vt.AttrDim}
	return Theme{
		Border:        dim,
		BorderFocused: vt.Style{FG: vt.IndexedColor(4)},
		Title:         dim,
		TitleFocused:  vt.Style{FG: vt.IndexedColor(4), Attrs: vt.AttrBold},

		Status:      vt.Style{Attrs: vt.AttrReverse},
		StatusKey:   vt.Style{Attrs: vt.AttrReverse | vt.AttrBold},
		StatusAlert: vt.Style{Attrs: vt.AttrReverse | vt.AttrBold, FG: vt.IndexedColor(1)},

		Overlay:      vt.Style{Attrs: vt.AttrReverse},
		OverlayTitle: vt.Style{Attrs: vt.AttrReverse | vt.AttrBold},

		Sidebar:            vt.Style{},
		SidebarActive:      vt.Style{Attrs: vt.AttrBold},
		SidebarSelected:    vt.Style{Attrs: vt.AttrReverse | vt.AttrBold},
		SidebarDetail:      dim,
		SidebarGroup:       dim,
		SidebarGroupActive: vt.Style{FG: vt.IndexedColor(4), Attrs: vt.AttrBold},

		Working: vt.Style{FG: vt.IndexedColor(3)},
		Blocked: vt.Style{FG: vt.IndexedColor(1), Attrs: vt.AttrBold},
		Idle:    vt.Style{FG: vt.IndexedColor(2)},
		Unknown: dim,
		Exited:  dim,
	}
}

// ThemeFrom applies a user's colour choices over the defaults. An empty value
// keeps the default, so a file naming one colour changes one colour.
func ThemeFrom(c config.Theme) Theme {
	t := DefaultTheme()
	apply := func(target *vt.Style, value string) {
		if value == "" {
			return
		}
		parsed, ok := config.ParseColor(value)
		if !ok {
			return // rejected at load; nothing sensible to do here
		}
		if parsed.RGB {
			target.FG = vt.RGBColor(parsed.R, parsed.G, parsed.B)
		} else {
			target.FG = vt.IndexedColor(parsed.Index)
		}
		// A colour the user chose replaces dimming, which would fight it.
		target.Attrs &^= vt.AttrDim
	}
	apply(&t.Border, c.Border)
	apply(&t.BorderFocused, c.BorderFocused)
	apply(&t.Title, c.Border)
	apply(&t.TitleFocused, c.BorderFocused)
	apply(&t.Working, c.Working)
	apply(&t.Blocked, c.Blocked)
	apply(&t.Idle, c.Idle)
	return t
}

// StateStyle picks the colour for an agent state.
func (t Theme) StateStyle(state string, running bool) vt.Style {
	if !running {
		return t.Exited
	}
	switch state {
	case "working":
		return t.Working
	case "blocked":
		return t.Blocked
	case "idle":
		return t.Idle
	default:
		return t.Unknown
	}
}

// Rect is a region of the screen in cells.
type Rect struct {
	X, Y, Cols, Rows int
}

// Pane is one pane to draw.
type Pane struct {
	ID    uint64
	Rect  Rect
	Title string
	Agent string
	State string
	// Command is what the pane is running, used as a label until the pane
	// names itself. A pane called "pane" tells nobody anything.
	Command string

	// Screen is the pane's own terminal. Nil draws an empty frame, which is
	// what a pane looks like between being opened and its first output.
	Screen *vt.Screen

	Running bool
	Focused bool
}

// Tab is one tab in the bar across the top.
type Tab struct {
	ID     uint64
	Name   string
	Panes  int
	Active bool
	// Alert marks a tab holding an agent that needs an answer, so a blocked
	// agent in a tab you are not looking at is still visible.
	Alert bool
}

// Frame is everything to draw.
type Frame struct {
	Panes []Pane
	Tabs  []Tab

	Session   string
	Workspace string
	Tab       string
	// Offline marks that the session is unreachable. It is shown rather than
	// hidden: a frozen screen with no explanation is the worst version of
	// this, because the user cannot tell it from an agent that has stopped.
	Offline bool
	// Scroll is how far back a pane is being read, and ScrollDepth how far it
	// could go. Both are shown: a view that says only "scrolled" leaves the
	// user unable to tell a glance back from being lost in a long history.
	Scroll      int
	ScrollDepth int
	// Zoomed marks that one pane is filling the area, so the status bar can
	// say so — a zoomed pane and a session with one pane look identical
	// otherwise.
	Zoomed bool

	// Message is shown in place of the pane list, for a moment, after an
	// action or an error.
	Message string
	// Alert marks Message as a problem rather than a confirmation.
	Alert bool
	// Prefix marks that the prefix key is armed and the next key is a command.
	Prefix bool

	// Sidebar shows the spaces and agents down the left edge, and SidebarRows
	// is what it holds.
	Sidebar bool
	// Spaces and Agents are the sidebar's two lists. They are separate
	// because they answer different questions and because one sharing the
	// other's scroll meant the spaces pushed the agents off the bottom.
	Spaces SidebarSection
	Agents SidebarSection
	// SidebarSplit is the line the divider sits on. Zero means the sidebar
	// decides; anything else is where the user dragged it to.
	SidebarSplit int
	// Navigating marks that the list has the keyboard.
	Navigating bool

	// Prompt and PromptText are a line being typed, such as a new name. When
	// Prompt is set it is drawn as a box over the middle of the screen: the
	// status bar is where tend says things, not where the user says them.
	Prompt     string
	PromptText string
	// PromptSelected marks the text as the seed, about to be replaced by the
	// next keystroke. It is drawn differently so that is visible rather than
	// surprising.
	PromptSelected bool

	// Overlay is a panel drawn over the middle of the screen, for things that
	// do not fit on one status line. A list of keys is the obvious case: there
	// are fourteen of them, and cramming those into a status bar means
	// truncating exactly the ones somebody was looking for.
	Overlay []string

	// Menu is a context menu, opened on the thing it acts on. Nil when none
	// is open.
	Menu *Menu
}

// StatusRows is how many rows at the bottom the status bar occupies. Panes are
// laid out above it.
const StatusRows = 1

// TabRows is how many rows the tab bar takes for a given number of tabs.
//
// The bar is shown whenever there is a session, not only once there are two
// tabs. It carries the button that makes a new one, and a button that appears
// only after you already have what it creates is a button nobody finds.
func TabRows(tabs int) int {
	if tabs > 0 {
		return 1
	}
	return 0
}

// NewTabLabel is the button at the end of the bar.
const NewTabLabel = " + "

// TabSegment is where one entry of the tab bar was drawn, in columns.
//
// Drawing and hit-testing both work from these, so a click cannot land
// somewhere other than what it looks like it is on: there is one description
// of the layout, not two that have to agree.
type TabSegment struct {
	Tab uint64
	// New marks the button that creates a tab rather than selects one.
	New        bool
	Start, End int
}

// TabSegments lays out the bar across cols columns.
//
// The bar belongs to the panes, so it starts where they do rather than at the
// edge of the screen. The sidebar lists every space; the tabs list one space's
// tabs, and a bar running over the sidebar would read as though those tabs
// belonged to the whole session.
func TabSegments(f Frame, cols int) []TabSegment {
	var out []TabSegment
	x := SidebarColumns(f, cols)
	for _, tab := range f.Tabs {
		label := tabLabel(tab)
		width := runewidth.StringWidth(label)
		if x+width > cols {
			break
		}
		out = append(out, TabSegment{Tab: tab.ID, Start: x, End: x + width})
		x += width
	}
	if width := runewidth.StringWidth(NewTabLabel); x+width <= cols {
		out = append(out, TabSegment{New: true, Start: x, End: x + width})
	}
	return out
}

func tabLabel(tab Tab) string {
	label := tab.Name
	if label == "" {
		label = itoa(tab.ID)
	}
	return " " + label + " "
}

// TabAt reports what a click on the bar landed on.
func TabAt(f Frame, x, y, cols int) (tab uint64, newTab bool, ok bool) {
	if TabRows(len(f.Tabs)) == 0 || y != 0 || x < SidebarColumns(f, cols) {
		return 0, false, false
	}
	for _, seg := range TabSegments(f, cols) {
		if x >= seg.Start && x < seg.End {
			return seg.Tab, seg.New, true
		}
	}
	return 0, false, false
}

// Draw fills dst with the frame.
//
// dst is cleared first: a frame describes the whole screen, so anything left
// from the last one is stale by definition. The painter that sends this to a
// terminal is what avoids redrawing the parts that did not change.
func Draw(dst *vt.Grid, f Frame, theme Theme) {
	dst.Clear(vt.DefaultStyle)

	drawTabs(dst, f, theme)
	drawSidebar(dst, f, theme)
	for _, p := range f.Panes {
		drawPane(dst, p, theme)
	}
	drawStatus(dst, f, theme)

	// Last, so they sit over the panes rather than under them. The menu is
	// last of all: it is opened on top of whatever is already showing.
	if len(f.Overlay) > 0 {
		drawOverlay(dst, f.Overlay, theme)
	}
	if f.Menu != nil {
		drawMenu(dst, *f.Menu, theme)
	}
	// The field is last: it has the keyboard, so nothing may sit over it.
	drawPrompt(dst, f, theme)
}

// drawTabs draws the bar across the top.
func drawTabs(dst *vt.Grid, f Frame, theme Theme) {
	if TabRows(len(f.Tabs)) == 0 {
		return
	}
	row := dst.Line(0)
	if row == nil {
		return
	}
	for x := SidebarColumns(f, dst.Cols()); x < dst.Cols(); x++ {
		row.SetCell(x, vt.Cell{R: ' ', Style: theme.Status, Width: 1})
	}

	byID := make(map[uint64]Tab, len(f.Tabs))
	for _, tab := range f.Tabs {
		byID[tab.ID] = tab
	}

	for _, seg := range TabSegments(f, dst.Cols()) {
		if seg.New {
			writeString(dst, seg.Start, 0, NewTabLabel, theme.StatusKey, dst.Cols())
			continue
		}
		tab := byID[seg.Tab]
		style := theme.Status
		if tab.Active {
			style = theme.StatusKey
		}
		if tab.Alert && !tab.Active {
			// A blocked agent in a tab you are not looking at is the one thing
			// the bar exists to tell you.
			style = theme.StatusAlert
		}
		writeString(dst, seg.Start, 0, tabLabel(tab), style, dst.Cols())
	}
}

// drawOverlay puts a panel in the middle of the screen.
//
// A list too long for the window is laid out in two columns rather than being
// cut off. Cutting it drops the entries at the end, which for a key list is
// the half nobody has memorised yet.
func drawOverlay(dst *vt.Grid, lines []string, theme Theme) {
	if len(lines) == 0 {
		return
	}
	title, body := lines[0], lines[1:]

	columns := [][]string{body}
	if len(lines)+4 > dst.Rows() && len(body) > 1 {
		half := (len(body) + 1) / 2
		columns = [][]string{body[:half], body[half:]}
	}

	const gap = 2
	height := 0
	width := runewidth.StringWidth(title)
	inner := 0
	for _, col := range columns {
		height = max(height, len(col))
		colWidth := 0
		for _, line := range col {
			colWidth = max(colWidth, runewidth.StringWidth(line))
		}
		if inner > 0 {
			inner += gap
		}
		inner += colWidth
	}
	width = max(width, inner)

	box := Rect{Cols: min(width+6, dst.Cols()), Rows: min(height+5, dst.Rows())}
	box.X = (dst.Cols() - box.Cols) / 2
	box.Y = (dst.Rows() - box.Rows) / 2

	// Fill first: an overlay that lets the pane behind it show through is
	// unreadable, whatever it says.
	for y := box.Y; y < box.Y+box.Rows; y++ {
		row := dst.Line(y)
		if row == nil {
			continue
		}
		for x := box.X; x < box.X+box.Cols; x++ {
			row.SetCell(x, vt.Cell{R: ' ', Style: theme.Overlay, Width: 1})
		}
	}
	drawBox(dst, box, theme.OverlayTitle)

	limit := box.X + box.Cols - 2
	writeString(dst, box.X+3, box.Y+1, truncate(title, box.Cols-6), theme.OverlayTitle, limit)

	x := box.X + 3
	for _, col := range columns {
		colWidth := 0
		for _, line := range col {
			colWidth = max(colWidth, runewidth.StringWidth(line))
		}
		for i, line := range col {
			y := box.Y + 3 + i
			if y >= box.Y+box.Rows-1 {
				break
			}
			writeString(dst, x, y, truncate(line, limit-x), theme.Overlay, limit)
		}
		x += colWidth + gap
	}
}

// CursorPosition returns where the terminal's cursor belongs: inside the
// focused pane, at that pane's own cursor. Typing has to appear where the user
// is looking, which means the real cursor follows the focused pane's.
//
// While a field is up it belongs in the field instead, for the same reason:
// that is where the keystrokes are going.
func CursorPosition(f Frame, cols, rows int) (x, y int, visible bool) {
	if x, y, ok := PromptCursor(f, cols, rows); ok {
		return x, y, true
	}
	for _, p := range f.Panes {
		if !p.Focused || p.Screen == nil {
			continue
		}
		inner := innerRect(p.Rect)
		if inner.Cols <= 0 || inner.Rows <= 0 {
			return 0, 0, false
		}
		cur := p.Screen.Cursor()
		if cur.X >= inner.Cols || cur.Y >= inner.Rows {
			return 0, 0, false
		}
		return inner.X + cur.X, inner.Y + cur.Y, p.Screen.Modes().CursorVisible && p.Running
	}
	return 0, 0, false
}

// innerRect is the area inside a pane's border.
func innerRect(r Rect) Rect {
	return Rect{X: r.X + 1, Y: r.Y + 1, Cols: r.Cols - 2, Rows: r.Rows - 2}
}

// InnerSize is the terminal size a pane of this rect can hold. The client
// resizes panes to it, so the server's terminal matches what is drawn.
func InnerSize(r Rect) (cols, rows int) {
	inner := innerRect(r)
	return max(inner.Cols, 1), max(inner.Rows, 1)
}

func drawPane(dst *vt.Grid, p Pane, theme Theme) {
	if p.Rect.Cols < 2 || p.Rect.Rows < 2 {
		// Too small for a border and anything inside it. Drawing half a frame
		// looks like a glitch; drawing nothing looks like a small pane.
		return
	}

	border := theme.Border
	title := theme.Title
	if p.Focused {
		border = theme.BorderFocused
		title = theme.TitleFocused
	}
	drawBox(dst, p.Rect, border)
	drawPaneTitle(dst, p, title, theme)

	if p.Screen == nil {
		return
	}
	blitScreen(dst, innerRect(p.Rect), p.Screen)
}

// drawPaneTitle writes the label into the top border.
func drawPaneTitle(dst *vt.Grid, p Pane, style vt.Style, theme Theme) {
	label := p.Title
	if label == "" {
		label = p.Agent
	}
	if label == "" {
		label = p.Command
	}
	if label == "" {
		label = "pane"
	}

	prefix := " " + itoa(p.ID) + " "
	text := prefix + label + " "

	// The state marker carries its own colour, so it is written separately
	// rather than folded into the title's style.
	marker := stateMarker(p.State, p.Running)

	avail := p.Rect.Cols - 4
	if avail < 1 {
		return
	}
	text = truncate(text, avail-runewidth.StringWidth(marker))

	x := p.Rect.X + 1
	x = writeString(dst, x, p.Rect.Y, text, style, p.Rect.X+p.Rect.Cols-1)
	writeString(dst, x, p.Rect.Y, marker, theme.StateStyle(p.State, p.Running), p.Rect.X+p.Rect.Cols-1)
}

func stateMarker(state string, running bool) string {
	if !running {
		return "exited "
	}
	switch state {
	case "working":
		return "● "
	case "blocked":
		return "▲ "
	case "idle":
		return "○ "
	default:
		return ""
	}
}

// blitScreen copies a pane's terminal into the grid.
func blitScreen(dst *vt.Grid, area Rect, screen *vt.Screen) {
	if area.Cols <= 0 || area.Rows <= 0 {
		return
	}
	src := screen.Grid()

	for y := 0; y < area.Rows && y < src.Rows(); y++ {
		srcRow := src.Line(y)
		dstRow := dst.Line(area.Y + y)
		if srcRow == nil || dstRow == nil {
			continue
		}
		for x := 0; x < area.Cols && x < srcRow.Len(); x++ {
			cell := srcRow.Cell(x)
			// A wide character whose second half falls outside the area would
			// be drawn with nothing to occupy the column it needs, so it is
			// replaced rather than clipped in half.
			if cell.Width == 2 && x+1 >= area.Cols {
				cell = vt.Cell{R: ' ', Style: cell.Style, Width: 1}
			}
			dstRow.SetCell(area.X+x, cell)
			for _, mark := range srcRow.Combining(x) {
				dstRow.AddCombining(area.X+x, mark)
			}
		}
	}
}

// drawBox draws a single-line frame around the rect.
func drawBox(dst *vt.Grid, r Rect, style vt.Style) {
	right := r.X + r.Cols - 1
	bottom := r.Y + r.Rows - 1

	for x := r.X + 1; x < right; x++ {
		setCell(dst, x, r.Y, '─', style)
		setCell(dst, x, bottom, '─', style)
	}
	for y := r.Y + 1; y < bottom; y++ {
		setCell(dst, r.X, y, '│', style)
		setCell(dst, right, y, '│', style)
	}
	setCell(dst, r.X, r.Y, '┌', style)
	setCell(dst, right, r.Y, '┐', style)
	setCell(dst, r.X, bottom, '└', style)
	setCell(dst, right, bottom, '┘', style)
}

func drawStatus(dst *vt.Grid, f Frame, theme Theme) {
	y := dst.Rows() - StatusRows
	if y < 0 {
		return
	}
	row := dst.Line(y)
	if row == nil {
		return
	}
	// Fill the whole row first, so the bar reads as one band rather than as
	// text floating on the pane above it.
	for x := 0; x < dst.Cols(); x++ {
		row.SetCell(x, vt.Cell{R: ' ', Style: theme.Status, Width: 1})
	}

	limit := dst.Cols()
	x := 0

	if f.Offline {
		x = writeString(dst, x, y, " OFFLINE ", theme.StatusAlert, limit)
	}
	if f.Prefix {
		x = writeString(dst, x, y, " PREFIX ", theme.StatusAlert, limit)
	}
	if f.Navigating {
		x = writeString(dst, x, y, " NAVIGATE ", theme.StatusKey, limit)
	}

	left := " " + f.Session
	if f.Workspace != "" {
		left += " · " + f.Workspace
	}
	if f.Tab != "" {
		left += " · " + f.Tab
	}
	if f.Zoomed {
		left += " · zoom"
	}
	if f.Scroll > 0 {
		left += " · scroll " + itoa(uint64(f.Scroll)) + "/" + itoa(uint64(f.ScrollDepth))
	}
	x = writeString(dst, x, y, left+"  ", theme.StatusKey, limit)

	if f.Message != "" {
		style := theme.Status
		if f.Alert {
			style = theme.StatusAlert
		}
		writeString(dst, x, y, f.Message, style, limit)
		return
	}

	for _, p := range f.Panes {
		label := itoa(p.ID)
		if p.Agent != "" {
			label += ":" + p.Agent
		}
		style := theme.Status
		if p.Focused {
			style = theme.StatusKey
		}
		x = writeString(dst, x, y, " "+label, style, limit)
		x = writeString(dst, x, y, " "+strings.TrimSpace(stateMarker(p.State, p.Running)), theme.Status, limit)
		if x >= limit {
			break
		}
	}
}

// --- small helpers ---------------------------------------------------------

func setCell(dst *vt.Grid, x, y int, r rune, style vt.Style) {
	row := dst.Line(y)
	if row == nil || x < 0 || x >= row.Len() {
		return
	}
	row.SetCell(x, vt.Cell{R: r, Style: style, Width: 1})
}

// writeString draws text and returns the column after it. Wide characters
// take the two columns they need, so the caller's next write does not land on
// top of one.
func writeString(dst *vt.Grid, x, y int, text string, style vt.Style, limit int) int {
	row := dst.Line(y)
	if row == nil {
		return x
	}
	for _, r := range text {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			row.AddCombining(x-1, r)
			continue
		}
		if x+w > limit || x+w > row.Len() {
			break
		}
		row.SetCell(x, vt.Cell{R: r, Style: style, Width: uint8(w)})
		for i := 1; i < w; i++ {
			row.SetCell(x+i, vt.Cell{Style: style, Width: 0})
		}
		x += w
	}
	return x
}

// truncate shortens text to fit a number of columns, counting the width each
// rune actually occupies rather than the number of runes.
func truncate(text string, cols int) string {
	if cols <= 0 {
		return ""
	}
	if runewidth.StringWidth(text) <= cols {
		return text
	}
	var b strings.Builder
	used := 0
	for _, r := range text {
		w := runewidth.RuneWidth(r)
		if used+w > cols-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	b.WriteRune('…')
	return b.String()
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// fill paints a run of blank cells on one row, which is how a selection band
// is drawn under text that is written over it afterwards.
func fill(dst *vt.Grid, y, from, to int, style vt.Style) {
	row := dst.Line(y)
	if row == nil {
		return
	}
	for x := max(from, 0); x < min(to, dst.Cols()); x++ {
		row.SetCell(x, vt.Cell{R: ' ', Style: style, Width: 1})
	}
}
