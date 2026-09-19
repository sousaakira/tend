package vt

import "strconv"

// Rendering turns a grid back into the escape sequences that reproduce it.
//
// The server uses it to put a pane on the wire with its colours intact, and
// the client uses it to paint composited panes onto the real terminal. Both
// are the same problem — a grid of styled cells, and a stream that has to
// arrive at that state — so both use the same encoder.
//
// The output is deliberately unclever. Every cell is written, including
// trailing blanks, rather than using erase-to-end-of-line: an erase paints in
// the current background, so getting it right means tracking what the
// background was, and getting it wrong leaves coloured streaks that are very
// hard to trace back here. Runs of identical style still cost nothing, which
// is where nearly all of the saving is.

// encoder accumulates escape sequences, remembering the style it has already
// emitted so an unchanged run writes only its text.
type encoder struct {
	buf   []byte
	style Style
	// styleKnown is false until the first style is written, so the first cell
	// always emits one rather than assuming the terminal starts clean.
	styleKnown bool
}

func (e *encoder) reset() {
	e.buf = e.buf[:0]
	e.style = DefaultStyle
	e.styleKnown = false
}

// moveTo emits absolute cursor positioning, which is 1-based on the wire.
func (e *encoder) moveTo(x, y int) {
	e.buf = append(e.buf, 0x1b, '[')
	e.buf = strconv.AppendInt(e.buf, int64(y+1), 10)
	e.buf = append(e.buf, ';')
	e.buf = strconv.AppendInt(e.buf, int64(x+1), 10)
	e.buf = append(e.buf, 'H')
}

// setStyle emits the transition to s.
//
// A change always starts from a reset rather than computing the minimal set of
// attributes to add and remove. Working out that a bold-red run becomes a
// plain-blue one takes more code than it saves bytes, and every bug in it
// shows up as colour leaking between unrelated parts of the screen.
func (e *encoder) setStyle(s Style) {
	if e.styleKnown && s == e.style {
		return
	}
	e.style = s
	e.styleKnown = true

	e.buf = append(e.buf, 0x1b, '[', '0')
	if s.Attrs&AttrBold != 0 {
		e.buf = append(e.buf, ";1"...)
	}
	if s.Attrs&AttrDim != 0 {
		e.buf = append(e.buf, ";2"...)
	}
	if s.Attrs&AttrItalic != 0 {
		e.buf = append(e.buf, ";3"...)
	}
	if s.Attrs&AttrUnderline != 0 {
		e.appendUnderline(s.Underline)
	}
	if s.Attrs&AttrBlink != 0 {
		e.buf = append(e.buf, ";5"...)
	}
	if s.Attrs&AttrReverse != 0 {
		e.buf = append(e.buf, ";7"...)
	}
	if s.Attrs&AttrHidden != 0 {
		e.buf = append(e.buf, ";8"...)
	}
	if s.Attrs&AttrStrike != 0 {
		e.buf = append(e.buf, ";9"...)
	}
	e.appendColor(s.FG, true)
	e.appendColor(s.BG, false)
	e.buf = append(e.buf, 'm')
}

func (e *encoder) appendUnderline(style UnderlineStyle) {
	switch style {
	case UnderlineDouble:
		e.buf = append(e.buf, ";4:2"...)
	case UnderlineCurly:
		e.buf = append(e.buf, ";4:3"...)
	case UnderlineDotted:
		e.buf = append(e.buf, ";4:4"...)
	case UnderlineDashed:
		e.buf = append(e.buf, ";4:5"...)
	default:
		e.buf = append(e.buf, ";4"...)
	}
}

func (e *encoder) appendColor(c Color, foreground bool) {
	if c.IsDefault() {
		return
	}
	base := 30
	extended := ";38;"
	if !foreground {
		base = 40
		extended = ";48;"
	}

	if c.IsIndexed() {
		idx := c.Index()
		switch {
		case idx < 8:
			e.buf = append(e.buf, ';')
			e.buf = strconv.AppendInt(e.buf, int64(base+int(idx)), 10)
		case idx < 16:
			// The bright range is 90-97 and 100-107, sixty above the base.
			e.buf = append(e.buf, ';')
			e.buf = strconv.AppendInt(e.buf, int64(base+60+int(idx)-8), 10)
		default:
			e.buf = append(e.buf, extended...)
			e.buf = append(e.buf, '5', ';')
			e.buf = strconv.AppendInt(e.buf, int64(idx), 10)
		}
		return
	}

	r, g, b := c.RGB()
	e.buf = append(e.buf, extended...)
	e.buf = append(e.buf, '2', ';')
	e.buf = strconv.AppendInt(e.buf, int64(r), 10)
	e.buf = append(e.buf, ';')
	e.buf = strconv.AppendInt(e.buf, int64(g), 10)
	e.buf = append(e.buf, ';')
	e.buf = strconv.AppendInt(e.buf, int64(b), 10)
}

// row emits one row's cells, left to right.
func (e *encoder) row(r *Row, cols int) {
	if cols > r.Len() {
		cols = r.Len()
	}
	for x := 0; x < cols; x++ {
		cell := r.Cell(x)
		if cell.IsContinuation() {
			// The right half of a wide character carries no rune of its own;
			// the terminal advanced past it when the left half was written.
			continue
		}
		e.setStyle(cell.Style)
		if cell.R == 0 {
			e.buf = append(e.buf, ' ')
		} else {
			e.buf = appendRune(e.buf, cell.R)
		}
		for _, mark := range r.Combining(x) {
			e.buf = appendRune(e.buf, mark)
		}
	}
}

