package server

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/session"
)

// The user's own commands, bound to keys in [[keys.command]]: herdr's custom
// commands (`app/custom_commands.rs`). The key is the client's; the command
// runs here, because the machine it means is the one the panes are on — a
// `git pull` bound to a key pulls the checkout the pane is in, not one on the
// laptop the client happens to be attached from.

// ErrUnknownCommandType means a type [[keys.command]] does not have.
var ErrUnknownCommandType = errors.New("server: unknown command type")

// RunCommand runs a user's command from a pane: in the background (shell), in
// a pane of its own that closes when it is done (pane, and popup, which tend
// runs as a pane), or as an installed plugin's action. It returns the pane it
// opened, if it opened one.
func (s *Server) RunCommand(from session.PaneID, kind, command string, size ...string) (session.PaneID, error) {
	if strings.TrimSpace(command) == "" {
		return 0, errors.New("server: the command is empty")
	}
	env, dir := s.commandEnv(from)
	switch kind {
	case "", config.CommandShell:
		return 0, s.runDetached(command, env, dir)
	case config.CommandPopup:
		if from == 0 {
			return 0, fmt.Errorf("%w: no pane to open it over", session.ErrNoSuchPane)
		}
		var width, height string
		if len(size) == 2 {
			width, height = size[0], size[1]
		}
		// A popup over the pane's tab, herdr's spawn_custom_popup_command.
		// Titled by the command's first line: the frame is one row.
		title, _, _ := strings.Cut(strings.TrimSpace(command), "\n")
		return s.OpenPopup(from, PaneSpec{
			Command: []string{"/bin/sh", "-c", command},
			Env:     env,
			Dir:     dir,
			Title:   title,
		}, width, height)
	case config.CommandPane:
		if from == 0 {
			return 0, fmt.Errorf("%w: no pane to open it beside", session.ErrNoSuchPane)
		}
		// Beside the pane it came from, as herdr splits the focused one; the
		// client zooms it, so where exactly matters only once it is unzoomed.
		return s.SplitPane(from, session.Columns, PaneSpec{
			Command:     []string{"/bin/sh", "-c", command},
			Env:         env,
			Dir:         dir,
			Title:       command,
			Named:       true,
			CloseOnExit: true,
		})
	case config.CommandPluginAction:
		return 0, s.runPluginAction(from, command)
	}
	return 0, fmt.Errorf("%w: %q", ErrUnknownCommandType, kind)
}

// runDetached starts a background command and forgets it, as herdr does:
// its output goes nowhere, and it may outlive the key press by as long as it
// likes. A login shell (-l), as herdr's, so it finds what the user's own
// shell would.
func (s *Server) runDetached(command string, env []string, dir string) error {
	cmd := exec.Command("/bin/sh", "-lc", command)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Waited for only so it does not linger as a zombie; nobody reads the
	// answer, and the server does not wait for it to stop.
	go func() {
		if err := cmd.Wait(); err != nil {
			s.logf("command %q: %v", command, err)
		}
	}()
	return nil
}

// runPluginAction invokes an installed plugin's action by id, in the
// background, as herdr's plugin_action command type does.
func (s *Server) runPluginAction(from session.PaneID, id string) error {
	host := s.cfg.Plugins
	if host == nil {
		return errors.New("server: this server runs no plugins")
	}
	installed, action, err := host.Registry.Action(strings.TrimSpace(id))
	if err != nil {
		return err
	}
	inv := host.Invocation(installed, action.Command)
	inv.ActionID = action.ID
	inv.Context = s.pluginContext(from)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if result := host.RunPlugin(s.context(), inv); result.Err != "" {
			s.logf("plugin %s: action %s: %s", installed.ID, action.ID, result.Err)
		}
	}()
	return nil
}

// commandEnv is what a command the server runs for the user is told, herdr's
// `custom_command_env`: how to call back on the automation socket, and which
// space, tab and pane it was run from. It runs in that pane's directory as
// the pane has it now — where the user cd'd to, not where the pane started.
func (s *Server) commandEnv(pane session.PaneID) ([]string, string) {
	env := append(os.Environ(), s.cfg.CommandEnv...)
	var tab session.TabID
	var ws session.WorkspaceID
	var rt *paneRuntime
	dir := ""
	s.mu.Lock()
	for _, w := range s.session.Workspaces() {
		for _, t := range w.Tabs() {
			if p, ok := t.Pane(pane); ok {
				tab, ws, dir = t.ID, w.ID, p.Dir
			}
		}
	}
	rt = s.runtimes[pane]
	s.mu.Unlock()

	// After the lock: asking the terminal reads /proc, which is I/O.
	if rt != nil {
		if now := rt.pty.Cwd(); now != "" {
			dir = now
		}
	}
	if ws != 0 {
		env = append(env, "TEND_ACTIVE_WORKSPACE_ID=w_"+itoa(uint64(ws)))
	}
	if tab != 0 {
		env = append(env, "TEND_ACTIVE_TAB_ID=t_"+itoa(uint64(tab)))
	}
	if pane != 0 && ws != 0 {
		env = append(env, "TEND_ACTIVE_PANE_ID=p_"+itoa(uint64(pane)))
	}
	if dir != "" {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			dir = ""
		}
	}
	return env, dir
}

// ActivateLink hands a URL clicked in a pane to the plugin that claims it,
// herdr's invoke_plugin_link_handler_for_url: the first enabled plugin, in
// id order, with a link handler whose pattern matches, runs that handler's
// action in the background with the URL in its environment. It reports
// whether one did; when none does, the client opens the URL itself.
func (s *Server) ActivateLink(from session.PaneID, url string) (bool, error) {
	host := s.cfg.Plugins
	if host == nil || url == "" {
		return false, nil
	}
	installed, handler, action, ok := host.Registry.LinkHandler(url)
	if !ok {
		return false, nil
	}
	inv := host.Invocation(installed, action.Command)
	inv.ActionID = action.ID
	inv.Context = s.pluginContext(from)
	inv.ClickedURL, inv.LinkHandlerID = url, handler.ID
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if result := host.RunPlugin(s.context(), inv); result.Err != "" {
			s.logf("plugin %s: link handler %s: %s", installed.ID, handler.ID, result.Err)
		}
	}()
	return true, nil
}
