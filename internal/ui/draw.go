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

		Working: vt.Style{FG: vt.IndexedColor(3)},
		Blocked: vt.Style{FG: vt.IndexedColor(1), Attrs: vt.AttrBold},
		Idle:    vt.Style{FG: vt.IndexedColor(2)},
		Unknown: dim,
		Exited:  dim,
	}
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

// Frame is everything to draw.
type Frame struct {
	Panes []Pane

	Session   string
	Workspace string
	Tab       string

	// Message is shown in place of the pane list, for a moment, after an
	// action or an error.
	Message string
	// Alert marks Message as a problem rather than a confirmation.
	Alert bool
	// Prefix marks that the prefix key is armed and the next key is a command.
	Prefix bool

	// Overlay is a panel drawn over the middle of the screen, for things that
	// do not fit on one status line. A list of keys is the obvious case: there
	// are fourteen of them, and cramming those into a status bar means
	// truncating exactly the ones somebody was looking for.
	Overlay []string
}

// StatusRows is how many rows at the bottom the status bar occupies. Panes are
// laid out above it.
const StatusRows = 1

// Draw fills dst with the frame.
//
// dst is cleared first: a frame describes the whole screen, so anything left
// from the last one is stale by definition. The painter that sends this to a
// terminal is what avoids redrawing the parts that did not change.
func Draw(dst *vt.Grid, f Frame, theme Theme) {
	dst.Clear(vt.DefaultStyle)

	for _, p := range f.Panes {
		drawPane(dst, p, theme)
	}
	drawStatus(dst, f, theme)

	// Last, so it sits over the panes rather than under them.
	if len(f.Overlay) > 0 {
		drawOverlay(dst, f.Overlay, theme)
	}
}

// drawOverlay puts a panel in the middle of the screen.
func drawOverlay(dst *vt.Grid, lines []string, theme Theme) {
	width := 0
	for _, line := range lines {
		width = max(width, runewidth.StringWidth(line))
	}
	// Two cells of padding each side, plus the border.
	box := Rect{Cols: width + 6, Rows: len(lines) + 4}
	if box.Cols > dst.Cols() {
		box.Cols = dst.Cols()
	}
	if box.Rows > dst.Rows() {
		box.Rows = dst.Rows()
	}
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
	for i, line := range lines {
		y := box.Y + 2 + i
		if y >= box.Y+box.Rows-1 {
			break
		}
		style := theme.Overlay
		if i == 0 {
			style = theme.OverlayTitle
		}
		writeString(dst, box.X+3, y, truncate(line, box.Cols-6), style, limit)
	}
}

// CursorPosition returns where the terminal's cursor belongs: inside the
// focused pane, at that pane's own cursor. Typing has to appear where the user
// is looking, which means the real cursor follows the focused pane's.
func CursorPosition(f Frame) (x, y int, visible bool) {
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

	if f.Prefix {
		x = writeString(dst, x, y, " PREFIX ", theme.StatusAlert, limit)
	}

	left := " " + f.Session
	if f.Workspace != "" {
		left += " · " + f.Workspace
	}
	if f.Tab != "" {
		left += " · " + f.Tab
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
