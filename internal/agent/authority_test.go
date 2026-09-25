package agent

import (
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/detect"
)

func seq(n uint64) *uint64 { return &n }

func knownAgents(label string) bool {
	switch label {
	case "claude", "codex", "pi":
		return true
	}
	return false
}

var t0 = time.Unix(1_000, 0)

// TestAHookIsBelievedOverTheScreen is the point of hooks. If it regresses, an
// agent that says it is working is shown as whatever its screen last looked
// like, which is the unreliability hooks exist to remove.
func TestAHookIsBelievedOverTheScreen(t *testing.T) {
	a := NewArbiter(knownAgents)
	a.Observe("codex", detect.StateIdle, false, t0)
	if !a.Report(Report{Source: "tend:codex", Agent: "codex", State: detect.StateWorking, Seq: seq(1)}, t0.Add(time.Second)) {
		t.Fatal("the report was refused")
	}
	if got := a.Effective(); got.State != detect.StateWorking || got.Source != "tend:codex" {
		t.Errorf("Effective = %+v, want working from the hook", got)
	}
}

// TestAReportForAnotherAgentIsIgnored: the screen says who is in the pane. A
// stale hook from an agent that ran there earlier must not relabel it.
func TestAReportForAnotherAgentIsIgnored(t *testing.T) {
	a := NewArbiter(knownAgents)
	a.Observe("claude", detect.StateIdle, false, t0)
	if a.Report(Report{Source: "tend:codex", Agent: "codex", State: detect.StateWorking, Seq: seq(1)}, t0) {
		t.Error("a codex report was believed in a pane running claude")
	}
	// A name detection could never produce is not contradicted by it.
	if !a.Report(Report{Source: "my-script", Agent: "deploy-bot", State: detect.StateWorking, Seq: seq(1)}, t0) {
		t.Error("a custom agent's report was refused")
	}
	if got := a.Effective().Agent; got != "deploy-bot" {
		t.Errorf("the pane is shown as %q, want the custom agent", got)
	}
}

// TestReportsOutOfOrderAreDropped: hooks are separate processes racing to a
// socket. Without this the pane flickers back to a state the agent has left.
func TestReportsOutOfOrderAreDropped(t *testing.T) {
	a := NewArbiter(knownAgents)
	a.Report(Report{Source: "s", Agent: "codex", State: detect.StateIdle, Seq: seq(5)}, t0)
	if a.Report(Report{Source: "s", Agent: "codex", State: detect.StateWorking, Seq: seq(4)}, t0) {
		t.Error("an older report replaced a newer one")
	}
	if a.Report(Report{Source: "s", Agent: "codex", State: detect.StateWorking}, t0) {
		t.Error("an unnumbered report was taken from a source that numbers them")
	}
	if got := a.Effective().State; got != detect.StateIdle {
		t.Errorf("state = %v, want idle", got)
	}
}

// TestABlockerOnTheScreenBeatsTheHook: an agent asking for permission is
// blocked whatever it last reported, and showing "working" hides the one state
// the user has to act on.
func TestABlockerOnTheScreenBeatsTheHook(t *testing.T) {
	a := NewArbiter(knownAgents)
	a.Observe("codex", detect.StateWorking, false, t0)
	a.Report(Report{Source: "tend:codex", Agent: "codex", State: detect.StateWorking, Seq: seq(1)}, t0.Add(time.Second))
	a.Observe("codex", detect.StateBlocked, true, t0.Add(2*time.Second))
	if got := a.Effective().State; got != detect.StateBlocked {
		t.Errorf("state = %v, want blocked", got)
	}
	// Seen before the hook spoke, it is old news and the hook stands.
	a.Report(Report{Source: "tend:codex", Agent: "codex", State: detect.StateWorking, Seq: seq(2)}, t0.Add(3*time.Second))
	if got := a.Effective().State; got != detect.StateWorking {
		t.Errorf("state = %v, want working once the hook is the newer word", got)
	}
}

// TestAFullLifecycleHookSilencesTheScreen, and only while its agent is there.
func TestAFullLifecycleHookSilencesTheScreen(t *testing.T) {
	a := NewArbiter(knownAgents)
	a.Observe("pi", detect.StateIdle, false, t0)
	a.Report(Report{Source: "tend:pi", Agent: "pi", State: detect.StateWorking, Seq: seq(1)}, t0.Add(time.Second))
	a.Observe("pi", detect.StateBlocked, true, t0.Add(2*time.Second))
	if !a.IgnoresScreen() || a.Effective().State != detect.StateWorking {
		t.Errorf("Effective = %+v, want the hook's word alone", a.Effective())
	}
}

