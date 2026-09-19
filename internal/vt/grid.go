package vt

import "strings"

// Row is one line of cells.
//
// Combining marks live in a side table rather than in Cell, because they are
// rare and a slice header in every cell would cost more memory than the marks
// themselves ever do. The table stays nil until a row actually needs one.
type Row struct {
	cells     []Cell
	wrapped   bool
	combining map[int][]rune
}

// Len returns the number of columns in the row.
func (r *Row) Len() int { return len(r.cells) }

// Cells exposes the row for reading. The slice aliases the row's storage and
// is invalidated by the next resize.
func (r *Row) Cells() []Cell { return r.cells }

// Cell returns the cell at x, or a blank cell when x is out of range.
func (r *Row) Cell(x int) Cell {
	if x < 0 || x >= len(r.cells) {
		return blankCell(DefaultStyle)
	}
	return r.cells[x]
}

// SetCell writes a cell, dropping any combining marks previously attached to
// that column.
func (r *Row) SetCell(x int, c Cell) {
	if x < 0 || x >= len(r.cells) {
		return
	}
	r.cells[x] = c
	if r.combining != nil {
		delete(r.combining, x)
	}
}

// Wrapped reports whether this row continues onto the next one because text
// ran past the last column, rather than because of an explicit newline.
// Reflow depends on this distinction.
func (r *Row) Wrapped() bool { return r.wrapped }

// SetWrapped records that the row continues onto the next.
func (r *Row) SetWrapped(v bool) { r.wrapped = v }

// Combining returns the marks attached to the cell at x, or nil.
func (r *Row) Combining(x int) []rune {
	if r.combining == nil {
		return nil
	}
	return r.combining[x]
}

// AddCombining attaches a zero-width mark to the cell at x.
func (r *Row) AddCombining(x int, mark rune) {
	if x < 0 || x >= len(r.cells) {
		return
	}
	if r.combining == nil {
		r.combining = make(map[int][]rune, 1)
	}
	r.combining[x] = append(r.combining[x], mark)
}

// Text renders the row as a string with trailing blanks removed. Continuation
// halves of wide characters contribute nothing, and combining marks follow
// their base rune. This is the form the agent detector reads.
func (r *Row) Text() string {
	end := len(r.cells)
	for end > 0 && r.cells[end-1].IsBlank() && r.Combining(end-1) == nil {
		end--
	}
	if end == 0 {
		return ""
	}
	var sb strings.Builder
	sb.Grow(end)
	for x := 0; x < end; x++ {
		c := r.cells[x]
		if c.IsContinuation() {
			continue
		}
		if c.R == 0 {
			sb.WriteByte(' ')
		} else {
			sb.WriteRune(c.R)
		}
		for _, m := range r.Combining(x) {
			sb.WriteRune(m)
		}
	}
	return sb.String()
}

// reset blanks the row to cols columns of style, reusing storage.
func (r *Row) reset(cols int, style Style) {
	r.resize(cols, style)
	blank := blankCell(style)
	for i := range r.cells {
		r.cells[i] = blank
	}
	r.wrapped = false
	clear(r.combining)
}

// resize changes the row's width, blanking any newly exposed columns.
func (r *Row) resize(cols int, style Style) {
	if cols == len(r.cells) {
		return
	}
	if cols < len(r.cells) {
		for x := cols; x < len(r.cells); x++ {
			if r.combining != nil {
				delete(r.combining, x)
			}
		}
		r.cells = r.cells[:cols]
		return
	}
	if cap(r.cells) >= cols {
		old := len(r.cells)
		r.cells = r.cells[:cols]
		blank := blankCell(style)
		for i := old; i < cols; i++ {
			r.cells[i] = blank
		}
		return
	}
	grown := make([]Cell, cols)
	copy(grown, r.cells)
	blank := blankCell(style)
	for i := len(r.cells); i < cols; i++ {
		grown[i] = blank
	}
	r.cells = grown
}

