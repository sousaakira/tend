package vt

// Reflow re-wraps a terminal's history when its width changes.
//
// Without it, narrowing a pane leaves every line broken where it was: text
// that wrapped at 120 columns keeps its break at column 120 inside an 80
// column pane, so half the screen is ragged and the other half is empty. The
// information needed to undo that is already recorded — Row.wrapped marks a
// line that continues onto the next because it ran out of width, as opposed to
// one that ended because the program wrote a newline — and reflow is what
// spends it.
//
// Only the main screen reflows. An application on the alternate screen owns
// every cell of it and redraws on SIGWINCH, so rewrapping there would fight
// the redraw that is already coming.

// logical is one line as the program meant it, before any wrapping.
type logical struct {
	cells     []Cell
	combining map[int][]rune
}

// collectLogical joins wrapped rows back into the lines the program wrote.
//
// Trailing blank cells are dropped from each line: a terminal pads every row
// to its full width, and keeping that padding would make every line wrap
// again at the new width regardless of how much text it holds. Only unstyled
// blanks go, so a line ending in a coloured bar keeps it.
func collectLogical(g *Grid, cursorRow, cursorCol int) (lines []logical, cursorLine, cursorOffset int) {
	total := g.HistoryLen() + g.Rows()
	rowAt := func(i int) *Row {
		if i < g.HistoryLen() {
			return g.HistoryLine(i)
		}
		return g.Line(i - g.HistoryLen())
	}

	// Everything after the last row with content is blank padding at the
	// bottom of the screen, which is rebuilt rather than carried over. The
	// cursor's row is kept even when blank, or a cursor sitting below the
	// text would be dragged back up to it.
	last := -1
	for i := 0; i < total; i++ {
		if rowAt(i).Text() != "" {
			last = i
		}
	}
	if cursorRow > last {
		last = cursorRow
	}

	cursorLine, cursorOffset = 0, 0
	for i := 0; i <= last; {
		current := logical{}

		for {
			row := rowAt(i)
			base := len(current.cells)

			end := row.Len()
			if row.Wrapped() {
				// A wrapped row's trailing space is content, but its spacers
				// are not: they are the columns a wide character could not use.
				for end > 0 && isSpacer(row, end-1) {
					end--
				}
			} else {
				end = trimmedEnd(row)
			}
			for x := 0; x < end; x++ {
				current.cells = append(current.cells, row.Cell(x))
				if marks := row.Combining(x); len(marks) > 0 {
					if current.combining == nil {
						current.combining = make(map[int][]rune, 1)
					}
					current.combining[base+x] = append([]rune(nil), marks...)
				}
			}

			if i == cursorRow {
				cursorLine = len(lines)
				cursorOffset = base + cursorCol
			}

			i++
			if !row.Wrapped() || i > last {
				break
			}
		}
		lines = append(lines, current)
	}

	if len(lines) == 0 {
		lines = append(lines, logical{})
	}
	if cursorLine >= len(lines) {
		cursorLine = len(lines) - 1
	}
	return lines, cursorLine, cursorOffset
}

// trimmedEnd is the length of a row with its unstyled trailing blanks removed.
func trimmedEnd(r *Row) int {
	end := r.Len()
	for end > 0 {
		cell := r.Cell(end - 1)
		if !cell.IsBlank() || cell.Style != DefaultStyle || len(r.Combining(end-1)) > 0 {
			break
		}
		end--
	}
	return end
}

// wrapped is the result of re-wrapping: rows, and where the cursor landed.
type wrapped struct {
	rows      []Row
	cursorRow int
	cursorCol int
}

// wrapLogical breaks logical lines back into rows of the new width.
func wrapLogical(lines []logical, cols int, cursorLine, cursorOffset int, style Style) wrapped {
	out := wrapped{}
	blank := blankCell(style)

	for li, line := range lines {
		offset := 0
		for {
			row := Row{}
			row.reset(cols, style)

			n := 0
			short := false
			for n < cols && offset+n < len(line.cells) {
				cell := line.cells[offset+n]
				// A double-width character cannot be split across the margin,
				// so it moves whole to the next row and this one ends short.
				if cell.Width == 2 && n+1 >= cols {
					short = true
					break
				}
				row.cells[n] = cell
				if marks, ok := line.combining[offset+n]; ok {
					for _, mark := range marks {
						row.AddCombining(n, mark)
					}
				}
				n++
			}
			pad := blank
			if short {
				// The columns the wide character could not use are padding,
				// which must not read as content when this is rewrapped again.
				pad = spacerCell(style)
			}
			for x := n; x < cols; x++ {
				row.cells[x] = pad
			}

			if li == cursorLine && cursorOffset >= offset && (cursorOffset < offset+n || offset+n >= len(line.cells)) {
				out.cursorRow = len(out.rows)
				out.cursorCol = min(cursorOffset-offset, cols-1)
			}

			offset += n
			more := offset < len(line.cells)
			row.SetWrapped(more)
			out.rows = append(out.rows, row)

			if !more {
				break
			}
			// A line of nothing but a wide character too big for the width
			// would otherwise loop forever.
			if n == 0 {
				offset++
			}
		}
	}
	return out
}

// applyReflow puts rewrapped rows back into the grid, filling the viewport
// from the bottom and pushing the rest into history.
func applyReflow(g *Grid, rows []Row, cols, viewportRows int, style Style) {
	g.cols = cols
	g.rows = viewportRows

	// The cursor's row must stay on screen, so the viewport is anchored to the
	// end of the content rather than to its beginning.
	viewportStart := max(len(rows)-viewportRows, 0)

	g.ClearHistory()
	for i := 0; i < viewportStart; i++ {
		g.pushHistory(rows[i])
	}

	g.lines = make([]Row, viewportRows)
	for y := 0; y < viewportRows; y++ {
		if src := viewportStart + y; src < len(rows) {
			g.lines[y] = rows[src]
			g.lines[y].resize(cols, style)
			continue
		}
		g.lines[y].reset(cols, style)
	}
}

// viewportOffset is where the viewport starts within the rewrapped rows. The
// viewport is anchored to the end of the content so the cursor stays on screen.
func viewportOffset(produced, viewportRows int) int {
	return max(produced-viewportRows, 0)
}
