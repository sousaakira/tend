package integration

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed assets/opencode/tend-agent-state.js
var opencodePluginAsset string

//go:embed assets/opencode/tend-tui-session.js
var opencodeTUIPluginAsset string

//go:embed assets/opencode/tui.js
var opencodeV2TUIPluginAsset string

// OpenCode plugin install names and specs. V1 registers a file path in
// tui.jsonc; V2 registers a directory package in cli.json.
const (
	OpenCodePluginInstallName    = "tend-agent-state.js"
	OpenCodeTUIPluginInstallName = "tend-tui-session.js"
	OpenCodeTUIPluginSpec        = "./tend-tui-session.js"
	OpenCodeV2TUIPluginDir       = "tend-opencode"
	OpenCodeV2TUIPluginSpec      = "./tend-opencode"
	OpenCodeIntegrationVersion   = 12
)

// OpenCodeInstallPaths are the files InstallOpencode wrote or updated.
type OpenCodeInstallPaths struct {
	PluginPath    string
	TUIPluginPath string
	TUIConfigPath string
	// CLIConfigPath is empty when V2 registration was deferred for migration.
	CLIConfigPath string
}

// OpenCodeUninstallResult reports what UninstallOpencode removed.
type OpenCodeUninstallResult struct {
	PluginPath       string
	TUIPluginPath    string
	TUIConfigPath    string
	RemovedPlugin    bool
	RemovedTUIPlugin bool
	UpdatedTUIConfig bool
}

// InstallOpencode writes the server plugin, V1 TUI plugin, V2 package dir, and
// registers them in tui.jsonc / cli.json.
func InstallOpencode() (OpenCodeInstallPaths, error) {
	dir, err := OpencodeDir()
	if err != nil {
		return OpenCodeInstallPaths{}, err
	}
	if err := requireConfigDir(dir, "opencode"); err != nil {
		return OpenCodeInstallPaths{}, err
	}
	if err := ValidateTUIPluginConfig(dir); err != nil {
		return OpenCodeInstallPaths{}, err
	}

	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return OpenCodeInstallPaths{}, err
	}
	pluginPath := filepath.Join(pluginsDir, OpenCodePluginInstallName)
	if err := os.WriteFile(pluginPath, []byte(opencodePluginAsset), 0o644); err != nil {
		return OpenCodeInstallPaths{}, err
	}
	tuiPluginPath := filepath.Join(dir, OpenCodeTUIPluginInstallName)
	if err := os.WriteFile(tuiPluginPath, []byte(opencodeTUIPluginAsset), 0o644); err != nil {
		return OpenCodeInstallPaths{}, err
	}
	tuiConfigPath, err := AddTUIPlugin(dir, OpenCodeTUIPluginSpec)
	if err != nil {
		return OpenCodeInstallPaths{}, err
	}

	v2Dir := filepath.Join(dir, OpenCodeV2TUIPluginDir)
	if err := os.MkdirAll(v2Dir, 0o755); err != nil {
		return OpenCodeInstallPaths{}, err
	}
	if err := os.WriteFile(filepath.Join(v2Dir, "tui.js"), []byte(opencodeV2TUIPluginAsset), 0o644); err != nil {
		return OpenCodeInstallPaths{}, err
	}

	stateDir, err := OpencodeStateDir()
	if err != nil {
		return OpenCodeInstallPaths{}, err
	}
	cliConfigPath, err := AddCLIPlugin(dir, stateDir, OpenCodeV2TUIPluginSpec)
	if err != nil {
		return OpenCodeInstallPaths{}, err
	}

	return OpenCodeInstallPaths{
		PluginPath:    pluginPath,
		TUIPluginPath: tuiPluginPath,
		TUIConfigPath: tuiConfigPath,
		CLIConfigPath: cliConfigPath,
	}, nil
}

// UninstallOpencode removes plugins and managed config entries. Collects
// per-step errors the way herdr does so a partial failure still reports all
// of them.
func UninstallOpencode() (OpenCodeUninstallResult, error) {
	dir, err := OpencodeDir()
	if err != nil {
		return OpenCodeUninstallResult{}, err
	}
	tuiConfigPath := TUIConfigPath(dir)
	pluginPath := filepath.Join(dir, "plugins", OpenCodePluginInstallName)
	tuiPluginPath := filepath.Join(dir, OpenCodeTUIPluginInstallName)
	result := OpenCodeUninstallResult{
		PluginPath:    pluginPath,
		TUIPluginPath: tuiPluginPath,
		TUIConfigPath: tuiConfigPath,
	}

	var errors []string
	if _, err := RemoveCLIPlugin(dir, OpenCodeV2TUIPluginSpec); err != nil {
		errors = append(errors, err.Error())
	}
	v2Dir := filepath.Join(dir, OpenCodeV2TUIPluginDir)
	if _, err := RemoveDirAllIfExists(v2Dir); err != nil {
		errors = append(errors, fmt.Sprintf("failed to remove %s: %v", v2Dir, err))
	}
	updated, err := RemoveTUIPlugin(dir, OpenCodeTUIPluginSpec)
	if err != nil {
		errors = append(errors, err.Error())
	} else {
		result.UpdatedTUIConfig = updated
	}
	removed, err := RemoveFileIfExists(pluginPath)
	if err != nil {
		errors = append(errors, fmt.Sprintf("failed to remove %s: %v", pluginPath, err))
	} else {
		result.RemovedPlugin = removed
	}
	removedTUI, err := RemoveFileIfExists(tuiPluginPath)
	if err != nil {
		errors = append(errors, fmt.Sprintf("failed to remove %s: %v", tuiPluginPath, err))
	} else {
		result.RemovedTUIPlugin = removedTUI
	}
	if len(errors) > 0 {
		return result, fmt.Errorf("%s", strings.Join(errors, "; "))
	}
	return result, nil
}
