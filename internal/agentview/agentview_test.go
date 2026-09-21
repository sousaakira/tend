package agentview

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAViewWrittenForHerdrFiltersAndOrders: herdr's JSON — ops, fields,
// token fields, context values, sorts — reads here and does what it says.
// If it regresses, a plugin's view either fails to set or shows the wrong
// agents.
func TestAViewWrittenForHerdrFiltersAndOrders(t *testing.T) {
	var v View
	err := json.Unmarshal([]byte(`{
		"source": "plugin:triage", "label": "waiting here",
		"filter": {"op": "all", "filters": [
			{"op": "eq", "field": "workspace_id", "value": {"context": "current_workspace_id"}},
			{"op": "in", "field": "status", "values": ["blocked", "done"]},
			{"op": "not", "filter": {"op": "eq", "field": {"token": "muted"}, "value": "yes"}}
		]},
		"sort": [{"field": "attention", "order": "desc"}, {"field": "pane_order"}]
	}`), &v)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	ctx := Context{WorkspaceID: "w_1"}
	blocked := Entry{Status: "blocked", WorkspaceID: "w_1", Attention: 4, PaneOrder: 2}
	done := Entry{Status: "done", WorkspaceID: "w_1", Attention: 3, PaneOrder: 1}
	elsewhere := Entry{Status: "blocked", WorkspaceID: "w_2", Attention: 4}
	muted := Entry{Status: "blocked", WorkspaceID: "w_1", Tokens: map[string]string{"muted": "yes"}}
	working := Entry{Status: "working", WorkspaceID: "w_1"}
	for name, c := range map[string]struct {
		e    Entry
		want bool
	}{
		"blocked here": {blocked, true}, "done here": {done, true}, "elsewhere": {elsewhere, false},
		"muted": {muted, false}, "working": {working, false},
	} {
		if got := v.Filter.Matches(ctx, c.e); got != c.want {
			t.Errorf("%s: matches = %v, want %v", name, got, c.want)
		}
	}
	if !v.Less(blocked, done) || v.Less(done, blocked) {
		t.Error("attention descending should put blocked before done")
	}
	out, _ := json.Marshal(v)
	if !strings.Contains(string(out), `{"token":"muted"}`) || !strings.Contains(string(out), `{"context":"current_workspace_id"}`) {
		t.Errorf("round trip lost a form: %s", out)
	}
}

// TestBadViewsAreRefused holds herdr's limits and names.
func TestBadViewsAreRefused(t *testing.T) {
	deep := `{"op":"exists","field":"agent"}`
	for i := 0; i < 9; i++ {
		deep = `{"op":"not","filter":` + deep + `}`
	}
	for name, body := range map[string]string{
		"no source":     `{"source": ""}`,
		"bad source":    `{"source": "a b"}`,
		"unknown op":    `{"source": "s", "filter": {"op": "like", "field": "agent"}}`,
		"unknown field": `{"source": "s", "filter": {"op": "exists", "field": "colour"}}`,
		"bad context":   `{"source": "s", "filter": {"op": "eq", "field": "tab_id", "value": {"context": "home"}}}`,
		"too deep":      `{"source": "s", "filter": ` + deep + `}`,
		"bad sort":      `{"source": "s", "sort": [{"field": "colour"}]}`,
		"long label":    `{"source": "s", "label": "` + strings.Repeat("x", 33) + `"}`,
	} {
		var v View
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			continue // refused while reading, which is also refusing
		}
		if err := v.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
