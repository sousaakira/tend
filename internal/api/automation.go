package api

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sousaakira/tend/internal/detect"
	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/server"
	"github.com/sousaakira/tend/internal/session"
)

// The half of the socket that exists for scripts rather than for hooks: read a
// pane, send it a prompt, wait for the agent to finish, then read the answer.
// herdr calls this "the entire CLI is the plugin API", and its names are kept
// so that something written against one runtime works against the other.
//
// What is deliberately missing, and why, is in docs/PORTING.md: focus and
// scroll are what one person is looking at, and in tend they live in the
// client, so a server-side method for them would be answering for a client
// that may not be attached.

// Methods this half answers.
const (
	MethodSessionSnapshot = "session.snapshot"

	MethodWorkspaceList   = "workspace.list"
	MethodWorkspaceGet    = "workspace.get"
	MethodWorkspaceCreate = "workspace.create"
	MethodWorkspaceRename = "workspace.rename"
	MethodWorkspaceClose  = "workspace.close"

	MethodTabList   = "tab.list"
	MethodTabGet    = "tab.get"
	MethodTabCreate = "tab.create"
	MethodTabRename = "tab.rename"
	MethodTabClose  = "tab.close"

	MethodPaneRead          = "pane.read"
	MethodPaneSendText      = "pane.send_text"
	MethodPaneSendKeys      = "pane.send_keys"
	MethodPaneSplit         = "pane.split"
	MethodPaneClose         = "pane.close"
	MethodPaneResize        = "pane.resize"
	MethodPaneWaitForOutput = "pane.wait_for_output"

	MethodAgentList     = "agent.list"
	MethodAgentGet      = "agent.get"
	MethodAgentRead     = "agent.read"
	MethodAgentPrompt   = "agent.prompt"
	MethodAgentSendKeys = "agent.send_keys"
	MethodAgentWait     = "agent.wait"
	MethodAgentStart    = "agent.start"

	MethodEventsWait = "events.wait"

	MethodPaneSwap      = "pane.swap"
	MethodTabMove       = "tab.move"
	MethodWorkspaceMove = "workspace.move"

	MethodServerStop    = "server.stop"
	MethodServerHandoff = "server.live_handoff"
)

// waitCeiling bounds any wait, however long the caller asked for. A script
// that asks to wait forever and then walks away would otherwise hold a
// connection and a subscription for as long as the server runs.
const waitCeiling = 6 * time.Hour

// defaultWait is how long a wait runs when the caller names no timeout.
const defaultWait = 5 * time.Minute

// WorkspaceID and TabID are named as panes are: "w_3", "t_9".
func WorkspaceID(id session.WorkspaceID) string {
	return "w_" + strconv.FormatUint(uint64(id), 10)
}

// TabID names a tab.
func TabID(id session.TabID) string { return "t_" + strconv.FormatUint(uint64(id), 10) }

func parseID(prefix, s string) (uint64, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), prefix)
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n == 0 {
		return 0, false
	}
	return n, true
}

// WorkspaceInfo describes a space.
type WorkspaceInfo struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name,omitempty"`
	Dir         string `json:"dir,omitempty"`
	Group       string `json:"group,omitempty"`
	Tabs        int    `json:"tabs"`
}

// TabInfo describes a tab.
type TabInfo struct {
	TabID       string   `json:"tab_id"`
	WorkspaceID string   `json:"workspace_id"`
	Name        string   `json:"name,omitempty"`
	Panes       []string `json:"panes"`
}

// where locates a pane in the session, so a caller reading one pane knows
// which space and tab to look at next.
type where struct {
	workspace session.WorkspaceID
	tab       session.TabID
}

// locate finds a pane's place. A pane with no place is one that closed while
// the call was in flight.
func (a *API) locate(id session.PaneID) (where, bool) {
	var (
		out   where
		found bool
	)
	a.srv.Session(func(sess *session.Session) {
		for _, w := range sess.Workspaces() {
			for _, t := range w.Tabs() {
				if _, ok := t.Pane(id); ok {
					out, found = where{workspace: w.ID, tab: t.ID}, true
					return
				}
			}
		}
	})
	return out, found
}

