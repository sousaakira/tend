package integration

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/grok/tend-agent-state.sh
var grokHookAsset string

// GrokHookInstallName is the hook script under ~/.grok/hooks/.
const GrokHookInstallName = "tend-agent-state.sh"

// GrokHookConfigInstallName is the tend-owned JSON config Grok merges.
// Renamed from herdr.json; uninstall also deletes a leftover herdr.json.
const GrokHookConfigInstallName = "tend.json"

const legacyGrokHookConfigInstallName = "herdr.json"

// GrokIntegrationVersion is stamped in the hook header.
const GrokIntegrationVersion = 2

// GrokInstallPaths are the files InstallGrok wrote.
type GrokInstallPaths struct {
	HookPath   string
	ConfigPath string
}

// GrokUninstallResult reports what UninstallGrok removed.
type GrokUninstallResult struct {
	HookPath          string
	ConfigPath        string
	RemovedHookFile   bool
	RemovedConfigFile bool
}

// GrokHookCommand builds the SessionStart command Grok stores. Unlike other
// agents (bash …), Grok uses sh — keep this so status/outdated checks match
// the installed config byte-for-byte.
func GrokHookCommand(hookPath string) string {
	return "sh " + ShellSingleQuote(hookPath) + " session"
}

// GrokHookConfig is the complete tend-owned hooks file. Install and status
// share this value so config drift is reported as outdated.
func GrokHookConfig(hookPath string) map[string]any {
	return map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": GrokHookCommand(hookPath),
							"timeout": uint64(10),
						},
					},
				},
			},
		},
	}
}

// InstallGrok writes the hook script and a dedicated tend.json under hooks/.
// Grok merges every ~/.grok/hooks/*.json, so tend owns one file and never
// edits the user's other hook configs.
func InstallGrok() (GrokInstallPaths, error) {
	dir, err := GrokDir()
	if err != nil {
		return GrokInstallPaths{}, err
	}
	if err := requireConfigDir(dir, "grok"); err != nil {
		return GrokInstallPaths{}, err
	}

	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return GrokInstallPaths{}, err
	}

	hookPath := filepath.Join(hooksDir, GrokHookInstallName)
	if err := os.WriteFile(hookPath, []byte(grokHookAsset), 0o755); err != nil {
		return GrokInstallPaths{}, err
	}
	if err := MakeExecutable(hookPath); err != nil {
		return GrokInstallPaths{}, err
	}

	configPath := filepath.Join(hooksDir, GrokHookConfigInstallName)
	if err := WriteJSONPretty(configPath, GrokHookConfig(hookPath)); err != nil {
		return GrokInstallPaths{}, err
	}
	// Drop a leftover herdr-owned config so Grok does not run both.
	_, _ = RemoveFileIfExists(filepath.Join(hooksDir, legacyGrokHookConfigInstallName))

	return GrokInstallPaths{HookPath: hookPath, ConfigPath: configPath}, nil
}

// UninstallGrok deletes the tend-owned hook script and config (and herdr.json).
func UninstallGrok() (GrokUninstallResult, error) {
	dir, err := GrokDir()
	if err != nil {
		return GrokUninstallResult{}, err
	}
	hooksDir := filepath.Join(dir, "hooks")
	hookPath := filepath.Join(hooksDir, GrokHookInstallName)
	configPath := filepath.Join(hooksDir, GrokHookConfigInstallName)
	result := GrokUninstallResult{HookPath: hookPath, ConfigPath: configPath}

	removedConfig, err := RemoveFileIfExists(configPath)
	if err != nil {
		return result, err
	}
	legacyRemoved, err := RemoveFileIfExists(filepath.Join(hooksDir, legacyGrokHookConfigInstallName))
	if err != nil {
		return result, err
	}
	result.RemovedConfigFile = removedConfig || legacyRemoved

	removedHook, err := RemoveFileIfExists(hookPath)
	if err != nil {
		return result, err
	}
	result.RemovedHookFile = removedHook
	return result, nil
}
