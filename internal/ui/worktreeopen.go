package ui

import (
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// Opening a worktree is herdr's popup (`worktree_overlays.rs`,
// render_worktree_open_overlay): the repository's checkouts two lines each
// — the branch, and the path under it — with a filter, a count, and whether
// each is open already.

// WorktreeEntry is one checkout.
type WorktreeEntry struct {
	Label  string
	Branch string
	Path   string
	// Status is herdr's status_label: "open" when a space shows it, "" for
	// a branch, "detached" or "root" otherwise.
	Status string
}

// WorktreeOpen is the popup's state.
type WorktreeOpen struct {
	Entries   []WorktreeEntry
	Selected  int
	Query     string
	Searching bool
	Opening   bool
	Error     string
}

// matches is herdr's matches_query: the query in the label, branch, path
// or status, without regard to case.
func (e WorktreeEntry) matches(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	return q == "" || strings.Contains(strings.ToLower(e.Label+" "+e.Branch+" "+e.Path+" "+e.Status), q)
}

// Filtered is the entries the query leaves, by index.
func (w WorktreeOpen) Filtered() []int {
	var out []int
	for i, e := range w.Entries {
		if e.matches(w.Query) {
			out = append(out, i)
		}
	}
	return out
}

// SelectedEntry is the entry the cursor is on, or the first the filter
// leaves when the cursor is on one it hid.
func (w WorktreeOpen) SelectedEntry() (int, bool) {
	f := w.Filtered()
	for _, i := range f {
		if i == w.Selected {
			return i, true
		}
	}
	if len(f) > 0 {
		return f[0], true
	}
	return 0, false
}

// WorktreeOpenRect is herdr's popup: 96 wide, as tall as the entries need
// between 12 and 26 rows, in the middle.
func WorktreeOpenRect(w WorktreeOpen, cols, rows int) Rect {
	height := min(max(len(w.Entries)*2+7, 12), 26)
	r := Rect{Cols: min(96, cols), Rows: min(height, rows)}
	r.X, r.Y = (cols-r.Cols)/2, (rows-r.Rows)/2
	return r
}

// worktreeBody is where the entries go: under the title, the filter and
// the rule, over the message and the keys.
func worktreeBody(w WorktreeOpen, cols, rows int) Rect {
	r := WorktreeOpenRect(w, cols, rows)
	return Rect{X: r.X + 1, Y: r.Y + 4, Cols: r.Cols - 2, Rows: max(r.Rows-7, 0)}
}

// worktreeStart is the first entry shown, of the filtered, keeping the
// selection in view.
func worktreeStart(w WorktreeOpen, visible int) int {
	f := w.Filtered()
	sel, _ := w.SelectedEntry()
	pos := 0
	for i, idx := range f {
		if idx == sel {
			pos = i
		}
	}
	return max(min(pos-(visible-1), len(f)-visible), 0)
}

// WorktreeOpenAt is the entry at a point, whether the point is on the
// filter line, and whether it is inside the popup at all.
func WorktreeOpenAt(w WorktreeOpen, cols, rows, x, y int) (entry int, search, inside bool) {
	r := WorktreeOpenRect(w, cols, rows)
	if x < r.X || x >= r.X+r.Cols || y < r.Y || y >= r.Y+r.Rows {
		return -1, false, false
	}
	if y == r.Y+2 {
		return -1, true, true
	}
	body := worktreeBody(w, cols, rows)
	visible := max(body.Rows/2, 1)
	if y >= body.Y && y < body.Y+visible*2 {
		f := w.Filtered()
		at := worktreeStart(w, visible) + (y-body.Y)/2
		if at < len(f) {
			return f[at], false, true
		}
	}
	return -1, false, true
}

// WorktreeOpenCursor is where the cursor goes while filtering.
func WorktreeOpenCursor(w WorktreeOpen, cols, rows int) (int, int, bool) {
	if !w.Searching {
		return 0, 0, false
	}
	r := WorktreeOpenRect(w, cols, rows)
	return r.X + 1 + 3 + runewidth.StringWidth(w.Query), r.Y + 2, true
}

func drawWorktreeOpen(dst *vt.Grid, w WorktreeOpen, theme Theme) {
	cols, rows := dst.Cols(), dst.Rows()
	r := WorktreeOpenRect(w, cols, rows)
	if r.Cols < 20 || r.Rows < 9 {
		return
	}
	for y := r.Y; y < r.Y+r.Rows; y++ {
		fill(dst, y, r.X, r.X+r.Cols, theme.Menu)
	}
	drawBox(dst, r, theme.BorderFocused)
	drawCloseMark(dst, r, theme.BorderFocused)
	inner := Rect{X: r.X + 1, Y: r.Y + 1, Cols: r.Cols - 2, Rows: r.Rows - 2}
	limit := inner.X + inner.Cols
	bold, dim := theme.Menu, theme.Menu
	bold.Attrs |= vt.AttrBold
	dim.Attrs |= vt.AttrDim
	writeString(dst, inner.X+1, inner.Y, "open worktree", bold, limit)

	filter := " / filter worktrees"
	style := dim
	if w.Searching || w.Query != "" {
		filter = " / " + w.Query
	}
	if w.Searching {
		style = theme.Menu
	}
	writeString(dst, inner.X, inner.Y+1, truncate(filter, inner.Cols), style, limit)
	f := w.Filtered()
	count := strconv.Itoa(len(w.Entries)) + " checkouts "
	if len(f) != len(w.Entries) {
		count = strconv.Itoa(len(f)) + "/" + strconv.Itoa(len(w.Entries)) + " checkouts "
	}
	writeString(dst, limit-runewidth.StringWidth(count), inner.Y+1, count, dim, limit)
	writeString(dst, inner.X, inner.Y+2, strings.Repeat("─", inner.Cols), dim, limit)

	body := worktreeBody(w, cols, rows)
	visible := max(body.Rows/2, 1)
	sel, _ := w.SelectedEntry()
	start := worktreeStart(w, visible)
	for i := 0; i < visible && start+i < len(f); i++ {
		e := w.Entries[f[start+i]]
		y := body.Y + 2*i
		top, under := bold, dim
		if f[start+i] == sel {
			top, under = theme.MenuSelected, theme.MenuSelected
			top.Attrs |= vt.AttrBold
			fill(dst, y, body.X, limit, top)
			fill(dst, y+1, body.X, limit, under)
		}
		writeString(dst, body.X, y, " "+truncate(e.Label, body.Cols-12), top, limit)
		if e.Status != "" {
			writeString(dst, limit-runewidth.StringWidth(e.Status)-1, y, e.Status, top, limit)
		}
		writeString(dst, body.X, y+1, " "+truncate(e.Path, body.Cols-2), under, limit)
	}
	if len(f) == 0 {
		writeString(dst, body.X, body.Y, " no matching worktrees", dim, limit)
	}
	switch {
	case w.Opening:
		writeString(dst, inner.X, inner.Y+inner.Rows-2, " opening…", theme.BorderFocused, limit)
	case w.Error != "":
		writeString(dst, inner.X, inner.Y+inner.Rows-2, " "+truncate(w.Error, inner.Cols-2), theme.Blocked, limit)
	}
	writeString(dst, inner.X, inner.Y+inner.Rows-1, " ↵ open · / filter · esc cancel", dim, limit)
}
