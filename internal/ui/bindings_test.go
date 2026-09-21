package ui

import (
	"strings"
	"testing"
)

// TestAUserCanRebindAKey is what the settings table is for. If it regresses,
// somebody whose muscle memory says "q detaches" is stuck with tend's choice.
func TestAUserCanRebindAKey(t *testing.T) {
	bindings, notes, err := BindingsFrom(map[string]string{"detach": "q"})
	if err != nil {
		t.Fatalf("BindingsFrom: %v", err)
	}
	if bindings["q"] != CommandDetach {
		t.Errorf("q is bound to %v, want detach", bindings["q"])
	}
	// The default key keeps working: binding a key adds one, it does not
	// take the old one away, which is what "bind" means everywhere else.
	if bindings["d"] != CommandDetach {
		t.Errorf("d is bound to %v, want detach still", bindings["d"])
	}
	if len(notes) != 0 {
		t.Errorf("notes = %v, want none for a free key", notes)
	}

	in := Input{Bindings: bindings}
	_, commands, _ := in.FeedAll([]byte{Prefix, 'q'})
	if len(commands) != 1 || commands[0].Command != CommandDetach {
		t.Errorf("prefix q gave %v", commands)
	}
}

// TestTakingAKeyFromSomethingElseIsSaid: a user who binds a key that already
// does something should be told, not left wondering where the old one went.
func TestTakingAKeyFromSomethingElseIsSaid(t *testing.T) {
	bindings, notes, err := BindingsFrom(map[string]string{"detach": "z"})
	if err != nil {
		t.Fatal(err)
	}
	if bindings["z"] != CommandDetach {
		t.Errorf("z is bound to %v, want detach", bindings["z"])
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "zoom") {
		t.Errorf("notes = %v, want one naming zoom", notes)
	}
}

// TestABindingThatNamesNothingIsRefused: a typo in a settings file must be
// reported where it can be fixed rather than doing nothing for ever.
func TestABindingThatNamesNothingIsRefused(t *testing.T) {
	if _, _, err := BindingsFrom(map[string]string{"detatch": "q"}); err == nil {
		t.Error("a misspelled command was accepted")
	}
	if _, _, err := BindingsFrom(map[string]string{"detach": "ctrl+q"}); err == nil {
		t.Error("a key the parser cannot see after the prefix was accepted")
	}
	if _, _, err := BindingsFrom(map[string]string{"detach": ""}); err == nil {
		t.Error("an empty key was accepted")
	}
}

// TestTheHelpShowsTheKeysInEffect: help that lists the default key while the
// user's own key does the work is worse than no help.
func TestTheHelpShowsTheKeysInEffect(t *testing.T) {
	bindings, _, err := BindingsFrom(map[string]string{"detach": "q"})
	if err != nil {
		t.Fatal(err)
	}
	help := strings.Join(HelpLinesFor(bindings), "\n")
	if !strings.Contains(help, "d q") {
		t.Errorf("the help does not show the rebound key:\n%s", help)
	}
}

// TestEveryCommandInTheHelpHasAKey: a command nobody can reach is a command
// that does not exist, and the help promising one is a bug report waiting.
func TestEveryCommandInTheHelpHasAKey(t *testing.T) {
	bindings := DefaultBindings()
	for _, pair := range Bound(bindings) {
		if pair[1] == "" {
			t.Errorf("%s has no key", pair[0])
		}
	}
}
