package notify

import (
	"strings"
	"testing"
)

// TestTheSequenceIsWhatEachTerminalUnderstands: a sequence the terminal does
// not take is either ignored or printed as text in the middle of a pane, and
// both are worse than saying nothing.
func TestTheSequenceIsWhatEachTerminalUnderstands(t *testing.T) {
	if got := string(Sequence(BackendOSC9, "claude finished", "main · tab 1", false)); got != "\x1b]9;claude finished: main · tab 1\x1b\\" {
		t.Errorf("OSC 9 = %q", got)
	}
	kitty := string(Sequence(BackendKitty, "claude finished", "main · tab 1", false))
	if !strings.Contains(kitty, "]99;i=1:d=0;claude finished") || !strings.Contains(kitty, "]99;i=1:p=body;main · tab 1") {
		t.Errorf("kitty = %q", kitty)
	}
	if got := string(Sequence(BackendKitty, "just a title", "", false)); got != "\x1b]99;;just a title\x1b\\" {
		t.Errorf("kitty with no body = %q", got)
	}
	if got := Sequence(BackendNone, "x", "", false); got != nil {
		t.Errorf("a terminal that takes none got %q", got)
	}
}

// TestInsideTmuxTheSequenceIsPassedThrough: tend inside tmux is a program
// inside a program, and without the wrapper the outer one eats the sequence.
func TestInsideTmuxTheSequenceIsPassedThrough(t *testing.T) {
	got := Sequence(BackendOSC9, "hi", "", true)
	if want := "\x1bPtmux;\x1b\x1b]9;hi\x1b\x1b\\\x1b\\"; string(got) != want {
		t.Errorf("wrapped = %q, want %q", got, want)
	}
}

// TestTextFromAnAgentCannotEscapeTheSequence: the title is an agent's own
// output. An escape in it would otherwise end the notification early and hand
// the rest to the terminal as commands.
func TestTextFromAnAgentCannotEscapeTheSequence(t *testing.T) {
	got := string(Sequence(BackendOSC9, "a\x1b]0;stolen\x07b", "line\nbreak", false))
	if strings.Contains(got[4:], "\x1b") && !strings.HasSuffix(got, "\x1b\\") {
		t.Errorf("an escape survived: %q", got)
	}
	if !strings.Contains(got, "a]0;stolenb") || !strings.Contains(got, "line break") {
		t.Errorf("sanitised text = %q", got)
	}
}

func TestBackendsAreRecognisedTheWayHerdrDoesIt(t *testing.T) {
	env := func(pairs map[string]string) func(string) string {
		return func(k string) string { return pairs[k] }
	}
	for _, c := range []struct {
		pairs map[string]string
		want  Backend
	}{
		{map[string]string{"TERM_PROGRAM": "ghostty"}, BackendOSC9},
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, BackendOSC9},
		{map[string]string{"KITTY_WINDOW_ID": "3"}, BackendKitty},
		{map[string]string{"TERM": "xterm-kitty"}, BackendKitty},
		{map[string]string{"TERM": "wezterm"}, BackendOSC9},
		{map[string]string{"TERM": "xterm-256color"}, BackendNone},
	} {
		if got := Detect(env(c.pairs)); got != c.want {
			t.Errorf("Detect(%v) = %v, want %v", c.pairs, got, c.want)
		}
	}
}

func TestAMessageSplitsIntoATitleAndABody(t *testing.T) {
	if title, body := Split("claude needs attention: main · tab 1"); title != "claude needs attention" || body != "main · tab 1" {
		t.Errorf("Split = %q, %q", title, body)
	}
	if title, body := Split("no colon here"); title != "no colon here" || body != "" {
		t.Errorf("Split = %q, %q", title, body)
	}
}
