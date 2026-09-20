package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallHermesWritesPluginAndEnablesIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(HermesHomeEnv, "")
	hermesDir := filepath.Join(home, ".hermes")
	if err := os.MkdirAll(hermesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hermesDir, "config.yaml"), []byte("model:\n  provider: auto\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallHermes()
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(hermesDir, "plugins", HermesPluginInstallName)
	if installed.PluginDir != wantDir {
		t.Fatalf("plugin dir = %q", installed.PluginDir)
	}
	manifest, err := os.ReadFile(filepath.Join(installed.PluginDir, HermesPluginManifestInstallName))
	if err != nil {
		t.Fatal(err)
	}
	if string(manifest) != hermesPluginManifestAsset {
		t.Fatal("manifest mismatch")
	}
	init, err := os.ReadFile(filepath.Join(installed.PluginDir, HermesPluginInitInstallName))
	if err != nil {
		t.Fatal(err)
	}
	if string(init) != hermesPluginInitAsset {
		t.Fatal("init mismatch")
	}
	config, err := os.ReadFile(installed.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "plugins:\n  enabled:\n    - " + HermesPluginInstallName
	if !strings.Contains(string(config), want) {
		t.Fatalf("config = %q", config)
	}
}

func TestInstallHermesIsIdempotentForEnabledEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(HermesHomeEnv, "")
	hermesDir := filepath.Join(home, ".hermes")
	if err := os.MkdirAll(hermesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initial := "plugins:\n  enabled:\n    - " + HermesPluginInstallName + "\n"
	if err := os.WriteFile(filepath.Join(hermesDir, "config.yaml"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallHermes(); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallHermes(); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(hermesDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(config), HermesPluginInstallName) != 1 {
		t.Fatalf("config = %q", config)
	}
}

func TestInstallHermesPreservesFlatPluginList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(HermesHomeEnv, "")
	hermesDir := filepath.Join(home, ".hermes")
	if err := os.MkdirAll(hermesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hermesDir, "config.yaml"), []byte("plugins:\n  - platforms/discord\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallHermes(); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(hermesDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "plugins:\n  - " + HermesPluginInstallName + "\n  - platforms/discord\n"
	if string(config) != want {
		t.Fatalf("got %q, want %q", config, want)
	}
}

func TestInstallHermesConvertsFlowPluginListToBlockList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(HermesHomeEnv, "")
	hermesDir := filepath.Join(home, ".hermes")
	if err := os.MkdirAll(hermesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hermesDir, "config.yaml"), []byte("plugins: [platforms/discord]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallHermes(); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(hermesDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "plugins:\n  - " + HermesPluginInstallName + "\n  - platforms/discord\n"
	if string(config) != want {
		t.Fatalf("got %q, want %q", config, want)
	}
}

func TestUpdateHermesEnabledPluginConvertsInlineEnabledList(t *testing.T) {
	config := updateHermesEnabledPlugin("plugins:\n  enabled: [example-plugin]\n", true)
	want := "plugins:\n  enabled:\n    - " + HermesPluginInstallName + "\n    - example-plugin\n"
	if config != want {
		t.Fatalf("got %q, want %q", config, want)
	}
}

func TestUpdateHermesEnabledPluginPreservesQuotedInlineEnabledItems(t *testing.T) {
	config := updateHermesEnabledPlugin("plugins:\n  enabled: [\"null\", 'foo: bar']\n", true)
	want := "plugins:\n  enabled:\n    - " + HermesPluginInstallName + "\n    - \"null\"\n    - 'foo: bar'\n"
	if config != want {
		t.Fatalf("got %q, want %q", config, want)
	}
}

func TestUpdateHermesEnabledPluginPreservesInlineEnabledComment(t *testing.T) {
	config := updateHermesEnabledPlugin(
		"plugins:\n  enabled: [example-plugin] # managed locally\n",
		true,
	)
	want := "plugins:\n  enabled: # managed locally\n    - " + HermesPluginInstallName + "\n    - example-plugin\n"
	if config != want {
		t.Fatalf("got %q, want %q", config, want)
	}
}

func TestUninstallHermesRemovesPluginAndEnabledEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(HermesHomeEnv, "")
	hermesDir := filepath.Join(home, ".hermes")
	pluginDir := filepath.Join(hermesDir, "plugins", HermesPluginInstallName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, HermesPluginInitInstallName), []byte(hermesPluginInitAsset), 0o644); err != nil {
		t.Fatal(err)
	}
	configBody := "plugins:\n  enabled:\n    - " + HermesPluginInstallName + "\n    - other\n"
	if err := os.WriteFile(filepath.Join(hermesDir, "config.yaml"), []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := UninstallHermes()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedPluginDir || !result.UpdatedConfig {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(pluginDir); !os.IsNotExist(err) {
		t.Fatal("plugin dir still present")
	}
	config, err := os.ReadFile(filepath.Join(hermesDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), HermesPluginInstallName) {
		t.Fatalf("config still has plugin: %q", config)
	}
	if !strings.Contains(string(config), "other") {
		t.Fatalf("other plugin lost: %q", config)
	}
}

func TestInstallHermesErrorsWhenConfigDirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(HermesHomeEnv, "")

	_, err := InstallHermes()
	if err == nil || !strings.Contains(err.Error(), "hermes config directory not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpencodeAndKiloAssetsExportTendAgentStatePlugin(t *testing.T) {
	if !strings.Contains(opencodePluginAsset, "export const TendAgentStatePlugin") {
		t.Fatal("opencode asset missing TendAgentStatePlugin export")
	}
	if strings.Contains(opencodePluginAsset, "HerdrAgentStatePlugin") {
		t.Fatal("opencode asset still references HerdrAgentStatePlugin")
	}
	if !strings.Contains(kiloPluginAsset, "export const TendAgentStatePlugin") {
		t.Fatal("kilo asset missing TendAgentStatePlugin export")
	}
	if strings.Contains(kiloPluginAsset, "HerdrAgentStatePlugin") {
		t.Fatal("kilo asset still references HerdrAgentStatePlugin")
	}
}
