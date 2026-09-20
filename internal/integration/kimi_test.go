package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallKimiWritesHookAndUpdatesConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(KimiCodeHomeEnv, "")

	kimiDir := filepath.Join(home, ".kimi-code")
	if err := os.MkdirAll(kimiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "default_model = \"moonshot\"\n\n[[hooks]]\nevent = \"Notification\"\nmatcher = \"task.completed\"\ncommand = \"echo keep\"\ntimeout = 3\n"
	if err := os.WriteFile(filepath.Join(kimiDir, "config.toml"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallKimi()
	if err != nil {
		t.Fatal(err)
	}
	wantHook := filepath.Join(kimiDir, "hooks", KimiHookInstallName)
	if installed.HookPath != wantHook {
		t.Fatalf("hook = %q, want %q", installed.HookPath, wantHook)
	}
	got, err := os.ReadFile(installed.HookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != kimiHookAsset {
		t.Fatal("hook content mismatch")
	}
	config, err := os.ReadFile(installed.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(config)
	if !strings.Contains(s, "default_model = \"moonshot\"") || !strings.Contains(s, `command = "echo keep"`) {
		t.Fatal("user config lost")
	}
	if !strings.Contains(s, KimiConfigBlockBegin) || !strings.Contains(s, KimiConfigBlockEnd) {
		t.Fatal("tend kimi block missing")
	}
	for _, ev := range kimiHookEvents {
		if !strings.Contains(s, "event = "+tomlBasicString(ev.Event)) {
			t.Fatalf("missing event %s", ev.Event)
		}
	}
}

func TestInstallKimiUsesKimiCodeHomeEnv(t *testing.T) {
	base := t.TempDir()
	kimiDir := filepath.Join(base, "custom-kimi")
	if err := os.MkdirAll(kimiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(KimiCodeHomeEnv, kimiDir)

	installed, err := InstallKimi()
	if err != nil {
		t.Fatal(err)
	}
	if installed.HookPath != filepath.Join(kimiDir, "hooks", KimiHookInstallName) {
		t.Fatalf("hook = %q", installed.HookPath)
	}
	if installed.ConfigPath != filepath.Join(kimiDir, "config.toml") {
		t.Fatalf("config = %q", installed.ConfigPath)
	}
}

func TestInstallKimiIsIdempotentForConfigBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(KimiCodeHomeEnv, "")
	if err := os.MkdirAll(filepath.Join(home, ".kimi-code"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallKimi(); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallKimi(); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(home, ".kimi-code", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(config)
	if strings.Count(s, KimiConfigBlockBegin) != 1 || strings.Count(s, KimiConfigBlockEnd) != 1 {
		t.Fatalf("expected one block, got begin=%d end=%d", strings.Count(s, KimiConfigBlockBegin), strings.Count(s, KimiConfigBlockEnd))
	}
}

func TestUninstallKimiRemovesHookAndConfigBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(KimiCodeHomeEnv, "")
	kimiDir := filepath.Join(home, ".kimi-code")
	if err := os.MkdirAll(kimiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	installed, err := InstallKimi()
	if err != nil {
		t.Fatal(err)
	}
	merged := "default_model = \"moonshot\"\n\n[[hooks]]\nevent = \"Notification\"\ncommand = \"echo keep\"\n\n" + mustRead(t, installed.ConfigPath)
	if err := os.WriteFile(installed.ConfigPath, []byte(merged), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := UninstallKimi()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedHookFile || !result.UpdatedConfig {
		t.Fatalf("result = %+v", result)
	}
	config := mustRead(t, filepath.Join(kimiDir, "config.toml"))
	if strings.Contains(config, KimiConfigBlockBegin) || strings.Contains(config, KimiConfigBlockEnd) {
		t.Fatal("tend block still present")
	}
	if !strings.Contains(config, "echo keep") || !strings.Contains(config, "moonshot") {
		t.Fatal("user config lost")
	}
}

func TestInstallKimiErrorsWhenConfigDirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(KimiCodeHomeEnv, "")
	_, err := InstallKimi()
	if err == nil || !strings.Contains(err.Error(), "kimi code config directory not found") {
		t.Fatalf("error = %v", err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
