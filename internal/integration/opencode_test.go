package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallOpencodeWritesServerAndTUIPlugins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	opencodeDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(opencodeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallOpencode()
	if err != nil {
		t.Fatal(err)
	}
	wantPlugin := filepath.Join(opencodeDir, "plugins", OpenCodePluginInstallName)
	if installed.PluginPath != wantPlugin {
		t.Fatalf("plugin = %q", installed.PluginPath)
	}
	got, err := os.ReadFile(installed.PluginPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != opencodePluginAsset {
		t.Fatal("plugin asset mismatch")
	}
	if installed.TUIPluginPath != filepath.Join(opencodeDir, OpenCodeTUIPluginInstallName) {
		t.Fatalf("tui plugin = %q", installed.TUIPluginPath)
	}
	gotTUI, err := os.ReadFile(installed.TUIPluginPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotTUI) != opencodeTUIPluginAsset {
		t.Fatal("tui asset mismatch")
	}
	if installed.TUIConfigPath != filepath.Join(opencodeDir, "tui.jsonc") {
		t.Fatalf("tui config = %q", installed.TUIConfigPath)
	}
	tuiConfig := readJSONObject(t, installed.TUIConfigPath)
	plugins := tuiConfig["plugin"].([]any)
	if len(plugins) != 1 || plugins[0] != OpenCodeTUIPluginSpec {
		t.Fatalf("plugin list = %#v", plugins)
	}
	if installed.CLIConfigPath != filepath.Join(opencodeDir, "cli.json") {
		t.Fatalf("cli config = %q", installed.CLIConfigPath)
	}
	cliConfig := readJSONObject(t, installed.CLIConfigPath)
	cliPlugins := cliConfig["plugins"].([]any)
	if len(cliPlugins) != 1 || cliPlugins[0] != OpenCodeV2TUIPluginSpec {
		t.Fatalf("cli plugins = %#v", cliPlugins)
	}
}

func TestOpencodeInstallDefersV2RegistrationWhileMigrationPending(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	opencodeDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(opencodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opencodeDir, "tui.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallOpencode()
	if err != nil {
		t.Fatal(err)
	}
	if installed.CLIConfigPath != "" {
		t.Fatalf("cli should be deferred, got %q", installed.CLIConfigPath)
	}
	if _, err := os.Stat(filepath.Join(opencodeDir, "cli.json")); !os.IsNotExist(err) {
		t.Fatal("cli.json should not exist")
	}
	v2 := filepath.Join(opencodeDir, OpenCodeV2TUIPluginDir, "tui.js")
	if _, err := os.Stat(v2); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallOpencodeRemovesPluginsAndManagedTUIConfigEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	opencodeDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(opencodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	installed, err := InstallOpencode()
	if err != nil {
		t.Fatal(err)
	}

	result, err := UninstallOpencode()
	if err != nil {
		t.Fatal(err)
	}
	if !result.RemovedPlugin || !result.RemovedTUIPlugin || !result.UpdatedTUIConfig {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(result.PluginPath); !os.IsNotExist(err) {
		t.Fatal("plugin still present")
	}
	if _, err := os.Stat(result.TUIPluginPath); !os.IsNotExist(err) {
		t.Fatal("tui plugin still present")
	}
	tuiConfig := readJSONObject(t, result.TUIConfigPath)
	if len(tuiConfig) != 0 {
		t.Fatalf("tui config = %#v", tuiConfig)
	}
	if installed.PluginPath != result.PluginPath {
		t.Fatal("plugin path mismatch")
	}
}

func TestInstallOpencodeInvalidTUIConfigDoesNotWritePlugins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	opencodeDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(opencodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opencodeDir, "tui.jsonc"), []byte(`{"plugin":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := InstallOpencode()
	if err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(filepath.Join(opencodeDir, "plugins", OpenCodePluginInstallName)); !os.IsNotExist(err) {
		t.Fatal("plugin should not be written")
	}
	if _, err := os.Stat(filepath.Join(opencodeDir, OpenCodeTUIPluginInstallName)); !os.IsNotExist(err) {
		t.Fatal("tui plugin should not be written")
	}
}

func TestOpencodeInvalidCLIConfigDoesNotOverwriteExistingPlugins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(dir, "plugins", OpenCodePluginInstallName)
	if err := os.WriteFile(plugin, []byte("previous integration"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cli.json"), []byte(`{"plugins":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallOpencode(); err == nil {
		t.Fatal("expected error")
	}
	got, err := os.ReadFile(plugin)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "previous integration" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "tui.jsonc")); !os.IsNotExist(err) {
		t.Fatal("tui.jsonc should not exist")
	}
}

func TestInstallOpencodeErrorsWhenConfigDirMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := InstallOpencode()
	if err == nil || !strings.Contains(err.Error(), "opencode config directory not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestAddCLIPluginDefersWhileMigrationPending(t *testing.T) {
	dir := t.TempDir()
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tui.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := AddCLIPlugin(dir, state, OpenCodeV2TUIPluginSpec)
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Fatalf("expected defer, got %q", path)
	}
	if _, err := os.Stat(filepath.Join(dir, "cli.json")); !os.IsNotExist(err) {
		t.Fatal("cli.json should not exist")
	}
}

func TestAddAndRemoveTUIPlugin(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "tui.jsonc")
	if err := os.WriteFile(configPath, []byte(`{"theme":"system","plugin":["example"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AddTUIPlugin(dir, OpenCodeTUIPluginSpec); err != nil {
		t.Fatal(err)
	}
	if _, err := AddTUIPlugin(dir, OpenCodeTUIPluginSpec); err != nil {
		t.Fatal(err)
	}
	cfg := readJSONObject(t, configPath)
	plugins := cfg["plugin"].([]any)
	if len(plugins) != 2 || plugins[1] != OpenCodeTUIPluginSpec {
		t.Fatalf("plugins = %#v", plugins)
	}
	removed, err := RemoveTUIPlugin(dir, OpenCodeTUIPluginSpec)
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	cfg = readJSONObject(t, configPath)
	plugins = cfg["plugin"].([]any)
	if len(plugins) != 1 || plugins[0] != "example" {
		t.Fatalf("plugins = %#v", plugins)
	}
}

func readJSONObject(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	return root
}
