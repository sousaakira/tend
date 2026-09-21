package agent

import (
	"testing"
	"time"
)

func str(s string) *string { return &s }

// TestAHookCanSayMoreThanTheState is what the sidebar is made of in herdr: the
// model, what is left of the context, what to call the agent. Without it a
// pane says only "working".
func TestAHookCanSayMoreThanTheState(t *testing.T) {
	a := NewArbiter(knownAgents)
	a.Observe("claude", 0, false, t0)

	changed := a.ReportMetadata(MetadataReport{
		Source: "tend:claude", Agent: "claude",
		DisplayAgent: "claude opus",
		Tokens:       map[string]*string{"ctx": str("23%"), "model": str("opus")},
		StateLabels:  map[string]string{"blocked": "waiting for approval"},
	}, t0)
	if !changed {
		t.Fatal("the first report changed nothing")
	}

	p := a.Presentation(t0)
	if p.DisplayAgent != "claude opus" {
		t.Errorf("display agent = %q", p.DisplayAgent)
	}
	if p.StateLabels["blocked"] != "waiting for approval" {
		t.Errorf("state labels = %v", p.StateLabels)
	}
	if got := p.Label("claude"); got != "claude opus · 23% · opus" {
		t.Errorf("label = %q", got)
	}

	// A report that says the same thing again is not a redraw.
	if a.ReportMetadata(MetadataReport{
		Source: "tend:claude", Agent: "claude", DisplayAgent: "claude opus",
		Tokens: map[string]*string{"ctx": str("23%")},
	}, t0) {
		t.Error("an unchanged report was reported as a change")
	}

	// A token set to nothing goes.
	a.ReportMetadata(MetadataReport{
		Source: "tend:claude", Agent: "claude",
		Tokens: map[string]*string{"model": nil},
	}, t0)
	if got := a.Presentation(t0).Label("claude"); got != "claude opus · 23%" {
		t.Errorf("after removing a token: %q", got)
	}
}

// TestAValueWithALifetimeStopsBeingShown: "23% of context left" is true for a
// minute and misleading for an hour.
func TestAValueWithALifetimeStopsBeingShown(t *testing.T) {
	a := NewArbiter(knownAgents)
	a.ReportMetadata(MetadataReport{
		Source: "s", Agent: "claude",
		Tokens: map[string]*string{"ctx": str("23%")},
		TTL:    time.Minute,
	}, t0)

	if len(a.Presentation(t0.Add(30*time.Second)).Tokens) != 1 {
		t.Error("the value went before its time was up")
	}
	if got := a.Presentation(t0.Add(2 * time.Minute)).Tokens; len(got) != 0 {
		t.Errorf("the value outlived its lifetime: %v", got)
	}
	if !a.ExpireMetadata(t0.Add(2 * time.Minute)) {
		t.Error("expiring did not report that something went")
	}
	if a.ExpireMetadata(t0.Add(3 * time.Minute)) {
		t.Error("expiring reported a change with nothing left to expire")
	}
}

// TestMetadataFollowsTheSameRulesAsState: out of order, or about another
// agent, and it is not believed — for the same reasons.
func TestMetadataFollowsTheSameRulesAsState(t *testing.T) {
	a := NewArbiter(knownAgents)
	a.Observe("claude", 0, false, t0)

	a.ReportMetadata(MetadataReport{Source: "s", Agent: "claude", DisplayAgent: "one", Seq: seq(5)}, t0)
	if a.ReportMetadata(MetadataReport{Source: "s", Agent: "claude", DisplayAgent: "two", Seq: seq(4)}, t0) {
		t.Error("an older report was taken")
	}
	if got := a.Presentation(t0).DisplayAgent; got != "one" {
		t.Errorf("display agent = %q, want the newer report's", got)
	}
	if a.ReportMetadata(MetadataReport{Source: "s", Agent: "codex", DisplayAgent: "three", Seq: seq(9)}, t0) {
		t.Error("a report about another agent was taken")
	}
}
