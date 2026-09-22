package main

import (
	"os"

	"github.com/sousaakira/tend/internal/config"
	"github.com/sousaakira/tend/internal/ui"
)

// The first-run welcome is herdr's onboarding (ui/onboarding.rs,
// overlays.rs render_onboarding_overlay, overlay_input.rs
// complete_onboarding): while `onboarding` is missing from the settings, or
// true, a client opens on a short word about how tend is used, and nothing
// else takes a key until it is through. Going on writes `onboarding = false`
// and opens the settings screen at the agent integrations, which is what the
// welcome's last line offers.

// skipOnboardingEnv turns the welcome off for tests, which start tend with no
// settings file by the hundred: herdr's HERDR_TEST_* variables are the
// precedent for a test-only switch in the product.
const skipOnboardingEnv = "TEND_TEST_SKIP_ONBOARDING"

// onboardingContinue is the button's line, which a click on goes on.
const onboardingContinue = "[ ↵ continue ]"

// onboardingLines is the welcome, herdr's words with tend's name, and the
// keys this client has: its prefix and the help's key after it.
func onboardingLines(cfg config.Config, bindings map[string]ui.Command) []string {
	keys := "? shows keybinds · s the settings"
	if help := ui.KeyFor(bindings, ui.CommandHelp); help != "" {
		keys = help + " shows keybinds · " + ui.KeyFor(bindings, ui.CommandSettings) + " the settings"
	}
	prefix := "prefix"
	if p := cfg.Keys.Prefix; p != "" && p != "none" {
		prefix = p
	}
	return []string{
		"tend",
		"terminal workspace manager for coding agents",
		"",
		"this is a mouse-first terminal.",
		"click the sidebar to switch spaces, drag pane",
		"borders to resize, right-click for context menus.",
		"",
		prefix + " enters prefix mode · then " + keys,
		"next: install optional agent integrations for more reliable state",
		"",
		onboardingContinue,
	}
}

// startOnboarding puts the welcome up when the settings ask for it.
func (t *tui) startOnboarding() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.config.ShowOnboarding() || os.Getenv(skipOnboardingEnv) != "" {
		return
	}
	t.onboarding = true
	t.overlay = onboardingLines(t.config, t.keys.Bindings)
	t.dirty = true
}

func (t *tui) onboardingUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.onboarding
}

// onboardingInput is every key and click while the welcome is up. Enter,
// right and l go on, as herdr's do, and so does a click on the button;
// everything else, the prefix included, is taken and does nothing.
func (t *tui) onboardingInput(data []byte) error {
	forward, _, mice := t.keys.FeedAll(data)
	for _, ev := range mice {
		if ev.Kind != ui.MousePress || ev.Button != 0 {
			continue
		}
		t.mu.Lock()
		lines, cols, rows := t.overlay, t.cols, t.rows
		t.mu.Unlock()
		if len(lines) > 1 && ui.OverlayLineAt(lines, cols, rows, ev.X, ev.Y) == len(lines)-2 {
			return t.completeOnboarding()
		}
	}
	for _, key := range splitKeys(forward) {
		switch key {
		case "\r", "\n", "\x1b[C", "\x1bOC", "l":
			return t.completeOnboarding()
		}
	}
	return nil
}

// completeOnboarding writes that the welcome has been through and opens the
// settings at the integrations. The file failing to take it is said, and
// the welcome still goes: it would otherwise come back on every start with
// no way past it but editing the file by hand.
func (t *tui) completeOnboarding() error {
	if err := config.SetTopLevel("onboarding", "false"); err != nil {
		t.setMessage(err.Error(), true)
	}
	t.mu.Lock()
	t.onboarding = false
	t.overlay = nil
	f := false
	t.config.Onboarding = &f
	t.dirty = true
	t.mu.Unlock()

	if err := t.openSettings(); err != nil {
		return err
	}
	t.mu.Lock()
	if s := t.settings; s != nil && len(s.rows) > len(settingRows) {
		// herdr's select_settings_section(Integrations): the first
		// integration row. Without any on this machine, the top.
		s.row = len(settingRows)
	}
	t.mu.Unlock()
	t.drawSettings()
	return nil
}