func appendRune(dst []byte, r rune) []byte {
	if r < 0x80 {
		return append(dst, byte(r))
	}
	return append(dst, string(r)...)
}

// RenderScreen encodes a screen as the escape sequences that reproduce it.
//
// Feeding the result to an empty Screen of the same size yields the same
// screen, which is the property the wire depends on and the one the tests
// check. Scrollback is not included: this describes what is on screen.
func RenderScreen(s *Screen) []byte {
	var e encoder
	e.reset()

	g := s.Grid()
	cols, rows := s.Size()

	// Start from a known state. Without the reset, the first run would inherit
	// whatever the receiving terminal happened to be left in.
	e.buf = append(e.buf, 0x1b, '[', 'H')
	e.setStyle(DefaultStyle)

	for y := 0; y < rows; y++ {
		if y > 0 {
			e.buf = append(e.buf, '\r', '\n')
		}
		if line := g.Line(y); line != nil {
			e.row(line, cols)
		}
	}

	e.setStyle(DefaultStyle)
	cur := s.Cursor()
	e.moveTo(cur.X, cur.Y)
	if s.Modes().CursorVisible {
		e.buf = append(e.buf, "\x1b[?25h"...)
	} else {
		e.buf = append(e.buf, "\x1b[?25l"...)
	}
	return e.buf
}

// RenderScrolled encodes a view of the screen as it was `offset` lines ago,
// taking the rows above the viewport from the scrollback.
//
// Offset zero is the live screen, which is why scrolling is a view rather than
// a mode down here: the terminal keeps running and the cursor keeps moving
// while somebody reads what went past.
func RenderScrolled(s *Screen, offset int) []byte {
	if offset <= 0 {
		return RenderScreen(s)
	}
	g := s.MainGrid()
	if offset > g.HistoryLen() {
		offset = g.HistoryLen()
	}

	cols, rows := s.Size()
	var e encoder
	e.reset()
	e.buf = append(e.buf, 0x1b, '[', 'H')
	e.setStyle(DefaultStyle)

	// The window starts `offset` rows above the top of the viewport, so the
	// first rows come from history and the rest from the screen itself.
	for y := 0; y < rows; y++ {
		if y > 0 {
			e.buf = append(e.buf, '\r', '\n')
		}
		index := y - offset
		var line *Row
		if index < 0 {
			line = g.HistoryLine(g.HistoryLen() + index)
		} else {
			line = g.Line(index)
		}
		if line != nil {
			e.row(line, cols)
		}
	}

	e.setStyle(DefaultStyle)
	e.moveTo(0, 0)
	// The cursor belongs to the live screen, not to a view of the past.
	e.buf = append(e.buf, "\x1b[?25l"...)
	return e.buf
}

// Painter draws a grid onto a real terminal, emitting only what changed.
//
// It keeps the last frame it sent. A full repaint of a large terminal is tens
// of kilobytes; most frames change a handful of rows, and sending the rest
// again is both slow and visibly flickery on a slow link.
type Painter struct {
	enc  encoder
	prev *Grid
}

// NewPainter returns a painter with no history, so its first frame is a full
// repaint.
func NewPainter() *Painter { return &Painter{} }

// Invalidate forgets the last frame, forcing the next one to repaint fully.
// A client calls it after anything that may have disturbed the terminal
// underneath it, such as a resize or reattaching.
func (p *Painter) Invalidate() { p.prev = nil }

// Paint returns the escape sequences that bring the terminal from the last
// painted frame to this one. cursor is where to leave the cursor; pass
// visible=false to hide it.
func (p *Painter) Paint(g *Grid, cursorX, cursorY int, visible bool) []byte {
	p.enc.reset()

	cols, rows := g.Cols(), g.Rows()
	full := p.prev == nil || p.prev.Cols() != cols || p.prev.Rows() != rows

	// Hide the cursor for the duration of the repaint, or it visibly skates
	// across the screen as rows are rewritten.
	p.enc.buf = append(p.enc.buf, "\x1b[?25l"...)

	for y := 0; y < rows; y++ {
		line := g.Line(y)
		if line == nil {
			continue
		}
		if !full && rowsEqual(line, p.prev.Line(y)) {
			continue
		}
		p.enc.moveTo(0, y)
		p.enc.row(line, cols)
	}

	p.enc.setStyle(DefaultStyle)
	p.enc.moveTo(cursorX, cursorY)
	if visible {
		p.enc.buf = append(p.enc.buf, "\x1b[?25h"...)
	}

	p.remember(g)
	return p.enc.buf
}

// remember copies the frame just painted, reusing the previous copy's storage.
func (p *Painter) remember(g *Grid) {
	if p.prev == nil || p.prev.Cols() != g.Cols() || p.prev.Rows() != g.Rows() {
		p.prev = NewGrid(g.Cols(), g.Rows(), 0)
	}
	for y := 0; y < g.Rows(); y++ {
		src, dst := g.Line(y), p.prev.Line(y)
		if src == nil || dst == nil {
			continue
		}
		copy(dst.cells, src.cells)
		clear(dst.combining)
		for x, marks := range src.combining {
			for _, mark := range marks {
				dst.AddCombining(x, mark)
			}
		}
	}
}

func rowsEqual(a, b *Row) bool {
	if a == nil || b == nil || a.Len() != b.Len() {
		return false
	}
	for x := range a.cells {
		if a.cells[x] != b.cells[x] {
			return false
		}
	}
	if len(a.combining) != len(b.combining) {
		return false
	}
	for x, marks := range a.combining {
		other, ok := b.combining[x]
		if !ok || len(other) != len(marks) {
			return false
		}
		for i := range marks {
			if marks[i] != other[i] {
				return false
			}
		}
	}
	return true
}
