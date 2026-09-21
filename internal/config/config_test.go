package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultsAreUsable(t *testing.T) {
	c := Defaults()
	if err := c.validate(); err != nil {
		t.Fatalf("the defaults do not validate: %v", err)
	}
	if key, _ := c.PrefixKey(); key != 0x02 {
		t.Errorf("prefix = %#x, want ctrl+b", key)
	}
	if got := c.Scrollback(); got != 5000 {
		t.Errorf("scrollback = %d", got)
	}
	if !c.UI.Mouse {
		t.Error("the mouse should be on by default")
	}
}

// TestMissingFileIsNotAnError: the settings file is optional, and a user who
// has never written one should not be told about it.
func TestMissingFileIsNotAnError(t *testing.T) {
	t.Setenv("TEND_CONFIG", filepath.Join(t.TempDir(), "absent.toml"))
	c, err := Load()
	if err != nil {
		t.Fatalf("Load with no file: %v", err)
	}
	if key, _ := c.PrefixKey(); key != 0x02 {
		t.Error("a missing file should give the defaults")
	}
}

// TestPartialFileKeepsDefaults: a user changing one setting writes one
// setting, not a whole file.
func TestPartialFileKeepsDefaults(t *testing.T) {
	path := writeConfig(t, "[keys]\nprefix = \"ctrl+a\"\n")
	c, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if key, _ := c.PrefixKey(); key != 0x01 {
		t.Errorf("prefix = %#x, want ctrl+a", key)
	}
	if got := c.Scrollback(); got != 5000 {
		t.Errorf("scrollback = %d, want the default kept", got)
	}
	if !c.UI.Mouse {
		t.Error("mouse should keep its default")
	}
}

// TestUnknownSettingIsAnError: a misspelled setting that is silently ignored
// is one the user believes is in effect.
func TestUnknownSettingIsAnError(t *testing.T) {
	path := writeConfig(t, "[keys]\nprefx = \"ctrl+a\"\n")
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("a misspelled setting should fail")
	}
	if !strings.Contains(err.Error(), "prefx") {
		t.Errorf("err = %v, want it to name the setting", err)
	}
}

func TestMalformedFileIsAnError(t *testing.T) {
	path := writeConfig(t, "[keys\nprefix =\n")
	if _, err := LoadFile(path); err == nil {
		t.Error("malformed TOML should fail")
	}
}

