package integration

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/antigravity_cli/tend-agent-state.sh
var antigravityCLIHookAsset string

// AntigravityCLIHookInstallName is the hook under ~/.gemini/config/hooks/.
const AntigravityCLIHookInstallName = "tend-agent-state.sh"

// AntigravityCLIIntegrationVersion is stamped in the hook header.
const AntigravityCLIIntegrationVersion = 3

// AntigravityCLIHookBlockName is the named hooks.json block Antigravity keys
// by. Renamed from "herdr"; install/uninstall also clear a leftover herdr block.
const AntigravityCLIHookBlockName = "tend"

const legacyAntigravityCLIHookBlockName = "herdr"

// AntigravityCLIHookTimeoutSec is the flat-handler timeout (seconds).
const AntigravityCLIHookTimeoutSec = 10

// Session-only: PreInvocation carries conversationId. Antigravity has no
// blocked event; PostInvocation is skipped on interruption; Stop is end-of-turn
// rather than process exit — screen detection owns agent state instead.
var antigravityCLIHookEvents = []struct{ Event, Action string }{
	{"PreInvocation", "session"},
}

// AntigravityCLIInstallPaths are the files InstallAntigravityCLI wrote.
type AntigravityCLIInstallPaths struct {
	HookPath  string
	HooksPath string
}

// AntigravityCLIUninstallResult reports what UninstallAntigravityCLI removed.
type AntigravityCLIUninstallResult struct {
	HookPath        string
	HooksPath       string
	RemovedHookFile bool
	UpdatedHooks    bool
}

// InstallAntigravityCLI writes the hook and a tend-owned named block in hooks.json.
// Handlers are a flat list; the matcher/hooks wrapper is only valid for tool
// events and would invalidate the whole file here.
func InstallAntigravityCLI() (AntigravityCLIInstallPaths, error) {
	dir, err := AntigravityCLIDir()
	if err != nil {
		return AntigravityCLIInstallPaths{}, err
	}
	if err := requireConfigDir(dir, "antigravity cli"); err != nil {
		return AntigravityCLIInstallPaths{}, err
	}

	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return AntigravityCLIInstallPaths{}, err
	}

	hookPath := filepath.Join(hooksDir, AntigravityCLIHookInstallName)
	if err := os.WriteFile(hookPath, []byte(antigravityCLIHookAsset), 0o755); err != nil {
		return AntigravityCLIInstallPaths{}, err
	}
	if err := MakeExecutable(hookPath); err != nil {
		return AntigravityCLIInstallPaths{}, err
	}

	hooksPath := filepath.Join(dir, "hooks.json")
	existing := "{}"
	if data, err := os.ReadFile(hooksPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return AntigravityCLIInstallPaths{}, err
	}

	hooks, err := ParseJSONObject(existing, hooksPath, "antigravity cli hooks file")
	if err != nil {
		return AntigravityCLIInstallPaths{}, err
	}

	// The tend block is tend-owned: rewrite wholesale and leave other named hooks.
	hooks[AntigravityCLIHookBlockName] = antigravityCLIHookBlock(hookPath)
	delete(hooks, legacyAntigravityCLIHookBlockName)

	if err := WriteJSONPretty(hooksPath, hooks); err != nil {
		return AntigravityCLIInstallPaths{}, err
	}

	return AntigravityCLIInstallPaths{HookPath: hookPath, HooksPath: hooksPath}, nil
}

func antigravityCLIHookBlock(hookPath string) map[string]any {
	block := map[string]any{}
	for _, ev := range antigravityCLIHookEvents {
		block[ev.Event] = []any{
			map[string]any{
				"type":    "command",
				"command": HookCommand(hookPath, ev.Action),
				"timeout": uint64(AntigravityCLIHookTimeoutSec),
			},
		}
	}
	return block
}

// UninstallAntigravityCLI removes the tend (and legacy herdr) named block and hook file.
func UninstallAntigravityCLI() (AntigravityCLIUninstallResult, error) {
	dir, err := AntigravityCLIDir()
	if err != nil {
		return AntigravityCLIUninstallResult{}, err
	}
	hookPath := filepath.Join(dir, "hooks", AntigravityCLIHookInstallName)
	hooksPath := filepath.Join(dir, "hooks.json")
	result := AntigravityCLIUninstallResult{HookPath: hookPath, HooksPath: hooksPath}

	if data, err := os.ReadFile(hooksPath); err == nil {
		hooks, err := ParseJSONObject(string(data), hooksPath, "antigravity cli hooks file")
		if err != nil {
			return result, err
		}
		_, hadTend := hooks[AntigravityCLIHookBlockName]
		_, hadLegacy := hooks[legacyAntigravityCLIHookBlockName]
		if hadTend || hadLegacy {
			delete(hooks, AntigravityCLIHookBlockName)
			delete(hooks, legacyAntigravityCLIHookBlockName)
			if err := WriteJSONPretty(hooksPath, hooks); err != nil {
				return result, err
			}
			result.UpdatedHooks = true
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
