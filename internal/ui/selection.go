package ui

import (
	"encoding/base64"

	"github.com/sousaakira/tend/internal/vt"
)

// Selecting text is the one thing a terminal multiplexer takes away.
//
// Asking the terminal for mouse reporting is what makes panes clickable, and
// it is also what stops the terminal doing its own selection: the drag now
// belongs to the program, not to the window. Most terminals keep a way out
// under Shift, but it selects across the whole window — the other panes, the
// borders, the sidebar — which for a column of text in a split is not a way
// out at all.
//
// So the selection is drawn here, over the pane it started in, and copied to
// the clipboard through the terminal rather than through any one platform's
// tools.

// Selection is a range of cells in one pane, from where the drag started to
// where the pointer is now.
//
// It keeps both ends rather than a normalised pair, because which one moves
// depends on which way the drag went, and normalising early loses that.
type Selection struct {
	Pane uint64
	// Anchor is where the press landed and Cursor is where the pointer is,
	// both in the pane's own cells with the top-left at zero.
	AnchorX, AnchorY int
	CursorX, CursorY int
	// Scroll is how far back the pane was being read when the drag started.
	// A selection follows the text, not the screen, so scrolling under a
	// finished selection must not move what it covers.
	Scroll int
	// Dragging marks that the button is still down.
	Dragging bool
}

// Empty reports whether the selection covers nothing.
func (s Selection) Empty() bool {
	return s.Pane == 0 || (s.AnchorX == s.CursorX && s.AnchorY == s.CursorY)
}

// ordered returns the two ends with the earlier one first.
func (s Selection) ordered() (fromX, fromY, toX, toY int) {
	if s.AnchorY < s.CursorY || (s.AnchorY == s.CursorY && s.AnchorX <= s.CursorX) {
		return s.AnchorX, s.AnchorY, s.CursorX, s.CursorY
	}
	return s.CursorX, s.CursorY, s.AnchorX, s.AnchorY
}

// covers reports whether a cell of the pane is inside the selection.
//
// The shape is a run of text, not a rectangle: a selection spanning three
// lines takes the end of the first, all of the second and the start of the
// third, which is what dragging over prose is asking for.
func (s Selection) covers(x, y int) bool {
	if s.Empty() {
		return false
	}
	fromX, fromY, toX, toY := s.ordered()
	switch {
	case y < fromY || y > toY:
		return false
	case fromY == toY:
		return x >= fromX && x <= toX
	case y == fromY:
		return x >= fromX
	case y == toY:
		return x <= toX
	}
	return true
}

// Text is what the selection covers, as lines.
//
// Trailing blanks are dropped from every line but the last: a terminal pads
// its rows to the full width, and pasting that padding turns one line of code
// into one line of code followed by seventy spaces.
func (s Selection) Text(screen *vt.Screen) string {
	if s.Empty() || screen == nil {
		return ""
	}
	fromX, fromY, toX, toY := s.ordered()

	var out []byte
	for y := fromY; y <= toY; y++ {
		row := lineAt(screen, y, s.Scroll)
		if row == nil {
			continue
		}
		start, end := 0, row.Len()-1
		if y == fromY {
			start = fromX
		}
		if y == toY {
			end = toX
		}
		if y > fromY {
			out = append(out, '\n')
		}
		out = append(out, trimRight(runsOf(row, start, end))...)
	}
	return string(out)
}

// runsOf reads a row's runes between two columns.
func runsOf(row *vt.Row, from, to int) []byte {
	var out []byte
	for x := max(from, 0); x <= to && x < row.Len(); x++ {
		cell := row.Cell(x)
		if cell.Width == 0 {
			// The right half of a wide character holds no rune of its own.
			continue
		}
		r := cell.R
		if r == 0 {
			r = ' '
		}
		out = append(out, []byte(string(r))...)
	}
	return out
}

// trimRight drops trailing spaces, which a terminal pads its rows with.
func trimRight(line []byte) []byte {
	at := len(line)
	for at > 0 && line[at-1] == ' ' {
		at--
	}
	return line[:at]
}

// lineAt returns a pane's row, counting the scrollback the view is showing.
func lineAt(screen *vt.Screen, y, scroll int) *vt.Row {
	grid := screen.MainGrid()
	if scroll <= 0 {
		return grid.Line(y)
	}
	// Scrolled back, the top of the view is `scroll` lines into the history,
	// and the view runs on into the live screen once the history is spent.
	at := y - scroll
	if at < 0 {
		return grid.HistoryLine(grid.HistoryLen() + at)
	}
	return grid.Line(at)
}

// drawSelection reverses the cells a selection covers.
//
// It reverses what is there rather than painting a colour over it, so text
// that is already styled stays readable and the mark reads as a selection in
// any theme.
func drawSelection(dst *vt.Grid, f Frame) {
	if f.Selection == nil || f.Selection.Empty() {
		return
	}
	sel := *f.Selection
	for _, p := range f.Panes {
		if p.ID != sel.Pane {
			continue
		}
		inner := innerRect(p.Rect)
		for y := 0; y < inner.Rows; y++ {
			row := dst.Line(inner.Y + y)
			if row == nil {
				continue
			}
			for x := 0; x < inner.Cols; x++ {
				if !sel.covers(x, y) {
					continue
				}
				cell := row.Cell(inner.X + x)
				cell.Style.Attrs ^= vt.AttrReverse
				row.SetCell(inner.X+x, cell)
			}
		}
		return
	}
}

// SetClipboard is the escape sequence that puts text on the clipboard through
// the terminal, which works over ssh and needs no platform tool.
//
// Terminals cap how much they will accept and many refuse it unless told to
// allow it, so this is best-effort by nature: there is no reply to wait for
// and no way to know it landed.
func SetClipboard(text string) string {
	if text == "" {
		return ""
	}
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
}
