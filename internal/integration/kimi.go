package integration

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/kimi/tend-agent-state.sh
var kimiHookAsset string

// KimiHookInstallName is the filename written under ~/.kimi-code/hooks/.
const KimiHookInstallName = "tend-agent-state.sh"

// KimiIntegrationVersion is stamped in the hook header.
const KimiIntegrationVersion = 7

const (
	kimiAskUserQuestionMatcher = "^AskUserQuestion$"
	kimiOtherToolMatcher       = "^(?!AskUserQuestion$).*$"
)

// kimiHookEvents mirrors herdr's KIMI_HOOK_EVENTS: AskUserQuestion is blocked
// until the question finishes; every other PreToolUse reports working.
var kimiHookEvents = []KimiHookEvent{
	{Event: "SessionStart", Action: "session"},
	{Event: "UserPromptSubmit", Action: "working"},
	{Event: "PreToolUse", Matcher: kimiOtherToolMatcher, Action: "working"},
	{Event: "PreToolUse", Matcher: kimiAskUserQuestionMatcher, Action: "blocked"},
	{Event: "PostToolUse", Matcher: kimiAskUserQuestionMatcher, Action: "working"},
	{Event: "PostToolUseFailure", Matcher: kimiAskUserQuestionMatcher, Action: "working"},
	{Event: "SubagentStart", Action: "working"},
	{Event: "PreCompact", Action: "working"},
	{Event: "PermissionRequest", Action: "blocked"},
	{Event: "PermissionResult", Action: "working"},
	{Event: "Stop", Action: "idle"},
	{Event: "Interrupt", Action: "idle"},
}

// KimiInstallPaths are the files InstallKimi wrote or updated.
type KimiInstallPaths struct {
	HookPath   string
	ConfigPath string
}

// KimiUninstallResult reports what UninstallKimi removed.
type KimiUninstallResult struct {
	HookPath        string
	ConfigPath      string
	RemovedHookFile bool
	UpdatedConfig   bool
}

// InstallKimi writes the hook script and a marked [[hooks]] block in config.toml.
func InstallKimi() (KimiInstallPaths, error) {
	dir, err := KimiDir()
	if err != nil {
		return KimiInstallPaths{}, err
	}
	if err := requireConfigDir(dir, "kimi code"); err != nil {
		return KimiInstallPaths{}, err
	}

	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return KimiInstallPaths{}, err
	}

	hookPath := filepath.Join(hooksDir, KimiHookInstallName)
	if err := os.WriteFile(hookPath, []byte(kimiHookAsset), 0o755); err != nil {
		return KimiInstallPaths{}, err
	}
	if err := MakeExecutable(hookPath); err != nil {
		return KimiInstallPaths{}, err
	}

	configPath := filepath.Join(dir, "config.toml")
	existing := ""
	if data, err := os.ReadFile(configPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return KimiInstallPaths{}, err
	}

	newConfig := BuildKimiConfigWithHooks(existing, hookPath, kimiHookEvents)
	if newConfig != existing {
		if err := os.WriteFile(configPath, []byte(newConfig), 0o644); err != nil {
			return KimiInstallPaths{}, err
		}
	}

	return KimiInstallPaths{HookPath: hookPath, ConfigPath: configPath}, nil
}

// UninstallKimi removes the tend/herdr kimi config block and the hook file.
func UninstallKimi() (KimiUninstallResult, error) {
	dir, err := KimiDir()
	if err != nil {
		return KimiUninstallResult{}, err
	}
	hookPath := filepath.Join(dir, "hooks", KimiHookInstallName)
	configPath := filepath.Join(dir, "config.toml")
	result := KimiUninstallResult{HookPath: hookPath, ConfigPath: configPath}

	if data, err := os.ReadFile(configPath); err == nil {
		newConfig := RemoveKimiConfigBlock(string(data))
		if newConfig != string(data) {
			if err := os.WriteFile(configPath, []byte(newConfig), 0o644); err != nil {
				return result, err
			}
			result.UpdatedConfig = true
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
