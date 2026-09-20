package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallOmpWritesEmbeddedAssetToOmpExtensionsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(PiCodingAgentDirEnv, "")
	t.Setenv(OmpConfigDirEnv, "")
	extDir := filepath.Join(home, ".omp", "agent", "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallOmp()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(extDir, OmpExtensionInstallName)
	if installed.ExtensionPath != want {
		t.Fatalf("path = %q, want %q", installed.ExtensionPath, want)
	}
	if installed.RemovedLegacyPiExtension {
		t.Fatal("unexpected legacy removal")
	}
	got, err := os.ReadFile(installed.ExtensionPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != ompExtensionAsset {
		t.Fatal("extension content does not match embedded asset")
	}
}

func TestInstallOmpRemovesLegacyPiIntegrationFromOmpExtensionsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(PiCodingAgentDirEnv, "")
	t.Setenv(OmpConfigDirEnv, "")
	extDir := filepath.Join(home, ".omp", "agent", "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(extDir, PiExtensionInstallName)
	if err := os.WriteFile(legacyPath, []byte(piExtensionAsset), 0o644); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallOmp()
	if err != nil {
		t.Fatal(err)
	}
	if !installed.RemovedLegacyPiExtension {
		t.Fatal("expected legacy pi removal")
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatal("legacy pi extension still present")
	}
	if installed.ExtensionPath != filepath.Join(extDir, OmpExtensionInstallName) {
		t.Fatalf("path = %q", installed.ExtensionPath)
	}
}

func TestInstallOmpPreservesNonTendFileWithPiInstallName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(PiCodingAgentDirEnv, "")
	t.Setenv(OmpConfigDirEnv, "")
	extDir := filepath.Join(home, ".omp", "agent", "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(extDir, PiExtensionInstallName)
	if err := os.WriteFile(userPath, []byte("// user extension\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallOmp()
	if err != nil {
		t.Fatal(err)
	}
	if installed.RemovedLegacyPiExtension {
		t.Fatal("user file should be preserved")
	}
	got, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "// user extension\n" {
		t.Fatalf("got %q", got)
	}
}

func TestInstallOmpUsesPiConfigDirEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(PiCodingAgentDirEnv, "")
	extDir := filepath.Join(home, "custom-omp", "agent", "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(OmpConfigDirEnv, "custom-omp")

	installed, err := InstallOmp()
	if err != nil {
		t.Fatal(err)
	}
	if installed.ExtensionPath != filepath.Join(extDir, OmpExtensionInstallName) {
		t.Fatalf("path = %q", installed.ExtensionPath)
	}
}

func TestInstallOmpRefusesSharedPiExtensionDirectory(t *testing.T) {
	base := t.TempDir()
	agentDir := filepath.Join(base, "shared-agent")
	extDir := filepath.Join(agentDir, "extensions")
	piExtension := filepath.Join(extDir, PiExtensionInstallName)
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(piExtension, []byte(piExtensionAsset), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(PiCodingAgentDirEnv, agentDir)
	t.Setenv(OmpConfigDirEnv, "ignored-omp-config")

	_, err := InstallOmp()
	if err == nil || !strings.Contains(err.Error(), "Pi and OMP resolve to the same extension directory") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), extDir) {
		t.Fatalf("err missing path: %v", err)
	}
	if _, err := os.Stat(piExtension); err != nil {
		t.Fatal("pi extension should remain")
	}
	if _, err := os.Stat(filepath.Join(extDir, OmpExtensionInstallName)); !os.IsNotExist(err) {
		t.Fatal("omp extension should not be written")
	}
}

func TestInstallOmpCreatesExtensionsDirWhenAgentDirExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(PiCodingAgentDirEnv, "")
	t.Setenv(OmpConfigDirEnv, "")
	agentDir := filepath.Join(home, ".omp", "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallOmp()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(agentDir, "extensions", OmpExtensionInstallName)
	if installed.ExtensionPath != want {
		t.Fatalf("path = %q, want %q", installed.ExtensionPath, want)
	}
}

func TestUninstallOmpRemovesEmbeddedExtensionWhenPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(PiCodingAgentDirEnv, "")
	t.Setenv(OmpConfigDirEnv, "")
	extDir := filepath.Join(home, ".omp", "agent", "extensions")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDir, OmpExtensionInstallName), []byte(ompExtensionAsset), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := UninstallOmp()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedExtension {
		t.Fatal("expected removed")
	}
	if _, err := os.Stat(result.ExtensionPath); !os.IsNotExist(err) {
		t.Fatal("still present")
	}
}

func TestInstallOmpErrorsWhenExtensionDirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(PiCodingAgentDirEnv, "")
	t.Setenv(OmpConfigDirEnv, "")

	_, err := InstallOmp()
	if err == nil || !strings.Contains(err.Error(), "omp extension directory not found") {
		t.Fatalf("err = %v", err)
	}
}
