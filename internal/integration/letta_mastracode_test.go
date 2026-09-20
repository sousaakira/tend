package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAndUninstallLetta(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	lettaDir := filepath.Join(home, ".letta")
	if err := os.MkdirAll(lettaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(lettaDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"theme":"dark","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo user"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallLetta()
	if err != nil {
		t.Fatal(err)
	}
	wantHook := filepath.Join(lettaDir, "hooks", LettaHookInstallName)
	if installed.HookPath != wantHook {
		t.Fatalf("hook = %q", installed.HookPath)
	}
	if string(mustReadBytes(t, installed.HookPath)) != lettaHookAsset {
		t.Fatal("hook asset mismatch")
	}
	settings := readSettings(t, settingsPath)
	if settings["theme"] != "dark" {
		t.Fatal("theme lost")
	}
	entries := settings["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(entries) != 2 {
		t.Fatalf("SessionStart len = %d", len(entries))
	}
	if entries[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"] != "echo user" {
		t.Fatal("user hook lost")
	}
	tendHook := entries[1].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if tendHook["quiet"] != true {
		t.Fatalf("quiet = %v", tendHook["quiet"])
	}
	timeout, _ := tendHook["timeout"].(float64)
	if timeout != float64(LettaHookTimeoutMS) {
		t.Fatalf("timeout = %v", tendHook["timeout"])
	}

	if _, err := InstallLetta(); err != nil {
		t.Fatal(err)
	}
	settings = readSettings(t, settingsPath)
	entries = settings["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(entries) != 2 {
		t.Fatalf("after reinstall SessionStart len = %d", len(entries))
	}

	result, err := UninstallLetta()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedHookFile || !result.UpdatedSettings {
		t.Fatalf("result = %+v", result)
	}
	settings = readSettings(t, settingsPath)
	entries = settings["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"] != "echo user" {
		t.Fatalf("user hook not preserved: %#v", entries)
	}
}

func TestInstallLettaErrorsWhenConfigDirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, err := InstallLetta()
	if err == nil || !strings.Contains(err.Error(), "letta code config directory not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestInstallLettaDoesNotPublishHookWhenSettingsInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	lettaDir := filepath.Join(home, ".letta")
	if err := os.MkdirAll(lettaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lettaDir, "settings.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallLetta(); err == nil {
		t.Fatal("expected error")
	}
	hookPath := filepath.Join(lettaDir, "hooks", LettaHookInstallName)
	if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
		t.Fatal("hook should not be published when settings are invalid")
	}
}

func TestInstallMastracodeWritesFlatHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mastraDir := filepath.Join(home, ".mastracode")
	if err := os.MkdirAll(mastraDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mastraDir, "hooks.json"), []byte(`{"PostToolUse":[{"type":"command","command":"echo keep-me"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallMastracode()
	if err != nil {
		t.Fatal(err)
	}
	if string(mustReadBytes(t, installed.HookPath)) != mastracodeHookAsset {
		t.Fatal("hook asset mismatch")
	}
	hooks := readSettings(t, installed.HooksPath)
	for _, ev := range mastracodeHookEvents {
		entries := hooks[ev.Event].([]any)
		if len(entries) != 1 {
			t.Fatalf("%s len = %d", ev.Event, len(entries))
		}
		cmd := entries[0].(map[string]any)["command"].(string)
		if cmd != HookCommand(installed.HookPath, ev.Action) {
			t.Fatalf("%s command = %q", ev.Event, cmd)
		}
	}
	if hooks["PostToolUse"].([]any)[0].(map[string]any)["command"] != "echo keep-me" {
		t.Fatal("user PostToolUse lost")
	}
}

func TestInstallMastracodeCreatesDirWhenMissing(t *testing.T) {
	// herdr create_dir_all creates ~/.mastracode; there is no missing-dir error.
	home := t.TempDir()
	t.Setenv("HOME", home)
	installed, err := InstallMastracode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(installed.HookPath); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallMastracodePreservesOthers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".mastracode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallMastracode(); err != nil {
		t.Fatal(err)
	}
	hooksPath := filepath.Join(home, ".mastracode", "hooks.json")
	hooks := readSettings(t, hooksPath)
	hooks["PostToolUse"] = []any{map[string]any{"type": "command", "command": "echo keep-me"}}
	writeSettings(t, hooksPath, hooks)

	result, err := UninstallMastracode()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedHookFile || !result.UpdatedHooks {
		t.Fatalf("result = %+v", result)
	}
	got := readSettings(t, hooksPath)
	if got["PostToolUse"].([]any)[0].(map[string]any)["command"] != "echo keep-me" {
		t.Fatal("user hook lost")
	}
	for _, ev := range mastracodeHookEvents {
		if _, ok := got[ev.Event]; ok && ev.Event != "PostToolUse" {
			// SessionStart etc. should be gone
			if ev.Event == "SessionStart" || ev.Event == "Stop" {
				t.Fatalf("%s should be removed", ev.Event)
			}
		}
	}
	if _, ok := got["SessionStart"]; ok {
		t.Fatal("SessionStart should be gone")
	}
}

func TestInstallMastracodeErrorsWhenEventNotArray(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mastraDir := filepath.Join(home, ".mastracode")
	if err := os.MkdirAll(mastraDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mastraDir, "hooks.json"), []byte(`{"SessionStart":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := InstallMastracode()
	if err == nil || !strings.Contains(err.Error(), "hook entries for SessionStart must be an array") {
		t.Fatalf("error = %v", err)
	}
}
