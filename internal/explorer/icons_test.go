package explorer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// TestIconsAreHerdrSidebars: none draws nothing; emoji and nerd draw
// herdr-sidebar's icon for the kind — a whole name before its extension,
// without regard to case, folders open and shut — and the nerd one in its
// colour. The cases are its own tests' (`icons.rs`). If it regresses, the
// setting does nothing, or the tree stops looking like herdr-sidebar's.
func TestIconsAreHerdrSidebars(t *testing.T) {
	cases := []struct {
		node  Node
		nerd  string
		emoji string
	}{
		{Node{Name: "main.rs"}, "\ue7a8 ", "🦀 "},
		{Node{Name: "MAIN.RS"}, "\ue7a8 ", "🦀 "},
		{Node{Name: "Cargo.toml"}, "\uf487 ", "📦 "},
		{Node{Name: "Cargo.lock"}, "\uf023 ", "🔒 "},
		{Node{Name: "README.md"}, "\uf02d ", "📖 "},
		{Node{Name: "Dockerfile"}, "\uf308 ", "🐳 "},
		{Node{Name: ".gitignore"}, "\ue702 ", "🙈 "},
		{Node{Name: ".env.local"}, "\uf084 ", "🔑 "},
		{Node{Name: "photo.JPG"}, "\uf1c5 ", "📷 "},
		{Node{Name: "CNAME"}, "\uf15b ", "📄 "},
		{Node{Name: "src", Dir: true}, "\uf07b ", "📁 "},
		{Node{Name: "src", Dir: true, Expanded: true}, "\uf07c ", "📂 "},
	}
	for _, c := range cases {
		if got, _ := iconFor(&c.node, IconsNerd); got != c.nerd {
			t.Errorf("nerd %s = %q, want %q", c.node.Name, got, c.nerd)
		}
		if got, _ := iconFor(&c.node, IconsEmoji); got != c.emoji {
			t.Errorf("emoji %s = %q, want %q", c.node.Name, got, c.emoji)
		}
		if got, _ := iconFor(&c.node, IconsNone); got != "" {
			t.Errorf("none %s = %q", c.node.Name, got)
		}
	}
	if _, style := iconFor(&Node{Name: "main.rs"}, IconsNerd); style.FG != vt.RGBColor(0xde, 0xa5, 0x84) {
		t.Errorf("rust's icon in its orange: %+v", style)
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

// TestTheActivityBarIsHerdrSidebars: with icons on, the panel is laid out
// as herdr-sidebar's — the activity bar with the views as icons on the
// left, the one shown in a chip reaching a half block above and below, and
// a gear against the right edge; the project's name in capitals; the tree;
// the menus hint; the branch and sync on the bottom line — and clicks
// still find each part. If it regresses, the bar runs off the panel, a
// click on an icon lands on the wrong view, or a click on a file selects
// the one two rows up.
func TestTheActivityBarIsHerdrSidebars(t *testing.T) {
	dir := repo(t)
	m := New(dir, nil)
	m.Configure(Settings{Icons: IconsNerd, Follow: true})

	lines := strings.Split(screen(m, 32, 14), "\n")
	if !strings.HasPrefix(lines[1], "  \uf07b    \uf002    \uf126  ") || !strings.HasSuffix(strings.TrimRight(lines[1], " "), "\uf013") {
		t.Errorf("icons on the left, the gear on the right:\n%q", lines[1])
	}
	if !strings.HasPrefix(lines[0], " ▄▄▄▄") || !strings.HasPrefix(lines[2], " ▀▀▀▀") {
		t.Errorf("the shown view's chip reaches above and below:\n%q\n%q", lines[0], lines[2])
	}
	if lines[3] != " "+strings.ToUpper(filepath.Base(dir)) || !strings.Contains(strings.Join(lines[4:], "\n"), "kept.txt") {
		t.Errorf("the project's name and the tree under the activity bar:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.HasSuffix(lines[12], "m / right-click for menus") || !strings.HasPrefix(lines[13], " \ue725 main ⟳") {
		t.Errorf("the hint, and the branch and sync on the bottom line:\n%q\n%q", lines[12], lines[13])
	}

	kept := -1
	for y, line := range lines {
		if strings.Contains(line, "kept.txt") {
			kept = y
		}
	}
	m.Mouse(Mouse{X: 6, Y: kept, Press: true})
	if n := m.selectedNode(); n == nil || n.Name != "kept.txt" {
		t.Errorf("a click on kept.txt's row selects it: %+v", n)
	}

	m.Mouse(Mouse{X: 2, Y: 13, Press: true})
	if m.mode != modeBranch {
		t.Errorf("clicking the branch opens the branch picker: mode %d", m.mode)
	}
	m.mode = modeList
	screen(m, 32, 14)

	m.Mouse(Mouse{X: 11, Y: 1, Press: true})
	if m.view != ViewChanges {
		t.Errorf("clicking the third icon shows the changes: view %d", m.view)
	}
	screen(m, 32, 14)
	m.Mouse(Mouse{X: 31, Y: 1, Press: true})
	if m.mode != modeSettings {
		t.Fatalf("clicking the gear opens the settings: mode %d", m.mode)
	}
	if text := screen(m, 32, 14); !strings.Contains(text, "Settings") || !strings.Contains(text, "nerd font") {
		t.Errorf("the settings:\n%s", text)
	}

	m.Configure(Settings{Icons: IconsEmoji, Follow: true})
	m.mode = modeList
	if line := strings.Split(screen(m, 32, 14), "\n")[1]; !strings.HasPrefix(line, "  📁   🔍   🔀  ") || !strings.Contains(line, "⚙") {
		t.Errorf("emoji bar:\n%q", line)
	}

	m.Configure(Settings{Icons: IconsNone, Follow: true})
	if line := strings.Split(screen(m, 32, 14), "\n")[0]; !strings.Contains(line, "files │ search │ changes 2") {
		t.Errorf("without icons, the one line of names:\n%q", line)
	}
}

// TestTheGearsSettingsWriteTheFilesSection: enter on a setting moves it to
// its next value, writes it to [files] and applies it here at once; a side
// or a width says it is for next time. If it regresses, the gear opens a
// screen that changes nothing, or writes what the panel then ignores.
func TestTheGearsSettingsWriteTheFilesSection(t *testing.T) {
	dir := repo(t)
	m := New(dir, nil)
	m.Configure(Settings{Icons: IconsNerd, Follow: true, Width: 32})
	written := map[string]string{}
	m.SetSettingsWriter(func(key, value string) error { written[key] = value; return nil })
	m.openSettings()

	keys(m, "j", "j", "j") // dotfiles
	m.Key(Key{Name: "enter"})
	if written["hidden"] != "true" || !m.settings.Hidden || !m.tree.Hidden {
		t.Errorf("dotfiles hidden, written and applied: %v %+v", written, m.settings)
	}
	keys(m, "j", "j") // width
	m.Key(Key{Name: "enter"})
	if written["width"] != "40" || !strings.Contains(m.message, "when the panel opens again") {
		t.Errorf("width is next: %v %q", written, m.message)
	}
	keys(m, "j") // back to icons
	m.Key(Key{Name: "enter"})
	if written["icons"] != `"emoji"` || m.settings.Icons != IconsEmoji {
		t.Errorf("icons move on to emoji: %v %+v", written, m.settings)
	}
	m.Key(Key{Name: "esc"})
	if m.mode != modeList {
		t.Errorf("esc closes the settings")
	}
}
