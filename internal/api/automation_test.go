package api

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// result digs the result object out of a reply, failing on an error reply.
func result(t *testing.T, reply map[string]any) map[string]any {
	t.Helper()
	if body, bad := reply["error"].(map[string]any); bad {
		t.Fatalf("call failed: %v", body)
	}
	out, _ := reply["result"].(map[string]any)
	return out
}

func call(t *testing.T, h *harness, method string, params any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"id": method, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	return h.call(t, string(raw))
}

// TestAScriptCanDriveAPaneAndReadItBack is what the automation half is for:
// send something, wait for the pane to answer, read the answer. If it
// regresses, nothing written against tend from the outside works.
func TestAScriptCanDriveAPaneAndReadItBack(t *testing.T) {
	h := start(t)
	pane := PaneID(h.pane)

	// The pane is running "sleep 30", so give it a shell of its own to talk to.
	res := result(t, call(t, h, MethodPaneSplit, map[string]any{
		"pane_id": pane, "direction": "down", "command": []string{"/bin/sh"},
	}))
	shell, _ := res["pane"].(map[string]any)
	id, _ := shell["pane_id"].(string)
	if id == "" {
		t.Fatalf("split returned %v", res)
	}

	call(t, h, MethodPaneSendText, map[string]any{
		"pane_id": id, "text": "printf hello-from-a-script\n",
	})
	res = result(t, call(t, h, MethodPaneWaitForOutput, map[string]any{
		"pane_id": id, "contains": "hello-from-a-script", "timeout_ms": 5000,
	}))
	if res["timed_out"] == true {
		t.Fatal("the pane never printed what was sent to it")
	}

	res = result(t, call(t, h, MethodPaneRead, map[string]any{"pane_id": id, "source": "recent"}))
	text, _ := res["text"].(string)
	if !strings.Contains(text, "hello-from-a-script") {
		t.Errorf("read = %q, want what the shell printed", text)
	}
	// A read says where the pane is, so a script can walk the session from it.
	if res["workspace_id"] == nil || res["tab_id"] == nil {
		t.Errorf("read did not say where the pane is: %v", res)
	}

	// ctrl+c reaches the program as a signal, not as the letter c.
	call(t, h, MethodPaneSendText, map[string]any{"pane_id": id, "text": "sleep 9"})
	call(t, h, MethodPaneSendKeys, map[string]any{"pane_id": id, "keys": []string{"ctrl+c"}})
	call(t, h, MethodPaneSendText, map[string]any{"pane_id": id, "text": "printf after-ctrl-c\n"})
	res = result(t, call(t, h, MethodPaneWaitForOutput, map[string]any{
		"pane_id": id, "contains": "after-ctrl-c", "timeout_ms": 5000,
	}))
	if res["timed_out"] == true {
		t.Error("the shell never came back after ctrl+c")
	}
}

// TestTheSessionCanBeWalkedAndChanged: a script has to be able to find its way
// around and make room for itself without a client attached.
func TestTheSessionCanBeWalkedAndChanged(t *testing.T) {
	h := start(t)

	snap := result(t, call(t, h, MethodSessionSnapshot, nil))
	spaces, _ := snap["workspaces"].([]any)
	if len(spaces) != 1 {
		t.Fatalf("snapshot has %d spaces, want 1", len(spaces))
	}

	made := result(t, call(t, h, MethodWorkspaceCreate, map[string]any{"name": "scripted"}))
	ws, _ := made["workspace"].(map[string]any)
	wsID, _ := ws["workspace_id"].(string)
	if wsID == "" {
		t.Fatalf("create returned %v", made)
	}

	made = result(t, call(t, h, MethodTabCreate, map[string]any{
		"workspace_id": wsID, "name": "work", "command": []string{"/bin/sh", "-c", "sleep 20"},
	}))
	tab, _ := made["tab"].(map[string]any)
	tabID, _ := tab["tab_id"].(string)
	if tabID == "" {
		t.Fatalf("tab.create returned %v", made)
	}

	tabs := result(t, call(t, h, MethodTabList, map[string]any{"workspace_id": wsID}))
	if list, _ := tabs["tabs"].([]any); len(list) != 1 {
		t.Errorf("the new space has %d tabs, want 1", len(list))
	}

	call(t, h, MethodWorkspaceClose, map[string]any{"workspace_id": wsID})
	snap = result(t, call(t, h, MethodSessionSnapshot, nil))
	if spaces, _ := snap["workspaces"].([]any); len(spaces) != 1 {
		t.Errorf("after closing, %d spaces remain, want 1", len(spaces))
	}
}

