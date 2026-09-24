package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/sousaakira/tend/internal/proto"
)

// The browser, tend's own (server/browser.go has the rest). A page asked for
// here — the toolbar's Browser, prefix+B — goes to the browsers attached to
// the session when there are any, which is where the extension picking
// elements for the context will be; with none it opens on this machine, in
// [browser] command or the desktop's own browser, as a link ctrl+clicked in
// a pane does. This machine, not the server's: over --remote the page is
// wanted where the user is.

// openInBrowser opens a page.
func (t *tui) openInBrowser(url string) {
	url = strings.TrimSpace(url)
	if !strings.Contains(url, "://") {
		url = "https://" + url
	}
	t.mu.Lock()
	t.lastURL = url
	command := t.config.Browser.Command
	t.mu.Unlock()

	if t.serverKnows(proto.MethodBrowserOpen) {
		if n, err := t.client.BrowserOpen(url); err == nil && n > 0 {
			t.setMessage("opened in the attached browser: "+url, false)
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
