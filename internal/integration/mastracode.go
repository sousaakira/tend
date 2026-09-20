package integration

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/mastracode/tend-agent-state.sh
var mastracodeHookAsset string

// MastracodeHookInstallName is the filename under ~/.mastracode/hooks/.
const MastracodeHookInstallName = "tend-agent-state.sh"

// MastracodeIntegrationVersion is stamped in the hook header.
const MastracodeIntegrationVersion = 2

// MastracodeHookTimeoutMS is the flat-hook timeout MastraCode expects.
const MastracodeHookTimeoutMS = 10_000

const mastracodeHookDescription = "Report MastraCode agent state to Tend"

var mastracodeHookEvents = []struct{ Event, Action string }{
	{"SessionStart", "session"},
	{"UserPromptSubmit", "working"},
	{"AgentStart", "working"},
	{"PreToolUse", "working"},
	{"PermissionRequest", "blocked"},
	{"PermissionResult", "working"},
	{"SubagentStart", "working"},
	{"SubagentEnd", "working"},
	{"Interrupt", "idle"},
	{"AgentEnd", "idle"},
	{"Stop", "idle"},
}

var mastracodeRemovedHookEvents = []struct{ Event, Action string }{
	{"SessionStart", "idle"},
	{"SessionEnd", "release"},
}

// MastracodeInstallPaths are the files InstallMastracode wrote or updated.
type MastracodeInstallPaths struct {
	HookPath  string
	HooksPath string
}

// MastracodeUninstallResult reports what UninstallMastracode removed.
type MastracodeUninstallResult struct {
	HookPath        string
	HooksPath       string
	RemovedHookFile bool
	UpdatedHooks    bool
}

// InstallMastracode writes flat command hooks into ~/.mastracode/hooks.json.
// MastraCode uses a top-level event→array map (no nested "hooks" object).
func InstallMastracode() (MastracodeInstallPaths, error) {
	home, err := MastracodeDir()
	if err != nil {
		return MastracodeInstallPaths{}, err
	}
	hookDir := filepath.Join(home, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		return MastracodeInstallPaths{}, err
	}

	hookPath := filepath.Join(hookDir, MastracodeHookInstallName)
	if err := os.WriteFile(hookPath, []byte(mastracodeHookAsset), 0o755); err != nil {
		return MastracodeInstallPaths{}, err
	}
	if err := MakeExecutable(hookPath); err != nil {
		return MastracodeInstallPaths{}, err
	}

	hooksPath := filepath.Join(home, "hooks.json")
	existing := "{}"
	if data, err := os.ReadFile(hooksPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return MastracodeInstallPaths{}, err
	}

	hooks, err := ParseJSONObject(existing, hooksPath, "mastracode hooks file")
	if err != nil {
		return MastracodeInstallPaths{}, err
	}

	for _, rem := range mastracodeRemovedHookEvents {
		if _, err := RemoveFlatCommandHook(hooks, rem.Event, HookCommand(hookPath, rem.Action)); err != nil {
			return MastracodeInstallPaths{}, err
		}
	}
	for _, ev := range mastracodeHookEvents {
		if _, err := RemoveFlatCommandHook(hooks, ev.Event, HookCommand(hookPath, ev.Action)); err != nil {
			return MastracodeInstallPaths{}, err
		}
		if err := EnsureFlatCommandHook(hooks, ev.Event, HookCommand(hookPath, ev.Action), MastracodeHookTimeoutMS, mastracodeHookDescription); err != nil {
			return MastracodeInstallPaths{}, err
		}
	}

	if err := WriteJSONPretty(hooksPath, hooks); err != nil {
		return MastracodeInstallPaths{}, err
	}

	return MastracodeInstallPaths{HookPath: hookPath, HooksPath: hooksPath}, nil
}

// UninstallMastracode removes tend flat hooks and the hook file.
func UninstallMastracode() (MastracodeUninstallResult, error) {
	home, err := MastracodeDir()
	if err != nil {
		return MastracodeUninstallResult{}, err
	}
	hookPath := filepath.Join(home, "hooks", MastracodeHookInstallName)
	hooksPath := filepath.Join(home, "hooks.json")
	result := MastracodeUninstallResult{HookPath: hookPath, HooksPath: hooksPath}

	if data, err := os.ReadFile(hooksPath); err == nil {
		hooks, err := ParseJSONObject(string(data), hooksPath, "mastracode hooks file")
		if err != nil {
			return result, err
		}
		updated := false
		for _, rem := range append(append([]struct{ Event, Action string }{}, mastracodeHookEvents...), mastracodeRemovedHookEvents...) {
			got, err := RemoveFlatCommandHook(hooks, rem.Event, HookCommand(hookPath, rem.Action))
			if err != nil {
				return result, err
			}
			updated = updated || got
		}
		if updated {
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
