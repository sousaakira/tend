package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallKiloWritesPluginToPluginDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	kiloDir := filepath.Join(home, ".config", "kilo")
	if err := os.MkdirAll(kiloDir, 0o755); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallKilo()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(kiloDir, "plugin", KiloPluginInstallName)
	if installed.PluginPath != want {
		t.Fatalf("path = %q, want %q", installed.PluginPath, want)
	}
	got, err := os.ReadFile(installed.PluginPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != kiloPluginAsset {
		t.Fatal("asset mismatch")
	}
}

func TestUninstallKiloRemovesPluginWhenPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	pluginDir := filepath.Join(home, ".config", "kilo", "plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, KiloPluginInstallName), []byte(kiloPluginAsset), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := UninstallKilo()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedPlugin {
		t.Fatal("expected removed")
	}
	if _, err := os.Stat(result.PluginPath); !os.IsNotExist(err) {
		t.Fatal("still present")
	}
}

func TestInstallKiloErrorsWhenConfigDirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := InstallKilo()
	if err == nil || !strings.Contains(err.Error(), "kilo config directory not found") {
		t.Fatalf("err = %v", err)
	}
}