// shiftCells moves the cells from x rightwards by n, which may be negative to
// shift left, filling the vacated columns with blank. Combining marks move with
// their cells; any that fall off the row are dropped.
func (r *Row) shiftCells(x, n int, blank Cell) {
	if n == 0 || x < 0 || x >= len(r.cells) {
		return
	}
	if n > 0 {
		copy(r.cells[x+n:], r.cells[x:])
		end := min(x+n, len(r.cells))
		for i := x; i < end; i++ {
			r.cells[i] = blank
		}
	} else {
		copy(r.cells[x:], r.cells[x-n:])
		start := max(x, len(r.cells)+n)
		for i := start; i < len(r.cells); i++ {
			r.cells[i] = blank
		}
	}
	r.shiftCombining(x, n)
}

// shiftCombining keeps the side table in step with a cell shift.
func (r *Row) shiftCombining(x, n int) {
	if len(r.combining) == 0 {
		return
	}
	moved := make(map[int][]rune, len(r.combining))
	for k, v := range r.combining {
		if k < x {
			moved[k] = v
			continue
		}
		if nk := k + n; nk >= x && nk < len(r.cells) {
			moved[nk] = v
		}
	}
	r.combining = moved
}

// clearRange blanks columns [x0,x1) with style.
func (r *Row) clearRange(x0, x1 int, style Style) {
	if x0 < 0 {
		x0 = 0
	}
	if x1 > len(r.cells) {
		x1 = len(r.cells)
	}
	blank := blankCell(style)
	for x := x0; x < x1; x++ {
		r.cells[x] = blank
		if r.combining != nil {
			delete(r.combining, x)
		}
	}
}

// Grid is a viewport of rows plus the scrollback behind it.
//
// Scrollback is a ring. Once it is full, scrolling recycles the evicted row's
// storage as the new blank line, so a terminal producing output forever does
// not allocate forever.
type Grid struct {
	cols, rows int

	lines []Row

	sb     []Row
	sbHead int // index of the oldest row
	sbLen  int

	scratch []Row // reusable rotation buffer
}

// NewGrid returns a grid of cols × rows with room for scrollback lines of
// history. A scrollback of zero disables history, which is what the alternate
// screen wants.
func NewGrid(cols, rows, scrollback int) *Grid {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if scrollback < 0 {
		scrollback = 0
	}
	g := &Grid{
		cols:  cols,
		rows:  rows,
		lines: make([]Row, rows),
		sb:    make([]Row, scrollback),
	}
	for i := range g.lines {
		g.lines[i].reset(cols, DefaultStyle)
	}
	return g
}

// Cols returns the width.
func (g *Grid) Cols() int { return g.cols }

// Rows returns the viewport height.
func (g *Grid) Rows() int { return g.rows }

// HistoryLen returns how many scrollback lines are currently held.
func (g *Grid) HistoryLen() int { return g.sbLen }

// Line returns the viewport row at y, or nil when y is out of range.
func (g *Grid) Line(y int) *Row {
	if y < 0 || y >= g.rows {
		return nil
	}
	return &g.lines[y]
}

// HistoryLine returns scrollback row i, where 0 is the oldest retained line.
func (g *Grid) HistoryLine(i int) *Row {
	if i < 0 || i >= g.sbLen {
		return nil
	}
	return &g.sb[(g.sbHead+i)%len(g.sb)]
}

// pushHistory moves row into the scrollback ring and returns a row whose
// storage the caller may reuse. Once the ring is full that is the evicted
// row, which is what makes steady-state scrolling allocation-free.
func (g *Grid) pushHistory(row Row) Row {
	if len(g.sb) == 0 {
		return row
	}
	if g.sbLen < len(g.sb) {
		idx := (g.sbHead + g.sbLen) % len(g.sb)
		g.sb[idx] = row
		g.sbLen++
		return Row{}
	}
	recycled := g.sb[g.sbHead]
	g.sb[g.sbHead] = row
	g.sbHead = (g.sbHead + 1) % len(g.sb)
	return recycled
}

