package ui

import "github.com/sousaakira/tend/internal/vt"

// A panel's close mark, tend's own: a ✕ on its top edge, near its right
// corner, that closes it as esc does — for somebody reaching for the mouse
// rather than the keyboard. One description, which drawing and a click
// both read.

// closeMarkMin is the narrowest box that has one: a smaller one keeps its
// edge for its title.
const closeMarkMin = 16

// CloseMarkRect is where a box's ✕ is.
func CloseMarkRect(box Rect) Rect {
	return Rect{X: box.X + box.Cols - 5, Y: box.Y, Cols: 3, Rows: 1}
}

// OnCloseMark is whether a point is on a box's ✕.
func OnCloseMark(box Rect, x, y int) bool {
	r := CloseMarkRect(box)
	return box.Cols >= closeMarkMin && y == r.Y && x >= r.X && x < r.X+r.Cols
}

func drawCloseMark(dst *vt.Grid, box Rect, style vt.Style) {
	if box.Cols < closeMarkMin {
		return
	}
	r := CloseMarkRect(box)
	writeString(dst, r.X, r.Y, " ✕ ", style, box.X+box.Cols-1)
}

// OverlayBox is where the overlay of lines — the keys' help — is drawn.
func OverlayBox(lines []string, cols, rows int) (Rect, bool) {
	g, ok := overlayLayout(lines, cols, rows)
	return g.box, ok
}
