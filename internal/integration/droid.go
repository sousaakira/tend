package integration

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/droid/tend-agent-state.sh
var droidHookAsset string

// DroidHookInstallName is the filename written under ~/.factory/hooks/.
const DroidHookInstallName = "tend-agent-state.sh"

// DroidIntegrationVersion is stamped in the hook header; bump when the asset
// changes in a way that needs reinstall.
const DroidIntegrationVersion = 3

var droidHookEvents = []struct{ Event, Action string }{
	{"SessionStart", "session"},
}

var droidRemovedLifecycleHookEvents = []struct{ Event, Action string }{
	{"SessionStart", "idle"},
	{"UserPromptSubmit", "working"},
	{"PreToolUse", "working"},
	{"PostToolUse", "working"},
	{"Notification", "blocked"},
	{"Stop", "idle"},
	{"SubagentStop", "working"},
	{"PreCompact", "working"},
	{"SessionEnd", "release"},
}

// DroidInstallPaths are the files InstallDroid wrote or updated.
type DroidInstallPaths struct {
	HookPath           string
	HooksPath          string
	SettingsPath       string
	UpdatedLegacyHooks bool
}

// DroidUninstallResult reports what UninstallDroid removed.
type DroidUninstallResult struct {
	HookPath        string
	HooksPath       string
	SettingsPath    string
	RemovedHookFile bool
	UpdatedHooks    bool
	UpdatedSettings bool
}

