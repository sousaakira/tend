package main

import (
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/ui"
)

// Navigating a session means three things that are easy to confuse: which
// workspace is shown, which of its tabs, and which pane has the keyboard.
//
// All three are this client's, not the server's. Two people attached to one
// session should be able to look at different things, so nothing here is sent
// upstream — the server is asked only for what exists, never for what is being
// looked at.

// workspaceInfo returns the workspace being shown. The caller holds the lock.
func (t *tui) workspaceLocked() (proto.WorkspaceInfo, bool) {
	for _, w := range t.snap.Workspaces {
		if w.ID == t.workspace {
			return w, true
		}
	}
	return proto.WorkspaceInfo{}, false
}

// tabsLocked returns the tabs of the workspace being shown.
//
// Only that workspace's: a bar listing every tab in the session would put
// tabs the user cannot reach next to ones they can, which is worse than not
// showing them at all.
func (t *tui) tabsLocked() []proto.TabInfo {
	w, ok := t.workspaceLocked()
	if !ok {
		return nil
	}
	return w.Tabs
}

// resolveView settles which workspace and tab to show, given what exists.
//
// It is called after every refresh because the session can change underneath:
// a workspace can empty out, a tab can close, and the view has to land
// somewhere real rather than on an identifier that no longer resolves.
func (t *tui) resolveViewLocked() {
	if len(t.snap.Workspaces) == 0 {
		t.workspace, t.tab = 0, 0
		return
	}

	if _, ok := t.workspaceLocked(); !ok {
		t.workspace = t.snap.ActiveWorkspace
		if _, ok := t.workspaceLocked(); !ok {
			t.workspace = t.snap.Workspaces[0].ID
		}
	}

	tabs := t.tabsLocked()
	if len(tabs) == 0 {
		// An empty workspace is a real state — the user closed its last tab —
		// so it is shown empty rather than skipped past.
		t.tab = 0
		return
	}
	for _, tab := range tabs {
		if tab.ID == t.tab {
			return
		}
	}
	w, _ := t.workspaceLocked()
	if w.ActiveTab != 0 {
		for _, tab := range tabs {
			if tab.ID == w.ActiveTab {
				t.tab = w.ActiveTab
				return
			}
		}
	}
	t.tab = tabs[0].ID
}

// switchWorkspace moves to the next or previous workspace.
func (t *tui) switchWorkspace(forward bool) error {
	t.mu.Lock()
	if len(t.snap.Workspaces) < 2 {
		t.mu.Unlock()
		return nil
	}
	idx := 0
	for i, w := range t.snap.Workspaces {
		if w.ID == t.workspace {
			idx = i
			break
		}
	}
	if forward {
		idx = (idx + 1) % len(t.snap.Workspaces)
	} else {
		idx = (idx - 1 + len(t.snap.Workspaces)) % len(t.snap.Workspaces)
	}
	t.workspace = t.snap.Workspaces[idx].ID
	// The tab and pane belong to the workspace being left, so they are
	// dropped and resolved afresh against the one being entered.
	t.tab, t.focus, t.zoom = 0, 0, false
	t.mu.Unlock()

	return t.refresh()
}

// newWorkspace creates a workspace with one pane and moves to it.
//
// It is named rather than left blank. An unnamed space shows as a dash in the
// list and vanishes from the status bar, which makes the one thing the user
// just created the hardest one to find.
func (t *tui) newWorkspace() error {
	ws, err := t.client.NewWorkspace(t.nextName("space", len(t.snapshotWorkspaces())))
	if err != nil {
		return err
	}
	if _, _, err := t.client.NewTab(ws, t.nextName("tab", 0), proto.PaneSpec{Command: t.config.Shell()}); err != nil {
		return err
	}
	t.mu.Lock()
	t.workspace, t.tab, t.focus, t.zoom = ws, 0, 0, false
	t.mu.Unlock()
	return t.refresh()
}

// nextName labels a new workspace or tab by its position, so two of them are
// told apart without the user having to name everything by hand.
func (t *tui) nextName(kind string, existing int) string {
	return kind + " " + itoaInt(existing+1)
}

func (t *tui) snapshotWorkspaces() []proto.WorkspaceInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snap.Workspaces
}

