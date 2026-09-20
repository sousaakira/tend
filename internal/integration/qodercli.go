package integration

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/qodercli/tend-agent-state.sh
var qodercliHookAsset string

// QodercliHookInstallName is the filename under ~/.qoder/hooks/.
const QodercliHookInstallName = "tend-agent-state.sh"

// QodercliIntegrationVersion is stamped in the hook header.
const QodercliIntegrationVersion = 3

var qodercliHookEvents = []struct{ Event, Action string }{
	{"SessionStart", "session"},
}

// Deprecated lifecycle hooks tend/herdr once registered; stripped on every install.
var qodercliRemovedLifecycle = []struct{ Event, Action string }{
	{"SessionStart", "idle"},
	{"UserPromptSubmit", "working"},
	{"PreToolUse", "working"},
	{"PostToolUse", "working"},
	{"PostToolUseFailure", "working"},
	{"SubagentStart", "working"},
	{"SubagentStop", "working"},
	{"PreCompact", "working"},
	{"Notification", "blocked"},
	{"PermissionRequest", "blocked"},
	{"Stop", "idle"},
	{"SessionEnd", "release"},
}

// QodercliInstallPaths are the files InstallQodercli wrote or updated.
type QodercliInstallPaths struct {
	HookPath     string
	SettingsPath string
}

// QodercliUninstallResult reports what UninstallQodercli removed.
type QodercliUninstallResult struct {
	HookPath        string
	SettingsPath    string
	RemovedHookFile bool
	UpdatedSettings bool
}

// InstallQodercli writes the SessionStart hook into ~/.qoder/settings.json.
// The schema mirrors Claude settings (matcher + nested command hooks).
func InstallQodercli() (QodercliInstallPaths, error) {
	dir, err := QodercliDir()
	if err != nil {
		return QodercliInstallPaths{}, err
	}
	if err := requireConfigDir(dir, "qodercli"); err != nil {
		return QodercliInstallPaths{}, err
	}

	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return QodercliInstallPaths{}, err
	}

	hookPath := filepath.Join(hooksDir, QodercliHookInstallName)
	if err := os.WriteFile(hookPath, []byte(qodercliHookAsset), 0o755); err != nil {
		return QodercliInstallPaths{}, err
	}
	if err := MakeExecutable(hookPath); err != nil {
		return QodercliInstallPaths{}, err
	}

	settingsPath := filepath.Join(dir, "settings.json")
	existing := "{}"
	if data, err := os.ReadFile(settingsPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return QodercliInstallPaths{}, err
	}

	settings, err := ParseJSONObject(existing, settingsPath, "qodercli settings")
	if err != nil {
		return QodercliInstallPaths{}, err
	}
	hooks, err := EnsureHooksObject(settings, settingsPath, "qodercli settings", "qodercli settings hooks")
	if err != nil {
		return QodercliInstallPaths{}, err
	}
	for _, rem := range qodercliRemovedLifecycle {
		if _, err := RemoveHookCommands(hooks, rem.Event, hookPath, rem.Action); err != nil {
			return QodercliInstallPaths{}, err
		}
	}
	for _, ev := range qodercliHookEvents {
		if _, err := RemoveHookCommands(hooks, ev.Event, hookPath, ev.Action); err != nil {
			return QodercliInstallPaths{}, err
		}
	}
	for _, ev := range qodercliHookEvents {
		if err := EnsureCommandHook(hooks, ev.Event, HookCommand(hookPath, ev.Action), 10, "*"); err != nil {
			return QodercliInstallPaths{}, err
		}
	}
	if err := WriteJSONPretty(settingsPath, settings); err != nil {
		return QodercliInstallPaths{}, err
	}

	return QodercliInstallPaths{HookPath: hookPath, SettingsPath: settingsPath}, nil
}

// UninstallQodercli removes tend hook entries and the hook file.
func UninstallQodercli() (QodercliUninstallResult, error) {
	dir, err := QodercliDir()
	if err != nil {
		return QodercliUninstallResult{}, err
	}
	hookPath := filepath.Join(dir, "hooks", QodercliHookInstallName)
	settingsPath := filepath.Join(dir, "settings.json")
	result := QodercliUninstallResult{HookPath: hookPath, SettingsPath: settingsPath}

	if data, err := os.ReadFile(settingsPath); err == nil {
		settings, err := ParseJSONObject(string(data), settingsPath, "qodercli settings")
		if err != nil {
			return result, err
		}
		hooks, ok, err := HooksObjectIfPresent(settings, settingsPath, "qodercli settings", "qodercli settings hooks")
		if err != nil {
			return result, err
		}
		if ok {
			updated := false
			for _, rem := range qodercliRemovedLifecycle {
				got, err := RemoveHookCommands(hooks, rem.Event, hookPath, rem.Action)
				if err != nil {
					return result, err
				}
				updated = updated || got
			}
			for _, ev := range qodercliHookEvents {
				got, err := RemoveHookCommands(hooks, ev.Event, hookPath, ev.Action)
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