// callMore answers the automation half. It is reached when the first half has
// not recognised the method.
func (a *API) callMore(req Request, pend *pending) (any, error) {
	if result, mine, err := a.callPlugins(req); mine {
		return result, err
	}
	if result, mine, err := a.callWorktrees(req); mine {
		return result, err
	}
	switch req.Method {
	case MethodSessionSnapshot:
		return a.sessionSnapshot(), nil

	case MethodWorkspaceList:
		return map[string]any{"type": "workspace_list", "workspaces": a.workspaces()}, nil

	case MethodWorkspaceGet:
		var p struct {
			WorkspaceID string `json:"workspace_id"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		for _, w := range a.workspaces() {
			if w.WorkspaceID == p.WorkspaceID {
				return map[string]any{"type": "workspace_info", "workspace": w}, nil
			}
		}
		return nil, fail("workspace_not_found", "workspace %s not found", p.WorkspaceID)

	case MethodWorkspaceCreate:
		var p struct {
			Name string `json:"name"`
			Dir  string `json:"dir"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.srv.NewWorkspaceIn(p.Name, p.Dir)
		if err != nil {
			return nil, fail("workspace_create_failed", "%v", err)
		}
		return a.workspaceResult(id)

	case MethodWorkspaceRename:
		var p struct {
			WorkspaceID string `json:"workspace_id"`
			Name        string `json:"name"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, ok := parseID("w_", p.WorkspaceID)
		if !ok {
			return nil, fail("workspace_not_found", "workspace %s not found", p.WorkspaceID)
		}
		if err := a.srv.RenameWorkspace(session.WorkspaceID(id), p.Name); err != nil {
			return nil, workspaceErr(p.WorkspaceID, err)
		}
		return a.workspaceResult(session.WorkspaceID(id))

	case MethodWorkspaceClose:
		var p struct {
			WorkspaceID string `json:"workspace_id"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, ok := parseID("w_", p.WorkspaceID)
		if !ok {
			return nil, fail("workspace_not_found", "workspace %s not found", p.WorkspaceID)
		}
		if err := a.srv.CloseWorkspace(session.WorkspaceID(id)); err != nil {
			return nil, workspaceErr(p.WorkspaceID, err)
		}
		return ok2(), nil

	case MethodTabList:
		var p struct {
			WorkspaceID string `json:"workspace_id"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return map[string]any{"type": "tab_list", "tabs": a.tabs(p.WorkspaceID)}, nil

	case MethodTabGet:
		var p struct {
			TabID string `json:"tab_id"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		for _, t := range a.tabs("") {
			if t.TabID == p.TabID {
				return map[string]any{"type": "tab_info", "tab": t}, nil
			}
		}
		return nil, fail("tab_not_found", "tab %s not found", p.TabID)

	case MethodTabCreate:
		var p struct {
			WorkspaceID string   `json:"workspace_id"`
			Name        string   `json:"name"`
			Command     []string `json:"command"`
			Dir         string   `json:"dir"`
			Agent       string   `json:"agent"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		ws, ok := parseID("w_", p.WorkspaceID)
		if !ok {
			return nil, fail("workspace_not_found", "workspace %s not found", p.WorkspaceID)
		}
		spec, err := a.spec(p.Command, p.Dir, p.Agent)
		if err != nil {
			return nil, err
		}
		tab, pane, err := a.srv.NewTab(session.WorkspaceID(ws), p.Name, spec)
		if err != nil {
			return nil, tabErr(p.WorkspaceID, err)
		}
		st, _ := a.srv.PaneStatus(pane)
		return map[string]any{
			"type": "tab_created", "tab": a.tab(session.WorkspaceID(ws), tab), "root_pane": a.info(st),
		}, nil

	case MethodTabRename:
		var p struct {
			TabID string `json:"tab_id"`
			Name  string `json:"name"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, ok := parseID("t_", p.TabID)
		if !ok {
			return nil, fail("tab_not_found", "tab %s not found", p.TabID)
		}
		if err := a.srv.RenameTab(session.TabID(id), p.Name); err != nil {
			return nil, tabErr(p.TabID, err)
		}
		return ok2(), nil

	case MethodTabClose:
		var p struct {
			TabID string `json:"tab_id"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, ok := parseID("t_", p.TabID)
		if !ok {
			return nil, fail("tab_not_found", "tab %s not found", p.TabID)
		}
		if err := a.srv.CloseTab(session.TabID(id)); err != nil {
			return nil, tabErr(p.TabID, err)
		}
		return ok2(), nil

	case MethodPaneRead:
		var p struct {
			PaneID string `json:"pane_id"`
			Source string `json:"source"`
			Lines  int    `json:"lines"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		return a.read(p.PaneID, id, p.Source, p.Lines)

	case MethodPaneSendText:
		var p struct {
			PaneID string `json:"pane_id"`
			Text   string `json:"text"`
			Submit bool   `json:"submit"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		if p.Submit {
			err = a.srv.Submit(id, p.Text)
		} else {
			err = a.srv.SendText(id, p.Text)
		}
		if err != nil {
			return nil, paneErr(p.PaneID, err)
		}
		return ok2(), nil

	case MethodPaneSendKeys:
		var p struct {
			PaneID string   `json:"pane_id"`
			Keys   []string `json:"keys"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		if err := a.srv.SendKeys(id, p.Keys); err != nil {
			return nil, paneErr(p.PaneID, err)
		}
		return ok2(), nil

	case MethodPaneSplit:
		var p struct {
			PaneID    string   `json:"pane_id"`
			Direction string   `json:"direction"`
			Command   []string `json:"command"`
			Dir       string   `json:"dir"`
			Agent     string   `json:"agent"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		dir := session.Columns
		switch p.Direction {
		case "", "right", "horizontal", "columns":
		case "down", "vertical", "rows":
			dir = session.Rows
		default:
			return nil, fail("invalid_params", "direction %q is not right or down", p.Direction)
		}
		spec, err := a.spec(p.Command, p.Dir, p.Agent)
		if err != nil {
			return nil, err
		}
		child, err := a.srv.SplitPane(id, dir, spec)
		if err != nil {
			return nil, paneErr(p.PaneID, err)
		}
		st, _ := a.srv.PaneStatus(child)
		return map[string]any{"type": "pane_info", "pane": a.info(st)}, nil

	case MethodPaneClose:
		var p struct {
			PaneID string `json:"pane_id"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		if err := a.srv.ClosePane(id); err != nil {
			return nil, paneErr(p.PaneID, err)
		}
		return ok2(), nil

	case MethodPaneResize:
		var p struct {
			PaneID string `json:"pane_id"`
			Cols   int    `json:"cols"`
			Rows   int    `json:"rows"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		if p.Cols <= 0 || p.Rows <= 0 || p.Cols > 1000 || p.Rows > 1000 {
			return nil, fail("invalid_params", "cols and rows must be between 1 and 1000")
		}
		if err := a.srv.Resize(id, pty.Size{Cols: uint16(p.Cols), Rows: uint16(p.Rows)}); err != nil {
			return nil, paneErr(p.PaneID, err)
		}
		return ok2(), nil

	case MethodPaneWaitForOutput:
		var p struct {
			PaneID    string `json:"pane_id"`
			Contains  string `json:"contains"`
			TimeoutMs uint64 `json:"timeout_ms"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		return a.waitForOutput(p.PaneID, id, p.Contains, p.TimeoutMs)

	case MethodAgentList:
		agents := a.srv.AgentPanes()
		out := make([]PaneInfo, 0, len(agents))
		for _, st := range agents {
			out = append(out, a.info(st))
		}
		return map[string]any{"type": "agent_list", "agents": out}, nil

	case MethodAgentGet, MethodAgentRead, MethodAgentPrompt, MethodAgentSendKeys, MethodAgentWait:
		return a.agentCall(req)

	case MethodAgentStart:
		var p struct {
			PaneID  string   `json:"pane_id"`
			Agent   string   `json:"agent"`
			Command []string `json:"command"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		argv := p.Command
		if len(argv) == 0 {
			if p.Agent == "" {
				return nil, fail("invalid_params", "name an agent or a command to run")
			}
			argv = []string{p.Agent}
		}
		// Typed into whatever is in the pane, which is how a person starts an
		// agent: the shell runs it, and detection picks it up from the
		// foreground process as it does for any other pane.
		if err := a.srv.Submit(id, strings.Join(argv, " ")); err != nil {
			return nil, paneErr(p.PaneID, err)
		}
		return map[string]any{"type": "agent_started", "pane_id": p.PaneID, "argv": argv}, nil

	case MethodEventsWait:
		var p struct {
			Kinds     []string `json:"kinds"`
			PaneID    string   `json:"pane_id"`
			TimeoutMs uint64   `json:"timeout_ms"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return a.eventsWait(p.Kinds, p.PaneID, p.TimeoutMs)

	case MethodPaneSwap:
		var p struct {
			PaneID       string `json:"pane_id"`
			TargetPaneID string `json:"target_pane_id"`
			Direction    string `json:"direction"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		if p.TargetPaneID != "" {
			other, err := a.pane(p.TargetPaneID)
			if err != nil {
				return nil, err
			}
			if err := a.srv.SwapPanes(id, other); err != nil {
				return nil, moveErr(err)
			}
			return ok2(), nil
		}
		side, ok := sides[p.Direction]
		if !ok {
			return nil, fail("invalid_params", "direction %q is not left, right, up or down", p.Direction)
		}
		// A layout needs a size to find a neighbour in. Any will do for
		// deciding which pane is to the left, and this is the one panes start
		// at when nothing has sized them.
		other, err := a.srv.SwapPaneToward(id, side, session.Rect{W: 120, H: 40})
		if err != nil {
			return nil, moveErr(err)
		}
		return map[string]any{"type": "pane_swapped", "pane_id": PaneID(id), "other_pane_id": PaneID(other)}, nil

	case MethodTabMove, MethodWorkspaceMove:
		var p struct {
			TabID       string `json:"tab_id"`
			WorkspaceID string `json:"workspace_id"`
			Index       *int   `json:"index"`
			Delta       int    `json:"delta"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		if p.Index == nil && p.Delta == 0 {
			return nil, fail("invalid_params", "give an index or a delta")
		}
		index := 0
		if p.Index != nil {
			index = *p.Index
		}
		var err error
		if req.Method == MethodTabMove {
			id, ok := parseID("t_", p.TabID)
			if !ok {
				return nil, fail("tab_not_found", "tab %s not found", p.TabID)
			}
			err = a.srv.MoveTab(session.TabID(id), index, p.Delta)
		} else {
			id, ok := parseID("w_", p.WorkspaceID)
			if !ok {
				return nil, fail("workspace_not_found", "workspace %s not found", p.WorkspaceID)
			}
			err = a.srv.MoveWorkspace(session.WorkspaceID(id), index, p.Delta)
		}
		if err != nil {
			return nil, moveErr(err)
		}
		return ok2(), nil

	case MethodServerStop:
		pend.after = func() { _ = a.srv.Close() }
		return ok2(), nil

	case MethodServerHandoff:
		// Carried out now, so the reply says whether it worked, and committed
		// after the reply is written, because committing cuts this connection.
		h, err := a.srv.Replace()
		if err != nil {
			if errors.Is(err, server.ErrHandoffUnavailable) {
				return nil, fail("handoff_unavailable", "%v", err)
			}
			return nil, fail("handoff_failed", "%v", err)
		}
		pend.after = func() { _ = a.srv.CommitHandoff(h) }
		return ok2(), nil
	}
	return nil, fail("unknown_method", "unknown method %q", req.Method)
}

// agentCall serves the agent methods, which are the pane methods with a target
// that may name an agent instead of a pane.
func (a *API) agentCall(req Request) (any, error) {
	var p struct {
		Target    string   `json:"target"`
		PaneID    string   `json:"pane_id"`
		Text      string   `json:"text"`
		Keys      []string `json:"keys"`
		Lines     int      `json:"lines"`
		Source    string   `json:"source"`
		Until     []string `json:"until"`
		TimeoutMs uint64   `json:"timeout_ms"`
		Wait      bool     `json:"wait"`
	}
	if err := decode(req.Params, &p); err != nil {
		return nil, err
	}
	target := p.Target
	if target == "" {
		target = p.PaneID
	}
	id, err := a.target(target)
	if err != nil {
		return nil, err
	}

	switch req.Method {
	case MethodAgentGet:
		st, err := a.srv.PaneStatus(id)
		if err != nil {
			return nil, paneErr(target, err)
		}
		return map[string]any{"type": "agent_info", "agent": a.info(st)}, nil

	case MethodAgentRead:
		return a.read(target, id, p.Source, p.Lines)

	case MethodAgentSendKeys:
		if err := a.srv.SendKeys(id, p.Keys); err != nil {
			return nil, paneErr(target, err)
		}
		return ok2(), nil

	case MethodAgentPrompt:
		if strings.TrimSpace(p.Text) == "" {
			return nil, fail("invalid_params", "a prompt needs text")
		}
		if err := a.srv.Submit(id, p.Text); err != nil {
			return nil, paneErr(target, err)
		}
		if !p.Wait {
			return map[string]any{"type": "agent_prompted", "pane_id": PaneID(id)}, nil
		}
		// Asked to wait, the answer is where the agent got to. An agent that
		// has not started working yet must not be reported as already idle,
		// so the wait is for it to leave the state it was in first.
		return a.waitForState(target, id, p.Until, p.TimeoutMs, true)

	case MethodAgentWait:
		return a.waitForState(target, id, p.Until, p.TimeoutMs, false)
	}
	return nil, fail("unknown_method", "unknown method %q", req.Method)
}

// target resolves what a caller named: a pane, or an agent with exactly one
// pane running it.
func (a *API) target(name string) (session.PaneID, error) {
	if name == "" {
		return 0, fail("invalid_params", "name a pane or an agent")
	}
	if id, ok := ParsePaneID(name); ok {
		return id, nil
	}
	id, err := a.srv.ResolvePaneAgent(name)
	if err != nil {
		if errors.Is(err, session.ErrNoSuchPane) {
			return 0, fail("agent_not_found", "%v", err)
		}
		return 0, fail("ambiguous_target", "%v", err)
	}
	return id, nil
}

// read answers pane.read and agent.read.
func (a *API) read(name string, id session.PaneID, source string, lines int) (any, error) {
	var (
		text string
		err  error
	)
	switch source {
	case "", "recent":
		text, err = a.srv.RecentText(id, lines)
	case "visible", "detection":
		text, err = a.srv.VisibleText(id)
	default:
		return nil, fail("invalid_params", "source %q is not visible or recent", source)
	}
	if err != nil {
		return nil, paneErr(name, err)
	}
	if source == "" {
		source = "recent"
	}
	result := map[string]any{
		"type": "pane_read", "pane_id": PaneID(id), "source": source, "format": "text", "text": text,
	}
	if w, ok := a.locate(id); ok {
		result["workspace_id"] = WorkspaceID(w.workspace)
		result["tab_id"] = TabID(w.tab)
	}
	return result, nil
}

// spec turns what a caller asked to run into a pane spec. No command means the
// login shell, as opening a pane by hand does.
func (a *API) spec(command []string, dir, agentName string) (server.PaneSpec, error) {
	spec := server.PaneSpec{Command: command, Dir: dir, Agent: agentName}
	if len(spec.Command) == 0 {
		spec.Command = a.shell()
	}
	return spec, nil
}

func (a *API) sessionSnapshot() any {
	statuses := a.srv.Statuses()
	panes := make([]PaneInfo, 0, len(statuses))
	for _, st := range statuses {
		panes = append(panes, a.info(st))
	}
	return map[string]any{
		"type":       "session_snapshot",
		"workspaces": a.workspaces(),
		"tabs":       a.tabs(""),
		"panes":      panes,
	}
}

func (a *API) workspaces() []WorkspaceInfo {
	var out []WorkspaceInfo
	a.srv.Session(func(sess *session.Session) {
		for _, w := range sess.Workspaces() {
			out = append(out, WorkspaceInfo{
				WorkspaceID: WorkspaceID(w.ID), Name: w.Name, Dir: w.Dir,
				Group: w.Group, Tabs: len(w.Tabs()),
			})
		}
	})
	return out
}

// tabs lists a space's tabs, or every tab when the space is not named.
func (a *API) tabs(workspaceID string) []TabInfo {
	var out []TabInfo
	a.srv.Session(func(sess *session.Session) {
		for _, w := range sess.Workspaces() {
			if workspaceID != "" && WorkspaceID(w.ID) != workspaceID {
				continue
			}
			for _, t := range w.Tabs() {
				panes := make([]string, 0, 4)
				for _, id := range t.Panes() {
					panes = append(panes, PaneID(id))
				}
				out = append(out, TabInfo{
					TabID: TabID(t.ID), WorkspaceID: WorkspaceID(w.ID), Name: t.Name, Panes: panes,
				})
			}
		}
	})
	return out
}

// tab describes one tab after it has been made.
func (a *API) tab(ws session.WorkspaceID, id session.TabID) TabInfo {
	for _, t := range a.tabs(WorkspaceID(ws)) {
		if t.TabID == TabID(id) {
			return t
		}
	}
	return TabInfo{TabID: TabID(id), WorkspaceID: WorkspaceID(ws)}
}

func (a *API) workspaceResult(id session.WorkspaceID) (any, error) {
	for _, w := range a.workspaces() {
		if w.WorkspaceID == WorkspaceID(id) {
			return map[string]any{"type": "workspace_info", "workspace": w}, nil
		}
	}
	return nil, fail("workspace_not_found", "workspace %s not found", WorkspaceID(id))
}

func workspaceErr(name string, err error) error {
	if errors.Is(err, session.ErrNoSuchWorkspace) {
		return fail("workspace_not_found", "workspace %s not found", name)
	}
	return err
}

func tabErr(name string, err error) error {
	switch {
	case errors.Is(err, session.ErrNoSuchTab):
		return fail("tab_not_found", "tab %s not found", name)
	case errors.Is(err, session.ErrNoSuchWorkspace):
		return fail("workspace_not_found", "workspace %s not found", name)
	}
	return err
}

var sides = map[string]session.Side{
	"left": session.Left, "right": session.Right, "up": session.Up, "down": session.Down,
}

func moveErr(err error) error {
	switch {
	case errors.Is(err, session.ErrNoMove):
		return fail("nothing_to_move", "%v", err)
	case errors.Is(err, session.ErrNoSuchPane):
		return fail("pane_not_found", "%v", err)
	case errors.Is(err, session.ErrNoSuchTab):
		return fail("tab_not_found", "%v", err)
	case errors.Is(err, session.ErrNoSuchWorkspace):
		return fail("workspace_not_found", "%v", err)
	}
	return err
}

func ok2() any { return map[string]string{"type": "ok"} }

// stateNames are what a caller may wait for.
func parseStates(names []string) ([]detect.State, error) {
	if len(names) == 0 {
		// The states a prompt ends in. Waiting for "unknown" as well would end
		// the wait on the agent's first redraw.
		return []detect.State{detect.StateIdle, detect.StateBlocked}, nil
	}
	var out []detect.State
	for _, n := range names {
		st, err := parseState(n)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// deadline turns a caller's timeout into a channel, bounded.
func deadline(ms uint64) (<-chan time.Time, time.Duration) {
	d := defaultWait
	if ms > 0 {
		d = time.Duration(ms) * time.Millisecond
	}
	if d > waitCeiling {
		d = waitCeiling
	}
	timer := time.NewTimer(d)
	return timer.C, d
}

// waitForState waits until an agent is in one of the states asked for.
//
// leaveFirst is for a prompt just sent: the agent is still idle for the moment
// it takes to notice, and returning then would report the answer to the
// previous question as the answer to this one.
func (a *API) waitForState(name string, id session.PaneID, until []string, ms uint64, leaveFirst bool) (any, error) {
	states, err := parseStates(until)
	if err != nil {
		return nil, err
	}
	matches := func(st detect.State) bool {
		for _, want := range states {
			if st == want {
				return true
			}
		}
		return false
	}

	sub := a.srv.Subscribe(64)
	defer sub.Close()

	start, err := a.srv.PaneStatus(id)
	if err != nil {
		return nil, paneErr(name, err)
	}
	left := !leaveFirst
	if !left && !matches(start.State) {
		left = true // already somewhere else; no departure to wait for
	}

	timeout, waited := deadline(ms)
	for {
		if left {
			st, err := a.srv.PaneStatus(id)
			if err != nil {
				return nil, paneErr(name, err)
			}
			if matches(st.State) {
				return map[string]any{
					"type": "agent_info", "agent": a.info(st), "timed_out": false,
				}, nil
			}
		}
		select {
		case ev, open := <-sub.C:
			if !open {
				return nil, fail("server_stopping", "the server is shutting down")
			}
			if ev.Pane != id {
				continue
			}
			switch ev.Kind {
			case server.EventPaneState:
				if !left && !matches(ev.State) {
					left = true
				}
			case server.EventPaneExited:
				return nil, fail("pane_exited", "the program in pane %s ended", PaneID(id))
			}
		case <-timeout:
			st, _ := a.srv.PaneStatus(id)
			return map[string]any{
				"type": "agent_info", "agent": a.info(st), "timed_out": true,
				"waited_ms": waited.Milliseconds(),
			}, nil
		}
	}
}

// waitForOutput waits until a pane has printed something, or printed a
// particular string.
func (a *API) waitForOutput(name string, id session.PaneID, contains string, ms uint64) (any, error) {
	sub := a.srv.Subscribe(64)
	defer sub.Close()

	seen := func() (bool, error) {
		if contains == "" {
			return false, nil
		}
		text, err := a.srv.RecentText(id, 0)
		if err != nil {
			return false, paneErr(name, err)
		}
		return strings.Contains(text, contains), nil
	}
	if found, err := seen(); err != nil {
		return nil, err
	} else if found {
		return map[string]any{"type": "pane_output", "pane_id": PaneID(id), "timed_out": false}, nil
	}

	timeout, waited := deadline(ms)
	for {
		select {
		case ev, open := <-sub.C:
			if !open {
				return nil, fail("server_stopping", "the server is shutting down")
			}
			if ev.Pane != id || ev.Kind != server.EventPaneOutput {
				continue
			}
			found, err := seen()
			if err != nil {
				return nil, err
			}
			if contains == "" || found {
				return map[string]any{
					"type": "pane_output", "pane_id": PaneID(id), "timed_out": false,
				}, nil
			}
		case <-timeout:
			return map[string]any{
				"type": "pane_output", "pane_id": PaneID(id), "timed_out": true,
				"waited_ms": waited.Milliseconds(),
			}, nil
		}
	}
}

// eventsWait returns the next event a caller cares about.
func (a *API) eventsWait(kinds []string, paneID string, ms uint64) (any, error) {
	var only session.PaneID
	if paneID != "" {
		id, err := a.pane(paneID)
		if err != nil {
			return nil, err
		}
		only = id
	}
	want := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		want[k] = true
	}

	sub := a.srv.Subscribe(64)
	defer sub.Close()

	timeout, waited := deadline(ms)
	for {
		select {
		case ev, open := <-sub.C:
			if !open {
				return nil, fail("server_stopping", "the server is shutting down")
			}
			if only != 0 && ev.Pane != only {
				continue
			}
			name := eventName(ev.Kind)
			if len(want) > 0 && !want[name] {
				continue
			}
			out := map[string]any{
				"type": "event", "event": name, "pane_id": PaneID(ev.Pane), "timed_out": false,
			}
			if ev.Kind == server.EventPaneState {
				out["agent_state"] = ev.State.String()
				out["rule"] = ev.Rule
			}
			if ev.Err != "" {
				out["error"] = ev.Err
			}
			return out, nil
		case <-timeout:
			return map[string]any{
				"type": "event", "timed_out": true, "waited_ms": waited.Milliseconds(),
			}, nil
		}
	}
}

// eventName is how an event is named on this socket.
func eventName(k server.EventKind) string {
	switch k {
	case server.EventPaneOpened:
		return "pane.opened"
	case server.EventPaneClosed:
		return "pane.closed"
	case server.EventPaneExited:
		return "pane.exited"
	case server.EventPaneOutput:
		return "pane.output"
	case server.EventPaneState:
		return "agent.state"
	case server.EventPaneClipboard:
		return "pane.clipboard"
	}
	return fmt.Sprintf("event.%d", int(k))
}
