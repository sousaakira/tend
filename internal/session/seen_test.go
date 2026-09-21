package session

import (
	"testing"

	"github.com/sousaakira/tend/internal/detect"
)

// TestAnAgentThatFinishesUnwatchedIsUnseen holds herdr's rule for "done": an
// agent that goes from working or blocked to idle while its tab is not in
// view is unseen until the tab is looked at or it starts again; in view, it
// is seen at once. If it regresses, an agent that finished in another tab
// looks exactly like one that has been idle for an hour.
func TestAnAgentThatFinishesUnwatchedIsUnseen(t *testing.T) {
	s, tab, a, _, _ := threePanes(t)
	pane := func() *Pane { p, _ := s.Pane(a); return p }

	_ = s.SetPaneStateWatched(a, "claude", detect.StateWorking, 0)
	seq := pane().StateSeq
	_ = s.SetPaneStateWatched(a, "claude", detect.StateIdle, 0)
	if !pane().Unseen {
		t.Error("finishing while its tab is not in view should be unseen")
	}
	if pane().StateSeq <= seq {
		t.Error("a change of state should move the sequence on")
	}
	if !s.MarkTabSeen(tab.ID) || pane().Unseen {
		t.Error("looking at the tab should see it")
	}
	if s.MarkTabSeen(tab.ID) {
		t.Error("a second look changes nothing")
	}

	_ = s.SetPaneStateWatched(a, "claude", detect.StateBlocked, 0)
	_ = s.SetPaneStateWatched(a, "claude", detect.StateIdle, tab.ID)
	if pane().Unseen {
		t.Error("finishing in the tab in view is seen")
	}

	_ = s.SetPaneStateWatched(a, "claude", detect.StateWorking, 0)
	_ = s.SetPaneStateWatched(a, "claude", detect.StateIdle, 0)
	_ = s.SetPaneStateWatched(a, "claude", detect.StateWorking, 0)
	if pane().Unseen {
		t.Error("starting again clears it")
	}
	// Unknown to idle is not a finish.
	_ = s.SetPaneStateWatched(a, "claude", detect.StateUnknown, 0)
	_ = s.SetPaneStateWatched(a, "claude", detect.StateIdle, 0)
	if pane().Unseen {
		t.Error("unknown to idle is not a finish")
	}
}
