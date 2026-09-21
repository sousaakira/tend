package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Running a plugin's command is the part with the sharp edges, and they are
// all about where the command comes from and what it is allowed to take with
// it.
//
// A relative command resolves against the plugin's own directory, so a
// manifest can say "./target/release/thing" and mean its own binary rather
// than whatever is first on PATH. A bare name resolves on PATH as usual, since
// that is how "bash scripts/x.sh" is meant to work.
//
// The environment carries the same things a hook gets — where the automation
// socket is, which pane is in question — plus the plugin's own root and its
// two directories. A plugin that wants to change the session calls the socket.

// Environment a plugin's command is given.
const (
	EnvPluginRoot      = "TEND_PLUGIN_ROOT"
	EnvPluginID        = "TEND_PLUGIN_ID"
	EnvPluginConfigDir = "TEND_PLUGIN_CONFIG_DIR"
	EnvPluginStateDir  = "TEND_PLUGIN_STATE_DIR"
	EnvPluginActionID  = "TEND_PLUGIN_ACTION_ID"
	EnvPluginEvent     = "TEND_PLUGIN_EVENT"
	EnvPluginEventJSON = "TEND_PLUGIN_EVENT_JSON"
	EnvPluginContext   = "TEND_PLUGIN_CONTEXT_JSON"
	// EnvPluginClickedURL and EnvPluginLinkHandlerID tell a link handler's
	// action what was clicked and which of its handlers claimed it.
	EnvPluginClickedURL    = "TEND_PLUGIN_CLICKED_URL"
	EnvPluginLinkHandlerID = "TEND_PLUGIN_LINK_HANDLER_ID"
)

// RunTimeout is how long a plugin command may take before it is killed.
//
// An event hook runs on things that happen often, and one that hangs would
// otherwise pile up a process per event until the machine gives out. A build
// is given its own, longer, bound.
const (
	RunTimeout   = 30 * time.Second
	BuildTimeout = 15 * time.Minute
)

// maxOutput is how much of a command's output is kept for reporting. A plugin
// that prints a megabyte of progress should not be able to hold it in memory
// through the session.
const maxOutput = 64 << 10

// cancelGrace is how long a cancelled command has to finish writing before its
// pipes are closed under it and the wait returns.
const cancelGrace = 2 * time.Second

// Context is what a command is told about where it was invoked.
type Context struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	TabID       string `json:"tab_id,omitempty"`
	PaneID      string `json:"pane_id,omitempty"`
	PaneAgent   string `json:"pane_agent,omitempty"`
	PaneState   string `json:"pane_state,omitempty"`
	Dir         string `json:"dir,omitempty"`
}

// Result is how a command went.
type Result struct {
	Command  []string `json:"command"`
	ExitCode int      `json:"exit_code"`
	Output   string   `json:"output,omitempty"`
	TimedOut bool     `json:"timed_out,omitempty"`
	Err      string   `json:"error,omitempty"`
}

// Invocation is one command about to be run.
type Invocation struct {
	Plugin  Installed
	Command []string
	// Dirs are where the plugin's own config and state live. A plugin that
	// keeps settings puts them there rather than in the user's home.
	ConfigDir string
	StateDir  string
	// Env is what the rest of the session adds: the socket path, the marker,
	// the binary's own path.
	Env []string

	ActionID  string
	Event     string
	EventJSON string
	Context   Context
	// ClickedURL and LinkHandlerID are set when a link handler runs.
	ClickedURL    string
	LinkHandlerID string

	Timeout time.Duration
}

// Run starts the command and waits for it.
//
// Output is captured rather than inherited: a plugin's command has no terminal
// of its own, and anything it prints would otherwise land in the middle of the
// session's own log or, worse, in a pane.
func Run(ctx context.Context, inv Invocation) Result {
	timeout := inv.Timeout
	if timeout <= 0 {
		timeout = RunTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	name, err := resolve(inv.Plugin.Root, inv.Command[0])
	if err != nil {
		return Result{Command: inv.Command, ExitCode: -1, Err: err.Error()}
	}

	cmd := exec.CommandContext(ctx, name, inv.Command[1:]...)
	cmd.Dir = inv.Plugin.Root
	cmd.Env = inv.environ()
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.Stdin = nil
	setGroup(cmd)
	cmd.Cancel = func() error { return killGroup(cmd) }
	// Output goes to a buffer, so exec reads it through a pipe and waits for
	// that pipe to close — which a grandchild holding it keeps open long after
	// the command itself is dead. Without this, cancelling a hook did not end
	// the wait on it, and the server's own shutdown waited out a "sleep 30"
	// that had already been killed.
	cmd.WaitDelay = cancelGrace

	runErr := cmd.Run()
	result := Result{Command: inv.Command, Output: trim(out.String())}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		result.TimedOut = true
		result.Err = fmt.Sprintf("killed after %s", timeout)
		return result
	}
	if runErr != nil {
		result.Err = runErr.Error()
		if result.ExitCode == 0 {
			result.ExitCode = -1
		}
	}
	return result
}

