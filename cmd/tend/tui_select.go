package main

import (
	"errors"
	"io"
	"os"
	"time"

	"github.com/sousaakira/tend/internal/clipboard"
	"github.com/sousaakira/tend/internal/proto"
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

// blockModifier switches a drag from a run of text to a rectangle.
//
// It is the convention every terminal already uses for this, which matters
// more than any argument for a different key: somebody reaching for block
// selection reaches for alt without being told.
const blockModifier = ui.ModAlt

// A press cannot be told from the start of a drag, and in a pane whose own
// program asked for the mouse the two want opposite things: a click belongs to
// the program, a drag belongs to the selection.
//
// So the press is remembered and forwarded, and nothing is decided until the
// pointer moves. Moving turns it into a selection; releasing without moving
// leaves it the click the program already saw.

// pendingPress is a press in a mouse-reporting pane, waiting to find out
// whether it was a click or the start of a drag.
type pendingPress struct {
	pane   uint64
	x, y   int
	sent   bool
	native bool
}

// beginSelection starts marking text, or remembers the press until the
// pointer says which it was. It reports whether the press was taken.
func (t *tui) beginSelection(ev ui.MouseEvent) bool {
	pane := t.paneAt(ev.X, ev.Y)
	if pane == 0 {
		return false
	}
	native := t.forwardsMouse(pane)

	t.mu.Lock()
	x, y, ok := t.paneCellLocked(pane, ev.X, ev.Y)
	if !ok {
		t.mu.Unlock()
		return false
	}
	t.press = &pendingPress{pane: pane, x: x, y: y, native: native}
	if !native {
		// Nothing is waiting on the answer, so the selection starts at once
		// and the mark follows the pointer from the first cell.
		t.startSelectionLocked(pane, x, y)
	}
	t.mu.Unlock()

	if native {
		// The program is told about the press either way. If this turns out
		// to be a click it has already had it, and if it turns out to be a
		// drag it is told the button came back up.
		if err := t.client.SendInput(pane, ev.Raw); err != nil {
			return false
		}
		t.press.sent = true
	}
	return true
}

// startSelectionLocked anchors a selection. The caller holds the lock.
func (t *tui) startSelectionLocked(pane uint64, x, y int) {
	t.sel = &ui.Selection{
		Pane:     pane,
		AnchorX:  x,
		AnchorY:  y,
		CursorX:  x,
		CursorY:  y,
		Scroll:   t.selectionScrollLocked(pane),
		Native:   t.forwardsMouseLocked(pane),
		Dragging: true,
	}
	t.dirty = true
}

// forwardsMouseLocked is forwardsMouse for a caller that already holds the
// lock.
func (t *tui) forwardsMouseLocked(pane uint64) bool {
	for _, p := range t.snap.Panes {
		if p.ID == pane {
			return p.Mouse
		}
	}
	return false
}

// dragSelection moves the far end of the selection, and reports whether one is
// being dragged.
//
// The first drag after a press in a mouse-reporting pane is what turns that
// press into a selection. The program is sent a release first, so it is not
// left believing a button is still held down.
func (t *tui) dragSelection(ev ui.MouseEvent) bool {
	t.mu.Lock()
	if t.sel == nil && t.press != nil {
		press := *t.press
		t.startSelectionLocked(press.pane, press.x, press.y)
		t.press = nil
		t.mu.Unlock()
		if press.sent {
			t.releaseNative(press.pane, ev)
		}
		t.mu.Lock()
	}

	defer t.mu.Unlock()
	if t.sel == nil || !t.sel.Dragging {
		return false
	}
	// Clamped into the pane rather than ignored outside it: dragging past the
	// edge to take the rest of a line is the ordinary way to select, and
	// dropping those reports would make the selection stop short of where the
	// pointer plainly is.
	// Dragging past the edge asks for more than the screen is showing, so the
	// view follows the pointer. The direction is remembered rather than acted
	// on here: reports arrive only while the pointer moves, and holding still
	// against the edge is exactly when the scrolling has to keep going.
	t.autoScroll = t.edgeDirectionLocked(t.sel.Pane, ev.Y)

	x, y := t.clampToPaneLocked(t.sel.Pane, ev.X, ev.Y)
	// The shape is read on every report rather than fixed at the press, so
	// the modifier can be taken or let go part way through a drag and the
	// mark answers at once. Deciding it once would mean starting over to
	// change your mind.
	block := ev.Mods.Has(blockModifier)
	if x != t.sel.CursorX || y != t.sel.CursorY || block != t.sel.Block {
		t.sel.CursorX, t.sel.CursorY = x, y
		t.sel.Block = block
		t.dirty = true
	}
	return true
}

// takePendingPress returns the pane of a press that never became a drag, and
// clears it.
func (t *tui) takePendingPress() (uint64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.press == nil {
		return 0, false
	}
	press := *t.press
	t.press = nil
	return press.pane, press.sent
}

// wheelTo sends one notch of the wheel to a pane's own program.
//
// Coordinates are the middle of the pane rather than the pointer: the pointer
// is at the edge, and a program that treats the top row as a header would take
// a wheel event there as meaning something other than "scroll the transcript".
func (t *tui) wheelTo(pane uint64, dir int) error {
	t.mu.Lock()
	// Paced, unlike tend's own scrolling. One line per frame is a steady
	// creep through a scrollback; one wheel notch per frame is what a mouse
	// sends when it is spun as hard as it will go, and a program on the other
	// end would fling its view across the transcript.
	if time.Since(t.lastWheel) < wheelInterval {
		t.mu.Unlock()
		return nil
	}
	t.lastWheel = time.Now()

	var mid ui.Rect
	for _, r := range t.paneRects() {
		if r.Pane == pane {
			mid = ui.Rect{X: r.X + r.Cols/2, Y: r.Y + r.Rows/2}
		}
	}
	t.mu.Unlock()
	if mid.X == 0 && mid.Y == 0 {
		return nil
	}

	button := 64 // wheel up
	if dir > 0 {
		button = 65
	}
	seq := "\x1b[<" + itoaInt(button) + ";" + itoaInt(mid.X+1) + ";" + itoaInt(mid.Y+1) + "M"
	return t.client.SendInput(pane, []byte(seq))
}

// wheelInterval is how often a held drag hands a notch to the pane's program.
// About what a hand turning a wheel deliberately produces.
const wheelInterval = 120 * time.Millisecond

// releaseNative tells a pane's program the button came back up, so a drag that
// became a selection does not leave it holding one.
func (t *tui) releaseNative(pane uint64, ev ui.MouseEvent) {
	_ = t.client.SendInput(pane, []byte("\x1b[<0;"+itoaInt(ev.X+1)+";"+itoaInt(ev.Y+1)+"m"))
}

// endSelection finishes a drag and copies what was marked.
func (t *tui) endSelection() bool {
	t.mu.Lock()
	if t.sel == nil || !t.sel.Dragging {
		t.mu.Unlock()
		return false
	}
	t.sel.Dragging = false
	t.autoScroll = 0
	sel := *t.sel
	if sel.Empty() {
		// A click is not a selection. Clearing it here means a stray click
		// does not leave a one-cell mark behind.
		t.sel = nil
	}
	t.dirty = true
	t.mu.Unlock()

	if sel.Empty() {
		t.wakeUp()
		return true
	}

	// The text comes from the server, not from the rows on screen. A drag
	// that scrolled covers more than the view is showing, and the scrollback
	// it covers is not here.
	text, err := t.client.PaneText(proto.PaneTextParams{
		Pane:    sel.Pane,
		Scroll:  sel.Scroll,
		FromRow: sel.AnchorY,
		FromCol: sel.AnchorX,
		ToRow:   sel.CursorY,
		ToCol:   sel.CursorX,
		Block:   sel.Block,
	})
	if err != nil {
		t.setMessage("copy failed: "+err.Error(), true)
		return true
	}
	if text == "" {
		t.wakeUp()
		return true
	}
	t.copyToClipboard(text, sel.Block)
	return true
}

// edgeDirectionLocked reports which way the view should move for a pointer at
// a pane's edge: -1 back through the history, 1 towards the present, 0 for a
// pointer that is comfortably inside.
//
// The edge is the first and last line of text, not the border around them.
// Dragging up through the text stops at the topmost line, because that is
// where the text stops; a trigger one row further out is a single cell of
// border that nobody aims at, and the first version of this had exactly that
// and so never fired in ordinary use.
func (t *tui) edgeDirectionLocked(pane uint64, y int) int {
	for _, r := range t.paneRects() {
		if r.Pane != pane {
			continue
		}
		top, bottom := r.Y+1, r.Y+r.Rows-2
		switch {
		case y <= top:
			return -1
		case y >= bottom:
			return 1
		}
		return 0
	}
	return 0
}

// autoScrollSelection moves the view while a drag is held against an edge.
//
// It runs on the draw loop rather than on mouse reports because those stop
// arriving the moment the pointer stops moving, and a pointer held at the
// edge is precisely the case this exists for.
func (t *tui) autoScrollSelection() error {
	t.mu.Lock()
	dir := t.autoScroll
	dragging := t.sel != nil && t.sel.Dragging
	pane := uint64(0)
	if t.sel != nil {
		pane = t.sel.Pane
	}
	before := t.selectionScrollLocked(pane)
	t.mu.Unlock()
	if dir == 0 || !dragging || pane == 0 {
		return nil
	}

	if err := t.scrollPaneBy(pane, -dir); err != nil {
		return err
	}

	t.mu.Lock()
	moved := t.selectionScrollLocked(pane) - before
	native := t.sel != nil && t.sel.Native
	t.mu.Unlock()

	if moved == 0 && native {
		// Nothing to scroll to, because a full-screen program keeps no
		// scrollback here: its earlier output never reached this terminal,
		// and it redraws its window from its own memory. The wheel goes to it
		// instead, so its view moves even though tend's cannot.
		return t.wheelTo(pane, dir)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sel == nil {
		return nil
	}
	// The anchor follows the text and the cursor does not. The anchor marks
	// where the drag began, which has just moved down the screen; the cursor
	// marks where the pointer is, and the pointer has not moved. That is the
	// whole of the behaviour: holding against the edge sweeps the far end
	// backwards through text the view is only now showing.
	if moved == 0 {
		// Nothing left to scroll, so there is nothing more to take.
		return nil
	}
	t.sel.AnchorY += moved
	t.sel.Scroll = t.selectionScrollLocked(pane)
	t.dirty = true
	return nil
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

// copyToClipboard puts text where the user's paste will find it.
//
// Both routes are used, because neither is right everywhere. A local tool owns
// the clipboard of the desktop tend is running on, which over ssh is not the
// one the user is looking at. The escape sequence reaches the terminal in
// front of them wherever it is, but terminals cap its size and many refuse it
// outright, and there is no reply to say which.
//
// So the message reports what is actually known: a tool that took the text can
// be waited on and believed, and a sequence can only be said to have been sent.
func (t *tui) copyToClipboard(text string, block bool) {
	if seq := ui.SetClipboard(text); seq != "" {
		_, _ = io.WriteString(os.Stdout, seq)
	}

	via, err := clipboard.Copy(text)
	switch {
	case err == nil && via != "":
		t.setMessage(copiedMessage(text, via, block), false)
	case errors.Is(err, clipboard.ErrNoTool):
		// Nothing local to hand it to, so the terminal is the only hope and
		// there is no way to know whether it took it.
		t.setMessage(copiedMessage(text, "terminal", block)+"?", false)
	default:
		t.setMessage("copy failed: "+err.Error(), true)
	}
}

// copiedMessage says how much was taken and where it went, since the mark is
// about to be redrawn and the user has nothing else to go on.
func copiedMessage(text, via string, block bool) string {
	lines := 1
	for _, r := range text {
		if r == '\n' {
			lines++
		}
	}
	what := itoaInt(len([]rune(text))) + " characters"
	if lines > 1 {
		what = itoaInt(lines) + " lines"
	}
	if block {
		what += " (block)"
	}
	return "copied " + what + " · " + via
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
