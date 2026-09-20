package main

import (
	"strings"

	"github.com/sousaakira/tend/internal/copymode"
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/ui"
)

// Copy mode is keyboard selection through a pane's history, the way tmux does
// it and herdr after it (`client/shell/copy_mode.rs`): `ctrl+b [` to enter, vi
// motions to move, `v` or space to start a selection, `V` for whole lines, `y`
// or enter to copy, `/` and `?` to search, `q` or escape to leave.
//
// It sits on the scroll view: the pane is shown as a still picture at some
// distance back, and the cursor moves through that picture, scrolling it when
// the cursor would leave it. Where a word or a match is, the server answers,
// because that is where the text is; everything else — the cursor, the
// selection, the search being typed — is this client's and nobody else's.
//
// Rows are absolute here: row 0 is the oldest line the pane keeps. A viewport
// row names a different line every time the view moves, which is no way to
// remember where a selection started.

// copyState is copy mode while it is up.
type copyState struct {
	pane uint64
	// cursor is where the copy cursor is, and anchor where a selection began.
	cursor proto.CopyPoint
	anchor *proto.CopyPoint
	lines  bool // V: whole lines rather than a run of characters
	// history and rows are the pane's shape as last reported, which is what
	// turns an absolute row into a place on screen.
	history, rows, cols int

	// search is the query being typed after / or ?, and searching says the
	// prompt is up. lastQuery and lastDir are what n and N repeat.
	searching bool
	search    string
	searchDir string
	lastQuery string
	lastDir   string
}

// copying reports whether copy mode is up.
func (t *tui) copying() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.copy != nil
}

// enterCopy starts copy mode on the focused pane, with the cursor on the last
// line of what is shown.
func (t *tui) enterCopy() error {
	t.mu.Lock()
	pane := t.focus
	cols, rows := t.paneSizeLocked(pane)
	t.mu.Unlock()
	if pane == 0 || rows <= 0 {
		return nil
	}

	// Asked for its shape by asking it to move nowhere: the answer carries
	// the history, which nothing else this client holds does.
	shape, err := t.client.CopyMotion(pane, proto.CopyPoint{}, copymode.FirstNonBlank)
	if err != nil {
		if t.reportStaleServer(err) {
			return nil
		}
		return err
	}

	t.mu.Lock()
	t.copy = &copyState{
		pane:    pane,
		history: shape.History,
		rows:    max(shape.Rows, rows),
		cols:    cols,
		cursor:  proto.CopyPoint{Row: shape.History + max(shape.Rows, rows) - 1},
	}
	t.scrollPane = pane
	t.mu.Unlock()
	return t.showCopyCursor()
}

// leaveCopy ends copy mode and the view under it.
func (t *tui) leaveCopy() {
	t.mu.Lock()
	t.copy = nil
	t.sel = nil
	t.mu.Unlock()
	t.leaveScroll()
}

// showCopyCursor scrolls the view so the cursor is on it, and redraws the
// selection from the anchor to the cursor.
func (t *tui) showCopyCursor() error {
	t.mu.Lock()
	c := t.copy
	if c == nil {
		t.mu.Unlock()
		return nil
	}
	offset := t.scrollOffset
	top := c.history - offset
	switch {
	case c.cursor.Row < top:
		offset = c.history - c.cursor.Row
	case c.cursor.Row > top+c.rows-1:
		offset = c.history + c.rows - 1 - c.cursor.Row
	}
	offset = min(max(offset, 0), c.history)
	pane := c.pane
	t.mu.Unlock()

	if err := t.scrollTo(pane, offset); err != nil {
		return err
	}
	t.mu.Lock()
	t.sel = t.copySelectionLocked()
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	return nil
}

// copySelectionLocked is the selection copy mode shows, in the viewport terms
// the drawing uses. The caller holds the lock.
func (t *tui) copySelectionLocked() *ui.Selection {
	c := t.copy
	if c == nil || c.anchor == nil {
		return nil
	}
	top := c.history - t.scrollOffset
	from, to := *c.anchor, c.cursor
	if c.lines {
		if from.Row > to.Row {
			from, to = to, from
		}
		from.Col, to.Col = 0, max(c.cols-1, 0)
	}
	return &ui.Selection{
		Pane:    c.pane,
		AnchorX: from.Col, AnchorY: from.Row - top,
		CursorX: to.Col, CursorY: to.Row - top,
		Scroll: t.scrollOffset,
	}
}

// copyCursorLocked is where the copy cursor is on screen, for the frame.
func (t *tui) copyCursorLocked() *ui.CopyCursor {
	c := t.copy
	if c == nil {
		return nil
	}
	return &ui.CopyCursor{
		Pane: c.pane,
		X:    c.cursor.Col,
		Y:    c.cursor.Row - (c.history - t.scrollOffset),
	}
}

