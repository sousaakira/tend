package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCursorWritesHookAndUpdatesHooksJSON(t *testing.T) {
	cursorDir := filepath.Join(t.TempDir(), ".cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cursorDir, "hooks.json"), []byte(`{"version":1,"hooks":{"stop":[{"command":"echo keep-me"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(CursorConfigDirEnv, cursorDir)

	installed, err := InstallCursor()
	if err != nil {
		t.Fatal(err)
	}
	if installed.HookPath != filepath.Join(cursorDir, CursorHookInstallName) {
		t.Fatalf("hook = %q", installed.HookPath)
	}
	gotAsset, err := os.ReadFile(installed.HookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotAsset) != cursorHookAsset {
		t.Fatal("hook content mismatch")
	}

	hooksFile := readSettings(t, installed.HooksPath)
	hooks := hooksFile["hooks"].(map[string]any)
	session := hooks["sessionStart"].([]any)
	if len(session) != 1 {
		t.Fatalf("sessionStart len = %d", len(session))
	}
	want := HookCommand(installed.HookPath, "session")
	if session[0].(map[string]any)["command"] != want {
		t.Fatalf("command = %v, want %q", session[0], want)
	}
	if _, ok := hooks["beforeSubmitPrompt"]; ok {
		t.Fatal("beforeSubmitPrompt should be absent")
	}
	stop := hooks["stop"].([]any)
	if len(stop) != 1 || stop[0].(map[string]any)["command"] != "echo keep-me" {
		t.Fatalf("stop hook lost: %#v", stop)
	}
}

func TestInstallCursorIsIdempotentForHookEntries(t *testing.T) {
	cursorDir := filepath.Join(t.TempDir(), ".cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(CursorConfigDirEnv, cursorDir)

	if _, err := InstallCursor(); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallCursor(); err != nil {
		t.Fatal(err)
	}

	hooks := readSettings(t, filepath.Join(cursorDir, "hooks.json"))["hooks"].(map[string]any)
	if len(hooks["sessionStart"].([]any)) != 1 {
		t.Fatalf("sessionStart not idempotent: %#v", hooks["sessionStart"])
	}
}

func TestUninstallCursorRemovesHooksAndPreservesOthers(t *testing.T) {
	cursorDir := filepath.Join(t.TempDir(), ".cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(CursorConfigDirEnv, cursorDir)

	if _, err := InstallCursor(); err != nil {
		t.Fatal(err)
	}
	hooksFile := readSettings(t, filepath.Join(cursorDir, "hooks.json"))
	hooksFile["hooks"].(map[string]any)["beforeSubmitPrompt"] = []any{
		map[string]any{"command": "echo user-defined"},
	}
	writeSettings(t, filepath.Join(cursorDir, "hooks.json"), hooksFile)

	result, err := UninstallCursor()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedHookFile || !result.UpdatedHooks {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(cursorDir, CursorHookInstallName)); !os.IsNotExist(err) {
		t.Fatal("hook file still present")
	}

	hooks := readSettings(t, filepath.Join(cursorDir, "hooks.json"))["hooks"].(map[string]any)
	if _, ok := hooks["sessionStart"]; ok {
		t.Fatal("sessionStart should be gone")
	}
	if _, ok := hooks["beforeSubmitPrompt"]; !ok {
		t.Fatal("user beforeSubmitPrompt should remain")
	}
}

func TestInstallCursorUsesCursorConfigDirEnv(t *testing.T) {
	cursorDir := filepath.Join(t.TempDir(), "custom-cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(CursorConfigDirEnv, cursorDir)

	installed, err := InstallCursor()
	if err != nil {
		t.Fatal(err)
	}
	if installed.HookPath != filepath.Join(cursorDir, CursorHookInstallName) {
		t.Fatalf("hook = %q", installed.HookPath)
	}
	if installed.HooksPath != filepath.Join(cursorDir, "hooks.json") {
		t.Fatalf("hooks = %q", installed.HooksPath)
	}
}

func TestInstallCursorErrorsWhenConfigDirMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), ".cursor")
	t.Setenv(CursorConfigDirEnv, missing)

	_, err := InstallCursor()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cursor config directory not found") {
		t.Fatalf("error = %v", err)
	}
}
