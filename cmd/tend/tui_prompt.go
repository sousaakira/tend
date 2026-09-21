package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sousaakira/tend/internal/worktree"
)

// Renaming needs somewhere to type, and a prompt on the status line is the
// smallest thing that works: it costs no space when it is not up, and it is
// where the name it is changing is already shown.

type promptKind uint8

const (
	promptNone promptKind = iota
	promptRenameTab
	promptRenameSpace
	// promptRenamePane names one pane, which its program cannot overwrite.
	promptRenamePane
	// promptNewWorktree asks for the branch of a new worktree, with a
	// generated one already in the box.
	promptNewWorktree
	// promptGroupSpace names a new group for a space. Leaving a group and
	// joining one that exists are picked from a menu, so an empty answer
	// cancels here as it does everywhere else.
	promptGroupSpace
	// promptRenameGroup renames a group, which means moving every space in it
	// at once: a group is only the set of spaces naming it.
	promptRenameGroup
)

// startPrompt opens the prompt, seeded with the current name.
//
// The seed starts selected, as it would in any rename field: typing replaces
// it, and backspace keeps it and edits from the end. Seeding without that
// turns "rename this to backend" into "space 2backend", which is a name
// nobody asked for.
func (t *tui) startPrompt(kind promptKind) {
	t.mu.Lock()
	t.prompt = kind
	t.promptText = ""
	t.promptPristine = true

	switch kind {
	case promptRenameTab:
		for _, tab := range t.tabsLocked() {
			if tab.ID == t.tab {
				t.promptText = tab.Name
			}
		}
	case promptRenameSpace:
		if w, ok := t.workspaceLocked(); ok {
			t.promptText = w.Name
		}
	case promptNewWorktree:
		t.promptText = worktree.GeneratedBranch(uint64(time.Now().UnixNano()))
		// Found once, for the path shown as the branch is typed (herdr's
		// create overlay shows where the checkout will go). Only here: over
		// ssh the repository is on the other machine.
		t.promptRepo = ""
		if w, ok := t.workspaceLocked(); ok && t.host == "" && w.Dir != "" {
			if repo, err := worktree.Find(w.Dir); err == nil {
				t.promptRepo = repo.Name
			}
		}
	case promptRenamePane:
		for _, p := range t.snap.Panes {
			if p.ID == t.focus {
				t.promptText = p.Title
			}
		}
	case promptRenameGroup:
		t.promptText = t.promptGroup
	}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// startGroupRename opens the rename prompt on a group rather than on whatever
// space is being looked at.
func (t *tui) startGroupRename(group string) {
	t.mu.Lock()
	t.promptGroup = group
	t.mu.Unlock()
	t.startPrompt(promptRenameGroup)
}

func (t *tui) prompting() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.prompt != promptNone
}

func (t *tui) cancelPrompt() {
	t.mu.Lock()
	t.prompt, t.promptText, t.promptPristine = promptNone, "", false
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// promptKeys collects the name. It reports whether the input was consumed.
func (t *tui) promptKeys(data []byte) (bool, error) {
	if len(data) == 0 {
		return false, nil
	}

	for _, b := range data {
		switch b {
		case '\r', '\n':
			return true, t.commitPrompt()
		case 0x1b, 0x03: // escape, ctrl+c
			t.cancelPrompt()
			return true, nil
		case 0x7f, 0x08: // backspace
			t.mu.Lock()
			if t.promptPristine {
				// Backspace on the seed keeps it and starts editing, rather
				// than deleting a character of something about to be replaced.
				t.promptPristine = false
				t.dirty = true
				t.mu.Unlock()
				continue
			}
			if n := len(t.promptText); n > 0 {
				// Trimmed by rune, so deleting a character does not leave half
				// of one behind.
				runes := []rune(t.promptText)
				t.promptText = string(runes[:len(runes)-1])
			}
			t.dirty = true
			t.mu.Unlock()
		default:
			if b < 0x20 {
				continue // other control keys have no meaning here
			}
			t.mu.Lock()
			if t.promptPristine {
				t.promptText = ""
				t.promptPristine = false
			}
			if len(t.promptText) < 64 {
				t.promptText += string(b)
			}
			t.dirty = true
			t.mu.Unlock()
		}
	}
	t.wakeUp()
	return true, nil
}

func (t *tui) commitPrompt() error {
	t.mu.Lock()
	kind, name := t.prompt, strings.TrimSpace(t.promptText)
	tab, ws, group, pane := t.tab, t.workspace, t.promptGroup, t.focus
	t.prompt, t.promptText, t.promptPristine = promptNone, "", false
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()

	if name == "" {
		// An empty name would make the thing disappear from every list it is
		// in, so it is treated as cancelling rather than as a name.
		return nil
	}

	switch kind {
	case promptRenameTab:
		if tab == 0 {
			return nil
		}
		if err := t.client.RenameTab(tab, name); err != nil {
			return err
		}
	case promptRenameSpace:
		if ws == 0 {
			return nil
		}
		if err := t.client.RenameWorkspace(ws, name); err != nil {
			return err
		}
	case promptNewWorktree:
		if ws == 0 {
			return nil
		}
		t.createWorktree(ws, name)
	case promptRenamePane:
		if pane == 0 {
			return nil
		}
		if err := t.client.RenamePane(pane, name); err != nil {
			if t.reportStaleServer(err) {
				return nil
			}
			return err
		}
	case promptGroupSpace:
		if ws == 0 {
			return nil
		}
		if err := t.client.GroupWorkspace(ws, name); err != nil {
			return err
		}
	case promptRenameGroup:
		if group == "" {
			return nil
		}
		return t.renameGroup(group, name)
	default:
		return nil
	}
	return t.refresh()
}

// promptLabel is what the status line says while the prompt is up.
func (t *tui) promptLabelLocked() string {
	switch t.prompt {
	case promptRenameTab:
		return "rename tab"
	case promptRenameSpace:
		return "rename space"
	case promptRenamePane:
		return "rename pane"
	case promptNewWorktree:
		return "new worktree — branch"
	case promptGroupSpace:
		return "new group — name"
	case promptRenameGroup:
		return "rename group"
	}
	return ""
}

// promptHintLocked says where the answer being typed leads: for a new
// worktree, the checkout's path, recomputed with every key as herdr's create
// overlay shows it. The caller holds the lock.
func (t *tui) promptHintLocked() string {
	if t.prompt != promptNewWorktree || t.promptRepo == "" {
		return ""
	}
	return "→ " + tildeHome(worktree.DefaultPath(t.worktreeRootLocked(), t.promptRepo, t.promptText))
}

// worktreeRootLocked is where new worktrees go, as the server works it out:
// the setting, or ~/.tend/worktrees. The caller holds the lock.
func (t *tui) worktreeRootLocked() string {
	root := worktree.ExpandHome(t.config.Worktrees.Directory)
	if root == "" || !filepath.IsAbs(root) {
		root = worktree.ExpandHome("~/.tend/worktrees")
	}
	return root
}

// tildeHome writes the home directory as ~, so a path fits on a status line.
func tildeHome(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + path[len(home):]
	}
	return path
}
