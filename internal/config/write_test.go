package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAnEditKeepsTheFileTheUserWrote: a settings file is read by the person
// who wrote it. Rewriting the whole thing from the parsed values would take
// away their comments and their order, and they would notice.
func TestAnEditKeepsTheFileTheUserWrote(t *testing.T) {
	original := `# my settings
[keys]
# I use ctrl+a because screen did
prefix = "ctrl+a"

[ui]
sidebar = true   # keep it
grouped = false
`
	got := Upsert(original, "ui", "grouped", "true")
	for _, keep := range []string{"# my settings", "# I use ctrl+a because screen did", `prefix = "ctrl+a"`, "sidebar = true   # keep it"} {
		if !strings.Contains(got, keep) {
			t.Errorf("the edit lost %q:\n%s", keep, got)
		}
	}
	if !strings.Contains(got, "grouped = true") || strings.Contains(got, "grouped = false") {
		t.Errorf("the value was not changed:\n%s", got)
	}
}

// TestAnEditAddsWhatIsMissing covers a key or a whole section the file does
// not have yet, which is the ordinary case for a setting nobody has touched.
func TestAnEditAddsWhatIsMissing(t *testing.T) {
	got := Upsert("[ui]\nsidebar = true\n", "ui", "grouped", "true")
	if !strings.Contains(got, "sidebar = true\ngrouped = true") {
		t.Errorf("the key was not added to its section:\n%s", got)
	}

	got = Upsert("[ui]\nsidebar = true\n", "sound", "enabled", "true")
	if !strings.Contains(got, "[sound]\nenabled = true") {
		t.Errorf("the section was not added:\n%s", got)
	}

	// Into the right section, not the first one that happens to hold the key.
	two := "[notify]\ntoasts = \"tend\"\n\n[sound]\nenabled = false\n"
	got = Upsert(two, "sound", "enabled", "true")
	if !strings.Contains(got, "[notify]\ntoasts = \"tend\"") || !strings.Contains(got, "[sound]\nenabled = true") {
		t.Errorf("the edit went to the wrong section:\n%s", got)
	}

	// Spacing people actually write.
	got = Upsert("[ui]\nsidebar=true\n", "ui", "sidebar", "false")
	if !strings.Contains(got, "sidebar = false") || strings.Contains(got, "sidebar=true") {
		t.Errorf("an unspaced assignment was not replaced:\n%s", got)
	}
}

// TestAnEditThatWouldBreakTheFileIsRefused: writing a settings file tend will
// not start with locks the user out of their own settings.
func TestAnEditThatWouldBreakTheFileIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tend.toml")
	if err := os.WriteFile(path, []byte("[keys]\nprefix = \"ctrl+b\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEND_CONFIG", path)

	if err := Set("notify", "toasts", Quote("shouting")); err == nil {
		t.Error("a value the settings refuse was written anyway")
	}
	after, _ := os.ReadFile(path)
	if string(after) != "[keys]\nprefix = \"ctrl+b\"\n" {
		t.Errorf("the file was changed by a refused edit:\n%s", after)
	}

	if err := Set("ui", "grouped", Bool(true)); err != nil {
		t.Fatalf("a good edit failed: %v", err)
	}
	cfg, err := LoadFile(path)
	if err != nil || !cfg.UI.Grouped {
		t.Errorf("after the edit: %+v, %v", cfg.UI, err)
	}
}
