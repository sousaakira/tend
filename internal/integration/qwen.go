package integration

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/qwen/tend-agent-session.sh
var qwenHookAsset string

// QwenHookInstallName is the session hook under ~/.qwen/hooks/.
const QwenHookInstallName = "tend-agent-session.sh"

// QwenIntegrationVersion is stamped in the hook header.
const QwenIntegrationVersion = 1

var qwenHookEvents = []struct{ Event, Action string }{
	{"SessionStart", "session"},
}

// QwenInstallPaths are the files InstallQwen wrote or updated.
type QwenInstallPaths struct {
	HookPath     string
	SettingsPath string
}

// QwenUninstallResult reports what UninstallQwen removed.
type QwenUninstallResult struct {
	HookPath        string
	SettingsPath    string
	RemovedHookFile bool
	UpdatedSettings bool
}

// InstallQwen writes tend-agent-session.sh and registers SessionStart in settings.json.
// Timeout is milliseconds (10_000), matching qwen's schema rather than Claude's seconds.
func InstallQwen() (QwenInstallPaths, error) {
	dir, err := QwenDir()
	if err != nil {
		return QwenInstallPaths{}, err
	}
	if err := requireConfigDir(dir, "qwen code"); err != nil {
		return QwenInstallPaths{}, err
	}

	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return QwenInstallPaths{}, err
	}

	hookPath := filepath.Join(hooksDir, QwenHookInstallName)
	if err := os.WriteFile(hookPath, []byte(qwenHookAsset), 0o755); err != nil {
		return QwenInstallPaths{}, err
	}
	if err := MakeExecutable(hookPath); err != nil {
		return QwenInstallPaths{}, err
	}

	settingsPath := filepath.Join(dir, "settings.json")
	existing := "{}"
	if data, err := os.ReadFile(settingsPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return QwenInstallPaths{}, err
	}

	settings, err := ParseJSONObject(existing, settingsPath, "qwen settings")
	if err != nil {
		return QwenInstallPaths{}, err
	}
	hooks, err := EnsureHooksObject(settings, settingsPath, "qwen settings", "qwen settings hooks")
	if err != nil {
		return QwenInstallPaths{}, err
	}
	for _, ev := range qwenHookEvents {
		if _, err := RemoveHookCommands(hooks, ev.Event, hookPath, ev.Action); err != nil {
			return QwenInstallPaths{}, err
		}
		if err := EnsureCommandHook(hooks, ev.Event, HookCommand(hookPath, ev.Action), 10_000, "*"); err != nil {
			return QwenInstallPaths{}, err
		}
	}
	if err := WriteJSONPretty(settingsPath, settings); err != nil {
		return QwenInstallPaths{}, err
	}

	return QwenInstallPaths{HookPath: hookPath, SettingsPath: settingsPath}, nil
}

// UninstallQwen removes tend SessionStart entries and the session hook file.
func UninstallQwen() (QwenUninstallResult, error) {
	dir, err := QwenDir()
	if err != nil {
		return QwenUninstallResult{}, err
	}
	hookPath := filepath.Join(dir, "hooks", QwenHookInstallName)
	settingsPath := filepath.Join(dir, "settings.json")
	result := QwenUninstallResult{HookPath: hookPath, SettingsPath: settingsPath}

	if data, err := os.ReadFile(settingsPath); err == nil {
		settings, err := ParseJSONObject(string(data), settingsPath, "qwen settings")
		if err != nil {
			return result, err
		}
		hooks, ok, err := HooksObjectIfPresent(settings, settingsPath, "qwen settings", "qwen settings hooks")
		if err != nil {
			return result, err
		}
		if ok {
			updated := false
			for _, ev := range qwenHookEvents {
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
