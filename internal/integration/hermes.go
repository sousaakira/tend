package integration

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/hermes/plugin.yaml
var hermesPluginManifestAsset string

//go:embed assets/hermes/__init__.py
var hermesPluginInitAsset string

// Hermes plugin install names. The plugin directory leaf matches plugin.yaml's
// name field so Hermes can discover it.
const (
	HermesPluginInstallName         = "tend-agent-state"
	HermesPluginManifestInstallName = "plugin.yaml"
	HermesPluginInitInstallName     = "__init__.py"
	HermesIntegrationVersion        = 5
)

// HermesInstallPaths are the files InstallHermes wrote or updated.
type HermesInstallPaths struct {
	PluginDir  string
	ConfigPath string
}

// HermesUninstallResult reports what UninstallHermes removed.
type HermesUninstallResult struct {
	PluginDir        string
	ConfigPath       string
	RemovedPluginDir bool
	UpdatedConfig    bool
}

// InstallHermes writes plugin.yaml + __init__.py and enables the plugin in
// config.yaml.
func InstallHermes() (HermesInstallPaths, error) {
	dir, err := HermesDir()
	if err != nil {
		return HermesInstallPaths{}, err
	}
	if err := requireConfigDirHint(dir, "hermes config directory", "hermes agent"); err != nil {
		return HermesInstallPaths{}, err
	}

	pluginDir, err := HermesPluginDir()
	if err != nil {
		return HermesInstallPaths{}, err
	}
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return HermesInstallPaths{}, err
	}
	if err := os.WriteFile(
		filepath.Join(pluginDir, HermesPluginManifestInstallName),
		[]byte(hermesPluginManifestAsset),
		0o644,
	); err != nil {
		return HermesInstallPaths{}, err
	}
	if err := os.WriteFile(
		filepath.Join(pluginDir, HermesPluginInitInstallName),
		[]byte(hermesPluginInitAsset),
		0o644,
	); err != nil {
		return HermesInstallPaths{}, err
	}

	configPath := filepath.Join(dir, "config.yaml")
	existing := ""
	if data, err := os.ReadFile(configPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return HermesInstallPaths{}, err
	}
	newConfig := EnsureHermesPluginEnabled(existing)
	if newConfig != existing {
		if err := os.WriteFile(configPath, []byte(newConfig), 0o644); err != nil {
			return HermesInstallPaths{}, err
		}
	}

	return HermesInstallPaths{
		PluginDir:  pluginDir,
		ConfigPath: configPath,
	}, nil
}

// UninstallHermes removes the plugin directory and disables the config entry.
func UninstallHermes() (HermesUninstallResult, error) {
	dir, err := HermesDir()
	if err != nil {
		return HermesUninstallResult{}, err
	}
	pluginDir, err := HermesPluginDir()
	if err != nil {
		return HermesUninstallResult{}, err
	}
	configPath := filepath.Join(dir, "config.yaml")
	result := HermesUninstallResult{
		PluginDir:  pluginDir,
		ConfigPath: configPath,
	}

	removed, err := RemoveDirAllIfExists(pluginDir)
	if err != nil {
		return result, err
	}
	result.RemovedPluginDir = removed

	if data, err := os.ReadFile(configPath); err == nil {
		newConfig := RemoveHermesPluginEnabled(string(data))
		if newConfig != string(data) {
			if err := os.WriteFile(configPath, []byte(newConfig), 0o644); err != nil {
				return result, err
			}
			result.UpdatedConfig = true
		}
	} else if !os.IsNotExist(err) {
		return result, err
	}

	return result, nil
}
