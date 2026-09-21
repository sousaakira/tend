package main

import (
	"io"
	"os"

	"github.com/sousaakira/tend/internal/ui"
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

	t.mu.Lock()
	changed := false
	for _, r := range reports {
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
