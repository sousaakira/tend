package integration

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/devin/tend-agent-state.sh
var devinHookAsset string

// DevinHookInstallName is the filename written under the Devin config dir.
const DevinHookInstallName = "tend-agent-state.sh"

// DevinIntegrationVersion is stamped in the hook header; bump when the asset
// changes in a way that needs reinstall.
const DevinIntegrationVersion = 2

// Devin hooks all use the "session" action; older working/blocked/idle/release
// entries are removed on install so a prior lifecycle design does not linger.
var devinHookEvents = []struct{ Event, Action string }{
	{"SessionStart", "session"},
	{"UserPromptSubmit", "session"},
	{"PreToolUse", "session"},
	{"PostToolUse", "session"},
	{"PermissionRequest", "session"},
	{"Stop", "session"},
}

var devinRemovedLifecycleHookEvents = []struct{ Event, Action string }{
	{"UserPromptSubmit", "working"},
	{"PreToolUse", "working"},
	{"PostToolUse", "working"},
	{"PermissionRequest", "blocked"},
	{"Stop", "idle"},
	{"SessionEnd", "release"},
}

// DevinInstallPaths are the files InstallDevin wrote or updated.
type DevinInstallPaths struct {
	HookPath     string
	SettingsPath string
}

// DevinUninstallResult reports what UninstallDevin removed.
type DevinUninstallResult struct {
	HookPath        string
	SettingsPath    string
	RemovedHookFile bool
	UpdatedSettings bool
}

// InstallDevin writes nested session hooks into Devin's config.json.
func InstallDevin() (DevinInstallPaths, error) {
	dir, err := DevinDir()
	if err != nil {
		return DevinInstallPaths{}, err
	}
	if err := requireConfigDirHint(dir, "devin config directory", "devin cli"); err != nil {
		return DevinInstallPaths{}, err
	}

	hookPath := filepath.Join(dir, DevinHookInstallName)
	if err := os.WriteFile(hookPath, []byte(devinHookAsset), 0o755); err != nil {
		return DevinInstallPaths{}, err
	}
	if err := MakeExecutable(hookPath); err != nil {
		return DevinInstallPaths{}, err
	}

	settingsPath := filepath.Join(dir, "config.json")
	existing := "{}"
	if data, err := os.ReadFile(settingsPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return DevinInstallPaths{}, err
	}

	settings, err := ParseJSONObject(existing, settingsPath, "devin settings")
	if err != nil {
		return DevinInstallPaths{}, err
	}
	hooks, err := EnsureHooksObject(settings, settingsPath, "devin settings", "devin settings hooks")
	if err != nil {
		return DevinInstallPaths{}, err
	}
	for _, rem := range devinRemovedLifecycleHookEvents {
		if _, err := RemoveHookCommands(hooks, rem.Event, hookPath, rem.Action); err != nil {
			return DevinInstallPaths{}, err
		}
	}
	for _, rem := range devinHookEvents {
		if _, err := RemoveHookCommands(hooks, rem.Event, hookPath, rem.Action); err != nil {
			return DevinInstallPaths{}, err
		}
	}
	for _, rem := range devinHookEvents {
		if err := EnsureCommandHook(hooks, rem.Event, HookCommand(hookPath, rem.Action), 10, ""); err != nil {
			return DevinInstallPaths{}, err
		}
	}
	if err := WriteJSONPretty(settingsPath, settings); err != nil {
		return DevinInstallPaths{}, err
	}

	return DevinInstallPaths{HookPath: hookPath, SettingsPath: settingsPath}, nil
}

// UninstallDevin removes tend/herdr Devin hooks and the hook file.
func UninstallDevin() (DevinUninstallResult, error) {
	dir, err := DevinDir()
	if err != nil {
		return DevinUninstallResult{}, err
	}
	hookPath := filepath.Join(dir, DevinHookInstallName)
	settingsPath := filepath.Join(dir, "config.json")
	result := DevinUninstallResult{HookPath: hookPath, SettingsPath: settingsPath}

	if data, err := os.ReadFile(settingsPath); err == nil {
		settings, err := ParseJSONObject(string(data), settingsPath, "devin settings")
		if err != nil {
			return result, err
		}
		if hooks, ok, err := HooksObjectIfPresent(settings, settingsPath, "devin settings", "devin settings hooks"); err != nil {
			return result, err
		} else if ok {
			updated := false
			for _, rem := range devinRemovedLifecycleHookEvents {
				got, err := RemoveHookCommands(hooks, rem.Event, hookPath, rem.Action)
				if err != nil {
					return result, err
				}
				updated = updated || got
			}
			for _, rem := range devinHookEvents {
				got, err := RemoveHookCommands(hooks, rem.Event, hookPath, rem.Action)
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