// ScrollUp moves the rows in [top,bottom] up by n, blanking the n rows
// exposed at the bottom with style. When toHistory is set, the rows leaving
// the top are retained as scrollback; callers pass false for the alternate
// screen and for scrolls inside a region that does not start at row 0.
func (g *Grid) ScrollUp(top, bottom, n int, style Style, toHistory bool) {
	top, bottom, n, ok := g.clampRegion(top, bottom, n)
	if !ok {
		return
	}
	g.scratch = g.scratch[:0]
	for i := 0; i < n; i++ {
		out := g.lines[top+i]
		if toHistory {
			out = g.pushHistory(out)
		}
		out.reset(g.cols, style)
		g.scratch = append(g.scratch, out)
	}
	copy(g.lines[top:bottom+1-n], g.lines[top+n:bottom+1])
	copy(g.lines[bottom+1-n:bottom+1], g.scratch)
}

// ScrollDown moves the rows in [top,bottom] down by n, blanking the n rows
// exposed at the top. Nothing enters scrollback: history only grows from the
// top of the screen.
func (g *Grid) ScrollDown(top, bottom, n int, style Style) {
	top, bottom, n, ok := g.clampRegion(top, bottom, n)
	if !ok {
		return
	}
	g.scratch = g.scratch[:0]
	for i := 0; i < n; i++ {
		out := g.lines[bottom-i]
		out.reset(g.cols, style)
		g.scratch = append(g.scratch, out)
	}
	copy(g.lines[top+n:bottom+1], g.lines[top:bottom+1-n])
	// scratch holds the recycled rows bottom-first; order among blanks is
	// irrelevant, they are identical.
	copy(g.lines[top:top+n], g.scratch)
}

func (g *Grid) clampRegion(top, bottom, n int) (int, int, int, bool) {
	if top < 0 {
		top = 0
	}
	if bottom >= g.rows {
		bottom = g.rows - 1
	}
	if n <= 0 || top > bottom {
		return 0, 0, 0, false
	}
	if count := bottom - top + 1; n > count {
		n = count
	}
	return top, bottom, n, true
}

// Clear blanks the whole viewport with style, leaving scrollback untouched.
func (g *Grid) Clear(style Style) {
	for i := range g.lines {
		g.lines[i].reset(g.cols, style)
	}
}

// ClearHistory discards the scrollback.
func (g *Grid) ClearHistory() {
	for i := range g.sb {
		g.sb[i] = Row{}
	}
	g.sbHead = 0
	g.sbLen = 0
}

// Resize changes the grid's dimensions.
//
// Rows are not reflowed: text that ran past the old width stays broken where
// it was. Reflow needs the wrapped flag Row already tracks, and is deferred
// until the renderer needs it.
//
// When the viewport shrinks, trailing blank rows are dropped first and only
// then are rows scrolled off the top into history. That keeps the bottom of
// the screen — where a shell prompt or an agent's status lives — rather than
// the top.
func (g *Grid) Resize(cols, rows int, style Style) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if cols == g.cols && rows == g.rows {
		return
	}

	if cols != g.cols {
		for i := range g.lines {
			g.lines[i].resize(cols, style)
		}
		for i := 0; i < g.sbLen; i++ {
			g.HistoryLine(i).resize(cols, style)
		}
		g.cols = cols
	}

	switch {
	case rows > g.rows:
		grown := make([]Row, rows)
		copy(grown, g.lines)
		for i := g.rows; i < rows; i++ {
			grown[i].reset(cols, style)
		}
		g.lines = grown

	case rows < g.rows:
		drop := g.rows - rows
		// Discard trailing blank rows before sacrificing real content.
		for drop > 0 && g.rowIsBlank(len(g.lines)-1) {
			g.lines = g.lines[:len(g.lines)-1]
			drop--
		}
		if drop > 0 {
			// ScrollUp clamps against g.rows, so keep it in step with the
			// rows actually left after trimming.
			g.rows = len(g.lines)
			g.ScrollUp(0, g.rows-1, drop, style, true)
			g.lines = g.lines[:len(g.lines)-drop]
		}
	}
	g.rows = rows
}

func (g *Grid) rowIsBlank(i int) bool {
	if i < 0 || i >= len(g.lines) {
		return false
	}
	return g.lines[i].Text() == ""
}
