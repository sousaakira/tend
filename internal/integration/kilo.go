package integration

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/kilo/tend-agent-state.js
var kiloPluginAsset string

// KiloPluginInstallName is the filename written under ~/.config/kilo/plugin/.
const KiloPluginInstallName = "tend-agent-state.js"

// KiloIntegrationVersion is stamped in the plugin header; bump when the asset
// changes in a way that needs reinstall.
const KiloIntegrationVersion = 4

// KiloInstallPaths are the files InstallKilo wrote.
type KiloInstallPaths struct {
	PluginPath string
}

// KiloUninstallResult reports what UninstallKilo removed.
type KiloUninstallResult struct {
	PluginPath    string
	RemovedPlugin bool
}

// InstallKilo writes the JS plugin into Kilo's plugin directory.
func InstallKilo() (KiloInstallPaths, error) {
	dir, err := KiloDir()
	if err != nil {
		return KiloInstallPaths{}, err
	}
	if err := requireConfigDir(dir, "kilo"); err != nil {
		return KiloInstallPaths{}, err
	}

	pluginsDir := filepath.Join(dir, "plugin")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return KiloInstallPaths{}, err
	}
	pluginPath := filepath.Join(pluginsDir, KiloPluginInstallName)
	if err := os.WriteFile(pluginPath, []byte(kiloPluginAsset), 0o644); err != nil {
		return KiloInstallPaths{}, err
	}
	return KiloInstallPaths{PluginPath: pluginPath}, nil
}

// UninstallKilo removes the tend Kilo plugin when present.
func UninstallKilo() (KiloUninstallResult, error) {
	dir, err := KiloDir()
	if err != nil {
		return KiloUninstallResult{}, err
	}
	pluginPath := filepath.Join(dir, "plugin", KiloPluginInstallName)
	removed, err := RemoveFileIfExists(pluginPath)
	if err != nil {
		return KiloUninstallResult{PluginPath: pluginPath}, err
	}
	return KiloUninstallResult{
		PluginPath:    pluginPath,
		RemovedPlugin: removed,
	}, nil
}
