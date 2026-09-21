package main

import (
	"testing"

	"github.com/sousaakira/tend/internal/config"
	"github.com/sousaakira/tend/internal/proto"
)

// TestPriorityPutsWhatNeedsYouFirst is herdr's agent_panel_sort = "priority":
// blocked, then finished unseen, then working, then idle, and the most
// recent change first within each. If it regresses, the waiting agent is
// wherever the session happens to have put it.
func TestPriorityPutsWhatNeedsYouFirst(t *testing.T) {
	pane := func(id uint64, state string, done bool, seq uint64) proto.PaneInfo {
		return proto.PaneInfo{ID: id, Agent: "claude", State: state, Done: done, StateSeq: seq, Running: true}
	}
	tu := &tui{config: config.Defaults()}
	tu.config.UI.AgentPanelSort = "priority"
	tu.grouped = true // no headings in priority order, even when grouped
	tu.snap = proto.SessionSnapshot{
		Workspaces: []proto.WorkspaceInfo{{ID: 1, Name: "main", Tabs: []proto.TabInfo{
			{ID: 1, Panes: []uint64{1, 2, 3}}, {ID: 2, Panes: []uint64{4, 5}},
		}}},
		Panes: []proto.PaneInfo{
			pane(1, "idle", false, 1), pane(2, "working", false, 2), pane(3, "idle", true, 3),
			pane(4, "blocked", false, 4), pane(5, "working", false, 5),
		},
	}
	var got []uint64
	for _, r := range tu.agentRowsLocked() {
		got = append(got, r.Pane)
	}
	want := []uint64{4, 3, 5, 2, 1}
	if len(got) != len(want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}

	tu.config.UI.AgentPanelSort = ""
	got = got[:0]
	for _, r := range tu.agentRowsLocked() {
		if r.Pane != 0 {
			got = append(got, r.Pane)
		}
	}
	if got[0] != 1 || got[4] != 5 {
		t.Errorf("spaces order %v, want the session's", got)
	}
}
