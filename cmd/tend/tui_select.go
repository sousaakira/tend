package main

import (
	"io"
	"os"

	"github.com/sousaakira/tend/internal/ui"
)

// Asking the terminal for mouse reporting is what makes panes clickable, and
// it is also what takes text selection away: the drag now belongs to tend
// rather than to the window. Most terminals keep a way out under Shift, but it
// selects across the whole window — the other panes, the borders, the sidebar
// — which for one column of a split is not a way out at all.
//
// So tend marks the selection itself, over the pane the drag started in, and
// hands the text to the terminal to put on the clipboard.

// selectionModifier is what forces a selection in a pane whose own program
// asked for the mouse.
//
// Without it those panes could not be selected in at all, which is most of
// them: an agent asks for the mouse. The plain drag still goes to the program,
// because that is what the program asked for and what its own selection needs.
const selectionModifier = ui.ModAlt

// beginSelection starts marking text, and reports whether the press was taken.
func (t *tui) beginSelection(ev ui.MouseEvent) bool {
	pane := t.paneAt(ev.X, ev.Y)
	if pane == 0 {
		return false
	}
	// A pane that asked for the mouse keeps it, unless the modifier says the
	// user wants tend's selection instead.
	if t.forwardsMouse(pane) && !ev.Mods.Has(selectionModifier) {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	x, y, ok := t.paneCellLocked(pane, ev.X, ev.Y)
	if !ok {
		return false
	}
	t.sel = &ui.Selection{
		Pane:     pane,
		AnchorX:  x,
		AnchorY:  y,
		CursorX:  x,
		CursorY:  y,
		Scroll:   t.selectionScrollLocked(pane),
		Dragging: true,
	}
	t.dirty = true
	return true
}

// dragSelection moves the far end of the selection, and reports whether one is
// being dragged.
func (t *tui) dragSelection(ev ui.MouseEvent) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sel == nil || !t.sel.Dragging {
		return false
	}
	// Clamped into the pane rather than ignored outside it: dragging past the
	// edge to take the rest of a line is the ordinary way to select, and
	// dropping those reports would make the selection stop short of where the
	// pointer plainly is.
	x, y := t.clampToPaneLocked(t.sel.Pane, ev.X, ev.Y)
	if x != t.sel.CursorX || y != t.sel.CursorY {
		t.sel.CursorX, t.sel.CursorY = x, y
		t.dirty = true
	}
	return true
}

// endSelection finishes a drag and copies what was marked.
func (t *tui) endSelection() bool {
	t.mu.Lock()
	if t.sel == nil || !t.sel.Dragging {
		t.mu.Unlock()
		return false
	}
	t.sel.Dragging = false
	sel := *t.sel
	screen := t.screens[sel.Pane]
	if sel.Empty() {
		// A click is not a selection. Clearing it here means a stray click
		// does not leave a one-cell mark behind.
		t.sel = nil
	}
	t.dirty = true
	t.mu.Unlock()

	text := sel.Text(screen)
	if text == "" {
		t.wakeUp()
		return true
	}
	t.copyToClipboard(text)
	return true
}

// clearSelection drops the mark, if there is one.
func (t *tui) clearSelection() {
	t.mu.Lock()
	had := t.sel != nil
	t.sel = nil
	if had {
		t.dirty = true
	}
	t.mu.Unlock()
	if had {
		t.wakeUp()
	}
}

// copyToClipboard hands text to the terminal to put on the clipboard.
//
// Through the terminal rather than through a platform tool: it is the only
// route that works over ssh, where the clipboard that matters belongs to the
// machine in front of the user and not to the one tend is running on. There is
// no reply to wait for, and terminals cap the size and often refuse it
// outright, so success cannot be reported — only that it was sent.
func (t *tui) copyToClipboard(text string) {
	if seq := ui.SetClipboard(text); seq != "" {
		_, _ = io.WriteString(os.Stdout, seq)
	}
	t.setMessage(copiedMessage(text), false)
}

// copiedMessage says how much was taken, since the mark is about to be
// redrawn and the user has nothing else to go on.
func copiedMessage(text string) string {
	lines := 1
	for _, r := range text {
		if r == '\n' {
			lines++
		}
	}
	if lines == 1 {
		return "copied " + itoaInt(len([]rune(text))) + " characters"
	}
	return "copied " + itoaInt(lines) + " lines"
}

// paneCellLocked turns a screen point into a cell of a pane, if it is in one.
func (t *tui) paneCellLocked(pane uint64, x, y int) (int, int, bool) {
	for _, r := range t.paneRects() {
		if r.Pane != pane {
			continue
		}
		cx, cy := x-r.X-1, y-r.Y-1
		if cx < 0 || cy < 0 || cx >= r.Cols-2 || cy >= r.Rows-2 {
			return 0, 0, false
		}
		return cx, cy, true
	}
	return 0, 0, false
}

// clampToPaneLocked turns a screen point into the nearest cell of a pane.
func (t *tui) clampToPaneLocked(pane uint64, x, y int) (int, int) {
	for _, r := range t.paneRects() {
		if r.Pane != pane {
			continue
		}
		return min(max(x-r.X-1, 0), max(r.Cols-3, 0)),
			min(max(y-r.Y-1, 0), max(r.Rows-3, 0))
	}
	return 0, 0
}

// selectionScrollLocked is how far back the pane is being read, so a selection
// made in the scrollback keeps covering the same text.
func (t *tui) selectionScrollLocked(pane uint64) int {
	if t.scrollPane == pane {
		return t.scrollOffset
	}
	return 0
}
