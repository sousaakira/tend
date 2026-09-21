package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/sousaakira/tend/internal/plugin"
	"github.com/sousaakira/tend/internal/server"
	"github.com/sousaakira/tend/internal/session"
)

// Plugins over the socket, so that the thing a plugin is invoked from — the
// command menu, a key binding, another plugin — is the same door everything
// else uses. herdr's method names.

// Methods for plugins.
const (
	MethodPluginList         = "plugin.list"
	MethodPluginLink         = "plugin.link"
	MethodPluginUnlink       = "plugin.unlink"
	MethodPluginEnable       = "plugin.enable"
	MethodPluginDisable      = "plugin.disable"
	MethodPluginReload       = "plugin.reload"
	MethodPluginActionList   = "plugin.action.list"
	MethodPluginActionInvoke = "plugin.action.invoke"
	MethodPluginLogList      = "plugin.log.list"
	MethodPluginPaneOpen     = "plugin.pane.open"
)

// ActionInfo is one action, with the plugin it came from.
type ActionInfo struct {
	PluginID    string `json:"plugin_id"`
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// callPlugins answers the plugin half. It reports whether the method was one
// of its own, so the dispatcher can carry on looking.
func (a *API) callPlugins(req Request) (any, bool, error) {
	host := a.srv.PluginHost()
	needsHost := strings.HasPrefix(req.Method, "plugin.")
	if needsHost && host == nil {
		return nil, true, fail("plugins_unavailable",
			"this server runs no plugins; it was started without a plugin registry")
	}

	switch req.Method {
	case MethodPluginList:
		installed := host.Registry.List()
		out := make([]plugin.Installed, 0, len(installed))
		out = append(out, installed...)
		return map[string]any{"type": "plugin_list", "plugins": out}, true, nil

	case MethodPluginLink:
		var p struct {
			Path string `json:"path"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, true, err
		}
		if strings.TrimSpace(p.Path) == "" {
			return nil, true, fail("invalid_params", "name the directory holding the plugin")
		}
		installed, err := host.Registry.Link(p.Path)
		if err != nil {
			return nil, true, fail("plugin_link_failed", "%v", err)
		}
		return map[string]any{"type": "plugin_info", "plugin": installed}, true, nil

	case MethodPluginUnlink, MethodPluginEnable, MethodPluginDisable, MethodPluginReload:
		var p struct {
			PluginID string `json:"plugin_id"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, true, err
		}
		var err error
		switch req.Method {
		case MethodPluginUnlink:
			err = host.Registry.Unlink(p.PluginID)
		case MethodPluginEnable:
			err = host.Registry.SetEnabled(p.PluginID, true)
		case MethodPluginDisable:
			err = host.Registry.SetEnabled(p.PluginID, false)
		case MethodPluginReload:
			var installed plugin.Installed
			if installed, err = host.Registry.Reload(p.PluginID); err == nil {
				return map[string]any{"type": "plugin_info", "plugin": installed}, true, nil
			}
		}
		if err != nil {
			return nil, true, pluginErr(p.PluginID, err)
		}
		return ok2(), true, nil

	case MethodPluginActionList:
		var out []ActionInfo
		for _, p := range host.Registry.Enabled() {
			for _, action := range p.Actions {
				out = append(out, ActionInfo{
					PluginID: p.ID, ID: action.ID, Title: action.Title, Description: action.Description,
				})
			}
		}
		return map[string]any{"type": "plugin_action_list", "actions": out}, true, nil

	case MethodPluginLogList:
		var p struct {
			PluginID string `json:"plugin_id"`
			Limit    int    `json:"limit"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, true, err
		}
		logs := host.Log(p.PluginID, p.Limit)
		if logs == nil {
			logs = []server.PluginLogEntry{}
		}
		return map[string]any{"type": "plugin_log_list", "logs": logs}, true, nil

	case MethodPluginActionInvoke:
		var p struct {
			ActionID string `json:"action_id"`
			PaneID   string `json:"pane_id"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, true, err
		}
		installed, action, err := host.Registry.Action(p.ActionID)
		if err != nil {
			return nil, true, fail("plugin_action_not_found", "%v", err)
		}
		inv := host.Invocation(installed, action.Command)
		inv.ActionID = action.ID
		if p.PaneID != "" {
			id, err := a.pane(p.PaneID)
			if err != nil {
				return nil, true, err
			}
			inv.Context = a.srv.PluginContext(id)
		}
		// Waited for, unlike an event hook: whoever invoked an action is
		// watching, and wants to know whether it worked.
		result := host.RunPlugin(context.Background(), inv)
		return map[string]any{
			"type": "plugin_action_invoked", "plugin_id": installed.ID, "action_id": action.ID,
			"exit_code": result.ExitCode, "output": result.Output,
			"timed_out": result.TimedOut, "error": result.Err,
		}, true, nil

	case MethodPluginPaneOpen:
		var p struct {
			Pane   string `json:"pane"`
			Target string `json:"pane_id"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, true, err
		}
		return a.openPluginPane(host, p.Pane, p.Target)
	}
	return nil, false, nil
}

// openPluginPane starts a pane a plugin offers, beside the pane named, or in a
// new tab when the manifest asked for one.
func (a *API) openPluginPane(host *server.Plugins, name, target string) (any, bool, error) {
	installed, pane, err := host.Registry.Pane(name)
	if err != nil {
		return nil, true, fail("plugin_pane_not_found", "%v", err)
	}
	command, err := plugin.ResolvePaneCommand(installed, pane.Command)
	if err != nil {
		return nil, true, fail("plugin_pane_failed", "%v", err)
	}

	spec := server.PaneSpec{Command: command, Title: pane.Title}
	if pane.Placement == "tab" {
		ws, err := a.firstWorkspace()
		if err != nil {
			return nil, true, err
		}
		_, opened, err := a.srv.NewTab(ws, pane.Title, spec)
		if err != nil {
			return nil, true, fail("plugin_pane_failed", "%v", err)
		}
		st, _ := a.srv.PaneStatus(opened)
		return map[string]any{"type": "pane_info", "pane": a.info(st)}, true, nil
	}

	var beside session.PaneID
	if target != "" {
		if beside, err = a.pane(target); err != nil {
			return nil, true, err
		}
	} else {
		statuses := a.srv.Statuses()
		if len(statuses) == 0 {
			return nil, true, fail("plugin_pane_failed", "the session has no pane to open beside")
		}
		beside = statuses[0].ID
	}
	opened, err := a.srv.SplitPane(beside, session.Columns, spec)
	if err != nil {
		return nil, true, fail("plugin_pane_failed", "%v", err)
	}
	st, _ := a.srv.PaneStatus(opened)
	return map[string]any{"type": "pane_info", "pane": a.info(st)}, true, nil
}

// firstWorkspace is where a plugin's tab goes when nothing says otherwise.
func (a *API) firstWorkspace() (session.WorkspaceID, error) {
	var found session.WorkspaceID
	a.srv.Session(func(sess *session.Session) {
		if spaces := sess.Workspaces(); len(spaces) > 0 {
			found = spaces[0].ID
		}
	})
	if found == 0 {
		return 0, fail("plugin_pane_failed", "the session has no space to open a tab in")
	}
	return found, nil
}

func pluginErr(id string, err error) error {
	if strings.Contains(err.Error(), "not installed") {
		return fail("plugin_not_found", "no plugin called %q is installed", id)
	}
	return fmt.Errorf("%w", err)
}
