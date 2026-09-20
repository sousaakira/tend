package main

import (
	"github.com/sousaakira/tend/internal/ui"
	"github.com/sousaakira/tend/internal/vt"
)

// Scrolling looks back through a pane's history without stopping it.
//
// The pane keeps running and the server keeps its live screen; what changes is
// only which rows this client asks to be drawn. That is why leaving the view
// needs no resynchronisation: nothing was ever out of step.

// splitKeys breaks a chunk into whole keys.
//
// A handler that looks at only the first byte and then reports the chunk
// consumed drops everything after it, which is how "k" followed by Enter
// moved a cursor and never acted on it. Splitting first makes that shape of
// mistake impossible: every key in the chunk is offered in turn.
func splitKeys(data []byte) []string {
	var keys []string
	for i := 0; i < len(data); {
		if data[i] == 0x1b {
			if n := escapeLength(data[i:]); n > 0 {
				keys = append(keys, string(data[i:i+n]))
				i += n
				continue
			}
		}
		keys = append(keys, string(data[i:i+1]))
		i++
	}
	return keys
}

// escapeLength is how many bytes the escape sequence at the start of data
// occupies, or zero when it is not one this cares about.
func escapeLength(data []byte) int {
	if len(data) < 3 || data[1] != '[' {
		return 0
	}
	for i := 2; i < len(data); i++ {
		// A final byte ends a control sequence; the parameters before it are
		// digits and semicolons.
		if data[i] >= '@' && data[i] <= '~' {
			return i + 1
		}
		if data[i] != ';' && (data[i] < '0' || data[i] > '9') {
			return 0
		}
	}
	return 0
}

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
	for _, key := range splitKeys(data) {
		handled, err := t.scrollKey(key)
		if err != nil {
			return true, err
		}
		if !handled {
			// The view is closed and this key belongs to the pane. Anything
			// after it does too, so the rest of the chunk goes back.
			return false, nil
		}
	}
	return len(data) > 0, nil
}