func itoaInt(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// selectTab moves to the nth tab of the current workspace, counting from one.
func (t *tui) selectTab(n int) error {
	t.mu.Lock()
	tabs := t.tabsLocked()
	if n < 1 || n > len(tabs) {
		t.mu.Unlock()
		return nil
	}
	t.tab = tabs[n-1].ID
	t.focus, t.zoom = 0, false
	t.mu.Unlock()
	return t.refresh()
}

// jumpToPane shows whichever workspace and tab hold a pane, and focuses it.
//
// This is what the agent list is for: the point of seeing every agent at once
// is being able to reach the one that stopped, wherever it happens to live.
func (t *tui) jumpToPane(pane uint64) error {
	t.mu.Lock()
	var ws, tab uint64
	for _, w := range t.snap.Workspaces {
		for _, tb := range w.Tabs {
			for _, id := range tb.Panes {
				if id == pane {
					ws, tab = w.ID, tb.ID
				}
			}
		}
	}
	if ws == 0 {
		t.mu.Unlock()
		return nil
	}
	same := t.workspace == ws && t.tab == tab
	t.workspace, t.tab, t.focus = ws, tab, pane
	if !same {
		t.zoom = false
	}
	t.mu.Unlock()

	if same {
		t.markDirty()
		return nil
	}
	return t.refresh()
}

// showTab moves to a tab by identifier, switching space if it lives in
// another one. Clicking something in a list should go there, wherever it is.
func (t *tui) showTab(tab uint64) error {
	t.mu.Lock()
	var ws uint64
	for _, w := range t.snap.Workspaces {
		for _, tb := range w.Tabs {
			if tb.ID == tab {
				ws = w.ID
			}
		}
	}
	if ws == 0 || (t.workspace == ws && t.tab == tab) {
		t.mu.Unlock()
		return nil
	}
	t.workspace, t.tab, t.focus, t.zoom = ws, tab, 0, false
	t.mu.Unlock()
	return t.refresh()
}

// showWorkspace moves to a space by identifier.
func (t *tui) showWorkspace(ws uint64) error {
	t.mu.Lock()
	if ws == 0 || t.workspace == ws {
		t.mu.Unlock()
		return nil
	}
	t.workspace, t.tab, t.focus, t.zoom = ws, 0, 0, false
	t.mu.Unlock()
	return t.refresh()
}

// newTabHere opens a tab in the space being shown and moves to it.
func (t *tui) newTabHere() error {
	ws := t.shownWorkspace()
	if ws == 0 {
		return nil
	}
	t.mu.Lock()
	name := t.nextName("tab", len(t.tabsLocked()))
	t.mu.Unlock()

	tab, _, err := t.client.NewTab(ws, name, proto.PaneSpec{Command: t.config.Shell()})
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.tab, t.focus, t.zoom = tab, 0, false
	t.mu.Unlock()
	return t.refresh()
}

// --- the agent list --------------------------------------------------------

// sidebarRowsLocked builds both lists: the places, then what is running.
//
// The two are separate because they answer different questions. Spaces exist
// whether or not anything runs in them, and the agent that stopped is rarely
// in the space being looked at — so listing agents under their spaces alone
// would bury the one thing the sidebar is for.
func (t *tui) sidebarRowsLocked() []ui.SidebarRow {
	rows := []ui.SidebarRow{{Kind: ui.SidebarHeading, Label: "spaces"}}

	for _, w := range t.snap.Workspaces {
		rows = append(rows, ui.SidebarRow{
			Kind:      ui.SidebarSpace,
			Label:     orDash(w.Name),
			Detail:    w.Branch,
			Workspace: w.ID,
			State:     t.spaceStateLocked(w),
			Running:   true,
			Active:    w.ID == t.workspace,
		})
	}
	rows = append(rows,
		ui.SidebarRow{Kind: ui.SidebarAction, Label: "new", Action: ui.ActionNewSpace},
		ui.SidebarRow{Kind: ui.SidebarBlank},
	)

	grouped := "flat"
	if t.grouped {
		grouped = "grouped"
	}
	rows = append(rows, ui.SidebarRow{
		Kind:     ui.SidebarHeading,
		Label:    "agents",
		Trailing: grouped,
		Action:   ui.ActionToggleGrouped,
		Active:   t.grouped,
	})
	return append(rows, t.agentRowsLocked()...)
}

// agentRowsLocked lists what is running, flat or under its tab.
func (t *tui) agentRowsLocked() []ui.SidebarRow {
	info := make(map[uint64]proto.PaneInfo, len(t.snap.Panes))
	for _, p := range t.snap.Panes {
		info[p.ID] = p
	}

	var rows []ui.SidebarRow
	for _, w := range t.snap.Workspaces {
		for _, tab := range w.Tabs {
			var entries []ui.SidebarRow
			for _, id := range tab.Panes {
				p := info[id]
				if p.Agent == "" {
					// A shell is not an agent. Listing every pane would make
					// the list as long as the session and hide what it exists
					// to show.
					continue
				}
				entries = append(entries, ui.SidebarRow{
					Kind:      ui.SidebarAgent,
					Label:     agentLabel(w, tab),
					Detail:    p.Agent,
					Pane:      id,
					Tab:       tab.ID,
					Workspace: w.ID,
					State:     p.State,
					Running:   p.Running,
					Active:    id == t.focus && tab.ID == t.tab && w.ID == t.workspace,
				})
			}
			if len(entries) == 0 {
				continue
			}
			if t.grouped {
				rows = append(rows, ui.SidebarRow{
					Kind:      ui.SidebarGroup,
					Label:     orDash(w.Name) + " · " + orDash(tab.Name),
					Tab:       tab.ID,
					Workspace: w.ID,
				})
			}
			rows = append(rows, entries...)
		}
	}
	return rows
}

// agentLabel names where an agent is, since what it is goes underneath.
func agentLabel(w proto.WorkspaceInfo, tab proto.TabInfo) string {
	name := orDash(w.Name)
	if tab.Name != "" {
		name += " · " + tab.Name
	}
	return name
}

// spaceStateLocked is the most urgent thing happening in a space, because a
// space marked idle while an agent inside it waits for an answer is worse
// than no mark at all.
func (t *tui) spaceStateLocked(w proto.WorkspaceInfo) string {
	panes := make(map[uint64]bool)
	for _, tab := range w.Tabs {
		for _, id := range tab.Panes {
			panes[id] = true
		}
	}

	state := ""
	for _, p := range t.snap.Panes {
		if !panes[p.ID] || p.Agent == "" || !p.Running {
			continue
		}
		switch p.State {
		case "blocked":
			return "blocked"
		case "working":
			state = "working"
		case "idle":
			if state == "" {
				state = "idle"
			}
		}
	}
	return state
}

// navTarget is where a sidebar row goes, and is how the navigation cursor
// remembers its place.
//
// The rows are rebuilt from the session on every refresh, so an index into
// them is a position in a list that moves underneath the cursor. What the row
// points at does not move.
type navTarget struct {
	pane      uint64
	workspace uint64
}

// targetOf returns where a row goes, and whether it goes anywhere. Headings,
// actions and spacers do not.
func targetOf(r ui.SidebarRow) (navTarget, bool) {
	switch r.Kind {
	case ui.SidebarAgent:
		return navTarget{pane: r.Pane, workspace: r.Workspace}, true
	case ui.SidebarSpace:
		return navTarget{workspace: r.Workspace}, true
	}
	return navTarget{}, false
}

// startTargetLocked is where the cursor opens: the focused pane if it is in
// the list, and otherwise the space being looked at.
//
// The fallback matters more than it looks. Most panes are shells, and shells
// are not listed, so opening on "nothing selected" would make the first
// keypress move from an arbitrary end of the list rather than from here.
func (t *tui) startTargetLocked() navTarget {
	rows := t.sidebarRowsLocked()
	focused := navTarget{pane: t.focus, workspace: t.workspace}
	space := navTarget{workspace: t.workspace}

	var first navTarget
	found := false
	for _, r := range rows {
		target, ok := targetOf(r)
		if !ok {
			continue
		}
		if target == focused {
			return focused
		}
		if target == space {
			found = true
		}
		if first == (navTarget{}) {
			first = target
		}
	}
	if found {
		return space
	}
	return first
}

// navigate moves the selection down the sidebar, over everything that goes
// somewhere and nothing that does not.
//
// Spaces are walked as well as agents. A space with no agent running in it is
// still somewhere the user wants to reach, and leaving it out would mean the
// keyboard could not reach half of what the mouse can click.
func (t *tui) navigate(delta int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	rows := t.sidebarRowsLocked()
	targets := make([]navTarget, 0, len(rows))
	for _, r := range rows {
		if target, ok := targetOf(r); ok {
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return
	}

	at := 0
	for i, target := range targets {
		if target == t.nav {
			at = i
			break
		}
	}
	t.nav = targets[(at+delta+len(targets))%len(targets)]
	t.dirty = true
}

// enterNavigate opens the list and puts the cursor on the focused pane, so
// moving from it is relative to where the user already is.
func (t *tui) enterNavigate() {
	t.mu.Lock()
	t.sidebar = true
	t.navigating = true
	t.nav = t.startTargetLocked()
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) leaveNavigate() {
	t.mu.Lock()
	t.navigating = false
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// navigateKeys drives the list while it has the keyboard.
func (t *tui) navigateKeys(data []byte) (bool, error) {
	for _, key := range splitKeys(data) {
		handled, err := t.navigateKey(key)
		if err != nil {
			return true, err
		}
		if !handled {
			return false, nil
		}
	}
	return len(data) > 0, nil
}

func (t *tui) navigateKey(key string) (bool, error) {
	switch key {
	case "\x1b[A", "k":
		t.navigate(-1)
		return true, nil
	case "\x1b[B", "j":
		t.navigate(1)
		return true, nil
	case "\r", "\n":
		t.mu.Lock()
		target := t.nav
		t.mu.Unlock()
		t.leaveNavigate()
		if target.pane != 0 {
			return true, t.jumpToPane(target.pane)
		}
		if target.workspace != 0 {
			return true, t.showWorkspace(target.workspace)
		}
		return true, nil
	case "\x1b", "q", "\x03":
		t.leaveNavigate()
		return true, nil
	}
	// Anything else leaves the list rather than being swallowed by a mode the
	// user has stopped thinking about.
	t.leaveNavigate()
	return false, nil
}
