package integration

import (
	"path/filepath"
	"strings"
)

// legacyHerdrHookInstallName is the filename herdr wrote; tend still matches
// those command strings so install/uninstall migrates herdr entries.
const legacyHerdrHookInstallName = "herdr-agent-state.sh"

// ShellSingleQuote wraps value in single quotes for safe inclusion in a POSIX
// shell command line.
func ShellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// HookCommand builds the shell invocation agents store in JSON hook configs.
// tend installs tend-agent-state.sh; the path is quoted so spaces and quotes
// in the install directory cannot break the command. action may be empty.
func HookCommand(hookPath, action string) string {
	command := "bash " + ShellSingleQuote(hookPath)
	if action != "" {
		command += " " + action
	}
	return command
}

// LegacyBashHookCommand matches the command string written by older installs.
// Uninstall walks every variant so a rename of the hook file does not leave
// stale entries behind.
func LegacyBashHookCommand(hookPath, action string) string {
	return HookCommand(hookPath, action)
}

// HookCommandVariants lists every hook command string uninstall must try,
// including the herdr-named sibling in the same directory.
func HookCommandVariants(hookPath, action string) []string {
	commands := []string{HookCommand(hookPath, action)}
	pushUniqueCommand(&commands, LegacyBashHookCommand(hookPath, action))
	dir := filepath.Dir(hookPath)
	for _, name := range []string{ClaudeHookInstallName, legacyHerdrHookInstallName} {
		sibling := filepath.Join(dir, name)
		if sibling == hookPath {
			continue
		}
		pushUniqueCommand(&commands, HookCommand(sibling, action))
		pushUniqueCommand(&commands, LegacyBashHookCommand(sibling, action))
	}
	return commands
}

func pushUniqueCommand(commands *[]string, command string) {
	for _, existing := range *commands {
		if existing == command {
			return
		}
	}
	*commands = append(*commands, command)
}
