package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/sousaakira/tend/internal/browserext"
	"github.com/sousaakira/tend/internal/proto"
)

// The browser, tend's own (server/browser.go, internal/browserext). A page
// asked for here — the toolbar's Browser, prefix+B — goes to the browser
// attached to the session when there is one. With none, tend's browser is
// opened: a Chromium-family browser in a profile of the session's own with
// tend's extension in it, which attaches as it starts, so picking elements
// for the context works from the first page. [browser] command instead
// opens a browser of the user's own, without the extension; and a machine
// with no browser that can load the extension opens the desktop's own, and
// says so. All on this machine, not the server's: over --remote the page is
// wanted where the user is.

// openInBrowser opens a page.
func (t *tui) openInBrowser(url string) {
	url = strings.TrimSpace(url)
	if !strings.Contains(url, "://") {
		url = "https://" + url
	}
	t.mu.Lock()
	t.lastURL = url
	command, program := t.config.Browser.Command, t.config.Browser.Program
	session, host := t.session, t.host
	t.mu.Unlock()

	if t.serverKnows(proto.MethodBrowserOpen) {
		if n, err := t.client.BrowserOpen(url); err == nil && n > 0 {
			t.setMessage("opened in the attached browser: "+url, false)
			return
		}
	}
	if command == "" {
		used, err := launchTendBrowser(session, host, program, url)
		if err == nil {
			t.setMessage("opening "+url+" in tend's browser ("+used+", with the extension)", false)
			return
		}
		if !errors.Is(err, browserext.ErrNoBrowser) {
			t.setMessage("tend's browser: "+err.Error(), true)
			return
		}
		// No browser here can load the extension: the desktop's own, then,
		// which opens the page and picks nothing.
		if err := openURL(url); err == nil {
			t.setMessage("opening "+url+" (no Chromium or Edge here, so without tend's extension)", false)
			return
		}
	}
	if err := openURLWith(command, url); err != nil {
		t.copyToClipboard(url, "no browser here ("+err.Error()+"); link copied")
		return
	}
	t.setMessage("opening "+url, false)
}

// openURLWith opens a page with the configured browser command, the URL
// last, or the desktop's own when none is set.
func openURLWith(command, url string) error {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return openURL(url)
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return errors.New("not a web link")
	}
	if strings.HasPrefix(fields[0], "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			fields[0] = home + fields[0][1:]
		}
	}
	cmd := exec.Command(fields[0], append(fields[1:], url)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