// copyKeys drives copy mode. It reports whether the input was taken, which it
// always is: a key pressed in copy mode is never meant for the pane.
func (t *tui) copyKeys(data []byte) (bool, error) {
	for _, key := range splitKeys(data) {
		if err := t.copyKey(key); err != nil {
			return true, err
		}
		if !t.copying() {
			break
		}
	}
	return true, nil
}

// copyKey is one key. The map is herdr's, which is vi's.
func (t *tui) copyKey(key string) error {
	t.mu.Lock()
	c := t.copy
	if c == nil {
		t.mu.Unlock()
		return nil
	}
	if c.searching {
		t.mu.Unlock()
		return t.searchKey(key)
	}
	page := max(c.rows-2, 1)
	half := max(c.rows/2, 1)
	t.mu.Unlock()

	switch key {
	case "q", "\x03":
		t.leaveCopy()
		return nil
	case "\x1b":
		// Escape clears a selection first and leaves on the second press,
		// so the one that undoes a mistake does not also throw the place away.
		t.mu.Lock()
		hadSelection := t.copy.anchor != nil
		t.copy.anchor = nil
		t.mu.Unlock()
		if hadSelection {
			return t.showCopyCursor()
		}
		t.leaveCopy()
		return nil
	case "y", "\r":
		return t.yank()
	case "v", " ":
		return t.beginCopySelection(false)
	case "V":
		return t.beginCopySelection(true)
	case "h", "\x1b[D":
		return t.moveCopy(0, -1)
	case "l", "\x1b[C":
		return t.moveCopy(0, 1)
	case "j", "\x1b[B":
		return t.moveCopy(1, 0)
	case "k", "\x1b[A":
		return t.moveCopy(-1, 0)
	case "\x1b[5~", "\x02": // page up, ctrl+b
		return t.moveCopy(-page, 0)
	case "\x1b[6~", "\x06": // page down, ctrl+f
		return t.moveCopy(page, 0)
	case "\x15": // ctrl+u
		return t.moveCopy(-half, 0)
	case "\x04": // ctrl+d
		return t.moveCopy(half, 0)
	case "g":
		return t.setCopyCursor(func(c *copyState) { c.cursor = proto.CopyPoint{} })
	case "G":
		return t.setCopyCursor(func(c *copyState) {
			c.cursor = proto.CopyPoint{Row: c.history + c.rows - 1}
		})
	case "0", "\x1b[H":
		return t.setCopyCursor(func(c *copyState) { c.cursor.Col = 0 })
	case "$", "\x1b[F":
		return t.copyMotion(copymode.LineEnd)
	case "^":
		return t.copyMotion(copymode.FirstNonBlank)
	case "w":
		return t.copyMotion(copymode.NextWordStart)
	case "b":
		return t.copyMotion(copymode.PreviousWordStart)
	case "e":
		return t.copyMotion(copymode.NextWordEnd)
	case "W":
		return t.copyMotion(copymode.NextBigWordStart)
	case "B":
		return t.copyMotion(copymode.PreviousBigWordStart)
	case "E":
		return t.copyMotion(copymode.NextBigWordEnd)
	case "{":
		return t.copyMotion(copymode.PreviousParagraph)
	case "}":
		return t.copyMotion(copymode.NextParagraph)
	case "/":
		return t.openCopySearch(copymode.Forward)
	case "?":
		return t.openCopySearch(copymode.Backward)
	case "n":
		return t.repeatCopySearch(false)
	case "N":
		return t.repeatCopySearch(true)
	}
	return nil // anything else does nothing, as in tmux
}

// moveCopy moves the cursor by rows and columns, inside what the pane has.
func (t *tui) moveCopy(rows, cols int) error {
	return t.setCopyCursor(func(c *copyState) {
		c.cursor.Row = min(max(c.cursor.Row+rows, 0), c.history+c.rows-1)
		c.cursor.Col = min(max(c.cursor.Col+cols, 0), max(c.cols-1, 0))
	})
}

func (t *tui) setCopyCursor(fn func(*copyState)) error {
	t.mu.Lock()
	if t.copy == nil {
		t.mu.Unlock()
		return nil
	}
	fn(t.copy)
	t.mu.Unlock()
	return t.showCopyCursor()
}

// copyMotion asks the server where a motion lands and goes there.
func (t *tui) copyMotion(motion string) error {
	t.mu.Lock()
	c := t.copy
	if c == nil {
		t.mu.Unlock()
		return nil
	}
	pane, from := c.pane, c.cursor
	t.mu.Unlock()

	result, err := t.client.CopyMotion(pane, from, motion)
	if err != nil {
		if t.reportStaleServer(err) {
			return nil
		}
		return err
	}
	return t.setCopyCursor(func(c *copyState) {
		c.cursor = result.To
		c.history, c.rows = result.History, max(result.Rows, 1)
	})
}