// InstallDroid writes SessionStart into settings.json and strips tend/herdr
// entries from a legacy hooks.json if one still exists.
func InstallDroid() (DroidInstallPaths, error) {
	dir, err := DroidDir()
	if err != nil {
		return DroidInstallPaths{}, err
	}
	if err := requireConfigDirHint(dir, "droid config directory", "droid"); err != nil {
		return DroidInstallPaths{}, err
	}

	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return DroidInstallPaths{}, err
	}

	hookPath := filepath.Join(hooksDir, DroidHookInstallName)
	if err := os.WriteFile(hookPath, []byte(droidHookAsset), 0o755); err != nil {
		return DroidInstallPaths{}, err
	}
	if err := MakeExecutable(hookPath); err != nil {
		return DroidInstallPaths{}, err
	}

	settingsPath := filepath.Join(dir, "settings.json")
	existing := "{}"
	if data, err := os.ReadFile(settingsPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return DroidInstallPaths{}, err
	}

	settings, err := ParseJSONObject(existing, settingsPath, "droid settings")
	if err != nil {
		return DroidInstallPaths{}, err
	}
	hooks, err := EnsureHooksObject(settings, settingsPath, "droid settings", "droid settings hooks")
	if err != nil {
		return DroidInstallPaths{}, err
	}
	if _, err := RemoveHookCommands(hooks, "SessionStart", hookPath, ""); err != nil {
		return DroidInstallPaths{}, err
	}
	for _, rem := range droidRemovedLifecycleHookEvents {
		if _, err := RemoveHookCommands(hooks, rem.Event, hookPath, rem.Action); err != nil {
			return DroidInstallPaths{}, err
		}
	}
	for _, rem := range droidHookEvents {
		if _, err := RemoveHookCommands(hooks, rem.Event, hookPath, rem.Action); err != nil {
			return DroidInstallPaths{}, err
		}
	}
	for _, rem := range droidHookEvents {
		if err := EnsureCommandHook(hooks, rem.Event, HookCommand(hookPath, rem.Action), 10, ""); err != nil {
			return DroidInstallPaths{}, err
		}
	}
	if err := WriteJSONPretty(settingsPath, settings); err != nil {
		return DroidInstallPaths{}, err
	}

	hooksPath := filepath.Join(dir, "hooks.json")
	updatedLegacy := false
	if data, err := os.ReadFile(hooksPath); err == nil {
		hooksFile, err := ParseJSONObject(string(data), hooksPath, "droid hooks file")
		if err != nil {
			return DroidInstallPaths{}, err
		}
		if legacyHooks, ok, err := HooksObjectIfPresent(hooksFile, hooksPath, "droid hooks file", "droid hooks file hooks"); err != nil {
			return DroidInstallPaths{}, err
		} else if ok {
			got, err := RemoveHookCommands(legacyHooks, "SessionStart", hookPath, "")
			if err != nil {
				return DroidInstallPaths{}, err
			}
			updatedLegacy = got
			for _, rem := range droidRemovedLifecycleHookEvents {
				got, err := RemoveHookCommands(legacyHooks, rem.Event, hookPath, rem.Action)
				if err != nil {
					return DroidInstallPaths{}, err
				}
				updatedLegacy = updatedLegacy || got
			}
			for _, rem := range droidHookEvents {
				got, err := RemoveHookCommands(legacyHooks, rem.Event, hookPath, rem.Action)
				if err != nil {
					return DroidInstallPaths{}, err
				}
				updatedLegacy = updatedLegacy || got
			}
			if updatedLegacy {
				if err := WriteJSONPretty(hooksPath, hooksFile); err != nil {
					return DroidInstallPaths{}, err
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return DroidInstallPaths{}, err
	}

	return DroidInstallPaths{
		HookPath:           hookPath,
		HooksPath:          hooksPath,
		SettingsPath:       settingsPath,
		UpdatedLegacyHooks: updatedLegacy,
	}, nil
}

// UninstallDroid removes tend/herdr hooks from settings.json and hooks.json.
func UninstallDroid() (DroidUninstallResult, error) {
	dir, err := DroidDir()
	if err != nil {
		return DroidUninstallResult{}, err
	}
	hookPath := filepath.Join(dir, "hooks", DroidHookInstallName)
	hooksPath := filepath.Join(dir, "hooks.json")
	settingsPath := filepath.Join(dir, "settings.json")
	result := DroidUninstallResult{
		HookPath:     hookPath,
		HooksPath:    hooksPath,
		SettingsPath: settingsPath,
	}

	if data, err := os.ReadFile(hooksPath); err == nil {
		hooksFile, err := ParseJSONObject(string(data), hooksPath, "droid hooks file")
		if err != nil {
			return result, err
		}
		if hooks, ok, err := HooksObjectIfPresent(hooksFile, hooksPath, "droid hooks file", "droid hooks file hooks"); err != nil {
			return result, err
		} else if ok {
			updated := false
			got, err := RemoveHookCommands(hooks, "SessionStart", hookPath, "")
			if err != nil {
				return result, err
			}
			updated = got
			for _, rem := range droidRemovedLifecycleHookEvents {
				got, err := RemoveHookCommands(hooks, rem.Event, hookPath, rem.Action)
				if err != nil {
					return result, err
				}
				updated = updated || got
			}
			for _, rem := range droidHookEvents {
				got, err := RemoveHookCommands(hooks, rem.Event, hookPath, rem.Action)
				if err != nil {
					return result, err
				}
				updated = updated || got
			}
			if updated {
				if err := WriteJSONPretty(hooksPath, hooksFile); err != nil {
					return result, err
				}
				result.UpdatedHooks = true
			}
		}
	} else if !os.IsNotExist(err) {
		return result, err
	}

	if data, err := os.ReadFile(settingsPath); err == nil {
		settings, err := ParseJSONObject(string(data), settingsPath, "droid settings")
		if err != nil {
			return result, err
		}
		if hooks, ok, err := HooksObjectIfPresent(settings, settingsPath, "droid settings", "droid settings hooks"); err != nil {
			return result, err
		} else if ok {
			updated := false
			got, err := RemoveHookCommands(hooks, "SessionStart", hookPath, "")
			if err != nil {
				return result, err
			}
			updated = got
			for _, rem := range droidRemovedLifecycleHookEvents {
				got, err := RemoveHookCommands(hooks, rem.Event, hookPath, rem.Action)
				if err != nil {
					return result, err
				}
				updated = updated || got
			}
			for _, rem := range droidHookEvents {
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
