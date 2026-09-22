package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain turns the first-run welcome off for every tend these tests start:
// each begins with no settings file, which is exactly when the welcome shows,
// and it takes every key. The one test about the welcome turns it back on.
func TestMain(m *testing.M) {
	os.Setenv(skipOnboardingEnv, "1")
	os.Exit(m.Run())
}

// TestTheFirstRunWelcomesAndThenOffersTheIntegrations is herdr's
// onboarding: with no settings file, the client opens on the welcome, which
// takes every key and does nothing with any but enter; enter writes
// `onboarding = false` and opens the settings screen; and a client started
// after that opens straight on the session. If it regresses, a first-time
// user is dropped into a shell with no word on how tend is used — or the
// welcome comes back on every start.
func TestTheFirstRunWelcomesAndThenOffersTheIntegrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tend.toml")
	withConfig(t, path)
	a := startSessionEnv(t, 110, 30, skipOnboardingEnv+"=")
	a.waitForScreen(t, "the welcome", func(s string) bool {
		return strings.Contains(s, "mouse-first terminal") && strings.Contains(s, "ctrl+b enters prefix mode") &&
			strings.Contains(s, "↵ continue")
	})

	// Everything but enter is taken and does nothing: typing, and the
	// prefix with it. What they would have done is looked for once the
	// welcome and the settings are gone.
	a.send(t, "echo TYPED")
	a.send(t, "\x02c")

	a.send(t, "\r")
	a.waitForScreen(t, "the settings screen", func(s string) bool {
		return !strings.Contains(s, "mouse-first terminal") && strings.Contains(s, "written to "+path)
	})
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "onboarding = false") {
		t.Fatalf("the settings file should say it has been through: %v\n%s", err, data)
	}
	if !strings.HasPrefix(string(data), "onboarding = false\n") {
		t.Errorf("the key belongs before every table:\n%s", data)
	}

	a.send(t, "q")
	a.sendUntil(t, "printf AFTER-%s OK\r", "the shell, after the welcome", func(s string) bool {
		return strings.Contains(s, "AFTER-OK")
	})
	if s := a.text(); strings.Contains(s, "TYPED") || strings.Contains(s, "tab 2") {
		t.Errorf("keys pressed during the welcome reached the session:\n%s", s)
	}
}
