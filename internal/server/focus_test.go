//go:build unix

package server

import (
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/session"
)

// TestOnlyAProgramThatAskedIsToldAboutFocus: a focus report to a program that
// never asked for one puts "[I" into its input — into a shell's command line,
// or an agent's prompt.
func TestOnlyAProgramThatAskedIsToldAboutFocus(t *testing.T) {
	s := newServer(t)

	// A pane that asks for focus events (mode 1004) and prints what it is
	// sent, and one that does not ask at all.
	_, asks := openTab(t, s, `printf '\033[?1004h'; cat`)
	quiet, err := s.SplitPane(asks, session.Columns, shell(`cat`))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the program to ask for focus events", func() bool {
		return s.WantsFocusEvents(asks)
	})
	if s.WantsFocusEvents(quiet) {
		t.Fatal("a program that asked for nothing is marked as wanting focus events")
	}

	s.FocusPane(asks, quiet)
	waitFor(t, "the focus report", func() bool {
		text, _ := s.ScreenText(asks)
		return strings.Contains(text, "[I")
	})
	// cat prints what it receives, so the quiet pane would show it.
	if text, _ := s.ScreenText(quiet); strings.Contains(text, "[O") {
		t.Errorf("a program that never asked was sent a focus report: %q", text)
	}

	// Losing it is reported too, to the program that asked.
	s.FocusPane(quiet, asks)
	waitFor(t, "the lost-focus report", func() bool {
		text, _ := s.ScreenText(asks)
		return strings.Contains(text, "[O")
	})
}
