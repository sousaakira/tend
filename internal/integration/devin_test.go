package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallDevinWritesHookAndUpdatesSettings(t *testing.T) {
	base := t.TempDir()
	xdg := filepath.Join(base, "xdg")
	devinDir := filepath.Join(xdg, "devin")
	if err := os.MkdirAll(devinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devinDir, "config.json"), []byte(`{"theme_mode":"dark","hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", filepath.Join(base, "home"))

	installed, err := InstallDevin()
	if err != nil {
		t.Fatal(err)
	}
	if installed.HookPath != filepath.Join(devinDir, DevinHookInstallName) {
		t.Fatalf("hook = %q", installed.HookPath)
	}
	gotAsset, err := os.ReadFile(installed.HookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotAsset) != devinHookAsset {
		t.Fatal("hook content mismatch")
	}

	settings := readSettings(t, installed.SettingsPath)
	if settings["theme_mode"] != "dark" {
		t.Fatal("theme_mode not preserved")
	}
	for _, rem := range devinHookEvents {
		cmd := settings["hooks"].(map[string]any)[rem.Event].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
		if !strings.Contains(cmd, DevinHookInstallName) || !strings.HasSuffix(cmd, rem.Action) {
			t.Fatalf("%s command = %q, want suffix %q", rem.Event, cmd, rem.Action)
		}
	}
}

func TestInstallDevinIsIdempotentForHookEntries(t *testing.T) {
	base := t.TempDir()
	xdg := filepath.Join(base, "xdg")
	devinDir := filepath.Join(xdg, "devin")
	if err := os.MkdirAll(devinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", filepath.Join(base, "home"))

	if _, err := InstallDevin(); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallDevin(); err != nil {
		t.Fatal(err)
	}

	settings := readSettings(t, filepath.Join(devinDir, "config.json"))
	for _, rem := range devinHookEvents {
		entries := settings["hooks"].(map[string]any)[rem.Event].([]any)
		if len(entries) != 1 {
			t.Fatalf("hooks.%s len = %d", rem.Event, len(entries))
		}
	}
}

func TestInstallDevinRemovesLegacyLifecycleHookEntries(t *testing.T) {
	base := t.TempDir()
	xdg := filepath.Join(base, "xdg")
	devinDir := filepath.Join(xdg, "devin")
	if err := os.MkdirAll(devinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", filepath.Join(base, "home"))

	hookPath := filepath.Join(devinDir, DevinHookInstallName)
	hooks := map[string]any{}
	for _, rem := range devinRemovedLifecycleHookEvents {
		hooks[rem.Event] = []any{map[string]any{
			"hooks": []any{map[string]any{
				"type": "command", "command": HookCommand(hookPath, rem.Action), "timeout": 10,
			}},
		}}
	}
	writeSettings(t, filepath.Join(devinDir, "config.json"), map[string]any{"hooks": hooks})

	if _, err := InstallDevin(); err != nil {
		t.Fatal(err)
	}

	settings := readSettings(t, filepath.Join(devinDir, "config.json"))
	sessionCommand := HookCommand(hookPath, "session")
	for _, rem := range devinRemovedLifecycleHookEvents {
		legacyCommand := HookCommand(hookPath, rem.Action)
		entries, ok := settings["hooks"].(map[string]any)[rem.Event]
		if ok {
			for _, entry := range entries.([]any) {
				nested := entry.(map[string]any)["hooks"].([]any)
				for _, hook := range nested {
					if hook.(map[string]any)["command"] == legacyCommand {
						t.Fatalf("legacy %s -> %s still present", rem.Event, rem.Action)
					}
				}
			}
		}
		installed := false
		for _, ev := range devinHookEvents {
			if ev.Event == rem.Event {
				installed = true
				break
			}
		}
		if !installed {
			continue
		}
		found := false
		for _, entry := range settings["hooks"].(map[string]any)[rem.Event].([]any) {
			for _, hook := range entry.(map[string]any)["hooks"].([]any) {
				if hook.(map[string]any)["command"] == sessionCommand {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("expected %s session hook", rem.Event)
		}
	}
}

func TestUninstallDevinRemovesHooksAndPreservesOthers(t *testing.T) {
	base := t.TempDir()
	xdg := filepath.Join(base, "xdg")
	devinDir := filepath.Join(xdg, "devin")
	if err := os.MkdirAll(devinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", filepath.Join(base, "home"))

	if _, err := InstallDevin(); err != nil {
		t.Fatal(err)
	}

	settings := readSettings(t, filepath.Join(devinDir, "config.json"))
	settings["hooks"].(map[string]any)["UserPromptSubmit"] = append(
		settings["hooks"].(map[string]any)["UserPromptSubmit"].([]any),
		map[string]any{
			"matcher": "*",
			"hooks": []any{map[string]any{
				"type": "command", "command": "echo keep", "timeout": 10,
			}},
		},
	)
	writeSettings(t, filepath.Join(devinDir, "config.json"), settings)

	result, err := UninstallDevin()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedHookFile || !result.UpdatedSettings {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(devinDir, DevinHookInstallName)); !os.IsNotExist(err) {
		t.Fatal("hook still present")
	}

	got := readSettings(t, filepath.Join(devinDir, "config.json"))
	ups := got["hooks"].(map[string]any)["UserPromptSubmit"].([]any)
	if len(ups) != 1 {
		t.Fatalf("UserPromptSubmit len = %d", len(ups))
	}
	if ups[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"] != "echo keep" {
		t.Fatalf("user hook lost: %#v", ups)
	}
	for _, rem := range devinHookEvents {
		if rem.Event == "UserPromptSubmit" {
			continue
		}
		if _, ok := got["hooks"].(map[string]any)[rem.Event]; ok {
			t.Fatalf("%s should be gone", rem.Event)
		}
	}
}

func TestInstallDevinErrorsWhenConfigDirMissing(t *testing.T) {
	base := t.TempDir()
	xdg := filepath.Join(base, "xdg")
	if err := os.MkdirAll(xdg, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", filepath.Join(base, "home"))

	_, err := InstallDevin()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "devin config directory not found") {
		t.Fatalf("error = %v", err)
	}
}
