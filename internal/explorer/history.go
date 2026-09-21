package explorer

import (
	"strconv"
	"strings"

	"github.com/sousaakira/tend/internal/vt"
)

// History is herdr-sidebar's browsing of what git remembers: the commits,
// one file's commits, the stashes and the tags, each shown as git shows it.
// It reads; the only things it changes are stashes, which are the user's
// own shelf.

type historyKind int

const (
	histCommits historyKind = iota
	histStashes
	histTags
)

var historyNames = [3]string{"commits", "stashes", "tags"}

// historyEntry is one line of a list: what to show it by, and what it says.
type historyEntry struct {
	ref     string
	subject string
	meta    string
}

type history struct {
	kind   historyKind
	file   string // a file's history, when set: commits that touched it
	items  []historyEntry
	cursor int
	err    error
}

// maxHistory bounds a list; a project's whole log is not read into a panel.
const maxHistory = 300

// Log is the recent commits, or those that touched path.
func (g Git) Log(path string) ([]historyEntry, error) {
	args := []string{"log", "-n", strconv.Itoa(maxHistory), "--format=%h%x1f%s%x1f%an, %ar"}
	if path != "" {
		args = append(args, "--follow", "--", path)
	}
	return g.entries(args...)
}

// Stashes is the stash list.
func (g Git) Stashes() ([]historyEntry, error) {
	return g.entries("stash", "list", "--format=%gd%x1f%s%x1f%ar")
}

// Tags is the tags, newest first.
func (g Git) Tags() ([]historyEntry, error) {
	return g.entries("for-each-ref", "--sort=-creatordate", "--count="+strconv.Itoa(maxHistory),
		"--format=%(refname:short)%1f%(subject)%1f%(creatordate:relative)", "refs/tags")
}

func (g Git) entries(args ...string) ([]historyEntry, error) {
	out, err := g.run(args...)
	if err != nil {
		return nil, err
	}
	var items []historyEntry
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, "\x1f")
		if len(f) == 3 && f[0] != "" {
			items = append(items, historyEntry{ref: f[0], subject: f[1], meta: f[2]})
		}
	}
	return items, nil
}

// Show is what git says about a commit, a stash or a tag, with its diff —
// of one file, when the history is that file's.
func (g Git) Show(kind historyKind, ref, path string) (string, error) {
	var args []string
	switch kind {
	case histStashes:
		args = []string{"stash", "show", "--stat", "--patch", "--no-color", ref}
	default:
		args = []string{"show", "--stat", "--patch", "--no-color", "--format=commit %H%nAuthor: %an <%ae>%nDate:   %ad%n%n    %s%n%n%b", ref}
		if path != "" {
			args = append(args, "--", path)
		}
	}
	out, err := g.run(args...)
	return string(out), err
}

// StashApply applies a stash; pop drops it once applied.
func (g Git) StashApply(ref string, pop bool) error {
	verb := "apply"
	if pop {
		verb = "pop"
	}
	_, err := g.run("stash", verb, ref)
	return err
}

// StashDrop throws a stash away.
func (g Git) StashDrop(ref string) error {
	_, err := g.run("stash", "drop", ref)
	return err
}

func (m *Model) openHistory(kind historyKind, file string) {
	if m.git.Top == "" {
		m.say("not a git repository", true)
		return
	}
	h := history{kind: kind, file: file}
	switch kind {
	case histStashes:
		h.items, h.err = m.git.Stashes()
	case histTags:
		h.items, h.err = m.git.Tags()
	default:
		h.items, h.err = m.git.Log(file)
	}
	m.hist = h
	m.mode = modeHistory
}

