package main

import (
	"github.com/sousaakira/tend/internal/proto"
)

// The file explorer is a panel: a pane docked along the left of the tab,
// the tab's full height, running `tend files` on the session's machine.
// prefix+f is one key for all of it — open the panel, go to it, put it
// away — so the key does what the moment needs rather than asking.

// filesTitle is the name the panel's pane goes by, which is how the client
// finds the one it opened.
const filesTitle = "files"

// filesCommand runs the tend the server runs, which a pane is told of in
// TEND_BIN_PATH: the client's own path means nothing on a remote machine,
// and "tend" may not be on the PATH a pane starts with.
var filesCommand = []string{"/bin/sh", "-c", `exec "${TEND_BIN_PATH:-tend}" files`}

// toggleFiles opens the panel, goes to it, or closes it.
func (t *tui) toggleFiles() error {
	t.mu.Lock()
	focus := t.focus
	var panel uint64
	inTab := map[uint64]bool{}
	for _, r := range t.rects {
		inTab[r.Pane] = true
	}
	for _, p := range t.snap.Panes {
		if inTab[p.ID] && p.Title == filesTitle {
			panel = p.ID
		}
	}
	left, right := 0, 0
	for i, r := range t.rects {
		if i == 0 || r.X < left {
			left = r.X
		}
		right = max(right, r.X+r.Cols)
	}
	t.mu.Unlock()

	switch {
	case panel != 0 && panel == focus:
		return t.client.ClosePane(panel)
	case panel != 0:
		t.focusPane(panel)
		return nil
	case focus == 0:
		return nil
	}

	t.mu.Lock()
	cols, onRight := t.config.FilesWidth(), t.config.Files.Dock == "right"
	t.mu.Unlock()
	share := 0.25
	if width := right - left; width > 0 {
		share = float64(cols) / float64(width)
	}
	created, err := t.client.DockPane(focus, share, onRight, proto.PaneSpec{Command: filesCommand, Title: filesTitle})
	if err != nil {
		if t.reportStaleServer(err) {
			return nil
		}
		return err
	}
	t.mu.Lock()
	t.focus = created
	t.mu.Unlock()
	return t.refresh()
}
