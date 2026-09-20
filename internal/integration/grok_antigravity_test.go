package integration

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInstallGrokWritesHookAndConfig(t *testing.T) {
	base := t.TempDir()
	grokDir := filepath.Join(base, ".grok")
	if err := os.MkdirAll(grokDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(GrokConfigDirEnv, grokDir)

	installed, err := InstallGrok()
	if err != nil {
		t.Fatal(err)
	}
	hooksDir := filepath.Join(grokDir, "hooks")
	if installed.HookPath != filepath.Join(hooksDir, GrokHookInstallName) {
		t.Fatalf("hook = %q", installed.HookPath)
	}
	if installed.ConfigPath != filepath.Join(hooksDir, GrokHookConfigInstallName) {
		t.Fatalf("config = %q", installed.ConfigPath)
	}
	if string(mustReadBytes(t, installed.HookPath)) != grokHookAsset {
		t.Fatal("hook asset mismatch")
	}
	got := readSettings(t, installed.ConfigPath)
	want := GrokHookConfig(installed.HookPath)
	// JSON round-trip normalizes numbers to float64.
	wantBytes, _ := MarshalJSONPretty(want)
	gotBytes, _ := MarshalJSONPretty(got)
	if string(wantBytes) != string(gotBytes) {
		t.Fatalf("config mismatch:\ngot  %s\nwant %s", gotBytes, wantBytes)
	}
	cmd := got["hooks"].(map[string]any)["SessionStart"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.HasPrefix(cmd, "sh ") || !strings.HasSuffix(cmd, " session") {
		t.Fatalf("grok command should use sh: %q", cmd)
	}
}

func TestInstallGrokUsesEnvAndIsIdempotent(t *testing.T) {
	base := t.TempDir()
	grokDir := filepath.Join(base, "custom-grok")
	if err := os.MkdirAll(grokDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(GrokConfigDirEnv, grokDir)
	t.Setenv(GrokHomeEnv, "")

	if _, err := InstallGrok(); err != nil {
		t.Fatal(err)
	}
	first := mustRead(t, filepath.Join(grokDir, "hooks", GrokHookConfigInstallName))
	if _, err := InstallGrok(); err != nil {
		t.Fatal(err)
	}
	second := mustRead(t, filepath.Join(grokDir, "hooks", GrokHookConfigInstallName))
	if first != second {
		t.Fatal("idempotent install changed config")
	}
}

func TestInstallGrokHonorsGrokHome(t *testing.T) {
	base := t.TempDir()
	homeDir := filepath.Join(base, "grok-home")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(GrokConfigDirEnv, "")
	t.Setenv(GrokHomeEnv, homeDir)

	installed, err := InstallGrok()
	if err != nil {
		t.Fatal(err)
	}
	if installed.HookPath != filepath.Join(homeDir, "hooks", GrokHookInstallName) {
		t.Fatalf("hook = %q", installed.HookPath)
	}

	seam := filepath.Join(base, "seam")
	if err := os.MkdirAll(seam, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(GrokConfigDirEnv, seam)
	installed, err = InstallGrok()
	if err != nil {
		t.Fatal(err)
	}
	if installed.HookPath != filepath.Join(seam, "hooks", GrokHookInstallName) {
		t.Fatalf("seam hook = %q", installed.HookPath)
	}
}

func TestUninstallGrokRemovesFiles(t *testing.T) {
	base := t.TempDir()
	grokDir := filepath.Join(base, ".grok")
	if err := os.MkdirAll(grokDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(GrokConfigDirEnv, grokDir)
	if _, err := InstallGrok(); err != nil {
		t.Fatal(err)
	}
	result, err := UninstallGrok()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedHookFile || !result.RemovedConfigFile {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(result.HookPath); !os.IsNotExist(err) {
		t.Fatal("hook still exists")
	}
	again, err := UninstallGrok()
	if err != nil {
		t.Fatal(err)
	}
	if again.RemovedHookFile || again.RemovedConfigFile {
		t.Fatalf("second uninstall = %+v", again)
	}
}

func TestInstallGrokErrorsWhenConfigDirMissing(t *testing.T) {
	base := t.TempDir()
	t.Setenv(GrokConfigDirEnv, filepath.Join(base, "missing"))
	_, err := InstallGrok()
	if err == nil || !strings.Contains(err.Error(), "grok config directory not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestInstallAntigravityCLIWritesNamedBlock(t *testing.T) {
	base := t.TempDir()
	agyDir := filepath.Join(base, ".gemini", "config")
	if err := os.MkdirAll(agyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agyDir, "hooks.json"), []byte(`{"lint-checker":{"PreInvocation":[{"type":"command","command":"echo keep-me"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(AntigravityCLIConfigDirEnv, agyDir)

	installed, err := InstallAntigravityCLI()
	if err != nil {
		t.Fatal(err)
	}
	if installed.HookPath != filepath.Join(agyDir, "hooks", AntigravityCLIHookInstallName) {
		t.Fatalf("hook = %q", installed.HookPath)
	}
	if string(mustReadBytes(t, installed.HookPath)) != antigravityCLIHookAsset {
		t.Fatal("hook asset mismatch")
	}

	hooks := readSettings(t, installed.HooksPath)
	block, ok := hooks[AntigravityCLIHookBlockName].(map[string]any)
	if !ok {
		t.Fatal("tend block missing")
	}
	if _, ok := hooks[legacyAntigravityCLIHookBlockName]; ok {
		t.Fatal("legacy herdr block should be gone")
	}
	for _, ev := range antigravityCLIHookEvents {
		entries := block[ev.Event].([]any)
		if len(entries) != 1 {
			t.Fatalf("%s len = %d", ev.Event, len(entries))
		}
		handler := entries[0].(map[string]any)
		if _, ok := handler["matcher"]; ok {
			t.Fatalf("%s must be flat", ev.Event)
		}
		if _, ok := handler["hooks"]; ok {
			t.Fatalf("%s must be flat", ev.Event)
		}
		if handler["command"] != HookCommand(installed.HookPath, ev.Action) {
			t.Fatalf("command = %v", handler["command"])
		}
	}
	for _, event := range []string{"PreToolUse", "PostToolUse", "PostInvocation", "Stop"} {
		if _, ok := block[event]; ok {
			t.Fatalf("%s must not be registered", event)
		}
	}
	lint := hooks["lint-checker"].(map[string]any)["PreInvocation"].([]any)[0].(map[string]any)["command"]
	if lint != "echo keep-me" {
		t.Fatal("lint-checker lost")
	}
}

func TestInstallAntigravityCLIRewritesStaleBlock(t *testing.T) {
	base := t.TempDir()
	agyDir := filepath.Join(base, ".gemini", "config")
	if err := os.MkdirAll(agyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agyDir, "hooks.json"), []byte(`{"herdr":{"Stop":[{"matcher":"*","hooks":[{"type":"command","command":"stale"}]}],"PostInvocation":[{"type":"command","command":"stale idle"}]},"tend":{"Legacy":[]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(AntigravityCLIConfigDirEnv, agyDir)

	if _, err := InstallAntigravityCLI(); err != nil {
		t.Fatal(err)
	}
	hooks := readSettings(t, filepath.Join(agyDir, "hooks.json"))
	if _, ok := hooks[legacyAntigravityCLIHookBlockName]; ok {
		t.Fatal("herdr block should be removed")
	}
	block := hooks[AntigravityCLIHookBlockName].(map[string]any)
	keys := make([]string, 0, len(block))
	for k := range block {
		keys = append(keys, k)
	}
	if !reflect.DeepEqual(keys, []string{"PreInvocation"}) && !(len(keys) == 1 && keys[0] == "PreInvocation") {
		t.Fatalf("keys = %v", keys)
	}
}

func TestUninstallAntigravityCLIRemovesBlock(t *testing.T) {
	base := t.TempDir()
	agyDir := filepath.Join(base, ".gemini", "config")
	if err := os.MkdirAll(agyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agyDir, "hooks.json"), []byte(`{"lint-checker":{"PreInvocation":[{"type":"command","command":"echo keep-me"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(AntigravityCLIConfigDirEnv, agyDir)
	if _, err := InstallAntigravityCLI(); err != nil {
		t.Fatal(err)
	}
	result, err := UninstallAntigravityCLI()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedHookFile || !result.UpdatedHooks {
		t.Fatalf("result = %+v", result)
	}
	hooks := readSettings(t, filepath.Join(agyDir, "hooks.json"))
	if _, ok := hooks[AntigravityCLIHookBlockName]; ok {
		t.Fatal("tend block still present")
	}
	if _, ok := hooks["lint-checker"]; !ok {
		t.Fatal("lint-checker lost")
	}
}

func TestInstallAntigravityCLIErrorsWhenConfigDirMissing(t *testing.T) {
	base := t.TempDir()
	agyDir := filepath.Join(base, ".gemini", "config")
	t.Setenv(AntigravityCLIConfigDirEnv, agyDir)
	_, err := InstallAntigravityCLI()
	if err == nil || !strings.Contains(err.Error(), "install antigravity cli first") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(agyDir); !os.IsNotExist(err) {
		t.Fatal("install must not create the config dir")
	}
}
