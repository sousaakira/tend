package ui

import (
	"github.com/mattn/go-runewidth"

	"github.com/sousaakira/tend/internal/vt"
)

// A notification is a card in a corner of the screen, herdr's
// (`client/shell/notifications.rs`, render_notification_card): a coloured
// dot for what kind it is, the title in bold, the body beneath, framed.
// One is shown at a time; the client queues the rest. A click on it goes to
// the pane it is about.

// Toast kinds, which colour the dot: red for an agent needing an answer,
// blue for one that finished, the accent for anything a script said.
const (
	ToastAttention = "attention"
	ToastFinished  = "finished"
	ToastCustom    = "custom"
)

// Toast positions, herdr's names.
const (
	ToastTopLeft     = "top-left"
	ToastTopRight    = "top-right"
	ToastBottomLeft  = "bottom-left"
	ToastBottomRight = "bottom-right"
)

// Toast is the card shown.
type Toast struct {
	Kind     string
	Title    string
	Body     string
	Position string
}

// ToastRect is where the card goes, above the status bar; the same answer
// draws it and says whether a click is on it.
func ToastRect(t Toast, cols, rows int) Rect {
	width := min(max(runewidth.StringWidth(t.Title), runewidth.StringWidth(t.Body))+6, cols)
	height := 3
	if t.Body != "" {
		height = 4
	}
	area := Rect{Cols: cols, Rows: max(rows-StatusRows, 0)}
	height = min(height, area.Rows)
	r := Rect{Cols: width, Rows: height}
	switch t.Position {
	case ToastTopLeft, ToastBottomLeft:
		r.X = 0
	default:
		r.X = max(cols-width, 0)
	}
	switch t.Position {
	case ToastTopLeft, ToastTopRight:
		r.Y = 0
	default:
		r.Y = max(area.Rows-height, 0)
	}
	return r
}

// drawToast paints the card.
func drawToast(dst *vt.Grid, t Toast, theme Theme) {
	r := ToastRect(t, dst.Cols(), dst.Rows())
	if r.Cols < 6 || r.Rows < 3 {
		return
	}
	for y := r.Y; y < r.Y+r.Rows; y++ {
		fill(dst, y, r.X, r.X+r.Cols, theme.Menu)
	}
	drawBox(dst, r, theme.Border)
	dot := theme.MenuTitle
	switch t.Kind {
	case ToastAttention:
		dot = theme.Blocked
	case ToastFinished:
		dot = vt.Style{FG: vt.IndexedColor(4), Attrs: vt.AttrBold}
	}
	dot.BG = theme.Menu.BG
	limit := r.X + r.Cols - 1
	x := writeString(dst, r.X+2, r.Y+1, "●", dot, limit)
	title := theme.Menu
	title.Attrs |= vt.AttrBold
	writeString(dst, x+1, r.Y+1, truncate(t.Title, limit-x-2), title, limit)
	if t.Body != "" && r.Rows > 3 {
		body := theme.Menu
		body.Attrs |= vt.AttrDim
		writeString(dst, r.X+4, r.Y+2, truncate(t.Body, limit-r.X-5), body, limit)
	}
}
