package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallClaudeWritesHookAndUpdatesSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(ClaudeConfigDirEnv, "")

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"permissions":{"allow":["Read"]},"hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallClaude()
	if err != nil {
		t.Fatal(err)
	}

	wantHook := filepath.Join(claudeDir, "hooks", ClaudeHookInstallName)
	if installed.HookPath != wantHook {
		t.Fatalf("hook path = %q, want %q", installed.HookPath, wantHook)
	}
	gotAsset, err := os.ReadFile(installed.HookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotAsset) != claudeHookAsset {
		t.Fatal("hook file content does not match embedded asset")
	}
	info, err := os.Stat(installed.HookPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("hook is not executable: mode=%v", info.Mode())
	}

	settings := readSettings(t, installed.SettingsPath)
	if _, ok := settings["permissions"].(map[string]any); !ok {
		t.Fatal("permissions were not preserved")
	}
	session := settings["hooks"].(map[string]any)["SessionStart"].([]any)
	entry := session[0].(map[string]any)
	if entry["matcher"] != sessionStartMatcher {
		t.Fatalf("matcher = %v, want %s", entry["matcher"], sessionStartMatcher)
	}
	cmd := entry["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.Contains(cmd, " session") {
		t.Fatalf("command %q missing session action", cmd)
	}
	for _, event := range []string{
		"UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse",
		"PostToolUseFailure", "SubagentStop", "Stop", "SessionEnd",
	} {
		if _, ok := settings["hooks"].(map[string]any)[event]; ok {
			t.Fatalf("unexpected hook event %s after install", event)
		}
	}
}

func TestInstallClaudeUsesClaudeConfigDirEnv(t *testing.T) {
	base := t.TempDir()
	claudeDir := filepath.Join(base, "custom-claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ClaudeConfigDirEnv, claudeDir)

	installed, err := InstallClaude()
	if err != nil {
		t.Fatal(err)
	}
	if installed.SettingsPath != filepath.Join(claudeDir, "settings.json") {
		t.Fatalf("settings = %q", installed.SettingsPath)
	}
	if installed.HookPath != filepath.Join(claudeDir, "hooks", ClaudeHookInstallName) {
		t.Fatalf("hook = %q", installed.HookPath)
	}
}

func TestInstallClaudeIsIdempotentForHookEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(ClaudeConfigDirEnv, "")
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallClaude(); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallClaude(); err != nil {
		t.Fatal(err)
	}

	settings := readSettings(t, filepath.Join(claudeDir, "settings.json"))
	session := settings["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(session) != 1 {
		t.Fatalf("SessionStart len = %d, want 1", len(session))
	}
}

func TestInstallClaudeRemovesDeprecatedHooksAndPreservesUserHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(ClaudeConfigDirEnv, "")
	claudeDir := filepath.Join(home, ".claude")
	hooksDir := filepath.Join(claudeDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, ClaudeHookInstallName)
	herdrHook := filepath.Join(hooksDir, legacyHerdrHookInstallName)

	settings := map[string]any{
		"hooks": map[string]any{
			"PostToolUse": []any{map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{"type": "command", "command": HookCommand(hookPath, "working"), "timeout": 10},
					map[string]any{"type": "command", "command": "echo keep-post", "timeout": 10},
				},
			}},
			"PostToolUseFailure": []any{map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{"type": "command", "command": HookCommand(herdrHook, "working"), "timeout": 10},
					map[string]any{"type": "command", "command": "echo keep-failure", "timeout": 10},
				},
			}},
			"SubagentStop": []any{map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{"type": "command", "command": HookCommand(hookPath, "working"), "timeout": 10},
					map[string]any{"type": "command", "command": "echo keep-subagent", "timeout": 10},
				},
			}},
			"SessionEnd": []any{map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{"type": "command", "command": HookCommand(hookPath, "release"), "timeout": 10},
					map[string]any{"type": "command", "command": "echo keep-session-end", "timeout": 10},
				},
			}},
		},
	}
	writeSettings(t, filepath.Join(claudeDir, "settings.json"), settings)

	if _, err := InstallClaude(); err != nil {
		t.Fatal(err)
	}

	got := readSettings(t, filepath.Join(claudeDir, "settings.json"))
	hooks := got["hooks"].(map[string]any)
	assertCommand(t, hooks, "PostToolUse", "echo keep-post")
	assertCommand(t, hooks, "PostToolUseFailure", "echo keep-failure")
	assertCommand(t, hooks, "SubagentStop", "echo keep-subagent")
	assertCommand(t, hooks, "SessionEnd", "echo keep-session-end")
	for _, event := range []string{"UserPromptSubmit", "PreToolUse", "Stop"} {
		if _, ok := hooks[event]; ok {
			t.Fatalf("unexpected event %s", event)
		}
	}
}

