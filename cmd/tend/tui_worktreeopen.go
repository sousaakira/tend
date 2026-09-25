package main

import (
	"unicode/utf8"

	"github.com/auth-com-br/tend/internal/api"
	sessionpkg "github.com/auth-com-br/tend/internal/session"
	"github.com/auth-com-br/tend/internal/ui"
)

// The open-worktree popup, herdr's: the space's repository's checkouts,
// filtered by typing after /, opened with enter or a click. What it looks
// like and where a click lands are ui's; this is the client's state and
// what it asks the session for.

type worktreeOpenState struct {
	popup     ui.WorktreeOpen
	workspace uint64
}

func (t *tui) worktreeOpenUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.worktreeOpen != nil
}

func (t *tui) worktreeOpenFrameLocked() *ui.WorktreeOpen {
	if t.worktreeOpen == nil {
		return nil
	}
	w := t.worktreeOpen.popup
	return &w
}

func (t *tui) closeWorktreeOpen() {
	t.mu.Lock()
	t.worktreeOpen = nil
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// openWorktreeMenu lists the repository's worktrees to open one.
func (t *tui) openWorktreeMenu(workspace uint64, x, y int) {
	t.mu.Lock()
	session := t.session
	t.mu.Unlock()

	go func() {
		result, err := apiCall(session, api.MethodWorktreeList, map[string]any{
			"workspace_id": api.WorkspaceID(sessionpkg.WorkspaceID(workspace)),
		}, false)
		if err != nil {
			t.setMessage(err.Error(), true)
			return
		}
		list, _ := result["worktrees"].([]any)
		var entries []ui.WorktreeEntry
		for _, raw := range list {
			wt, _ := raw.(map[string]any)
			if wt["is_bare"] == true || wt["is_prunable"] == true {
				continue
			}
			e := ui.WorktreeEntry{Branch: text(wt["branch"]), Path: text(wt["path"])}
			e.Label = e.Branch
			if e.Label == "" {
				e.Label = baseName(e.Path)
			}
			// herdr's status_label.
			switch {
			case text(wt["open_workspace_id"]) != "":
				e.Status = "open"
			case e.Branch != "":
			case wt["is_detached"] == true && wt["is_linked_worktree"] == true:
				e.Status = "detached"
			default:
				e.Status = "root"
			}
			entries = append(entries, e)
		}
		if len(entries) == 0 {
			t.setMessage("this repository has no worktrees to open", false)
			return
		}
		t.mu.Lock()
		t.worktreeOpen = &worktreeOpenState{popup: ui.WorktreeOpen{Entries: entries}, workspace: workspace}
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}()
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

// submitWorktreeOpen opens the selected checkout, saying so in the popup,
// and puts the popup away once the space is there — or keeps it up with
// git's reason when it is not.
func (t *tui) submitWorktreeOpen() {
	t.mu.Lock()
	w := t.worktreeOpen
	if w == nil || w.popup.Opening {
		t.mu.Unlock()
		return
	}
	i, ok := w.popup.SelectedEntry()
	if !ok {
		t.mu.Unlock()
		return
	}
	w.popup.Opening, w.popup.Error = true, ""
	path, workspace, session := w.popup.Entries[i].Path, w.workspace, t.session
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()

	go func() {
		result, err := apiCall(session, api.MethodWorktreeOpen, map[string]any{
			"workspace_id": api.WorkspaceID(sessionpkg.WorkspaceID(workspace)),
			"path":         path,
		}, false)
		if err != nil {
			t.mu.Lock()
			if t.worktreeOpen != nil {
				t.worktreeOpen.popup.Opening, t.worktreeOpen.popup.Error = false, err.Error()
				t.dirty = true
			}
			t.mu.Unlock()
			t.wakeUp()
			return
		}
		t.closeWorktreeOpen()
		if err := t.refresh(); err != nil {
			t.setMessage(err.Error(), true)
			return
		}
		ws, _ := result["workspace"].(map[string]any)
		if id, ok := parseWorkspaceID(text(ws["workspace_id"])); ok {
			_ = t.showWorkspace(id)
		}
	}()
}

// moveWorktreeOpen moves the selection among the filtered entries.
func (t *tui) moveWorktreeOpenLocked(delta int) {
	w := &t.worktreeOpen.popup
	f := w.Filtered()
	if len(f) == 0 {
		return
	}
	sel, _ := w.SelectedEntry()
	pos := 0
	for i, idx := range f {
		if idx == sel {
			pos = i
		}
	}
	w.Selected = f[min(max(pos+delta, 0), len(f)-1)]
}

// worktreeOpenKeys is herdr's keys for the popup: esc, enter, the arrows,
// / to filter, typing and backspace while filtering.
func (t *tui) worktreeOpenKeys(data []byte) {
	for _, key := range splitKeys(data) {
		t.mu.Lock()
		w := t.worktreeOpen
		if w == nil {
			t.mu.Unlock()
			return
		}
		opening, searching := w.popup.Opening, w.popup.Searching
		t.dirty = true
		switch {
		case key == "\x1b" || key == "\x03":
			if !opening {
				t.worktreeOpen = nil
			}
			t.mu.Unlock()
		case key == "\r" || key == "\n":
			t.mu.Unlock()
			t.submitWorktreeOpen()
		case opening:
			t.mu.Unlock()
		case key == "\x1b[A":
			t.moveWorktreeOpenLocked(-1)
			t.mu.Unlock()
		case key == "\x1b[B":
			t.moveWorktreeOpenLocked(1)
			t.mu.Unlock()
		case key == "/" && !searching:
			w.popup.Searching = true
			t.mu.Unlock()
		case searching && (key == "\x7f" || key == "\x08"):
			if q := w.popup.Query; q != "" {
				_, size := utf8.DecodeLastRuneInString(q)
				w.popup.Query = q[:len(q)-size]
			}
			if f := w.popup.Filtered(); len(f) > 0 {
				w.popup.Selected = f[0]
			}
			t.mu.Unlock()
		case searching && len(key) == 1 && (key[0] >= 0x20 && key[0] != 0x7f || key[0] >= 0x80):
			w.popup.Query += key
			if f := w.popup.Filtered(); len(f) > 0 {
				w.popup.Selected = f[0]
			}
			t.mu.Unlock()
		default:
			t.mu.Unlock()
		}
	}
	t.wakeUp()
}

// worktreeOpenMouse answers the mouse while the popup is up: a click on an
// entry opens it, on the filter line starts filtering, outside closes.
func (t *tui) worktreeOpenMouse(ev ui.MouseEvent) (bool, error) {
	t.mu.Lock()
	w := t.worktreeOpen
	if w == nil {
		t.mu.Unlock()
		return false, nil
	}
	entry, search, inside := ui.WorktreeOpenAt(w.popup, t.cols, t.rows, ev.X, ev.Y)
	t.dirty = true
	switch ev.Kind {
	case ui.MouseWheelUp:
		t.moveWorktreeOpenLocked(-1)
	case ui.MouseWheelDown:
		t.moveWorktreeOpenLocked(1)
	case ui.MousePress:
		switch {
		case (!inside || ui.OnCloseMark(ui.WorktreeOpenRect(w.popup, t.cols, t.rows), ev.X, ev.Y)) && !w.popup.Opening:
			t.worktreeOpen = nil
		case search:
			w.popup.Searching = true
		case entry >= 0:
			w.popup.Selected = entry
			t.mu.Unlock()
			t.submitWorktreeOpen()
			return true, nil
		}
	}
	t.mu.Unlock()
	t.wakeUp()
	return true, nil
}
