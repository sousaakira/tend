package main

import (
	"bytes"

	"github.com/sousaakira/tend/internal/ui"
	"github.com/sousaakira/tend/internal/vt"
)

// Scrolling looks back through a pane's history without stopping it.
//
// The pane keeps running and the server keeps its live screen; what changes is
// only which rows this client asks to be drawn. That is why leaving the view
// needs no resynchronisation: nothing was ever out of step.

func (t *tui) scrolling() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.scrollPane != 0
}

// enterScroll starts looking back through the focused pane.
func (t *tui) enterScroll() error {
	t.mu.Lock()
	pane := t.focus
	t.mu.Unlock()
	if pane == 0 {
		return nil
	}

	t.mu.Lock()
	t.scrollPane = pane
	t.scrollOffset = 0
	t.mu.Unlock()
	return t.scrollBy(1)
}

// leaveScroll returns to the live screen.
func (t *tui) leaveScroll() {
	t.mu.Lock()
	t.scrollPane, t.scrollOffset, t.scrollDepth, t.scrollScreen = 0, 0, 0, nil
	t.dirty = true
	t.mu.Unlock()
	t.painter.Invalidate()
	t.wakeUp()
}

// scrollBy moves the view, positive being further back.
func (t *tui) scrollBy(lines int) error {
	t.mu.Lock()
	pane := t.scrollPane
	want := t.scrollOffset + lines
	t.mu.Unlock()
	if pane == 0 {
		return nil
	}
	if want <= 0 {
		// Scrolling back to the present leaves the view rather than sitting
		// at an offset of zero, which would look live but not update.
		t.leaveScroll()
		return nil
	}

	view, err := t.client.PaneScreenAt(pane, want)
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.scrollPane != pane {
		return nil // left the view while the request was in flight
	}
	if t.scrollScreen == nil || t.scrollScreen.Grid().Cols() != view.Cols {
		t.scrollScreen = vt.NewScreen(view.Cols, view.Rows, 0)
	} else {
		t.scrollScreen.Resize(view.Cols, view.Rows)
	}
	_, _ = t.scrollScreen.Write([]byte(view.ANSI))

	t.scrollOffset = view.Offset
	t.scrollDepth = view.History
	t.dirty = true
	return nil
}

// scrollKeys drives the view while it is up. It reports whether the input was
// consumed, so that keys meant for a pane are not swallowed by a mode the
// user has already left.
func (t *tui) scrollKeys(data []byte) (bool, error) {
	if len(data) == 0 {
		return false, nil
	}

	t.mu.Lock()
	page := max(t.rows-ui.StatusRows-4, 1)
	t.mu.Unlock()

	switch {
	case bytes.HasPrefix(data, []byte("\x1b[A")): // up
		return true, t.scrollBy(1)
	case bytes.HasPrefix(data, []byte("\x1b[B")): // down
		return true, t.scrollBy(-1)
	case bytes.HasPrefix(data, []byte("\x1b[5~")): // page up
		return true, t.scrollBy(page)
	case bytes.HasPrefix(data, []byte("\x1b[6~")): // page down
		return true, t.scrollBy(-page)
	case bytes.HasPrefix(data, []byte("\x1b[H")): // home
		t.mu.Lock()
		depth := t.scrollDepth
		t.mu.Unlock()
		return true, t.scrollBy(depth)
	}

	switch data[0] {
	case 'k':
		return true, t.scrollBy(1)
	case 'j':
		return true, t.scrollBy(-1)
	case 'g':
		t.mu.Lock()
		depth := t.scrollDepth
		t.mu.Unlock()
		return true, t.scrollBy(depth)
	case 'G', 'q', 0x1b, 0x03: // bottom, quit, escape, ctrl+c
		t.leaveScroll()
		return true, nil
	}
	// Anything else leaves the view and reaches the pane, so typing at a
	// scrolled prompt does what it looks like it should.
	t.leaveScroll()
	return false, nil
}

// --- mouse -----------------------------------------------------------------