func TestUninstallClaudeRemovesHooksAndPreservesOthers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(ClaudeConfigDirEnv, "")
	claudeDir := filepath.Join(home, ".claude")
	hooksDir := filepath.Join(claudeDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, ClaudeHookInstallName)
	if err := os.WriteFile(hookPath, []byte(claudeHookAsset), 0o755); err != nil {
		t.Fatal(err)
	}

	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{"type": "command", "command": HookCommand(hookPath, "idle"), "timeout": 10},
				},
			}},
			"UserPromptSubmit": []any{map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{"type": "command", "command": HookCommand(hookPath, "working"), "timeout": 10},
					map[string]any{"type": "command", "command": "echo keep", "timeout": 10},
				},
			}},
			"PermissionRequest": []any{map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{"type": "command", "command": HookCommand(hookPath, "blocked"), "timeout": 10},
				},
			}},
			"PostToolUse": []any{map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{"type": "command", "command": HookCommand(hookPath, "working"), "timeout": 10},
				},
			}},
			"PostToolUseFailure": []any{map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{"type": "command", "command": HookCommand(hookPath, "working"), "timeout": 10},
				},
			}},
			"SubagentStop": []any{map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{"type": "command", "command": HookCommand(hookPath, "working"), "timeout": 10},
				},
			}},
			"Stop": []any{map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{"type": "command", "command": HookCommand(hookPath, "idle"), "timeout": 10},
				},
			}},
			"SessionEnd": []any{map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{"type": "command", "command": HookCommand(hookPath, "release"), "timeout": 10},
				},
			}},
		},
	}
	writeSettings(t, filepath.Join(claudeDir, "settings.json"), settings)

	result, err := UninstallClaude()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedHookFile {
		t.Fatal("expected hook file removed")
	}
	if !result.UpdatedSettings {
		t.Fatal("expected settings updated")
	}
	if _, err := os.Stat(result.HookPath); !os.IsNotExist(err) {
		t.Fatalf("hook still exists: %v", err)
	}

	got := readSettings(t, filepath.Join(claudeDir, "settings.json"))
	hooks := got["hooks"].(map[string]any)
	ups := hooks["UserPromptSubmit"].([]any)[0].(map[string]any)["hooks"].([]any)
	if len(ups) != 1 {
		t.Fatalf("UserPromptSubmit hooks len = %d", len(ups))
	}
	if ups[0].(map[string]any)["command"] != "echo keep" {
		t.Fatalf("user hook lost: %v", ups[0])
	}
	for _, event := range []string{
		"PermissionRequest", "SessionStart", "PostToolUse", "PostToolUseFailure",
		"SubagentStop", "Stop", "SessionEnd",
	} {
		if _, ok := hooks[event]; ok {
			t.Fatalf("event %s should be gone", event)
		}
	}
}

func TestInstallClaudeErrorsWhenClaudeDirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(ClaudeConfigDirEnv, "")

	_, err := InstallClaude()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "claude directory not found") {
		t.Fatalf("error = %v", err)
	}
}

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	return settings
}

func writeSettings(t *testing.T, path string, settings map[string]any) {
	t.Helper()
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertCommand(t *testing.T, hooks map[string]any, event, want string) {
	t.Helper()
	entries := hooks[event].([]any)
	cmd := entries[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"]
	if cmd != want {
		t.Fatalf("%s command = %v, want %s", event, cmd, want)
	}
}
