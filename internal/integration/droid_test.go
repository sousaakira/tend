package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallDroidWritesHookToSettingsAndCleansLegacyHooksJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	droidDir := filepath.Join(home, ".factory")
	if err := os.MkdirAll(filepath.Join(droidDir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyHookPath := filepath.Join(droidDir, "hooks", DroidHookInstallName)
	legacyCommand := HookCommand(legacyHookPath, "")
	legacy := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": legacyCommand, "timeout": 10}},
			}},
			"PreToolUse": []any{map[string]any{
				"matcher": "Read",
				"hooks":   []any{map[string]any{"type": "command", "command": "echo keep", "timeout": 10}},
			}},
		},
	}
	writeSettings(t, filepath.Join(droidDir, "hooks.json"), legacy)
	if err := os.WriteFile(filepath.Join(droidDir, "settings.json"), []byte(`{"theme":"factory-dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallDroid()
	if err != nil {
		t.Fatal(err)
	}
	if !installed.UpdatedLegacyHooks {
		t.Fatal("expected legacy hooks.json to be updated")
	}
	gotAsset, err := os.ReadFile(installed.HookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotAsset) != droidHookAsset {
		t.Fatal("hook content mismatch")
	}

	settings := readSettings(t, installed.SettingsPath)
	if settings["theme"] != "factory-dark" {
		t.Fatal("theme not preserved")
	}
	session := settings["hooks"].(map[string]any)["SessionStart"].([]any)[0].(map[string]any)
	if _, ok := session["matcher"]; ok {
		t.Fatal("SessionStart should have no matcher")
	}
	cmd := session["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.Contains(cmd, DroidHookInstallName) || !strings.HasSuffix(cmd, "session") {
		t.Fatalf("command = %q", cmd)
	}

	legacyHooks := readSettings(t, installed.HooksPath)
	if legacyHooks["hooks"].(map[string]any)["PreToolUse"].([]any)[0].(map[string]any)["matcher"] != "Read" {
		t.Fatal("PreToolUse matcher lost")
	}
	if _, ok := legacyHooks["hooks"].(map[string]any)["SessionStart"]; ok {
		t.Fatal("legacy SessionStart should be gone")
	}
}

func TestInstallDroidIsIdempotentForHookEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	droidDir := filepath.Join(home, ".factory")
	if err := os.MkdirAll(droidDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallDroid(); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallDroid(); err != nil {
		t.Fatal(err)
	}

	settings := readSettings(t, filepath.Join(droidDir, "settings.json"))
	for _, rem := range droidHookEvents {
		entries := settings["hooks"].(map[string]any)[rem.Event].([]any)
		if len(entries) != 1 {
			t.Fatalf("hooks.%s len = %d", rem.Event, len(entries))
		}
	}
}

func TestUninstallDroidRemovesHooksAndPreservesOthers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	droidDir := filepath.Join(home, ".factory")
	hooksDir := filepath.Join(droidDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, DroidHookInstallName)
	if err := os.WriteFile(hookPath, []byte(droidHookAsset), 0o755); err != nil {
		t.Fatal(err)
	}
	command := HookCommand(hookPath, "")
	writeSettings(t, filepath.Join(droidDir, "hooks.json"), map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{map[string]any{
				"hooks": []any{
					map[string]any{"type": "command", "command": command, "timeout": 10},
					map[string]any{"type": "command", "command": "echo keep", "timeout": 10},
				},
			}},
			"PreToolUse": []any{map[string]any{
				"matcher": "Read",
				"hooks":   []any{map[string]any{"type": "command", "command": "echo read", "timeout": 10}},
			}},
		},
	})
	writeSettings(t, filepath.Join(droidDir, "settings.json"), map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 10}},
			}},
			"PostToolUse": []any{map[string]any{
				"matcher": "Edit",
				"hooks":   []any{map[string]any{"type": "command", "command": "echo post", "timeout": 10}},
			}},
		},
	})

	result, err := UninstallDroid()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedHookFile || !result.UpdatedHooks || !result.UpdatedSettings {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(result.HookPath); !os.IsNotExist(err) {
		t.Fatal("hook still present")
	}

	hooks := readSettings(t, filepath.Join(droidDir, "hooks.json"))["hooks"].(map[string]any)
	sessionHooks := hooks["SessionStart"].([]any)[0].(map[string]any)["hooks"].([]any)
	if len(sessionHooks) != 1 || sessionHooks[0].(map[string]any)["command"] != "echo keep" {
		t.Fatalf("SessionStart = %#v", sessionHooks)
	}
	if hooks["PreToolUse"].([]any)[0].(map[string]any)["matcher"] != "Read" {
		t.Fatal("PreToolUse matcher lost")
	}

	settings := readSettings(t, filepath.Join(droidDir, "settings.json"))["hooks"].(map[string]any)
	if _, ok := settings["SessionStart"]; ok {
		t.Fatal("settings SessionStart should be gone")
	}
	if settings["PostToolUse"].([]any)[0].(map[string]any)["matcher"] != "Edit" {
		t.Fatal("PostToolUse matcher lost")
	}
}

func TestInstallDroidErrorsWhenConfigDirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := InstallDroid()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "droid config directory not found") {
		t.Fatalf("error = %v", err)
	}
}
