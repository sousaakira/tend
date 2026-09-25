package ui

import (
	"fmt"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
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
	if f.PromptHint != "" {
		// Wide enough for the hint and the keys beside it, up to the screen:
		// a path cut down to its last few characters says little.
		width = max(width, runewidth.StringWidth(f.PromptHint)+len(" · enter · esc ")+2)
	}
	for _, c := range f.PromptChoices {
		// Each is numbered, "1 " before it.
		width = max(width, runewidth.StringWidth(c)+2)
	}
	width = max(width, promptMinWidth)

	height := 5
	if len(f.PromptChoices) > 0 {
		// A blank, "recent", and one line each.
		height += len(f.PromptChoices) + 1
	}
	r := Rect{Cols: min(width+6, cols), Rows: min(height, rows)}
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

	drawPromptChoices(dst, f, r, theme)

	footer := " enter · esc "
	if len(f.PromptChoices) > 0 {
		footer = " ↑↓ recent · click opens · enter · esc "
	}
	if f.PromptHint != "" {
		// Cut from the left: the end of a path is the part that changes as
		// it is typed, and the part worth seeing.
		hint := f.PromptHint
		if room := r.Cols - 4 - len([]rune(footer)) - 3; room > 1 && len([]rune(hint)) > room {
			runes := []rune(hint)
			hint = "…" + string(runes[len(runes)-room+1:])
		}
		footer = " " + hint + " ·" + footer
	}
	writeString(dst, r.X+2, r.Y+r.Rows-1, footer, theme.OverlayTitle, limit)
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

// promptChoicesTop is the line the first answer given before is on.
func promptChoicesTop(r Rect) int { return r.Y + 4 }

// drawPromptChoices lists the answers given before under a "recent" label,
// numbered, the one the arrows are on marked.
func drawPromptChoices(dst *vt.Grid, f Frame, r Rect, theme Theme) {
	if len(f.PromptChoices) == 0 {
		return
	}
	limit := r.X + r.Cols - 1
	writeString(dst, r.X+2, r.Y+3, "recent", theme.OverlayTitle, limit)
	for i, c := range f.PromptChoices {
		y := promptChoicesTop(r) + i
		if y >= r.Y+r.Rows-1 {
			return
		}
		style := theme.Overlay
		if i == f.PromptChoice {
			style = theme.OverlayTitle
		}
		x := writeString(dst, r.X+2, y, fmt.Sprintf("%d ", i+1), theme.OverlayTitle, limit)
		writeString(dst, x, y, truncate(c, r.Cols-6), style, limit)
	}
}

// PromptChoiceAt is the answer given before under a point, by its place in
// PromptChoices.
func PromptChoiceAt(f Frame, cols, rows, x, y int) (int, bool) {
	if f.Prompt == "" || len(f.PromptChoices) == 0 {
		return 0, false
	}
	r := promptRect(f, cols, rows)
	i := y - promptChoicesTop(r)
	if x <= r.X || x >= r.X+r.Cols-1 || i < 0 || i >= len(f.PromptChoices) || y >= r.Y+r.Rows-1 {
		return 0, false
	}
	return i, true
}
