package main

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/ui"
)

// Links are herdr's: ctrl+click on a web URL in a pane opens it, and with
// ctrl held the one under the pointer is underlined. A plugin with a link
// handler whose pattern matches takes it first, on the server; otherwise it
// is opened here, on the machine in front of the user — over --remote too,
// which is where the browser is.

// rowTextLocked is a row of a pane as the client shows it, a character a
// column, for finding what is under the pointer.
func (t *tui) rowTextLocked(pane uint64, y int) string {
	screen := t.screenFor(pane)
	if screen == nil {
		return ""
	}
	line := screen.Grid().Line(y)
	if line == nil {
		return ""
	}
	var b strings.Builder
	for x := 0; x < line.Len(); x++ {
		c := line.Cell(x)
		switch {
		case c.Width == 0:
			continue // the second half of a wide character
		case c.R == 0:
			b.WriteByte(' ')
		default:
			b.WriteRune(c.R)
		}
	}
	return b.String()
}

// linkAt is the URL under a point, and the pane and row it is on.
func (t *tui) linkAt(x, y int) (uint64, int, ui.LinkSpan, bool) {
	pane := t.paneAt(x, y)
	if pane == 0 {
		return 0, 0, ui.LinkSpan{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	cx, cy, ok := t.paneCellLocked(pane, x, y)
	if !ok {
		return 0, 0, ui.LinkSpan{}, false
	}
	span, found := ui.LinkAt(t.rowTextLocked(pane, cy), cx)
	return pane, cy, span, found
}

// hoverLink underlines the link under the pointer while ctrl is held, and
// takes the underline away otherwise.
func (t *tui) hoverLink(ev ui.MouseEvent) {
	var hover *ui.LinkHover
	if ev.Mods.Has(ui.ModCtrl) {
		if pane, row, span, ok := t.linkAt(ev.X, ev.Y); ok {
			hover = &ui.LinkHover{Pane: pane, Row: row, Start: span.Start, End: span.End}
		}
	}
	t.mu.Lock()
	changed := (hover == nil) != (t.linkHover == nil) || hover != nil && *hover != *t.linkHover
	t.linkHover = hover
	if changed {
		t.dirty = true
	}
	t.mu.Unlock()
	if changed {
		t.wakeUp()
	}
}

// clickLink opens the link under a ctrl+click, and reports whether there
// was one: the click is then the link's, not the program's under it.
func (t *tui) clickLink(ev ui.MouseEvent) bool {
	if !ev.Mods.Has(ui.ModCtrl) || ev.Button != 0 {
		return false
	}
	pane, _, span, ok := t.linkAt(ev.X, ev.Y)
	if !ok {
		return false
	}
	go t.activateLink(pane, span.URL)
	return true
}

// activateLink offers the URL to the plugins, and opens it here when none
// takes it. A server from before link handlers has none to offer it to.
func (t *tui) activateLink(pane uint64, url string) {
	if t.serverKnows(proto.MethodPaneLinkActivate) {
		handled, err := t.client.ActivateLink(pane, url)
		if err == nil && handled {
			t.setMessage("opened by a plugin: "+url, false)
			return
		}
	}
	if err := openURL(url); err != nil {
		// Nowhere to open it — a machine with no desktop — so it goes to
		// the clipboard, which reaches the user's own machine.
		t.copyToClipboard(url, "no browser here ("+err.Error()+"); link copied")
		return
	}
	t.setMessage("opening "+url, false)
}

// openURL hands a web URL to the desktop: open on a Mac, xdg-open
// elsewhere. Only http and https, herdr's safe_web_url.
func openURL(url string) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return errors.New("not a web link")
	}
	program := "xdg-open"
	if runtime.GOOS == "darwin" {
		program = "open"
	} else if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return errors.New("no desktop")
	}
	if _, err := exec.LookPath(program); err != nil {
		return errors.New("no " + program)
	}
	cmd := exec.Command(program, url)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