// TestWaitingForAnAgentReportsWhetherItTimedOut: a wait that cannot say
// whether it ended because the agent finished or because time ran out makes
// every script that uses it wrong.
func TestWaitingForAnAgentReportsWhetherItTimedOut(t *testing.T) {
	h := start(t)
	pane := PaneID(h.pane)

	// Nothing is reporting, so the pane sits in "unknown" and the wait ends on
	// its own clock.
	started := time.Now()
	res := result(t, call(t, h, MethodAgentWait, map[string]any{
		"target": pane, "until": []string{"idle"}, "timeout_ms": 300,
	}))
	if res["timed_out"] != true {
		t.Errorf("wait = %v, want a timeout", res)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("the wait took %s, far past its timeout", elapsed)
	}

	// A hook reporting idle is what the wait is watching for. It comes over a
	// second connection: one connection answers one call at a time, so a
	// report sent down the connection that is waiting would arrive after it.
	reporter := h.another(t)
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = reporter.send(`{"id":"r","method":"pane.report_agent","params":{"pane_id":"` + pane +
			`","source":"s","agent":"deploy-bot","state":"idle","seq":1}}`)
	}()
	res = result(t, call(t, h, MethodAgentWait, map[string]any{
		"target": pane, "until": []string{"idle"}, "timeout_ms": 5000,
	}))
	if res["timed_out"] == true {
		t.Fatalf("the wait timed out although idle was reported: %v", res)
	}
	info, _ := res["agent"].(map[string]any)
	if info["agent_state"] != "idle" {
		t.Errorf("wait returned %v", info)
	}

	// And the agent is then findable by name rather than by pane.
	res = result(t, call(t, h, MethodAgentGet, map[string]any{"target": "deploy-bot"}))
	if got, _ := res["agent"].(map[string]any); got["pane_id"] != pane {
		t.Errorf("agent.get by name returned %v", got)
	}
}

// TestBadTargetsAreNamedNotGuessed: a prompt sent to the wrong agent cannot be
// taken back, so an ambiguous name is an error.
func TestBadTargetsAreNamedNotGuessed(t *testing.T) {
	h := start(t)
	if code := errorCode(call(t, h, MethodAgentPrompt, map[string]any{
		"target": "nobody", "text": "hi",
	})); code != "agent_not_found" {
		t.Errorf("code = %q, want agent_not_found", code)
	}
	if code := errorCode(call(t, h, MethodPaneSendKeys, map[string]any{
		"pane_id": PaneID(h.pane), "keys": []string{"chorus"},
	})); code != "invalid_key" {
		t.Errorf("code = %q, want invalid_key", code)
	}
}

// TestAWorktreeRootIsNeverRelative: with no directory configured, a new
// worktree was created as "project/branch" relative to the repository — inside
// the checkout it was meant to sit beside. Whatever the setting says, git must
// be handed an absolute path.
func TestAWorktreeRootIsNeverRelative(t *testing.T) {
	for _, setting := range []string{"", "relative/dir", "~/elsewhere", "/abs/dir"} {
		a := &API{worktreeDir: setting}
		if root := a.worktreeRoot(); !filepath.IsAbs(root) {
			t.Errorf("setting %q gave the root %q", setting, root)
		}
	}
}

