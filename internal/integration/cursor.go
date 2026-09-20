package integration

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/cursor/tend-agent-state.sh
var cursorHookAsset string

// CursorHookInstallName is the filename written under ~/.cursor/.
const CursorHookInstallName = "tend-agent-state.sh"

// CursorIntegrationVersion is stamped in the hook header; bump when the asset
// changes in a way that needs reinstall.
const CursorIntegrationVersion = 1

// CursorInstallPaths are the files InstallCursor wrote or updated.
type CursorInstallPaths struct {
	HookPath  string
	HooksPath string
}

// CursorUninstallResult reports what UninstallCursor removed.
type CursorUninstallResult struct {
	HookPath        string
	HooksPath       string
	RemovedHookFile bool
	UpdatedHooks    bool
}

// InstallCursor writes the sessionStart hook into Cursor's hooks.json.
// Cursor uses the minimal `{ "command": "..." }` shape, not nested groups.
func InstallCursor() (CursorInstallPaths, error) {
	dir, err := CursorDir()
	if err != nil {
		return CursorInstallPaths{}, err
	}
	if err := requireConfigDirHint(dir, "cursor config directory", "cursor agent cli"); err != nil {
		return CursorInstallPaths{}, err
	}

	hookPath := filepath.Join(dir, CursorHookInstallName)
	if err := os.WriteFile(hookPath, []byte(cursorHookAsset), 0o755); err != nil {
		return CursorInstallPaths{}, err
	}
	if err := MakeExecutable(hookPath); err != nil {
		return CursorInstallPaths{}, err
	}

	hooksPath := filepath.Join(dir, "hooks.json")
	existing := `{"version":1}`
	if data, err := os.ReadFile(hooksPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return CursorInstallPaths{}, err
	}

	hooksFile, err := ParseJSONObject(existing, hooksPath, "cursor hooks file")
	if err != nil {
		return CursorInstallPaths{}, err
	}
	if _, ok := hooksFile["version"]; !ok {
		hooksFile["version"] = float64(1)
	}

	hooks, err := EnsureHooksObject(hooksFile, hooksPath, "cursor hooks file", "cursor hooks file hooks")
	if err != nil {
		return CursorInstallPaths{}, err
	}
	sessionCommand := HookCommand(hookPath, "session")
	for _, event := range []string{
		"beforeSubmitPrompt", "beforeShellExecution", "beforeMCPExecution",
		"stop", "sessionEnd",
	} {
		if _, err := RemoveSimpleCommandHook(hooks, event, sessionCommand); err != nil {
			return CursorInstallPaths{}, err
		}
	}
	if err := EnsureSimpleCommandHook(hooks, "sessionStart", sessionCommand); err != nil {
		return CursorInstallPaths{}, err
	}
	if err := WriteJSONPretty(hooksPath, hooksFile); err != nil {
		return CursorInstallPaths{}, err
	}

	return CursorInstallPaths{HookPath: hookPath, HooksPath: hooksPath}, nil
}

// UninstallCursor removes tend session hooks and the hook file.
func UninstallCursor() (CursorUninstallResult, error) {
	dir, err := CursorDir()
	if err != nil {
		return CursorUninstallResult{}, err
	}
	hookPath := filepath.Join(dir, CursorHookInstallName)
	hooksPath := filepath.Join(dir, "hooks.json")
	result := CursorUninstallResult{HookPath: hookPath, HooksPath: hooksPath}

	if data, err := os.ReadFile(hooksPath); err == nil {
		hooksFile, err := ParseJSONObject(string(data), hooksPath, "cursor hooks file")
		if err != nil {
			return result, err
		}
		if hooks, ok, err := HooksObjectIfPresent(hooksFile, hooksPath, "cursor hooks file", "cursor hooks file hooks"); err != nil {
			return result, err
		} else if ok {
			sessionCommand := HookCommand(hookPath, "session")
			updated := false
			for _, event := range []string{
				"sessionStart", "beforeSubmitPrompt", "beforeShellExecution",
				"beforeMCPExecution", "stop", "sessionEnd",
			} {
				got, err := RemoveSimpleCommandHook(hooks, event, sessionCommand)
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