// handleMouse turns a mouse report into focus, a resize, a scroll, or input
// for a pane's own program.
func (t *tui) handleMouse(ev ui.MouseEvent) error {
	switch ev.Kind {
	case ui.MouseWheelUp:
		if pane := t.paneAt(ev.X, ev.Y); pane != 0 {
			t.focusPane(pane)
			if !t.scrolling() {
				return t.enterScroll()
			}
			return t.scrollBy(3)
		}
		return nil

	case ui.MouseWheelDown:
		if t.scrolling() {
			return t.scrollBy(-3)
		}
		return nil

	case ui.MousePress:
		if pane, side, ok := t.dividerAt(ev.X, ev.Y); ok {
			// A press on a border is a grab, not a focus change: the user is
			// reaching for the divider, not for the pane behind it.
			t.mu.Lock()
			t.dragPane, t.dragSide = pane, side
			t.dragAt = ev.X
			if side == "up" || side == "down" {
				t.dragAt = ev.Y
			}
			t.mu.Unlock()
			return nil
		}
		pane := t.paneAt(ev.X, ev.Y)
		if pane == 0 {
			return nil
		}
		if t.forwardsMouse(pane) {
			return t.client.SendInput(pane, ev.Raw)
		}
		t.focusPane(pane)
		return nil

	case ui.MouseDrag:
		return t.dragDivider(ev)

	case ui.MouseRelease:
		// paneAt takes the same lock, so the grab is released first and the
		// lookup happens after. A mutex that is not reentrant turns a nested
		// call into a frozen client, which is exactly how this was found.
		t.mu.Lock()
		dragging := t.dragPane != 0
		t.dragPane, t.dragSide = 0, ""
		t.mu.Unlock()

		if dragging {
			return nil
		}
		if pane := t.paneAt(ev.X, ev.Y); pane != 0 && t.forwardsMouse(pane) {
			return t.client.SendInput(pane, ev.Raw)
		}
		return nil
	}
	return nil
}

func (t *tui) dragDivider(ev ui.MouseEvent) error {
	t.mu.Lock()
	pane, side, at := t.dragPane, t.dragSide, t.dragAt
	t.mu.Unlock()
	if pane == 0 {
		return nil
	}

	now := ev.X
	if side == "up" || side == "down" {
		now = ev.Y
	}
	delta := now - at
	if delta == 0 {
		return nil
	}

	t.mu.Lock()
	t.dragAt = now
	t.mu.Unlock()

	area := t.layoutArea()
	if err := t.client.AdjustSplit(pane, side, delta, area.Cols, area.Rows); err != nil {
		return err
	}
	return t.refresh()
}

// paneAt returns the pane drawn at a point, or zero.
func (t *tui) paneAt(x, y int) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range t.paneRects() {
		if x >= r.X && x < r.X+r.Cols && y >= r.Y && y < r.Y+r.Rows {
			return r.Pane
		}
	}
	return 0
}

// dividerAt reports whether a point is on a border shared with a neighbour,
// and which of the pane's edges it is.
//
// A border on the outside of the layout is not a divider: there is nothing on
// the far side of it to take space from.
func (t *tui) dividerAt(x, y int) (uint64, string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	area := ui.Rect{Y: ui.TabRows(t.countTabsLocked()), Cols: t.cols,
		Rows: t.rows - ui.TabRows(t.countTabsLocked()) - ui.StatusRows}

	for _, r := range t.paneRects() {
		inRows := y >= r.Y && y < r.Y+r.Rows
		inCols := x >= r.X && x < r.X+r.Cols

		if inRows && x == r.X+r.Cols-1 && r.X+r.Cols < area.X+area.Cols {
			return r.Pane, "right", true
		}
		if inRows && x == r.X && r.X > area.X {
			return r.Pane, "left", true
		}
		if inCols && y == r.Y+r.Rows-1 && r.Y+r.Rows < area.Y+area.Rows {
			return r.Pane, "down", true
		}
		if inCols && y == r.Y && r.Y > area.Y {
			return r.Pane, "up", true
		}
	}
	return 0, "", false
}

// forwardsMouse reports whether a pane's own program asked for the mouse, in
// which case a click inside it belongs to that program rather than to tend.
func (t *tui) forwardsMouse(pane uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, p := range t.snap.Panes {
		if p.ID == pane {
			return p.Mouse
		}
	}
	return false
}

func (t *tui) focusPane(pane uint64) {
	t.mu.Lock()
	changed := t.focus != pane
	t.focus = pane
	t.dirty = true
	t.mu.Unlock()
	if changed {
		t.wakeUp()
	}
}