// TestSubscribingFollowsTheSessionWithoutGaps is what events.wait cannot do:
// a caller that acts on every event needs them in order, and a call that
// answers one and returns loses whatever happened before the next call.
func TestSubscribingFollowsTheSessionWithoutGaps(t *testing.T) {
	h := start(t)
	watcher := h.another(t)

	res := result(t, call(t, watcher, MethodEventsSubscribe, map[string]any{
		"kinds": []string{"pane.opened", "pane.closed"},
	}))
	if res["type"] != "subscribed" {
		t.Fatalf("subscribe = %v", res)
	}

	// Three panes in a row, on another connection. A caller that answered one
	// event at a time would see the first and miss the rest.
	var ids []string
	for i := 0; i < 3; i++ {
		made := result(t, call(t, h, MethodPaneSplit, map[string]any{
			"pane_id": PaneID(h.pane), "command": []string{"/bin/sh", "-c", "sleep 30"},
		}))
		pane, _ := made["pane"].(map[string]any)
		ids = append(ids, text(pane["pane_id"]))
	}

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		ev := watcher.next(t)
		if ev["event"] != "pane.opened" {
			t.Fatalf("event %d = %v, want pane.opened", i, ev)
		}
		seen[text(ev["pane_id"])] = true
	}
	for _, id := range ids {
		if !seen[id] {
			t.Errorf("the stream missed %s; it saw %v", id, seen)
		}
	}

	// Only what was asked for: a state change is not one of the two kinds.
	call(t, h, MethodPaneClose, map[string]any{"pane_id": ids[0]})
	if ev := watcher.next(t); ev["event"] != "pane.closed" {
		t.Errorf("after closing a pane the stream said %v", ev)
	}
}

// text is the string at a key, for reading a result.
func text(v any) string {
	s, _ := v.(string)
	return s
}

// TestALayoutCanBeSavedAndBuiltAgain: an arrangement somebody set up — panes
// in a shape, each in its own directory — is worth having back tomorrow. If
// it regresses, setting up a session is hand work every time.
func TestALayoutCanBeSavedAndBuiltAgain(t *testing.T) {
	h := start(t)
	pane := PaneID(h.pane)

	// Two more panes, so the shape is worth exporting.
	for i := 0; i < 2; i++ {
		result(t, call(t, h, MethodPaneSplit, map[string]any{
			"pane_id": pane, "direction": "down", "command": []string{"/bin/sh", "-c", "sleep 30"},
		}))
	}

	tabs := result(t, call(t, h, MethodTabList, nil))
	list, _ := tabs["tabs"].([]any)
	first, _ := list[0].(map[string]any)
	tabID := text(first["tab_id"])

	exported := result(t, call(t, h, MethodLayoutExport, map[string]any{"tab_id": tabID}))
	if exported["tree"] == nil {
		t.Fatalf("export = %v", exported)
	}
	panes, _ := exported["panes"].([]any)
	if len(panes) != 3 {
		t.Fatalf("the exported layout has %d panes, want 3", len(panes))
	}

	applied := result(t, call(t, h, MethodLayoutApply, map[string]any{
		"name": "rebuilt", "tree": exported["tree"], "panes": exported["panes"],
	}))
	if applied["type"] != "layout_applied" {
		t.Fatalf("apply = %v", applied)
	}

	// The new tab has the same number of panes, in a space of its own.
	rebuilt := result(t, call(t, h, MethodTabList, map[string]any{
		"workspace_id": text(applied["workspace_id"]),
	}))
	got, _ := rebuilt["tabs"].([]any)
	if len(got) != 1 {
		t.Fatalf("the new space has %d tabs", len(got))
	}
	tab, _ := got[0].(map[string]any)
	inPanes, _ := tab["panes"].([]any)
	if len(inPanes) != 3 {
		t.Errorf("the rebuilt tab has %d panes, want 3", len(inPanes))
	}
}