func (t *tui) beginCopySelection(lines bool) error {
	return t.setCopyCursor(func(c *copyState) {
		if c.anchor != nil && c.lines == lines {
			c.anchor = nil // pressing it again drops the selection, as in vi
			return
		}
		anchor := c.cursor
		c.anchor, c.lines = &anchor, lines
	})
}

// yank copies the selection, or the cursor's line when there is none, and
// leaves copy mode.
func (t *tui) yank() error {
	t.mu.Lock()
	c := t.copy
	if c == nil {
		t.mu.Unlock()
		return nil
	}
	from, to := c.cursor, c.cursor
	lines := c.lines
	if c.anchor != nil {
		from = *c.anchor
	} else {
		lines = true // nothing selected: the line under the cursor, as tmux does
	}
	if from.Row > to.Row || (from.Row == to.Row && from.Col > to.Col) {
		from, to = to, from
	}
	if lines {
		from.Col, to.Col = 0, max(c.cols-1, 0)
	}
	top := c.history - t.scrollOffset
	pane, scroll := c.pane, t.scrollOffset
	t.mu.Unlock()

	// PaneText reads rows relative to the view, and a row outside it is
	// reached by going past its edge — which is what a selection taller than
	// the screen has to do.
	text, err := t.client.PaneText(proto.PaneTextParams{
		Pane: pane, Scroll: scroll,
		FromRow: from.Row - top, FromCol: from.Col,
		ToRow: to.Row - top, ToCol: to.Col,
	})
	if err != nil {
		if t.reportStaleServer(err) {
			return nil
		}
		return err
	}
	t.leaveCopy()
	if strings.TrimSpace(text) == "" {
		t.setMessage("nothing to copy there", false)
		return nil
	}
	t.copyToClipboard(text, copiedMessage(text, false))
	return nil
}

// --- search ------------------------------------------------------------------

func (t *tui) openCopySearch(direction string) error {
	t.mu.Lock()
	if t.copy != nil {
		t.copy.searching, t.copy.search, t.copy.searchDir = true, "", direction
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
	return nil
}

// searchKey edits the query being typed, and runs it on enter.
func (t *tui) searchKey(key string) error {
	t.mu.Lock()
	c := t.copy
	if c == nil {
		t.mu.Unlock()
		return nil
	}
	switch key {
	case "\x1b", "\x03":
		c.searching = false
	case "\r":
		c.searching = false
		if c.search != "" {
			c.lastQuery, c.lastDir = c.search, c.searchDir
			t.mu.Unlock()
			return t.runCopySearch(false)
		}
	case "\x7f", "\b":
		if r := []rune(c.search); len(r) > 0 {
			c.search = string(r[:len(r)-1])
		}
	case "\x15": // ctrl+u clears the query, as in a shell
		c.search = ""
	default:
		if len(key) > 0 && key[0] >= 0x20 && key[0] != 0x7f {
			c.search += key
		}
	}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	return nil
}

func (t *tui) repeatCopySearch(reverse bool) error {
	t.mu.Lock()
	has := t.copy != nil && t.copy.lastQuery != ""
	t.mu.Unlock()
	if !has {
		return nil
	}
	return t.runCopySearch(reverse)
}

// runCopySearch goes to the next match of the last query.
func (t *tui) runCopySearch(reverse bool) error {
	t.mu.Lock()
	c := t.copy
	if c == nil {
		t.mu.Unlock()
		return nil
	}
	pane, from, query, dir := c.pane, c.cursor, c.lastQuery, c.lastDir
	t.mu.Unlock()
	if reverse {
		if dir == copymode.Backward {
			dir = copymode.Forward
		} else {
			dir = copymode.Backward
		}
	}

	result, err := t.client.CopySearch(pane, from, query, dir)
	if err != nil {
		if t.reportStaleServer(err) {
			return nil
		}
		return err
	}
	if !result.Found {
		t.setMessage("not found: "+query, true)
		return nil
	}
	t.setMessage("/"+query+" · "+itoaInt(result.Total)+" found", false)
	return t.setCopyCursor(func(c *copyState) {
		c.cursor = result.To
		c.history, c.rows = result.History, max(result.Rows, 1)
	})
}

// copyPromptLocked is the search line while it is being typed. The caller
// holds the lock.
func (t *tui) copyPromptLocked() string {
	if t.copy == nil || !t.copy.searching {
		return ""
	}
	lead := "/"
	if t.copy.searchDir == copymode.Backward {
		lead = "?"
	}
	return lead + t.copy.search
}
