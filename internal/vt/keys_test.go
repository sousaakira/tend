package vt

import "testing"

// TestArrowKeysFollowTheProgramsMode: an application that asked for DECCKM
// reads ESC [ A as a stray bracket and an A. If this regresses, scripting an
// agent's menu types garbage into it.
func TestArrowKeysFollowTheProgramsMode(t *testing.T) {
	normal, ok := EncodeKey("up", Modes{})
	if !ok || string(normal) != "\x1b[A" {
		t.Errorf("up = %q, %v; want ESC [ A", normal, ok)
	}
	app, ok := EncodeKey("up", Modes{ApplicationCur: true})
	if !ok || string(app) != "\x1bOA" {
		t.Errorf("up in application mode = %q, %v; want ESC O A", app, ok)
	}
}

// TestKeyNamesScriptsUse covers the names herdr accepts, so a script written
// for one runtime works against the other.
func TestKeyNamesScriptsUse(t *testing.T) {
	for _, c := range []struct{ name, want string }{
		{"enter", "\r"},
		{"Enter", "\r"},
		{"esc", "\x1b"},
		{"tab", "\t"},
		{"backspace", "\x7f"},
		{"ctrl+c", "\x03"},
		{"c-c", "\x03"},
		{"C-c", "\x03"},
		{"ctrl+d", "\x04"},
		{"alt+b", "\x1bb"},
		{"+", "+"},
		{"plus", "+"},
		{"a", "a"},
		{"shift+tab", "\x1b[Z"},
		{"f5", "\x1b[15~"},
	} {
		got, ok := EncodeKey(c.name, Modes{})
		if !ok || string(got) != c.want {
			t.Errorf("EncodeKey(%q) = %q, %v; want %q", c.name, got, ok, c.want)
		}
	}
	if _, ok := EncodeKey("chorus", Modes{}); ok {
		t.Error("a name nobody defined was encoded as something")
	}
}

// TestPastedTextIsBracketedWhenAskedFor: an agent's prompt box sends the first
// line on its own if a multi-line answer arrives as typing.
func TestPastedTextIsBracketedWhenAskedFor(t *testing.T) {
	plain := EncodeText("two\nlines", Modes{})
	if string(plain) != "two\nlines" {
		t.Errorf("plain = %q", plain)
	}
	wrapped := EncodeText("two\nlines", Modes{BracketedPaste: true})
	if string(wrapped) != "\x1b[200~two\nlines\x1b[201~" {
		t.Errorf("wrapped = %q", wrapped)
	}
}
