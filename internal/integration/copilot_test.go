package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCopilotWritesHookAndUpdatesSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(CopilotHomeEnv, "")

	copilotDir := filepath.Join(home, ".copilot")
	if err := os.MkdirAll(copilotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(copilotDir, "hooks", CopilotHookInstallName)
	stale := HookCommand(hookPath, "")
	settings := map[string]any{
		"theme": "dark",
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"type": "command", "command": "echo keep", "timeoutSec": float64(10)},
			},
			"sessionStart": []any{
				map[string]any{"type": "command", "bash": stale, "timeoutSec": float64(10)},
			},
		},
	}
	writeSettings(t, filepath.Join(copilotDir, "settings.json"), settings)

	installed, err := InstallCopilot()
	if err != nil {
		t.Fatal(err)
	}
	if installed.HookPath != hookPath {
		t.Fatalf("hook = %q, want %q", installed.HookPath, hookPath)
	}
	gotAsset, err := os.ReadFile(installed.HookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotAsset) != copilotHookAsset {
		t.Fatal("hook content mismatch")
	}

	got := readSettings(t, installed.SettingsPath)
	if got["theme"] != "dark" {
		t.Fatal("theme not preserved")
	}
	pre := got["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 1 || pre[0].(map[string]any)["command"] != "echo keep" {
		t.Fatalf("PreToolUse lost: %#v", pre)
	}
	field := DirectCommandField()
	sessionCmd := got["hooks"].(map[string]any)["SessionStart"].([]any)[0].(map[string]any)[field].(string)
	if !strings.Contains(sessionCmd, CopilotHookInstallName) {
		t.Fatalf("SessionStart command = %q", sessionCmd)
	}
	for _, event := range copilotRemovedLifecycleHookEvents {
		if entries, ok := got["hooks"].(map[string]any)[event]; ok {
			if strings.Contains(mustJSON(t, entries), CopilotHookInstallName) {
				t.Fatalf("expected herdr hooks.%s entries to be removed", event)
			}
		}
	}
	if _, ok := got["hooks"].(map[string]any)["sessionStart"]; ok {
		t.Fatal("sessionStart should be removed")
	}
}

func TestInstallCopilotUsesCopilotHomeEnvAndIsIdempotent(t *testing.T) {
	copilotDir := filepath.Join(t.TempDir(), "custom-copilot")
	if err := os.MkdirAll(copilotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(CopilotHomeEnv, copilotDir)

	installed, err := InstallCopilot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InstallCopilot(); err != nil {
		t.Fatal(err)
	}

	if installed.HookPath != filepath.Join(copilotDir, "hooks", CopilotHookInstallName) {
		t.Fatalf("hook = %q", installed.HookPath)
	}
	settings := readSettings(t, filepath.Join(copilotDir, "settings.json"))
	session := settings["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(session) != 1 {
		t.Fatalf("SessionStart len = %d", len(session))
	}
	for _, event := range copilotRemovedLifecycleHookEvents {
		if _, ok := settings["hooks"].(map[string]any)[event]; ok {
			t.Fatalf("expected hooks.%s to be absent", event)
		}
	}
}

func TestUninstallCopilotRemovesHooksAndPreservesOthers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(CopilotHomeEnv, "")
	copilotDir := filepath.Join(home, ".copilot")
	hooksDir := filepath.Join(copilotDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, CopilotHookInstallName)
	if err := os.WriteFile(hookPath, []byte(copilotHookAsset), 0o755); err != nil {
		t.Fatal(err)
	}
	command := HookCommand(hookPath, "")
	field := DirectCommandField()
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"type": "command", field: command, "timeoutSec": float64(10)},
				map[string]any{"type": "command", "command": "echo keep", "timeoutSec": float64(10)},
			},
			"PostToolUse": []any{
				map[string]any{"type": "command", field: command, "timeoutSec": float64(10)},
			},
			"notification": []any{
				map[string]any{
					"type":       "command",
					"matcher":    "permission_prompt|elicitation_dialog|agent_idle",
					field:        command,
					"timeoutSec": float64(10),
				},
			},
		},
	}
	writeSettings(t, filepath.Join(copilotDir, "settings.json"), settings)

	result, err := UninstallCopilot()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedHookFile || !result.UpdatedSettings {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(result.HookPath); !os.IsNotExist(err) {
		t.Fatal("hook still present")
	}

	got := readSettings(t, filepath.Join(copilotDir, "settings.json"))
	pre := got["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 1 || pre[0].(map[string]any)["command"] != "echo keep" {
		t.Fatalf("PreToolUse = %#v", pre)
	}
	if _, ok := got["hooks"].(map[string]any)["PostToolUse"]; ok {
		t.Fatal("PostToolUse should be gone")
	}
	if _, ok := got["hooks"].(map[string]any)["notification"]; ok {
		t.Fatal("notification should be gone")
	}
}

func TestInstallCopilotErrorsWhenConfigDirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(CopilotHomeEnv, "")

	_, err := InstallCopilot()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "copilot config directory not found") {
		t.Fatalf("error = %v", err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := MarshalJSONPretty(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
