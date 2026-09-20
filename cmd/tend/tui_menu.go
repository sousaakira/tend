package main

import (
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/ui"
)

// The menu is how everything that is not a jump gets done with the mouse.
//
// It is opened on the thing it acts on — right-click a pane and the menu
// closes that pane, right-click a space and it closes that space — so there
// is no current selection to be wrong about. Whatever the menu does, it does
// to what was under the pointer when it opened.

// openMenu shows a menu, replacing any already open.
func (t *tui) openMenu(m ui.Menu) {
	t.mu.Lock()
	t.menu = &m
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) closeMenu() {
	t.mu.Lock()
	had := t.menu != nil
	t.menu = nil
	t.dirty = true
	t.mu.Unlock()
	if had {
		t.wakeUp()
	}
}

func (t *tui) menuOpen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.menu != nil
}

// menuFor builds the menu for whatever is at a point, or reports that there is
// nothing there to act on.
func (t *tui) menuFor(x, y int) (ui.Menu, bool) {
	t.mu.Lock()
	frame := t.buildFrame()
	cols, rows := t.cols, t.rows
	panes := len(t.rects)
	t.mu.Unlock()

	if row, ok := ui.SidebarRowAt(frame, x, y, rows); ok {
		switch row.Kind {
		case ui.SidebarSpace:
			return ui.SpaceMenu(row.Workspace, x, y), true
		case ui.SidebarSpaceGroup:
			return ui.GroupMenu(row.Group, row.Folded, x, y), true
		case ui.SidebarAgent:
			return ui.AgentMenu(row.Pane, row.Tab, row.Workspace, x, y), true
		}
		return ui.Menu{}, false
	}
	if tab, newTab, ok := ui.TabAt(frame, x, y, cols); ok && !newTab {
		return ui.TabMenu(tab, x, y), true
	}
	if pane := t.paneAt(x, y); pane != 0 {
		// The last pane in a tab has no "close pane": closing it would leave
		// an empty tab, and "close tab" is the honest name for that.
		return ui.PaneMenu(pane, x, y, panes > 1), true
	}
	return ui.Menu{}, false
}

// clickMenu routes a press while a menu is open.
//
// A press inside the menu but not on an item is swallowed rather than falling
// through, or one gesture would close the menu and act on whatever it was
// covering.
func (t *tui) clickMenu(x, y int) (bool, error) {
	t.mu.Lock()
	menu := t.menu
	cols, rows := t.cols, t.rows
	t.mu.Unlock()
	if menu == nil {
		return false, nil
	}

	item, onItem, inside := ui.MenuItemAt(*menu, cols, rows, x, y)
	if !inside {
		t.closeMenu()
		return true, nil
	}
	if !onItem {
		return true, nil
	}
	t.closeMenu()
	return true, t.runMenu(*menu, item)
}

// menuKeys drives the menu from the keyboard, so it is not a mouse-only
// feature in a program people run over ssh.
func (t *tui) menuKeys(data []byte) (bool, error) {
	for _, key := range splitKeys(data) {
		switch key {
		case "\x1b[A", "k":
			t.moveMenu(-1)
		case "\x1b[B", "j":
			t.moveMenu(1)
		case "\r", "\n":
			t.mu.Lock()
			menu := t.menu
			t.mu.Unlock()
			if menu == nil {
				return true, nil
			}
			item := menu.Items[min(max(menu.Selected, 0), len(menu.Items)-1)]
			t.closeMenu()
			return true, t.runMenu(*menu, item)
		default:
			// Anything else closes it rather than being swallowed by a mode
			// the user has stopped thinking about.
			t.closeMenu()
			return true, nil
		}
	}
	return true, nil
}