// TestTheReportLeavesWithTheAgent: the last thing a hook said must not outlive
// the agent. Without this a pane back at a shell prompt shows "working" forever.
func TestTheReportLeavesWithTheAgent(t *testing.T) {
	a := NewArbiter(knownAgents)
	a.Observe("codex", detect.StateIdle, false, t0)
	a.Report(Report{
		Source: "tend:codex", Agent: "codex", State: detect.StateWorking, Seq: seq(1),
		Session: SessionRef{ID: "abc"},
	}, t0.Add(time.Second))
	a.Observe("", detect.StateUnknown, false, t0.Add(2*time.Second))

	if got := a.Effective(); got.Agent != "" || got.State != detect.StateUnknown {
		t.Errorf("Effective = %+v, want nothing", got)
	}
	// The conversation is kept: it can be resumed after the agent has gone.
	if s, ok := a.Session(); !ok || s.Session.ID != "abc" {
		t.Errorf("Session = %+v, %v; want the one the hook named", s, ok)
	}
	// And the source starts counting afresh, as a restarted agent does.
	if !a.Report(Report{Source: "tend:codex", Agent: "codex", State: detect.StateIdle, Seq: seq(1)}, t0.Add(3*time.Second)) {
		t.Error("a restarted agent's first report was taken for an old one")
	}
}

// TestASessionReportChangesNoState: Claude's hook says which conversation it is
// in and nothing else, and must not be read as "state unknown".
func TestASessionReportChangesNoState(t *testing.T) {
	a := NewArbiter(knownAgents)
	a.Observe("claude", detect.StateWorking, false, t0)
	if !a.ReportSession("tend:claude", "claude", SessionRefFromReport("tend:claude", "claude", "s1", "/t.jsonl"), seq(1)) {
		t.Fatal("the session report was refused")
	}
	if got := a.Effective(); got.State != detect.StateWorking || got.Source != "" {
		t.Errorf("Effective = %+v, want the screen's working", got)
	}
	// Claude resumes by id, so the path it also sent is not kept.
	if s, ok := a.Session(); !ok || s.Session.ID != "s1" || s.Session.Path != "" {
		t.Errorf("Session = %+v, %v", s, ok)
	}
	if a.Report(Report{Source: "tend:qwen", Agent: "qwen", State: detect.StateWorking, Seq: seq(1)}, t0) {
		t.Error("a session-only integration was allowed to set state")
	}
}

// TestOnlyTheHolderMayRelease, and releasing an agent the screen still shows
// drops the report, not the agent.
func TestOnlyTheHolderMayRelease(t *testing.T) {
	a := NewArbiter(knownAgents)
	a.Report(Report{Source: "mine", Agent: "deploy-bot", State: detect.StateWorking, Seq: seq(1)}, t0)
	if ok, _ := a.Release("other", "deploy-bot", seq(2)); ok {
		t.Error("another source released the agent")
	}
	ok, gone := a.Release("mine", "deploy-bot", seq(2))
	if !ok || !gone || a.Effective().Agent != "" {
		t.Errorf("Release = %v, %v; Effective = %+v", ok, gone, a.Effective())
	}

	a.Observe("codex", detect.StateIdle, false, t0)
	a.Report(Report{Source: "tend:codex", Agent: "codex", State: detect.StateWorking, Seq: seq(1)}, t0.Add(time.Second))
	if ok, gone := a.Release("tend:codex", "codex", seq(2)); !ok || gone {
		t.Errorf("Release = %v, %v; want released, agent still there", ok, gone)
	}
	if got := a.Effective(); got.Agent != "codex" || got.State != detect.StateIdle {
		t.Errorf("Effective = %+v, want the screen's codex", got)
	}
}

// TestOnlyTheOfficialIntegrationNamesASession: the reference ends up on a
// command line. A script that could name any file as "the session" would be
// choosing what tend runs.
func TestOnlyTheOfficialIntegrationNamesASession(t *testing.T) {
	if ref := SessionRefFromReport("my-script", "claude", "x", "/etc/passwd"); !ref.Empty() {
		t.Errorf("an unofficial source named a session: %+v", ref)
	}
	if ref := SessionRefFromReport("tend:pi", "pi", "id-1", "/s.jsonl"); ref.Kind() != "path" || ref.Value() != "/s.jsonl" {
		t.Errorf("pi resumes from its transcript, got %+v", ref)
	}
	if ref := SessionRefFromReport("tend:codex", "codex", "id-1", "/s.jsonl"); ref.Kind() != "id" || ref.Value() != "id-1" {
		t.Errorf("codex resumes by id, got %+v", ref)
	}
}