func (t *tui) scrollKey(key string) (bool, error) {
	t.mu.Lock()
	page := max(t.rows-ui.StatusRows-4, 1)
	depth := t.scrollDepth
	t.mu.Unlock()

	switch key {
	case "\x1b[A", "k":
		return true, t.scrollBy(1)
	case "\x1b[B", "j":
		return true, t.scrollBy(-1)
	case "\x1b[5~":
		return true, t.scrollBy(page)
	case "\x1b[6~":
		return true, t.scrollBy(-page)
	case "\x1b[H", "g":
		return true, t.scrollBy(depth)
	case "G", "q", "\x1b", "\x03":
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
		if t.scrollSidebar(ev.X, ev.Y, -sidebarScrollStep) {
			return nil
		}
		if pane := t.paneAt(ev.X, ev.Y); pane != 0 {
			// A program that asked for the mouse gets the wheel. It has its
			// own idea of what is above the screen — an agent's transcript, a
			// pager, an editor — and showing tend's scrollback instead means
			// the wheel does nothing the user recognises.
			if t.forwardsMouse(pane) {
				t.focusPane(pane)
				return t.client.SendInput(pane, ev.Raw)
			}
			t.focusPane(pane)
			if !t.scrolling() {
				return t.enterScroll()
			}
			return t.scrollBy(wheelLines)
		}
		return nil

	case ui.MouseWheelDown:
		if t.scrollSidebar(ev.X, ev.Y, sidebarScrollStep) {
			return nil
		}
		if pane := t.paneAt(ev.X, ev.Y); pane != 0 && t.forwardsMouse(pane) {
			return t.client.SendInput(pane, ev.Raw)
		}
		if t.scrolling() {
			return t.scrollBy(-wheelLines)
		}
		return nil

	case ui.MousePress:
		// An open menu is on top, so it answers first, and a press anywhere
		// else closes it rather than acting through it.
		if handled, err := t.clickMenu(ev.X, ev.Y); handled {
			return err
		}
		if ev.Button == mouseRight {
			if m, ok := t.menuFor(ev.X, ev.Y); ok {
				t.openMenu(m)
			}
			return nil
		}
		if t.grabSidebarDivider(ev.X, ev.Y) {
			return nil
		}
		if handled, err := t.clickTabBar(ev.X, ev.Y); handled {
			return err
		}
		if handled, err := t.clickSidebar(ev.X, ev.Y); handled {
			return err
		}
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

	case ui.MouseMove:
		// Nothing but an open menu follows the pointer, and motion reporting
		// is only on while one is.
		t.hoverMenu(ev.X, ev.Y)
		return nil

	case ui.MouseDrag:
		if t.dragSidebarDivider(ev.Y) {
			return nil
		}
		return t.dragDivider(ev)

	case ui.MouseRelease:
		// paneAt takes the same lock, so the grab is released first and the
		// lookup happens after. A mutex that is not reentrant turns a nested
		// call into a frozen client, which is exactly how this was found.
		t.mu.Lock()
		dragging := t.dragPane != 0 || t.draggingSidebar
		t.dragPane, t.dragSide = 0, ""
		t.draggingSidebar = false
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

// sidebarScrollStep is how far one notch of the wheel moves the list. Two
// entries, because most of them are two lines and moving by one would read as
// half a step.
const sidebarScrollStep = 2

// wheelLines is how far one notch moves a pane's view.
const wheelLines = 3

// scrollSidebar moves the list when the pointer is over it, and reports
// whether it took the event.
//
// The sidebar takes the wheel before the panes do: the pointer is over the
// list, and scrolling the pane under a pointer that is not on it is the kind
// of thing that makes people stop trusting the mouse.
func (t *tui) scrollSidebar(x, y, delta int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.sidebar {
		return false
	}
	frame := t.buildFrame()
	spaces, agents := ui.SidebarRegions(frame, t.rows)

	switch ui.SidebarPlaceAt(frame, x, y, t.rows) {
	case ui.SidebarSpacesList:
		t.spacesScroll = clampScroll(t.spacesScroll+delta, frame.Spaces, spaces.Rows)
	case ui.SidebarAgentsList:
		t.agentsScroll = clampScroll(t.agentsScroll+delta, frame.Agents, agents.Rows)
	case ui.SidebarDivider:
		// The divider is a handle, not a list. The wheel over it does nothing
		// rather than scrolling whichever side happens to be nearer.
		return true
	default:
		return false
	}
	t.dirty = true
	// Taken either way: at the end of a list the wheel has nowhere to go, and
	// falling through to the pane behind would be a surprise.
	return true
}

func clampScroll(at int, s ui.SidebarSection, height int) int {
	return min(max(at, 0), ui.SidebarMaxScroll(s, height))
}

// grabSidebarDivider takes hold of the line between the two lists, and reports
// whether the press was on it.
//
// A press on a divider is a grab, not a click: the user is reaching for the
// line, not for whatever is drawn under it.
func (t *tui) grabSidebarDivider(x, y int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.sidebar {
		return false
	}
	if ui.SidebarPlaceAt(t.buildFrame(), x, y, t.rows) != ui.SidebarDivider {
		return false
	}
	t.draggingSidebar = true
	return true
}

// dragSidebarDivider moves the line to where the pointer is, and reports
// whether it was the one being dragged.
//
// The line follows the pointer rather than moving by steps, and the clamping
// lives in the sidebar with the drawing, so a drag past either end stops where
// the divider can actually go instead of storing a position it cannot.
func (t *tui) dragSidebarDivider(y int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.draggingSidebar {
		return false
	}
	frame := t.buildFrame()
	frame.SidebarSplit = y
	if next := ui.SidebarSplitAt(frame, t.rows); next != t.sidebarSplit {
		t.sidebarSplit = next
		t.dirty = true
	}
	return true
}

// mouseRight is the button number a right-click reports, which is what opens
// a menu here as it does everywhere else.
const mouseRight = 2

// clickTabBar switches tabs, or makes one.
func (t *tui) clickTabBar(x, y int) (bool, error) {
	t.mu.Lock()
	frame := t.buildFrame()
	cols := t.cols
	t.mu.Unlock()

	tab, newTab, ok := ui.TabAt(frame, x, y, cols)
	if !ok {
		return false, nil
	}
	if newTab {
		return true, t.newTabHere()
	}
	return true, t.showTab(tab)
}

// clickSidebar goes wherever the clicked row points.
func (t *tui) clickSidebar(x, y int) (bool, error) {
	t.mu.Lock()
	frame := t.buildFrame()
	rows := t.rows
	t.mu.Unlock()

	// The handle answers first: it is drawn over the corner of a list, and a
	// click there means the handle, not whatever row is underneath.
	if ui.SidebarHandleAt(frame, x, y, rows) {
		return true, t.toggleSidebar()
	}

	row, ok := ui.SidebarRowAt(frame, x, y, rows)
	if !ok {
		return false, nil
	}
	switch {
	case row.Action == ui.ActionNewSpace:
		return true, t.newWorkspace()
	case row.Action == ui.ActionToggleGroup:
		t.toggleGroup(row.Group)
		return true, nil
	case row.Action == ui.ActionOpenMenu:
		t.mu.Lock()
		ws := t.workspace
		t.mu.Unlock()
		t.openMenu(ui.SpaceMenu(ws, x, y))
		return true, nil
	case row.Action == ui.ActionToggleGrouped:
		t.mu.Lock()
		t.grouped = !t.grouped
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
		return true, nil
	case row.Pane != 0:
		return true, t.jumpToPane(row.Pane)
	case row.Tab != 0:
		return true, t.showTab(row.Tab)
	case row.Workspace != 0:
		return true, t.showWorkspace(row.Workspace)
	}
	return true, nil
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

	area := t.layoutAreaLocked()

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