// TestInvalidValuesAreRejectedAtLoad: a bad value must be reported when the
// file is read, not when the setting is first used.
func TestInvalidValuesAreRejectedAtLoad(t *testing.T) {
	cases := map[string]string{
		"bad prefix":     "[keys]\nprefix = \"meta+q\"\n",
		"bad duration":   "[server]\ndetect_interval = \"soon\"\n",
		"zero duration":  "[server]\ndetect_interval = \"0s\"\n",
		"bad colour":     "[ui.theme]\nworking = \"chartreuse\"\n",
		"unknown theme":  "[ui.theme]\nname = \"mauve-dreams\"\n",
		"bad scrollback": "[pane]\nscrollback = -1\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadFile(writeConfig(t, body)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// TestInvalidConfigFallsBackToDefaults: whatever else happens, the caller gets
// a usable configuration rather than a zero one.
func TestInvalidConfigFallsBackToDefaults(t *testing.T) {
	c, err := LoadFile(writeConfig(t, "[keys]\nprefix = \"nonsense\"\n"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if key, _ := c.PrefixKey(); key != 0x02 {
		t.Error("the returned config should still be usable")
	}
}

func TestPrefixKey(t *testing.T) {
	cases := map[string]byte{
		"ctrl+a":   0x01,
		"ctrl+b":   0x02,
		"ctrl+z":   0x1a,
		"CTRL+B":   0x02,
		" ctrl+b ": 0x02,
		"":         0x02,
		"none":     0,
		"off":      0,
	}
	for input, want := range cases {
		c := Defaults()
		c.Keys.Prefix = input
		got, err := c.PrefixKey()
		if err != nil {
			t.Errorf("%q: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("%q = %#x, want %#x", input, got, want)
		}
	}

	for _, bad := range []string{"b", "alt+b", "ctrl+", "ctrl+bb", "ctrl+1"} {
		c := Defaults()
		c.Keys.Prefix = bad
		if _, err := c.PrefixKey(); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestDetectInterval(t *testing.T) {
	c := Defaults()
	if got, _ := c.DetectInterval(); got != 150*time.Millisecond {
		t.Errorf("default = %v", got)
	}
	c.Server.DetectInterval = "1s"
	if got, _ := c.DetectInterval(); got != time.Second {
		t.Errorf("got %v, want 1s", got)
	}
}

func TestShellFollowsTheEnvironment(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/fish")
	c := Defaults()
	if got := c.Shell(); len(got) != 1 || got[0] != "/usr/bin/fish" {
		t.Errorf("Shell = %v, want $SHELL", got)
	}

	// An explicit setting wins over the environment.
	c.Pane.Shell = []string{"/bin/dash", "-l"}
	if got := c.Shell(); len(got) != 2 || got[0] != "/bin/dash" {
		t.Errorf("Shell = %v, want the configured one", got)
	}
}

func TestParseColor(t *testing.T) {
	cases := map[string]Color{
		"red":         {Index: 1},
		"BrightBlue":  {Index: 12},
		"grey":        {Index: 8},
		"0":           {Index: 0},
		"208":         {Index: 208},
		"255":         {Index: 255},
		"#ff8800":     {RGB: true, R: 0xff, G: 0x88, B: 0x00},
		"  #000000  ": {RGB: true},
	}
	for input, want := range cases {
		got, ok := ParseColor(input)
		if !ok {
			t.Errorf("%q was rejected", input)
			continue
		}
		if got != want {
			t.Errorf("%q = %+v, want %+v", input, got, want)
		}
	}

	for _, bad := range []string{"", "chartreuse", "256", "-1", "#fff", "#gggggg", "#ff88000"} {
		if _, ok := ParseColor(bad); ok {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// TestExampleIsValid keeps the file tend writes for people from being one it
// would itself refuse to read.
func TestExampleIsValid(t *testing.T) {
	path := writeConfig(t, Example)
	c, err := LoadFile(path)
	if err != nil {
		t.Fatalf("the example config does not load: %v", err)
	}
	if key, _ := c.PrefixKey(); key != 0x02 {
		t.Error("the example should describe the defaults")
	}
}

func TestPathRespectsOverride(t *testing.T) {
	t.Setenv("TEND_CONFIG", "/tmp/somewhere/else.toml")
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/somewhere/else.toml" {
		t.Errorf("Path = %q", got)
	}
}

// TestSidebarDefaultsOn: the list of what needs attention is the reason to
// run tend, so it is not behind a keystroke nobody presses.
func TestSidebarDefaultsOn(t *testing.T) {
	if !Defaults().UI.Sidebar {
		t.Error("the sidebar should be on by default")
	}
	if Defaults().UI.Grouped {
		t.Error("agents should start flat")
	}

	c, err := LoadFile(writeConfig(t, "[ui]\nsidebar = false\ngrouped = true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.UI.Sidebar {
		t.Error("sidebar = false should turn it off")
	}
	if !c.UI.Grouped {
		t.Error("grouped = true should group")
	}
	if !c.UI.Mouse {
		t.Error("the other ui settings should keep their defaults")
	}
}

// TestThemeNamesAcceptTheSpellingsHerdrDoes: a theme written the way people
// know it — "Tokyo Night", "catppuccin_mocha", "onedark" — must load, or a
// config carried over from herdr is refused for its spelling.
func TestThemeNamesAcceptTheSpellingsHerdrDoes(t *testing.T) {
	cases := map[string]string{
		"Tokyo Night":      "tokyo-night",
		"catppuccin_mocha": "catppuccin",
		"onedark":          "one-dark",
		"dawn":             "rose-pine-dawn",
		"  VESPER ":        "vesper",
	}
	for input, want := range cases {
		if got, ok := CanonicalTheme(input); !ok || got != want {
			t.Errorf("CanonicalTheme(%q) = %q, %v; want %q", input, got, ok, want)
		}
	}
	for _, name := range ThemeNames {
		if got, ok := CanonicalTheme(name); !ok || got != name {
			t.Errorf("%q is listed but does not resolve to itself (got %q)", name, got)
		}
	}
	if _, ok := CanonicalTheme("mauve-dreams"); ok {
		t.Error("a name that is no theme should not resolve")
	}
}

// TestWindowTitleTemplates: the template is herdr's syntax, so a line copied
// from a herdr config means the same thing here, and a mistyped one is
// reported instead of writing "{worksapce}" into every window bar.
func TestWindowTitleTemplates(t *testing.T) {
	tmpl, err := ParseWindowTitle("{hostname}: { workspace } {{a}}")
	if err != nil || tmpl == nil {
		t.Fatalf("parse: %v", err)
	}
	values := map[WindowTitleToken]string{TitleHostname: "box", TitleWorkspace: "api"}
	got, ok := tmpl.Render(func(k WindowTitleToken) string { return values[k] })
	if !ok || got != "box: api {a}" {
		t.Errorf("rendered %q, want %q", got, "box: api {a}")
	}

	if tmpl, err := ParseWindowTitle(""); tmpl != nil || err != nil {
		t.Error("an empty template should mean no title, without an error")
	}
	for bad, want := range map[string]string{
		"{hostname": "unclosed",
		"a } b":     "unmatched",
		"{session}": "unknown token '{session}'",
	} {
		if _, err := ParseWindowTitle(bad); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseWindowTitle(%q) = %v, want an error about %s", bad, err, want)
		}
	}
	if _, err := LoadFile(writeConfig(t, "[ui]\nwindow_title = \"{worksapce}\"\n")); err == nil {
		t.Error("a mistyped token should be refused at load")
	}
	if c := Defaults(); c.UI.WindowTitle != DefaultWindowTitle {
		t.Errorf("default window title = %q, want herdr's %q", c.UI.WindowTitle, DefaultWindowTitle)
	}
}

// TestWindowTitlesAreSanitisedAndBounded: a pane title is chosen by whatever
// runs in the pane, and an escape in it would otherwise end the OSC early and
// write the rest to the terminal as commands.
func TestWindowTitlesAreSanitisedAndBounded(t *testing.T) {
	if got, _ := SanitizeWindowTitle("  tend\x1b api\x07\n  "); got != "tend api" {
		t.Errorf("got %q", got)
	}
	if _, ok := SanitizeWindowTitle("\x07\n"); ok {
		t.Error("a title of nothing but control characters is no title")
	}
	if got, _ := SanitizeWindowTitle(strings.Repeat("x", MaxWindowTitle+1)); len([]rune(got)) != MaxWindowTitle {
		t.Errorf("title of %d characters, want %d", len([]rune(got)), MaxWindowTitle)
	}
}
