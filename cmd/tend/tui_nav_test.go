package main

import (
	"encoding/json"
	"testing"

	"github.com/sousaakira/tend/internal/agentview"
	"github.com/sousaakira/tend/internal/config"
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/ui"
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

// TestAViewDecidesWhichAgentsAreListed: with a view set, the list is what
// its filter keeps, in its order, and the heading says the list is not all
// of them. If it regresses, a plugin's view is set and nothing changes.
func TestAViewDecidesWhichAgentsAreListed(t *testing.T) {
	var view agentview.View
	if err := json.Unmarshal([]byte(`{"source":"s","label":"urgent",
		"filter":{"op":"in","field":"status","values":["blocked","working"]},
		"sort":[{"field":"state_change_seq","order":"desc"}]}`), &view); err != nil {
		t.Fatal(err)
	}
	tu := &tui{config: config.Defaults()}
	tu.snap = proto.SessionSnapshot{
		Workspaces: []proto.WorkspaceInfo{{ID: 1, Name: "main", Tabs: []proto.TabInfo{{ID: 1, Panes: []uint64{1, 2, 3}}}}},
		Panes: []proto.PaneInfo{
			{ID: 1, Agent: "claude", State: "idle", Running: true, StateSeq: 9},
			{ID: 2, Agent: "claude", State: "working", Running: true, StateSeq: 1},
			{ID: 3, Agent: "codex", State: "blocked", Running: true, StateSeq: 5},
		},
		AgentView: &view,
	}
	var got []uint64
	for _, r := range tu.agentRowsLocked() {
		got = append(got, r.Pane)
	}
	if len(got) != 2 || got[0] != 3 || got[1] != 2 {
		t.Errorf("rows = %v, want the blocked then the working one", got)
	}
	if heading := tu.agentsSectionLocked().Rows[0].Label; heading != "agents · urgent" {
		t.Errorf("heading = %q", heading)
	}
}

// TestACompanyChosenStillSaysWhoIsWaiting: with a company chosen, the
// sidebar lists its spaces and their agents only, and an agent waiting in
// a space it hides is counted for the "!" beside the heading; the panel
// says which company that agent is in. If it regresses, choosing a company
// silences an agent that stopped for the user.
func TestACompanyChosenStillSaysWhoIsWaiting(t *testing.T) {
	tu := &tui{config: config.Defaults(), companyLoaded: true, company: 7}
	tu.snap = proto.SessionSnapshot{
		Workspaces: []proto.WorkspaceInfo{
			{ID: 1, Name: "acme-api", Tabs: []proto.TabInfo{{ID: 1, Panes: []uint64{1}}}},
			{ID: 2, Name: "globex-site", Tabs: []proto.TabInfo{{ID: 2, Panes: []uint64{2}}}},
		},
		Panes: []proto.PaneInfo{
			{ID: 1, Agent: "claude", State: "working", Running: true},
			{ID: 2, Agent: "claude", State: "blocked", Running: true},
		},
		Companies: []proto.CompanyInfo{{ID: 7, Name: "Acme", Workspaces: []uint64{1}}, {ID: 9, Name: "Globex", Workspaces: []uint64{2}}},
	}
	tu.workspace = 1
	var spaces, agents []uint64
	for _, r := range tu.spaceRowsLocked() {
		spaces = append(spaces, r.Workspace)
	}
	for _, r := range tu.agentRowsLocked() {
		agents = append(agents, r.Pane)
	}
	if len(spaces) != 1 || spaces[0] != 1 || len(agents) != 1 || agents[0] != 1 {
		t.Errorf("listed spaces %v and agents %v, want Acme's only", spaces, agents)
	}
	if n := tu.waitingElsewhereLocked(); n != 1 {
		t.Errorf("waiting elsewhere = %d, want the one in Globex", n)
	}

	tu.companies = &ui.CompaniesView{}
	tu.fillCompaniesLocked()
	if e := tu.companies.Entries; len(e) != 3 || e[0].Waiting != 1 || e[1].Waiting != 0 || e[2].Waiting != 1 || tu.companies.Active != 7 {
		t.Errorf("panel entries %+v", e)
	}

	tu.company = 0
	if n := tu.waitingElsewhereLocked(); n != 0 {
		t.Errorf("with every space shown nothing is elsewhere, got %d", n)
	}
}