// environ builds the command's environment.
func (inv Invocation) environ() []string {
	env := append(os.Environ(), inv.Env...)
	env = append(env,
		EnvPluginRoot+"="+inv.Plugin.Root,
		EnvPluginID+"="+inv.Plugin.ID,
	)
	if inv.ConfigDir != "" {
		env = append(env, EnvPluginConfigDir+"="+inv.ConfigDir)
	}
	if inv.StateDir != "" {
		env = append(env, EnvPluginStateDir+"="+inv.StateDir)
	}
	if inv.ActionID != "" {
		env = append(env, EnvPluginActionID+"="+inv.ActionID)
	}
	if inv.Event != "" {
		env = append(env, EnvPluginEvent+"="+inv.Event)
	}
	if inv.EventJSON != "" {
		env = append(env, EnvPluginEventJSON+"="+inv.EventJSON)
	}
	if inv.ClickedURL != "" {
		env = append(env, EnvPluginClickedURL+"="+inv.ClickedURL)
	}
	if inv.LinkHandlerID != "" {
		env = append(env, EnvPluginLinkHandlerID+"="+inv.LinkHandlerID)
	}
	if data, err := json.Marshal(inv.Context); err == nil {
		env = append(env, EnvPluginContext+"="+string(data))
	}
	return env
}

// resolve turns the first word of a command into something to execute.
//
// A path with a separator in it is the plugin's own file and is taken relative
// to its root; anything else is looked up on PATH. The check that it exists
// happens here so that a missing binary is reported as itself rather than as
// "exec format error" or a shell's own complaint.
func resolve(root, name string) (string, error) {
	if !strings.ContainsRune(name, os.PathSeparator) && !strings.HasPrefix(name, ".") {
		path, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("plugin: %s is not on PATH", name)
		}
		return path, nil
	}
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("plugin: %s: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("plugin: %s is a directory", path)
	}
	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("plugin: %s is not executable", path)
	}
	return path, nil
}

// ResolvePaneCommand turns a plugin pane's command into one the server can
// start, with its relative program made absolute against the plugin's root.
//
// The pane's process is started by the server like any other, so it cannot be
// handed a working directory and a relative name the way Run does: by the time
// it runs, nothing remembers which plugin it came from.
func ResolvePaneCommand(p Installed, command []string) ([]string, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("plugin: %s offers a pane with no command", p.ID)
	}
	name, err := resolve(p.Root, command[0])
	if err != nil {
		return nil, err
	}
	return append([]string{name}, command[1:]...), nil
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxOutput {
		return s[:maxOutput] + "\n… (output cut)"
	}
	return s
}

// Build runs a plugin's build steps, in order, writing their output to out as
// it arrives.
//
// A plugin that needs building — the owner's sidebar is a Rust program — is
// linked from a checkout, and the thing the manifest points at does not exist
// until this has run. herdr builds on install and refuses to install when a
// build fails, because a plugin whose binary is missing is one whose every
// action fails later, somewhere else.
func Build(installed Installed, env []string, out io.Writer) error {
	steps := ForThisPlatform(installed.Build, func(s Step) []string { return s.Platforms })
	for i, step := range steps {
		fmt.Fprintf(out, "%s: build %d of %d: %s\n",
			installed.ID, i+1, len(steps), strings.Join(step.Command, " "))
		if err := runStreaming(installed, step.Command, env, out); err != nil {
			return fmt.Errorf("plugin %s: build %d of %d failed: %w",
				installed.ID, i+1, len(steps), err)
		}
	}
	return nil
}

// runStreaming runs one command with its output going straight to out, which
// is what a build wants: a compiler's progress is the point of watching it.
func runStreaming(installed Installed, command, env []string, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), BuildTimeout)
	defer cancel()

	name, err := resolve(installed.Root, command[0])
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, name, command[1:]...)
	cmd.Dir = installed.Root
	cmd.Env = append(append(os.Environ(), env...),
		EnvPluginRoot+"="+installed.Root, EnvPluginID+"="+installed.ID)
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = out, out
	setGroup(cmd)
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = cancelGrace

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("killed after %s", BuildTimeout)
		}
		return err
	}
	return nil
}
