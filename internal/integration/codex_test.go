package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCodexWritesHookAndUpdatesHooksAndConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(CodexHomeEnv, "")

	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("model = \"gpt-5.4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallCodex()
	if err != nil {
		t.Fatal(err)
	}

	wantHook := filepath.Join(codexDir, CodexHookInstallName)
	if installed.HookPath != wantHook {
		t.Fatalf("hook path = %q, want %q", installed.HookPath, wantHook)
	}
	gotAsset, err := os.ReadFile(installed.HookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotAsset) != codexHookAsset {
		t.Fatal("hook file content does not match embedded asset")
	}

	hooks := readSettings(t, installed.HooksPath)
	cmd := hooks["hooks"].(map[string]any)["SessionStart"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.Contains(cmd, " session") {
		t.Fatalf("command %q missing session action", cmd)
	}
	for _, event := range []string{"UserPromptSubmit", "PreToolUse", "PermissionRequest", "Stop"} {
		if _, ok := hooks["hooks"].(map[string]any)[event]; ok {
			t.Fatalf("unexpected hook event %s after install", event)
		}
	}

	config, err := os.ReadFile(installed.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(config)
	if !strings.Contains(got, "model = \"gpt-5.4\"") {
		t.Fatal("model line was not preserved")
	}
	if !strings.Contains(got, "[features]") || !strings.Contains(got, "hooks = true") {
		t.Fatalf("features hooks flag missing: %q", got)
	}
	if strings.Contains(got, "codex_hooks") {
		t.Fatal("deprecated codex_hooks should be absent")
	}
}

func TestInstallCodexUsesCodexHomeEnv(t *testing.T) {
	base := t.TempDir()
	codexDir := filepath.Join(base, "custom-codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("model = \"gpt-5.4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(CodexHomeEnv, codexDir)

	installed, err := InstallCodex()
	if err != nil {
		t.Fatal(err)
	}
	if installed.HookPath != filepath.Join(codexDir, CodexHookInstallName) {
		t.Fatalf("hook = %q", installed.HookPath)
	}
	if installed.HooksPath != filepath.Join(codexDir, "hooks.json") {
		t.Fatalf("hooks = %q", installed.HooksPath)
	}
	if installed.ConfigPath != filepath.Join(codexDir, "config.toml") {
		t.Fatalf("config = %q", installed.ConfigPath)
	}
}

func TestInstallCodexIsIdempotentForHookEntriesAndFeatureFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(CodexHomeEnv, "")
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("[features]\ncodex_hooks = false\nother = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallCodex(); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallCodex(); err != nil {
		t.Fatal(err)
	}

	hooks := readSettings(t, filepath.Join(codexDir, "hooks.json"))
	session := hooks["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(session) != 1 {
		t.Fatalf("SessionStart len = %d, want 1", len(session))
	}
	config, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(config)
	if strings.Count(got, "hooks = true") != 1 {
		t.Fatalf("hooks = true count = %d", strings.Count(got, "hooks = true"))
	}
	if strings.Contains(got, "codex_hooks") {
		t.Fatal("codex_hooks should be gone")
	}
	if !strings.Contains(got, "other = true") {
		t.Fatal("other = true was not preserved")
	}
}

func TestInstallCodexOnlyMigratesTopLevelFeatureFlags(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(CodexHomeEnv, "")
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	input := "profile = \"work\"\n\n[profiles.work.features]\nhooks = false\ncodex_hooks = false\n\n[features]\ncodex_hooks = true\nother = true\n"
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallCodex(); err != nil {
		t.Fatal(err)
	}

	config, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(config)
	if !strings.Contains(got, "[profiles.work.features]\nhooks = false\ncodex_hooks = false") {
		t.Fatalf("profile features should be untouched: %q", got)
	}
	if !strings.Contains(got, "[features]\nhooks = true\nother = true") {
		t.Fatalf("top-level features not migrated: %q", got)
	}
}

func TestUninstallCodexRemovesHooksAndLeavesConfigAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(CodexHomeEnv, "")
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(codexDir, CodexHookInstallName)
	if err := os.WriteFile(hookPath, []byte(codexHookAsset), 0o755); err != nil {
		t.Fatal(err)
	}

	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": HookCommand(hookPath, "idle"), "timeout": 10}},
			}},
			"UserPromptSubmit": []any{map[string]any{
				"hooks": []any{
					map[string]any{"type": "command", "command": HookCommand(hookPath, "working"), "timeout": 10},
					map[string]any{"type": "command", "command": "echo keep", "timeout": 10},
				},
			}},
			"PreToolUse": []any{map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": HookCommand(hookPath, "working"), "timeout": 10}},
			}},
			"PermissionRequest": []any{map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": HookCommand(hookPath, "blocked"), "timeout": 10}},
			}},
			"Stop": []any{map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": HookCommand(hookPath, "idle"), "timeout": 10}},
			}},
		},
	}
	writeSettings(t, filepath.Join(codexDir, "hooks.json"), settings)
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("[features]\nhooks = true\nother = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := UninstallCodex()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedHookFile || !result.UpdatedHooks {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(result.HookPath); !os.IsNotExist(err) {
		t.Fatalf("hook still exists: %v", err)
	}

	hooks := readSettings(t, filepath.Join(codexDir, "hooks.json"))["hooks"].(map[string]any)
	ups := hooks["UserPromptSubmit"].([]any)[0].(map[string]any)["hooks"].([]any)
	if len(ups) != 1 || ups[0].(map[string]any)["command"] != "echo keep" {
		t.Fatalf("user hook lost: %#v", ups)
	}
	for _, event := range []string{"SessionStart", "PreToolUse", "PermissionRequest", "Stop"} {
		if _, ok := hooks[event]; ok {
			t.Fatalf("event %s should be gone", event)
		}
	}
	config, _ := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if !strings.Contains(string(config), "hooks = true") || !strings.Contains(string(config), "other = true") {
		t.Fatalf("config should be untouched: %q", config)
	}
}

func TestInstallCodexErrorsWhenConfigDirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(CodexHomeEnv, "")

	_, err := InstallCodex()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "codex config directory not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildCodexConfigWithHooksEmptyContent(t *testing.T) {
	got := BuildCodexConfigWithHooks("")
	if got != "[features]\nhooks = true\n" {
		t.Fatalf("got %q", got)
	}
}
