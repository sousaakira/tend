package explorer

import (
	"strings"
	"testing"
)

// TestIconsFollowTheThemeAndTheKindOfFile: none draws nothing, nerd and
// emoji draw a glyph for the kind, folders open and shut. If it regresses,
// the setting does nothing, or every file gets the same icon.
func TestIconsFollowTheThemeAndTheKindOfFile(t *testing.T) {
	cases := []struct {
		node        Node
		nerd, emoji string
	}{
		{Node{Name: "main.go"}, " ", "🐹 "},
		{Node{Name: "Cargo.lock"}, " ", "🔒 "},
		{Node{Name: "Dockerfile"}, " ", "🐳 "},
		{Node{Name: "weird.xyz"}, " ", "📄 "},
		{Node{Name: "src", Dir: true}, " ", "📁 "},
		{Node{Name: "src", Dir: true, Expanded: true}, " ", "📂 "},
	}
	for _, c := range cases {
		if got := iconFor(&c.node, IconsNerd); got != c.nerd {
			t.Errorf("nerd %s = %q, want %q", c.node.Name, got, c.nerd)
		}
		if got := iconFor(&c.node, IconsEmoji); got != c.emoji {
			t.Errorf("emoji %s = %q, want %q", c.node.Name, got, c.emoji)
		}
		if got := iconFor(&c.node, IconsNone); got != "" {
			t.Errorf("none %s = %q", c.node.Name, got)
		}
	}
}

// TestSettingsApplyAndDoNotUndoTheKeys: icons show once set; dotfiles
// hidden by setting, shown again by ".", stay shown when the same settings
// are read again; following off stops the panel following. If it
// regresses, the panel's settings screen does nothing, or undoes the
// user's toggle every two seconds.
func TestSettingsApplyAndDoNotUndoTheKeys(t *testing.T) {
	dir := repo(t)
	m := New(dir, nil)
	m.Configure(Settings{Icons: IconsEmoji, Hidden: true, Follow: true})
	text := screen(m, 40, 12)
	if !strings.Contains(text, "📁 src") || strings.Contains(text, ".gitignore") {
		t.Fatalf("emoji and dotfiles hidden:\n%s", text)
	}
	keys(m, ".")
	m.Configure(Settings{Icons: IconsEmoji, Hidden: true, Follow: true})
	if text := screen(m, 40, 12); !strings.Contains(text, ".gitignore") {
		t.Errorf("the same settings read again undid the toggle:\n%s", text)
	}

	other := repo(t)
	n := &fakeNeighbours{siblings: []Sibling{{Pane: "p_2", Cwd: other}}}
	m.FollowPanes(n)
	m.Configure(Settings{Follow: false})
	m.Tick()
	if m.tree.Root != dir {
		t.Errorf("following off, the panel moved to %s", m.tree.Root)
	}
	m.Configure(Settings{Follow: true})
	m.Tick()
	if m.tree.Root != other {
		t.Errorf("following on again should move it: %s", m.tree.Root)
	}
}
