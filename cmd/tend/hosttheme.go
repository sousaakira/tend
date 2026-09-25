package main

import (
	"io"
	"os"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// Following the outer terminal between light and dark: herdr's theme
// auto_switch. The terminal is asked once when tend starts or the setting is
// turned on, and a terminal that supports mode 2031 then says so itself every
// time its scheme changes — which is the case that matters, the whole desktop
// going dark at sunset.

// askHostScheme asks the terminal what it looks like, when the theme follows
// it. Written outside the painter: it is a question, not part of the frame.
func (t *tui) askHostScheme() {
	t.mu.Lock()
	follow := t.config.UI.Theme.AutoSwitch
	t.mu.Unlock()
	if !follow {
		return
	}
	_, _ = io.WriteString(os.Stdout,
		ui.HostSchemeReports+ui.HostSchemeQuery+ui.HostBackgroundQuery)
	t.mu.Lock()
	t.hostAsked = true
	t.mu.Unlock()
}

// watchWindowFocus asks the terminal to say when its window gains and loses
// focus, which decides whether an agent finishing on screen was seen and
// whether the pane in view still gets told about it.
func (t *tui) watchWindowFocus() func() {
	_, _ = io.WriteString(os.Stdout, ui.HostFocusReports)
	return func() { _, _ = io.WriteString(os.Stdout, ui.HostFocusReportsOff) }
}

// tellWindowFocus passes the window's focus to the server, which cannot see
// the terminal and needs it for what counts as seen.
func (t *tui) tellWindowFocus(focused bool) {
	if !t.serverHas(proto.FeatureWindowFocus) {
		return
	}
	_ = t.client.WindowFocus(focused)
}

// stopHostScheme turns the reports off on the way out, so the shell after tend
// does not receive them.
func (t *tui) stopHostScheme() {
	t.mu.Lock()
	asked := t.hostAsked
	t.mu.Unlock()
	if asked {
		_, _ = io.WriteString(os.Stdout, ui.HostSchemeReportsOff)
	}
}

// takeHostReports removes the terminal's answers from a chunk of input and
// acts on them; what is left is the user's typing.
func (t *tui) takeHostReports(data []byte) []byte {
	rest, reports, hold := ui.HostReports(t.hostPending, data)
	t.hostPending = hold
	if len(reports) == 0 {
		return rest
	}

	// Focus first, outside the lock: asking the terminal writes to it, and
	// this runs on the goroutine that paints, so the question cannot land in
	// the middle of a frame.
	for _, r := range reports {
		if r.Focus == ui.FocusNone {
			continue
		}
		focused := r.Focus == ui.FocusIn
		t.mu.Lock()
		changed := focused != t.windowFocused
		t.windowFocused = focused
		t.mu.Unlock()
		if changed {
			go t.tellWindowFocus(focused) // a round trip, off this goroutine
		}
		if focused {
			// herdr asks again when the window comes back: the desktop may
			// have gone dark while it was behind something.
			t.askHostScheme()
		}
	}

	t.mu.Lock()
	changed := false
	for _, r := range reports {
		if r.Focus != ui.FocusNone {
			continue
		}
		// A scheme report is the terminal saying which it is; a background
		// colour is only evidence, and does not overrule it (herdr's rule).
		if !r.Explicit && t.hostExplicit {
			continue
		}
		if r.Explicit {
			t.hostExplicit = true
		}
		if t.hostLight != r.Light {
			t.hostLight = r.Light
			changed = true
		}
	}
	if changed && t.config.UI.Theme.AutoSwitch {
		t.theme = ui.ThemeFor(t.config.UI.Theme, t.hostLight)
	} else {
		changed = false
	}
	t.mu.Unlock()
	if changed {
		// Every cell's colour may have moved, so the screen is painted whole.
		t.requestRepaint()
	}
	return rest
}
