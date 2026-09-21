//go:build unix

package server

import (
	"testing"

	"github.com/sousaakira/tend/internal/agent"
	"github.com/sousaakira/tend/internal/detect"
	"github.com/sousaakira/tend/internal/session"
)

func u64(n uint64) *uint64 { return &n }

// TestAHookReportReachesTheSessionAndItsClients: a report that stops at the
// arbiter is one nobody sees. It has to change the pane's record and tell
// subscribers, or the sidebar goes on showing what the screen last suggested.
func TestAHookReportReachesTheSessionAndItsClients(t *testing.T) {
	s := newServer(t)
	_, pane := openTab(t, s, "sleep 30")
	sub := s.Subscribe(64)
	defer sub.Close()

	ok, err := s.ReportAgent(pane, agent.Report{
		Source: "my-script", Agent: " deploy-bot ", State: detect.StateBlocked,
		Message: "needs approval", Seq: u64(1),
	})
	if err != nil || !ok {
		t.Fatalf("ReportAgent = %v, %v", ok, err)
	}
	ev := waitForEvent(t, sub, func(ev Event) bool { return ev.Kind == EventPaneState && ev.Pane == pane })
	if ev.State != detect.StateBlocked || ev.Rule != "hook:my-script" {
		t.Errorf("event = %+v, want blocked by hook:my-script", ev)
	}

	st, _ := s.PaneStatus(pane)
	if st.Agent != "deploy-bot" || st.State != detect.StateBlocked || st.Message != "needs approval" {
		t.Errorf("status = %+v", st)
	}
	s.Session(func(sess *session.Session) {
		if p, _ := sess.Pane(pane); p.Agent != "deploy-bot" || p.State != detect.StateBlocked {
			t.Errorf("session pane = %+v", p)
		}
	})

	// Released by the one who reported it, the pane is nobody's again.
	if ok, _ := s.ReleaseAgent(pane, "my-script", "deploy-bot", u64(2)); !ok {
		t.Fatal("the release was refused")
	}
	if st, _ := s.PaneStatus(pane); st.Agent != "" || st.State != detect.StateUnknown {
		t.Errorf("after release, status = %+v", st)
	}
}

// TestAReportMustNameAnAgentAndAPane: a hook with a bad environment must get an
// error it can log, not a silent success.
func TestAReportMustNameAnAgentAndAPane(t *testing.T) {
	s := newServer(t)
	_, pane := openTab(t, s, "sleep 30")
	if _, err := s.ReportAgent(pane, agent.Report{Source: "x", Agent: "  "}); err != ErrBadAgent {
		t.Errorf("err = %v, want ErrBadAgent", err)
	}
	if _, err := s.ReportAgent(pane+99, agent.Report{Source: "x", Agent: "a"}); err == nil {
		t.Error("a report for a pane that does not exist was accepted")
	}
}