func (m *Model) historyKey(k Key) {
	h := &m.hist
	selected := func() (historyEntry, bool) {
		if h.cursor < len(h.items) {
			return h.items[h.cursor], true
		}
		return historyEntry{}, false
	}
	switch k.Name {
	case "esc", "q", "ctrl+c":
		m.mode = modeList
	case "up", "k":
		h.cursor = max(h.cursor-1, 0)
	case "down", "j":
		h.cursor = min(h.cursor+1, max(len(h.items)-1, 0))
	case "pgup", "ctrl+u":
		h.cursor = max(h.cursor-m.listRows()/2, 0)
	case "pgdown", "ctrl+d":
		h.cursor = min(h.cursor+m.listRows()/2, max(len(h.items)-1, 0))
	case "tab":
		if h.file == "" {
			m.openHistory((h.kind+1)%3, "")
		}
	case "enter", "space":
		e, ok := selected()
		if !ok {
			return
		}
		text, err := m.git.Show(h.kind, e.ref, h.file)
		if err != nil {
			m.fail(err)
			return
		}
		m.viewer = viewer{
			title: e.ref + " " + e.subject,
			lines: strings.Split(strings.TrimRight(strings.ReplaceAll(text, "\t", "    "), "\n"), "\n"),
			diff:  true,
		}
		m.viewerBack = modeHistory
		m.mode = modeViewer
	case "a", "p":
		e, ok := selected()
		if !ok || h.kind != histStashes {
			return
		}
		if err := m.git.StashApply(e.ref, k.Name == "p"); err != nil {
			m.fail(err)
			return
		}
		m.mode = modeList
		m.say("applied "+e.ref, false)
		m.Refresh()
	case "d":
		e, ok := selected()
		if !ok || h.kind != histStashes {
			return
		}
		m.ask("drop "+e.ref+" ("+e.subject+")? type yes", "", func(m *Model, input string) {
			if strings.TrimSpace(strings.ToLower(input)) != "yes" {
				m.say("kept "+e.ref, false)
				return
			}
			if err := m.git.StashDrop(e.ref); err != nil {
				m.fail(err)
				return
			}
			m.say("dropped "+e.ref, false)
		})
	}
}

func (m *Model) historyMouse(ev Mouse) {
	h := &m.hist
	switch {
	case ev.Wheel != 0:
		h.cursor = min(max(h.cursor+3*ev.Wheel, 0), max(len(h.items)-1, 0))
	case ev.Press && ev.Y == 0 && h.file == "":
		// The three names on the title line switch, as the header's do.
		at := 1
		for i, name := range historyNames {
			if ev.X < at+len(name)+1 {
				m.openHistory(historyKind(i), "")
				return
			}
			at += len(name) + 3
		}
	case ev.Press && ev.Y >= 2:
		i := historyTop(m) + ev.Y - 2
		if i < len(h.items) {
			if i == h.cursor {
				m.historyKey(Key{Name: "enter"})
			} else {
				h.cursor = i
			}
		}
	}
}

func historyTop(m *Model) int { return max(m.hist.cursor-m.listRows()+1, 0) }

func (m *Model) drawHistory(g *vt.Grid) {
	h := &m.hist
	if h.file != "" {
		put(g, 1, 0, truncate("history of "+h.file, m.cols-2), styleAccent, m.cols)
	} else {
		x := 1
		for i, name := range historyNames {
			style := styleDim
			if historyKind(i) == h.kind {
				style = styleTabOn
			}
			if i > 0 {
				x = put(g, x+1, 0, "│", styleDim, m.cols) + 1
			}
			x = put(g, x, 0, name, style, m.cols)
		}
	}
	switch {
	case h.err != nil:
		put(g, 1, 2, truncate(h.err.Error(), m.cols-2), styleErr, m.cols)
	case len(h.items) == 0:
		put(g, 1, 2, "nothing here", styleDim, m.cols)
	}
	top := historyTop(m)
	for i := 0; i < m.listRows(); i++ {
		at := top + i
		if at >= len(h.items) {
			break
		}
		y := 2 + i
		e := h.items[at]
		refStyle, subjStyle := styleYellow, styleNormal
		if at == h.cursor {
			fill(g, y, 0, m.cols, styleSel)
			refStyle, subjStyle = styleSel, styleSel
		}
		x := put(g, 1, y, e.ref, refStyle, m.cols)
		put(g, x+1, y, truncate(e.subject, m.cols-x-2), subjStyle, m.cols)
	}
	// What the selected one is, under the list.
	if h.cursor < len(h.items) {
		put(g, 1, m.rows-2, truncate(h.items[h.cursor].meta, m.cols-2), styleDim, m.cols)
	}
	hint := "enter show  tab kind  esc"
	switch {
	case h.file != "":
		hint = "enter show  esc"
	case h.kind == histStashes:
		hint = "enter show  a apply  p pop  d drop"
	}
	put(g, 1, m.rows-1, truncate(hint, m.cols-2), styleDim, m.cols)
}
