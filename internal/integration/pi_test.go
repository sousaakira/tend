package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPiWritesEmbeddedAssetToPiExtensionsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(PiCodingAgentDirEnv, "")
	extDir := filepath.Join(home, ".pi", "agent", "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}

	path, err := InstallPi()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(extDir, PiExtensionInstallName)
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != piExtensionAsset {
		t.Fatal("extension content does not match embedded asset")
	}
}

func TestInstallPiCreatesExtensionsDirWhenAgentDirExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(PiCodingAgentDirEnv, "")
	agentDir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	path, err := InstallPi()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(agentDir, "extensions", PiExtensionInstallName)
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestInstallPiUsesPiCodingAgentDirEnv(t *testing.T) {
	base := t.TempDir()
	agentDir := filepath.Join(base, "custom-pi-agent")
	extDir := filepath.Join(agentDir, "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(PiCodingAgentDirEnv, agentDir)

	path, err := InstallPi()
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(extDir, PiExtensionInstallName) {
		t.Fatalf("path = %q", path)
	}
}

func TestInstallPiExpandsTildeInPiCodingAgentDirEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	extDir := filepath.Join(home, "custom-pi-agent", "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(PiCodingAgentDirEnv, "~/custom-pi-agent")

	path, err := InstallPi()
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(extDir, PiExtensionInstallName) {
		t.Fatalf("path = %q", path)
	}
}

func TestInstallPiErrorsWhenExtensionDirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(PiCodingAgentDirEnv, "")

	_, err := InstallPi()
	if err == nil || !strings.Contains(err.Error(), "pi extension directory not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestUninstallPiRemovesEmbeddedExtensionWhenPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(PiCodingAgentDirEnv, "")
	extDir := filepath.Join(home, ".pi", "agent", "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDir, PiExtensionInstallName), []byte(piExtensionAsset), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := UninstallPi()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedExtension {
		t.Fatal("expected removed extension")
	}
	if _, err := os.Stat(result.ExtensionPath); !os.IsNotExist(err) {
		t.Fatal("extension still present")
	}
}
