package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallQodercliWritesHookAndUpdatesSettings(t *testing.T) {
	base := t.TempDir()
	qoderDir := filepath.Join(base, ".qoder")
	if err := os.MkdirAll(qoderDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qoderDir, "settings.json"), []byte(`{"permissions":{"allow":["Read"]},"hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(QoderConfigDirEnv, qoderDir)

	installed, err := InstallQodercli()
	if err != nil {
		t.Fatal(err)
	}
	if installed.HookPath != filepath.Join(qoderDir, "hooks", QodercliHookInstallName) {
		t.Fatalf("hook = %q", installed.HookPath)
	}
	settings := readSettings(t, installed.SettingsPath)
	if _, ok := settings["permissions"]; !ok {
		t.Fatal("permissions lost")
	}
	for _, ev := range qodercliHookEvents {
		cmd := settings["hooks"].(map[string]any)[ev.Event].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
		if !strings.Contains(cmd, QodercliHookInstallName) || !strings.HasSuffix(cmd, ev.Action) {
			t.Fatalf("%s command = %q", ev.Event, cmd)
		}
	}
}

func TestInstallQodercliIsIdempotent(t *testing.T) {
	base := t.TempDir()
	qoderDir := filepath.Join(base, ".qoder")
	if err := os.MkdirAll(qoderDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(QoderConfigDirEnv, qoderDir)
	if _, err := InstallQodercli(); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallQodercli(); err != nil {
		t.Fatal(err)
	}
	settings := readSettings(t, filepath.Join(qoderDir, "settings.json"))
	for _, ev := range qodercliHookEvents {
		entries := settings["hooks"].(map[string]any)[ev.Event].([]any)
		if len(entries) != 1 {
			t.Fatalf("%s len = %d", ev.Event, len(entries))
		}
	}
}

func TestUninstallQodercliRemovesHooksPreservesOthers(t *testing.T) {
	base := t.TempDir()
	qoderDir := filepath.Join(base, ".qoder")
	if err := os.MkdirAll(qoderDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(QoderConfigDirEnv, qoderDir)
	if _, err := InstallQodercli(); err != nil {
		t.Fatal(err)
	}
	settings := readSettings(t, filepath.Join(qoderDir, "settings.json"))
	session := settings["hooks"].(map[string]any)["SessionStart"].([]any)
	settings["hooks"].(map[string]any)["SessionStart"] = append(session, map[string]any{
		"matcher": "*",
		"hooks":   []any{map[string]any{"type": "command", "command": "echo user-defined"}},
	})
	writeSettings(t, filepath.Join(qoderDir, "settings.json"), settings)

	result, err := UninstallQodercli()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedHookFile || !result.UpdatedSettings {
		t.Fatalf("result = %+v", result)
	}
	got := readSettings(t, filepath.Join(qoderDir, "settings.json"))
	entries := got["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(entries) != 1 {
		t.Fatalf("SessionStart len = %d", len(entries))
	}
	cmd := entries[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"]
	if cmd != "echo user-defined" {
		t.Fatalf("user hook lost: %v", cmd)
	}
}

func TestInstallQodercliErrorsWhenConfigDirMissing(t *testing.T) {
	base := t.TempDir()
	t.Setenv(QoderConfigDirEnv, filepath.Join(base, "missing"))
	_, err := InstallQodercli()
	if err == nil || !strings.Contains(err.Error(), "qodercli config directory not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestInstallQwenWritesSessionHook(t *testing.T) {
	base := t.TempDir()
	qwenDir := filepath.Join(base, ".qwen")
	if err := os.MkdirAll(qwenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qwenDir, "settings.json"), []byte(`{"theme":"dark","hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(QwenHomeEnv, qwenDir)

	installed, err := InstallQwen()
	if err != nil {
		t.Fatal(err)
	}
	if string(mustReadBytes(t, installed.HookPath)) != qwenHookAsset {
		t.Fatal("hook asset mismatch")
	}
	settings := readSettings(t, installed.SettingsPath)
	if settings["theme"] != "dark" {
		t.Fatal("theme lost")
	}
	entry := settings["hooks"].(map[string]any)["SessionStart"].([]any)[0].(map[string]any)
	if entry["matcher"] != "*" {
		t.Fatalf("matcher = %v", entry["matcher"])
	}
	hook := entry["hooks"].([]any)[0].(map[string]any)
	timeout, ok := hook["timeout"].(float64)
	if !ok || timeout != 10000 {
		t.Fatalf("timeout = %v", hook["timeout"])
	}
	cmd := hook["command"].(string)
	if !strings.HasSuffix(cmd, " session") {
		t.Fatalf("command = %q", cmd)
	}
}

func TestInstallQwenUsesEnvAndUninstall(t *testing.T) {
	base := t.TempDir()
	qwenDir := filepath.Join(base, "custom-qwen")
	if err := os.MkdirAll(qwenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(QwenHomeEnv, qwenDir)
	installed, err := InstallQwen()
	if err != nil {
		t.Fatal(err)
	}
	if installed.SettingsPath != filepath.Join(qwenDir, "settings.json") {
		t.Fatalf("settings = %q", installed.SettingsPath)
	}
	result, err := UninstallQwen()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedHookFile {
		t.Fatal("expected hook removed")
	}
	settings := readSettings(t, filepath.Join(qwenDir, "settings.json"))
	if hooks, ok := settings["hooks"].(map[string]any); ok {
		if _, ok := hooks["SessionStart"]; ok {
			t.Fatal("SessionStart should be gone")
		}
	}
}

func TestInstallQwenErrorsWhenConfigDirMissing(t *testing.T) {
	base := t.TempDir()
	t.Setenv(QwenHomeEnv, filepath.Join(base, "missing"))
	_, err := InstallQwen()
	if err == nil || !strings.Contains(err.Error(), "qwen code config directory not found") {
		t.Fatalf("error = %v", err)
	}
}

func mustReadBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