// TestMovingAPaneToAnotherTabDoesNotRestartIt: moving a running agent must
// leave it running. Closing and reopening would lose whatever it was doing,
// which is the reason to move rather than reopen.
func TestMovingAPaneToAnotherTabDoesNotRestartIt(t *testing.T) {
	h := start(t)

	// A pane whose process can be recognised afterwards.
	made := result(t, call(t, h, MethodPaneSplit, map[string]any{
		"pane_id": PaneID(h.pane), "command": []string{"/bin/sh", "-c", "sleep 30"},
	}))
	pane, _ := made["pane"].(map[string]any)
	moving := text(pane["pane_id"])
	pid := pane["pid"]

	spaces := result(t, call(t, h, MethodWorkspaceCreate, map[string]any{"name": "elsewhere"}))
	ws, _ := spaces["workspace"].(map[string]any)
	made = result(t, call(t, h, MethodTabCreate, map[string]any{
		"workspace_id": text(ws["workspace_id"]), "name": "there",
		"command": []string{"/bin/sh", "-c", "sleep 30"},
	}))
	root, _ := made["root_pane"].(map[string]any)

	if res := result(t, call(t, h, MethodPaneMove, map[string]any{
		"pane_id": moving, "target_pane_id": text(root["pane_id"]), "direction": "down",
	})); res["type"] != "ok" {
		t.Fatalf("move = %v", res)
	}

	got := result(t, call(t, h, MethodPaneGet, map[string]any{"pane_id": moving}))
	info, _ := got["pane"].(map[string]any)
	if info["pid"] != pid {
		t.Errorf("the pane's process changed: %v, was %v", info["pid"], pid)
	}
	tabs := result(t, call(t, h, MethodTabList, map[string]any{"workspace_id": text(ws["workspace_id"])}))
	list, _ := tabs["tabs"].([]any)
	tab, _ := list[0].(map[string]any)
	panes, _ := tab["panes"].([]any)
	if len(panes) != 2 {
		t.Errorf("the tab it moved to has %d panes, want 2", len(panes))
	}
}

// TestExplainingWhyAnAgentIsShownAsItIs: "why does it say that?" is the
// question detection raises most often, and guessing at a screen that has
// since changed is the alternative.
func TestExplainingWhyAnAgentIsShownAsItIs(t *testing.T) {
	h := start(t)

	// A pane that looks like claude waiting for an answer, so a rule fires.
	made := result(t, call(t, h, MethodPaneSplit, map[string]any{
		"pane_id": PaneID(h.pane), "agent": "claude",
		"command": []string{"/bin/sh", "-c", "printf 'Do you want to proceed?\\n  1. Yes\\n'; sleep 30"},
	}))
	pane, _ := made["pane"].(map[string]any)
	id := text(pane["pane_id"])

	deadline := time.Now().Add(5 * time.Second)
	var explain map[string]any
	for time.Now().Before(deadline) {
		res := result(t, call(t, h, MethodAgentExplain, map[string]any{"target": id, "screen": true}))
		explain, _ = res["explain"].(map[string]any)
		if text(explain["matched_rule"]) != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if text(explain["matched_rule"]) == "" {
		t.Fatalf("nothing explains the state: %v", explain)
	}
	if text(explain["source"]) != "screen" {
		t.Errorf("source = %q, want the screen", explain["source"])
	}
	rules, _ := explain["evaluated_rules"].([]any)
	if len(rules) < 2 {
		t.Errorf("only %d rules were reported; the point is seeing them all", len(rules))
	}
	if !strings.Contains(text(explain["screen"]), "Do you want to proceed?") {
		t.Errorf("the screen detection read was not reported: %q", explain["screen"])
	}

	// A hook answering for the pane says so, rather than naming a rule that
	// had nothing to do with it.
	h.call(t, `{"id":"r","method":"pane.report_agent","params":{"pane_id":"`+id+
		`","source":"my-hook","agent":"claude","state":"working","seq":1}}`)
	res := result(t, call(t, h, MethodAgentExplain, map[string]any{"target": id}))
	explain, _ = res["explain"].(map[string]any)
	if text(explain["source"]) != "hook:my-hook" {
		t.Errorf("source = %q, want the hook", explain["source"])
	}
}
