package main

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/sousaakira/tend/internal/clipboard"
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/ui"
)

// There are two kinds of pane as far as the mouse goes, and they are handled
// by two different parties.
//
// A program that asked for the mouse gets it, whole: press, drag, release and
// wheel, in its own coordinates. Such a program does its own selecting, over
// its own scrollback — which for a full-screen agent is the only scrollback
// there is, since its earlier output never reaches this terminal — and when
// it copies it says so with OSC 52, which is passed on to the clipboard.
//
// A program that did not ask has no idea the mouse exists, so tend marks the
// text itself, scrolls its own history under the drag, and cuts the text out
// of the server's copy of the terminal.
//
// An earlier version of this tried to do the first kind's selecting for it:
// holding the drag back from the program, guessing from repaints how far its
// view had moved, feeding it wheel events. Every part of that was a heuristic
// standing in for something the program already does correctly.

// forceModifier takes the mouse away from a program that asked for it, and
// selects a rectangle in a pane that did not.
//
// One key for both because both mean the same thing to the hand: "tend's
// selection, the precise kind". It is also the key every terminal already
// uses for block selection.
const forceModifier = ui.ModAlt

// beginSelection starts marking text, and reports whether the press was taken.
func (t *tui) beginSelection(ev ui.MouseEvent) bool {
	pane := t.paneAt(ev.X, ev.Y)
	if pane == 0 {
		return false
	}
	if t.forwardsMouse(pane) && !ev.Mods.Has(forceModifier) {
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
	// Dragging past the edge asks for more than the screen is showing, so the
	// view follows the pointer. The direction is remembered rather than acted
	// on here: reports arrive only while the pointer moves, and holding still
	// against the edge is exactly when the scrolling has to keep going.
	t.autoScroll = t.edgeDirectionLocked(t.sel.Pane, ev.Y)

	// Clamped into the pane rather than ignored outside it: dragging past the
	// edge to take the rest of a line is the ordinary way to select.
	x, y := t.clampToPaneLocked(t.sel.Pane, ev.X, ev.Y)
	// The shape is read on every report rather than fixed at the press, so
	// the modifier can be taken or let go part way through a drag.
	block := ev.Mods.Has(forceModifier)
	if x != t.sel.CursorX || y != t.sel.CursorY || block != t.sel.Block {
		t.sel.CursorX, t.sel.CursorY = x, y
		t.sel.Block = block
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
	if strings.TrimSpace(text) == "" {
		// Blank is not something to put on a clipboard, and saying "copied"
		// for it is worse than saying nothing: the user pastes and finds the
		// last real thing they copied replaced by empty lines.
		t.setMessage("nothing to copy there", false)
		return true
	}
	t.copyToClipboard(text, copiedMessage(text, sel.Block))
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
func (t *tui) copyToClipboard(text, what string) {
	if seq := ui.SetClipboard(text); seq != "" {
		_, _ = io.WriteString(os.Stdout, seq)
	}

	via, err := clipboard.Copy(text)
	switch {
	case err == nil && via != "":
		t.setMessage(what+" · "+via, false)
	case errors.Is(err, clipboard.ErrNoTool):
		// Nothing local to hand it to, so the terminal is the only hope and
		// there is no way to know whether it took it.
		t.setMessage(what+" · terminal?", false)
	default:
		t.setMessage("copy failed: "+err.Error(), true)
	}
}

// copiedMessage says how much was taken, since the mark is about to be redrawn
// and the user has nothing else to go on.
func copiedMessage(text string, block bool) string {
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
	return "copied " + what
}

// paneCopied handles a pane's own program asking for text to be copied.
//
// This is how copying works in a pane that holds the mouse: the program did
// the selecting and this is it handing over the result. It arrives on the
// client's reader goroutine, and a clipboard tool is a process that can take
// a moment, so the work is moved off it.
func (t *tui) paneCopied(text []byte) {
	if len(text) == 0 {
		return
	}
	go t.copyToClipboard(string(text), copiedMessage(string(text), false))
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

// edgeDirectionLocked reports which way the view should move for a pointer at
// a pane's edge: -1 back through the history, 1 towards the present, 0 for a
// pointer that is comfortably inside.
//
// The edge is the first and last line of text, not the border around them.
// Dragging up through the text stops at the topmost line, because that is
// where the text stops; a trigger one row further out is a single cell of
// border that nobody aims at.
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
	defer t.mu.Unlock()
	if t.sel == nil {
		return nil
	}
	// The anchor follows the text and the cursor does not. The anchor marks
	// where the drag began, which has just moved down the screen; the cursor
	// marks where the pointer is, and the pointer has not moved. Holding
	// against the edge sweeps the far end through text the view is only now
	// showing.
	moved := t.selectionScrollLocked(pane) - before
	if moved == 0 {
		return nil
	}
	t.sel.AnchorY += moved
	t.sel.Scroll = t.selectionScrollLocked(pane)
	t.dirty = true
	return nil
}

// --- the mouse, for a program that asked for it ------------------------------

// forwardMouse hands a mouse report to a pane's own program, rewritten into
// that pane's coordinates. It reports whether the pane wanted it.
//
// clamp pulls a point outside the pane to its nearest cell instead of dropping
// it: a drag that began inside belongs to the program until the button comes
// up, wherever the pointer wanders in between.
func (t *tui) forwardMouse(pane uint64, ev ui.MouseEvent, clamp bool) (bool, error) {
	t.mu.Lock()
	var info proto.PaneInfo
	for _, p := range t.snap.Panes {
		if p.ID == pane {
			info = p
		}
	}
	x, y, inside := t.paneCellLocked(pane, ev.X, ev.Y)
	if !inside && clamp {
		x, y = t.clampToPaneLocked(pane, ev.X, ev.Y)
		inside = true
	}
	t.mu.Unlock()

	if !info.Mouse || !inside {
		return false, nil
	}
	if !t.serverHas(proto.FeatureMouseDetail) {
		// A server from before it reported the detail says only that the
		// program wants the mouse. Reading the missing fields as "no drags,
		// legacy encoding" sends a modern program reports it cannot parse and
		// withholds the ones it needs, so the overwhelmingly common answer is
		// assumed instead: button events with drags, in SGR.
		info.MouseDrag, info.MouseSGR = true, true
	}
	// Only what it subscribed to. A program that asked for clicks alone has
	// no code for a drag report, and what it does with one is its own
	// business and nobody's idea of correct.
	if ev.Kind == ui.MouseDrag && !info.MouseDrag {
		return true, nil
	}
	if ev.Kind == ui.MouseMove && !info.MouseMotion {
		return true, nil
	}

	seq := ui.EncodeMouse(ev, x, y, info.MouseSGR)
	if seq == nil {
		return true, nil
	}
	return true, t.client.SendInput(pane, seq)
}

// serverHas reports whether the server said it provides a feature.
func (t *tui) serverHas(feature string) bool {
	for _, f := range t.client.Server().Features {
		if f == feature {
			return true
		}
	}
	return false
}

// beginGesture gives a press to the pane's program and remembers that the rest
// of the gesture is its too.
func (t *tui) beginGesture(pane uint64, ev ui.MouseEvent) (bool, error) {
	took, err := t.forwardMouse(pane, ev, false)
	if !took || err != nil {
		return took, err
	}
	t.mu.Lock()
	t.gesture = pane
	t.mu.Unlock()
	return true, nil
}

// continueGesture passes a drag or a release on to the pane that was given the
// press, and reports whether there was one.
//
// Bound to the pane rather than to wherever the pointer is: a drag that leaves
// the pane is still that program's drag, and a release it never hears about
// leaves it believing a button is held for the rest of the session.
func (t *tui) continueGesture(ev ui.MouseEvent) (bool, error) {
	t.mu.Lock()
	pane := t.gesture
	if ev.Kind == ui.MouseRelease {
		t.gesture = 0
	}
	t.mu.Unlock()
	if pane == 0 {
		return false, nil
	}
	_, err := t.forwardMouse(pane, ev, true)
	return true, err
}
