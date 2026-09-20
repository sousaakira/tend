package integration

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/codex/tend-agent-state.sh
var codexHookAsset string

// CodexHookInstallName is the filename written under ~/.codex/.
const CodexHookInstallName = "tend-agent-state.sh"

// CodexIntegrationVersion is stamped in the hook header; bump when the asset
// changes in a way that needs reinstall.
const CodexIntegrationVersion = 8

// CodexInstallPaths are the files InstallCodex wrote or updated.
type CodexInstallPaths struct {
	HookPath   string
	HooksPath  string
	ConfigPath string
}

// CodexUninstallResult reports what UninstallCodex removed.
// Uninstall leaves config.toml alone — herdr does not clear hooks = true so a
// reinstall does not need to re-enable the feature flag.
type CodexUninstallResult struct {
	HookPath        string
	HooksPath       string
	ConfigPath      string
	RemovedHookFile bool
	UpdatedHooks    bool
}

// InstallCodex writes the SessionStart hook, updates hooks.json, and enables
// [features] hooks = true in config.toml.
func InstallCodex() (CodexInstallPaths, error) {
	dir, err := CodexDir()
	if err != nil {
		return CodexInstallPaths{}, err
	}
	if err := requireConfigDirHint(dir, "codex config directory", "codex"); err != nil {
		return CodexInstallPaths{}, err
	}

	hookPath := filepath.Join(dir, CodexHookInstallName)
	if err := os.WriteFile(hookPath, []byte(codexHookAsset), 0o755); err != nil {
		return CodexInstallPaths{}, err
	}
	if err := MakeExecutable(hookPath); err != nil {
		return CodexInstallPaths{}, err
	}

	hooksPath := filepath.Join(dir, "hooks.json")
	existing := "{}"
	if data, err := os.ReadFile(hooksPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return CodexInstallPaths{}, err
	}

	hooksFile, err := ParseJSONObject(existing, hooksPath, "codex hooks file")
	if err != nil {
		return CodexInstallPaths{}, err
	}
	hooks, err := EnsureHooksObject(hooksFile, hooksPath, "codex hooks file", "codex hooks file hooks")
	if err != nil {
		return CodexInstallPaths{}, err
	}
	for _, rem := range []struct {
		event  string
		action string
	}{
		{"PermissionRequest", "blocked"},
		{"SessionStart", "idle"},
		{"UserPromptSubmit", "working"},
		{"PreToolUse", "working"},
		{"Stop", "idle"},
		{"SessionStart", "session"},
	} {
		if _, err := RemoveHookCommands(hooks, rem.event, hookPath, rem.action); err != nil {
			return CodexInstallPaths{}, err
		}
	}
	if err := EnsureCommandHook(hooks, "SessionStart", HookCommand(hookPath, "session"), 10, ""); err != nil {
		return CodexInstallPaths{}, err
	}
	if err := WriteJSONPretty(hooksPath, hooksFile); err != nil {
		return CodexInstallPaths{}, err
	}

	configPath := filepath.Join(dir, "config.toml")
	existingConfig := ""
	if data, err := os.ReadFile(configPath); err == nil {
		existingConfig = string(data)
	} else if !os.IsNotExist(err) {
		return CodexInstallPaths{}, err
	}
	newConfig := BuildCodexConfigWithHooks(existingConfig)
	if newConfig != existingConfig {
		if err := os.WriteFile(configPath, []byte(newConfig), 0o644); err != nil {
			return CodexInstallPaths{}, err
		}
	}

	return CodexInstallPaths{
		HookPath:   hookPath,
		HooksPath:  hooksPath,
		ConfigPath: configPath,
	}, nil
}

// UninstallCodex removes tend/herdr hook entries and the hook file. config.toml
// is left unchanged.
func UninstallCodex() (CodexUninstallResult, error) {
	dir, err := CodexDir()
	if err != nil {
		return CodexUninstallResult{}, err
	}
	hookPath := filepath.Join(dir, CodexHookInstallName)
	hooksPath := filepath.Join(dir, "hooks.json")
	configPath := filepath.Join(dir, "config.toml")
	result := CodexUninstallResult{
		HookPath:   hookPath,
		HooksPath:  hooksPath,
		ConfigPath: configPath,
	}

	if data, err := os.ReadFile(hooksPath); err == nil {
		hooksFile, err := ParseJSONObject(string(data), hooksPath, "codex hooks file")
		if err != nil {
			return result, err
		}
		if hooks, ok, err := HooksObjectIfPresent(hooksFile, hooksPath, "codex hooks file", "codex hooks file hooks"); err != nil {
			return result, err
		} else if ok {
			updated := false
			for _, rem := range []struct {
				event  string
				action string
			}{
				{"SessionStart", "idle"},
				{"SessionStart", "session"},
				{"UserPromptSubmit", "working"},
				{"PreToolUse", "working"},
				{"PermissionRequest", "blocked"},
				{"Stop", "idle"},
			} {
				got, err := RemoveHookCommands(hooks, rem.event, hookPath, rem.action)
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

	removed, err := RemoveFileIfExists(hookPath)
	if err != nil {
		return result, err
	}
	result.RemovedHookFile = removed
	return result, nil
}