func (t *tui) moveMenu(delta int) {
	t.mu.Lock()
	if t.menu != nil && len(t.menu.Items) > 0 {
		n := len(t.menu.Items)
		t.menu.Selected = (t.menu.Selected + delta + n) % n
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
}

// runMenu performs an item against the menu's target.
func (t *tui) runMenu(m ui.Menu, item ui.MenuItem) error {
	switch item.Action {
	case ui.MenuGoTo:
		return t.jumpToPane(m.Pane)

	case ui.MenuNewSpace:
		if m.Group != "" {
			return t.newWorkspaceIn(m.Group)
		}
		return t.newWorkspace()

	case ui.MenuGroup:
		if err := t.showWorkspace(m.Workspace); err != nil {
			return err
		}
		t.startPrompt(promptGroupSpace)
		return nil

	case ui.MenuFold:
		t.toggleGroup(m.Group)
		return nil

	case ui.MenuNewTab:
		if m.Workspace != 0 {
			if err := t.showWorkspace(m.Workspace); err != nil {
				return err
			}
		}
		return t.newTabHere()

	case ui.MenuRename:
		return t.renameFor(m)

	case ui.MenuSplitRight, ui.MenuSplitDown:
		dir := "columns"
		if item.Action == ui.MenuSplitDown {
			dir = "rows"
		}
		created, err := t.client.SplitPane(m.Pane, dir, proto.PaneSpec{Command: t.config.Shell()})
		if err != nil {
			return err
		}
		t.mu.Lock()
		t.focus = created
		t.mu.Unlock()
		return t.refresh()

	case ui.MenuZoom:
		if err := t.jumpToPane(m.Pane); err != nil {
			return err
		}
		t.mu.Lock()
		t.zoom = !t.zoom
		t.mu.Unlock()
		return t.refresh()

	case ui.MenuClose:
		return t.closeFor(m)
	}
	return nil
}

// renameGroup moves every space in a group to a new name, which is what
// renaming a group means when a group is only the set of spaces naming it.
func (t *tui) renameGroup(group, name string) error {
	t.mu.Lock()
	var ids []uint64
	for _, w := range t.snap.Workspaces {
		if w.Group == group {
			ids = append(ids, w.ID)
		}
	}
	folded := t.folded[group]
	delete(t.folded, group)
	if name != "" {
		t.folded[name] = folded
	}
	t.mu.Unlock()

	for _, id := range ids {
		if err := t.client.GroupWorkspace(id, name); err != nil {
			return err
		}
	}
	return t.refresh()
}

// renameFor opens the rename prompt on the menu's target, moving there first:
// the prompt edits what is being looked at, and renaming something out of
// view would leave the user reading a name that is not the one changing.
func (t *tui) renameFor(m ui.Menu) error {
	switch {
	case m.Group != "":
		t.startGroupRename(m.Group)
	case m.Pane != 0:
		if err := t.jumpToPane(m.Pane); err != nil {
			return err
		}
		t.startPrompt(promptRenameTab)
	case m.Tab != 0:
		if err := t.showTab(m.Tab); err != nil {
			return err
		}
		t.startPrompt(promptRenameTab)
	case m.Workspace != 0:
		if err := t.showWorkspace(m.Workspace); err != nil {
			return err
		}
		t.startPrompt(promptRenameSpace)
	}
	return nil
}

// closeFor closes the menu's target, whichever kind it is.
func (t *tui) closeFor(m ui.Menu) error {
	switch {
	case m.Group != "":
		// "Ungroup" is renaming the group to nothing: its spaces stay, they
		// just stop being kept together.
		return t.renameGroup(m.Group, "")
	case m.Pane != 0:
		if err := t.client.ClosePane(m.Pane); err != nil {
			return err
		}
	case m.Tab != 0:
		if err := t.client.CloseTab(m.Tab); err != nil {
			return err
		}
	case m.Workspace != 0:
		if err := t.client.CloseWorkspace(m.Workspace); err != nil {
			return err
		}
		t.mu.Lock()
		if t.workspace == m.Workspace {
			// The space being looked at has gone, so nothing about the view
			// still resolves. Dropping it lets the refresh land somewhere real
			// rather than on identifiers that no longer exist.
			t.workspace, t.tab, t.focus, t.zoom = 0, 0, 0, false
		}
		t.mu.Unlock()
	default:
		return nil
	}
	// Nothing is created to replace what was just closed. Closing the last
	// space used to hand back a fresh one immediately, which from the outside
	// is indistinguishable from the close having failed. An empty session is
	// a real state, and the "new" button is sitting in the list.
	return t.refresh()
}
