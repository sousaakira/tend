package integration

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

//go:embed assets/letta/tend-agent-session.sh
var lettaHookAsset string

// LettaHookInstallName is the session hook under ~/.letta/hooks/.
const LettaHookInstallName = "tend-agent-session.sh"

// LettaIntegrationVersion is stamped in the hook header.
const LettaIntegrationVersion = 1

// LettaHookTimeoutMS is the SessionStart timeout Letta expects (milliseconds).
const LettaHookTimeoutMS = 10_000

// LettaInstallPaths are the files InstallLetta wrote or updated.
type LettaInstallPaths struct {
	HookPath     string
	SettingsPath string
}

// LettaUninstallResult reports what UninstallLetta removed.
type LettaUninstallResult struct {
	HookPath        string
	SettingsPath    string
	RemovedHookFile bool
	UpdatedSettings bool
}

// InstallLetta atomically publishes the session hook and settings.json.
// SessionStart stdout is injected into Letta's next user message, so the hook
// stays quiet and the install stages both files before renaming into place.
func InstallLetta() (LettaInstallPaths, error) {
	dir, err := LettaDir()
	if err != nil {
		return LettaInstallPaths{}, err
	}
	if err := requireConfigDir(dir, "letta code"); err != nil {
		return LettaInstallPaths{}, err
	}

	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return LettaInstallPaths{}, err
	}

	hookPath := filepath.Join(hooksDir, LettaHookInstallName)
	settingsPath := filepath.Join(dir, "settings.json")

	existing := "{}"
	if data, err := os.ReadFile(settingsPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return LettaInstallPaths{}, err
	}

	settings, err := ParseJSONObject(existing, settingsPath, "letta settings")
	if err != nil {
		return LettaInstallPaths{}, err
	}
	hooks, err := EnsureHooksObject(settings, settingsPath, "letta settings", "letta settings hooks")
	if err != nil {
		return LettaInstallPaths{}, err
	}
	if _, err := RemoveHookCommands(hooks, "SessionStart", hookPath, "session"); err != nil {
		return LettaInstallPaths{}, err
	}
	if err := ensureLettaSessionHook(hooks, HookCommand(hookPath, "session")); err != nil {
		return LettaInstallPaths{}, err
	}

	settingsBytes, err := MarshalJSONPretty(settings)
	if err != nil {
		return LettaInstallPaths{}, err
	}
	settingsBytes = append(settingsBytes, '\n')

	hookStaged, hookBackup, err := prepareLettaInstallFile(hookPath, []byte(lettaHookAsset), true, false)
	if err != nil {
		return LettaInstallPaths{}, err
	}
	settingsStaged, settingsBackup, err := prepareLettaInstallFile(settingsPath, settingsBytes, false, true)
	if err != nil {
		_, _ = RemoveFileIfExists(hookStaged)
		return LettaInstallPaths{}, err
	}

	hookHadOriginal, err := publishLettaInstallFile(hookPath, hookStaged, hookBackup)
	if err != nil {
		_, _ = RemoveFileIfExists(hookStaged)
		_, _ = RemoveFileIfExists(settingsStaged)
		return LettaInstallPaths{}, err
	}

	settingsHadOriginal, err := publishLettaInstallFile(settingsPath, settingsStaged, settingsBackup)
	if err != nil {
		_ = rollbackLettaInstallFile(hookPath, hookBackup, hookHadOriginal)
		_, _ = RemoveFileIfExists(settingsStaged)
		return LettaInstallPaths{}, err
	}

	if hookHadOriginal {
		_, _ = RemoveFileIfExists(hookBackup)
	}
	if settingsHadOriginal {
		_, _ = RemoveFileIfExists(settingsBackup)
	}

	return LettaInstallPaths{HookPath: hookPath, SettingsPath: settingsPath}, nil
}

// UninstallLetta removes SessionStart tend entries and the session hook file.
func UninstallLetta() (LettaUninstallResult, error) {
	dir, err := LettaDir()
	if err != nil {
		return LettaUninstallResult{}, err
	}
	hookPath := filepath.Join(dir, "hooks", LettaHookInstallName)
	settingsPath := filepath.Join(dir, "settings.json")
	result := LettaUninstallResult{HookPath: hookPath, SettingsPath: settingsPath}

	if data, err := os.ReadFile(settingsPath); err == nil {
		settings, err := ParseJSONObject(string(data), settingsPath, "letta settings")
		if err != nil {
			return result, err
		}
		hooks, ok, err := HooksObjectIfPresent(settings, settingsPath, "letta settings", "letta settings hooks")
		if err != nil {
			return result, err
		}
		if ok {
			updated, err := RemoveHookCommands(hooks, "SessionStart", hookPath, "session")
			if err != nil {
				return result, err
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

func ensureLettaSessionHook(hooks map[string]any, command string) error {
	entries, err := hookEntries(hooks, "SessionStart")
	if err != nil {
		return err
	}
	hooks["SessionStart"] = append(entries, map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": command,
				"timeout": uint64(LettaHookTimeoutMS),
				"quiet":   true,
			},
		},
	})
	return nil
}

func lettaInstallArtifactPath(target, role string) (string, error) {
	dir := filepath.Dir(target)
	base := filepath.Base(target)
	if base == "." || base == "/" || base == "" {
		return "", fmt.Errorf("invalid install path: %s", target)
	}
	name := base + ".tend-install-" + strconv.Itoa(os.Getpid()) + "-" + role
	return filepath.Join(dir, name), nil
}

func prepareLettaInstallFile(target string, contents []byte, executable, preservePermissions bool) (staged, backup string, err error) {
	info, err := os.Lstat(target)
	if err == nil && !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("install target is not a file: %s", target)
	}
	if err != nil && !os.IsNotExist(err) {
		return "", "", err
	}

	staged, err = lettaInstallArtifactPath(target, "staged")
	if err != nil {
		return "", "", err
	}
	backup, err = lettaInstallArtifactPath(target, "backup")
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(staged); err == nil {
		return "", "", fmt.Errorf("stale install artifact exists for %s", target)
	} else if !os.IsNotExist(err) {
		return "", "", err
	}
	if _, err := os.Stat(backup); err == nil {
		return "", "", fmt.Errorf("stale install artifact exists for %s", target)
	} else if !os.IsNotExist(err) {
		return "", "", err
	}

	mode := os.FileMode(0o644)
	if preservePermissions && info != nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(staged, contents, mode); err != nil {
		return "", "", err
	}
	if preservePermissions && info != nil {
		if err := os.Chmod(staged, info.Mode().Perm()); err != nil {
			_, _ = RemoveFileIfExists(staged)
			return "", "", err
		}
	}
	if executable {
		if err := MakeExecutable(staged); err != nil {
			_, _ = RemoveFileIfExists(staged)
			return "", "", err
		}
	}
	return staged, backup, nil
}

func publishLettaInstallFile(target, staged, backup string) (hadOriginal bool, err error) {
	if _, err := os.Stat(target); err == nil {
		hadOriginal = true
		if err := os.Rename(target, backup); err != nil {
			return false, err
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}

	if err := os.Rename(staged, target); err != nil {
		if hadOriginal {
			_ = os.Rename(backup, target)
		}
		return hadOriginal, err
	}
	return hadOriginal, nil
}

func rollbackLettaInstallFile(target, backup string, hadOriginal bool) error {
	_, _ = RemoveFileIfExists(target)
	if hadOriginal {
		return os.Rename(backup, target)
	}
	return nil
}
