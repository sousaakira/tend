package ui

import (
	"github.com/mattn/go-runewidth"

	"github.com/sousaakira/tend/internal/vt"
)

// Typing a name belongs in front of the user, not on the status line.
//
// The status bar is where tend says things; it is not where the user says
// them. A field down there competes with the session name and the tab list for
// the same row, truncates when the name is long, and puts what is being typed
// furthest from where the eye already is. A box in the middle has none of
// those problems and says plainly that the next keystroke goes into it.

// promptMinWidth keeps the box from shrinking to the width of a short name and
// jumping about as it is typed.
const promptMinWidth = 34

// promptRect is where the box goes.
func promptRect(f Frame, cols, rows int) Rect {
	width := max(runewidth.StringWidth(f.Prompt), runewidth.StringWidth(f.PromptText)+1)
	width = max(width, promptMinWidth)

	r := Rect{Cols: min(width+6, cols), Rows: min(5, rows)}
	r.X = (cols - r.Cols) / 2
	r.Y = (rows - r.Rows) / 2
	return r
}

// drawPrompt puts the field over the middle of the screen.
func drawPrompt(dst *vt.Grid, f Frame, theme Theme) {
	if f.Prompt == "" {
		return
	}
	r := promptRect(f, dst.Cols(), dst.Rows())

	for y := r.Y; y < r.Y+r.Rows; y++ {
		fill(dst, y, r.X, r.X+r.Cols, theme.Overlay)
	}
	drawBox(dst, r, theme.OverlayTitle)

	limit := r.X + r.Cols - 1
	writeString(dst, r.X+2, r.Y, " "+truncate(f.Prompt, r.Cols-4)+" ", theme.OverlayTitle, limit)

	// The seed is drawn as a selection, because that is what it is: the next
	// keystroke replaces it. Showing it as ordinary text would make "rename
	// this to backend" produce "space 2backend".
	style := theme.Overlay
	if f.PromptSelected {
		style = theme.OverlayTitle
	}
	at := writeString(dst, r.X+2, r.Y+2, truncate(f.PromptText, r.Cols-5), style, limit)
	writeString(dst, at, r.Y+2, "▏", theme.OverlayTitle, limit)

	writeString(dst, r.X+2, r.Y+r.Rows-1, " enter · esc ", theme.OverlayTitle, limit)
}

// PromptCursor is where the terminal's own cursor belongs while the field is
// up, so it sits in the box rather than in a pane the user is not typing into.
func PromptCursor(f Frame, cols, rows int) (x, y int, ok bool) {
	if f.Prompt == "" {
		return 0, 0, false
	}
	r := promptRect(f, cols, rows)
	at := r.X + 2 + runewidth.StringWidth(truncate(f.PromptText, r.Cols-5))
	return min(at, r.X+r.Cols-2), r.Y + 2, true
}
