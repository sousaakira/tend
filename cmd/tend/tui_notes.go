package main

import (
	"strings"

	"github.com/sousaakira/tend/internal/notify"
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/ui"
)

// The release check's side of the client, herdr's (`client/shell/`): the
// notice when the server finds a release, "update ready" at the right of
// the status bar and a dot on the sidebar's menu button until it is
// installed, and the release notes panel, opened from that menu — "update
// ready" there while the release is newer than the build, "what's new"
// once it is the one running. herdr opens the notes from nowhere else, and
// neither does this.

// updateReadyLocked is the release found, if any.
func (t *tui) updateReadyLocked() string {
	if u := t.snap.Update; u != nil {
		return u.Ready
	}
	return ""
}

// notesAvailableLocked is whether there are notes for the menu to open.
func (t *tui) notesAvailableLocked() bool {
	return t.snap.Update != nil && t.snap.Update.Notes != ""
}

// updateAnnounced is the notice for EventUpdateReady: herdr's toast, "v…
// available" with how to install it. Said once, when the check finds the
// release; after that the status bar and the menu keep saying so.
func (t *tui) updateAnnounced(ev proto.Event) {
	version := "v" + strings.TrimPrefix(ev.Title, "v")
	t.raise(ui.ToastCustom, version+" available", ev.Body, 0, notify.SoundNone)
	t.askResync()
}

// openReleaseNotes reads the notes from the server and puts the panel up.
func (t *tui) openReleaseNotes() error {
	notes, err := t.client.ReleaseNotes()
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.notes = &ui.ReleaseNotesView{Version: notes.Version, Body: notes.Body, Newer: notes.Newer, Install: notes.Install}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	return nil
}

func (t *tui) notesUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.notes != nil
}

// closeReleaseNotes takes the panel down and tells the server they were
// read, which herdr does for the notes of the release running only; the
// server decides that. A dismiss that fails is said, and the panel stays
// down: the notes are still on the menu.
func (t *tui) closeReleaseNotes() {
	t.mu.Lock()
	v := t.notes
	t.notes = nil
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	if v == nil {
		return
	}
	go func() {
		if err := t.client.DismissReleaseNotes(v.Version); err != nil {
			t.setMessage("failed to update release notes status: "+err.Error(), true)
		}
	}()
}

// notesInput is every key and click while the panel is up, herdr's
// overlay_input: enter and esc close, the arrows and j/k scroll a line,
// page up and down eight, home and end to either end, the wheel three; a
// click on the close button closes. Everything else is taken and does
// nothing, as the panel is over everything.
func (t *tui) notesInput(data []byte) error {
	forward, _, mice := t.keys.FeedAll(data)
	for _, ev := range mice {
		switch ev.Kind {
		case ui.MouseWheelUp:
			t.scrollNotes(-3)
		case ui.MouseWheelDown:
			t.scrollNotes(3)
		case ui.MousePress:
			t.mu.Lock()
			cols, rows := t.cols, t.rows
			t.mu.Unlock()
			if g, ok := ui.ReleaseNotesLayout(cols, rows); ok && ev.Button == 0 {
				switch {
				case inRect(g.Close, ev.X, ev.Y):
					t.closeReleaseNotes()
					return nil
				case inRect(g.Update, ev.X, ev.Y):
					return t.updateNow()
				}
			}
		}
	}
	for _, key := range splitKeys(forward) {
		switch key {
		case "\r", "\n", "\x1b", "q":
			t.closeReleaseNotes()
			return nil
		case "u":
			return t.updateNow()
		case "\x1b[A", "k":
			t.scrollNotes(-1)
		case "\x1b[B", "j":
			t.scrollNotes(1)
		case "\x1b[5~":
			t.scrollNotes(-8)
		case "\x1b[6~":
			t.scrollNotes(8)
		case "\x1b[H", "\x1b[1~", "g":
			t.scrollNotes(-1 << 20)
		case "\x1b[F", "\x1b[4~", "G":
			t.scrollNotes(1 << 20)
		}
	}
	return nil
}

// scrollNotes moves the panel's first line, kept where the panel will show
// it so a scroll back from past the end moves at once.
func (t *tui) scrollNotes(by int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.notes == nil {
		return
	}
	t.notes.Scroll = max(t.notes.Scroll+by, 0)
	t.notes.Scroll = ui.ClampNotesScroll(t.notes, t.cols, t.rows, t.theme)
	t.dirty = true
}

// updateScript installs the release published and moves the session onto
// it — `tend update -handoff`, which downloads it, checks it against the
// manifest's checksum, puts it in place and hands the server over with the
// programs in its panes still running — then leaves a shell. It is the
// server's tend that is updated, the one a pane knows as TEND_BIN_PATH.
const updateScript = `"${TEND_BIN_PATH:-tend}" update -handoff
echo
echo "[tend] detach (prefix+d) and run tend again to use the new client too."
exec "${SHELL:-/bin/sh}"`

// updateNow runs the update in a tab of its own, for the user to watch,
// when the notes are of a newer release: asked for, never on its own.
func (t *tui) updateNow() error {
	t.mu.Lock()
	v := t.notes
	ws := t.workspace
	t.mu.Unlock()
	if v == nil || !v.Newer {
		return nil
	}
	t.closeReleaseNotes()
	tab, _, err := t.client.NewTab(ws, "update", proto.PaneSpec{
		Command: []string{"/bin/sh", "-c", updateScript},
		Title:   "update",
	})
	if err != nil {
		t.setMessage("could not start the update: "+err.Error(), true)
		return nil
	}
	t.mu.Lock()
	t.rememberFocusLocked()
	t.tab, t.focus, t.zoom = tab, 0, false
	t.mu.Unlock()
	return t.refresh()
}
