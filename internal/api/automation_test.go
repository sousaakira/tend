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
