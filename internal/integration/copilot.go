package integration

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/copilot/tend-agent-state.sh
var copilotHookAsset string

// CopilotHookInstallName is the filename written under ~/.copilot/hooks/.
const CopilotHookInstallName = "tend-agent-state.sh"

// CopilotIntegrationVersion is stamped in the hook header; bump when the asset
// changes in a way that needs reinstall.
const CopilotIntegrationVersion = 3

// Copilot installs only SessionStart; older lifecycle events are stripped on
// every install/uninstall so a prior herdr install does not leave noise.
var copilotHookEvents = []string{"SessionStart"}

var copilotRemovedLifecycleHookEvents = []string{
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
	"Stop",
	"agentStop",
	"SessionEnd",
	"notification",
	"sessionStart",
}

// CopilotInstallPaths are the files InstallCopilot wrote or updated.
type CopilotInstallPaths struct {
	HookPath     string
	SettingsPath string
}

// CopilotUninstallResult reports what UninstallCopilot removed.
type CopilotUninstallResult struct {
	HookPath        string
	SettingsPath    string
	RemovedHookFile bool
	UpdatedSettings bool
}

// InstallCopilot writes the SessionStart hook into Copilot's settings.json
// using the flat bash/timeoutSec shape Copilot documents.
func InstallCopilot() (CopilotInstallPaths, error) {
	dir, err := CopilotDir()
	if err != nil {
		return CopilotInstallPaths{}, err
	}
	if err := requireConfigDirHint(dir, "copilot config directory", "github copilot cli"); err != nil {
		return CopilotInstallPaths{}, err
	}

	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return CopilotInstallPaths{}, err
	}

	hookPath := filepath.Join(hooksDir, CopilotHookInstallName)
	if err := os.WriteFile(hookPath, []byte(copilotHookAsset), 0o755); err != nil {
		return CopilotInstallPaths{}, err
	}
	if err := MakeExecutable(hookPath); err != nil {
		return CopilotInstallPaths{}, err
	}

	settingsPath := filepath.Join(dir, "settings.json")
	existing := "{}"
	if data, err := os.ReadFile(settingsPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return CopilotInstallPaths{}, err
	}

	settings, err := ParseJSONObject(existing, settingsPath, "copilot settings")
	if err != nil {
		return CopilotInstallPaths{}, err
	}
	hooks, err := EnsureHooksObject(settings, settingsPath, "copilot settings", "copilot settings hooks")
	if err != nil {
		return CopilotInstallPaths{}, err
	}
	command := HookCommand(hookPath, "")
	for _, event := range copilotRemovedLifecycleHookEvents {
		if _, err := RemoveDirectHookCommands(hooks, event, hookPath, ""); err != nil {
			return CopilotInstallPaths{}, err
		}
	}
	for _, event := range copilotHookEvents {
		if _, err := RemoveDirectHookCommands(hooks, event, hookPath, ""); err != nil {
			return CopilotInstallPaths{}, err
		}
	}
	for _, event := range copilotHookEvents {
		if err := EnsureDirectCommandHook(hooks, event, command, 10, ""); err != nil {
			return CopilotInstallPaths{}, err
		}
	}
	if err := WriteJSONPretty(settingsPath, settings); err != nil {
		return CopilotInstallPaths{}, err
	}

	return CopilotInstallPaths{HookPath: hookPath, SettingsPath: settingsPath}, nil
}

// UninstallCopilot removes tend/herdr Copilot hooks and the hook file.
func UninstallCopilot() (CopilotUninstallResult, error) {
	dir, err := CopilotDir()
	if err != nil {
		return CopilotUninstallResult{}, err
	}
	hookPath := filepath.Join(dir, "hooks", CopilotHookInstallName)
	settingsPath := filepath.Join(dir, "settings.json")
	result := CopilotUninstallResult{HookPath: hookPath, SettingsPath: settingsPath}

	if data, err := os.ReadFile(settingsPath); err == nil {
		settings, err := ParseJSONObject(string(data), settingsPath, "copilot settings")
		if err != nil {
			return result, err
		}
		if hooks, ok, err := HooksObjectIfPresent(settings, settingsPath, "copilot settings", "copilot settings hooks"); err != nil {
			return result, err
		} else if ok {
			updated := false
			for _, event := range copilotHookEvents {
				got, err := RemoveDirectHookCommands(hooks, event, hookPath, "")
				if err != nil {
					return result, err
				}
				updated = updated || got
			}
			for _, event := range copilotRemovedLifecycleHookEvents {
				got, err := RemoveDirectHookCommands(hooks, event, hookPath, "")
				if err != nil {
					return result, err
				}
				updated = updated || got
			}
			if updated {
				if err := WriteJSONPretty(settingsPath, settings); err != nil {
					return result, err
				}
				result.UpdatedSettings = true
			}
		}
	} else if !os.IsNotExist(err) {
		return result, err
	}

	removed, err := RemoveFileIfExists(hookPath)
	if err != nil {
		return result, err
	}
	result.RemovedHookFile = removed
	return result, nil
}
