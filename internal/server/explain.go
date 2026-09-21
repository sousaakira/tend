package server

import (
	"fmt"

	"github.com/sousaakira/tend/internal/agent"
	"github.com/sousaakira/tend/internal/session"
)

// "Why does it say that?" is the question detection raises most often: an
// agent shown as working when it is waiting, or the other way round, and no
// way to see which rule decided. herdr answers it with `agent explain`, which
// runs the manifest over the pane's current screen and reports every rule and
// what it saw.
//
// It is the tool for writing a manifest, and the first thing to reach for when
// a state is wrong — the alternative is guessing at a screen that has since
// changed.

// Explanation is why a pane is shown as it is.
type Explanation struct {
	Pane  uint64 `json:"pane_id"`
	Agent string `json:"agent,omitempty"`
	State string `json:"state"`
	// Source says where the answer came from: a hook, or the screen.
	Source string `json:"source"`
	// Rule is the rule that decided, when the screen did.
	Rule     string `json:"matched_rule,omitempty"`
	Region   string `json:"region,omitempty"`
	Priority int    `json:"priority,omitempty"`

	VisibleIdle     bool `json:"visible_idle,omitempty"`
	VisibleBlocker  bool `json:"visible_blocker,omitempty"`
	VisibleWorking  bool `json:"visible_working,omitempty"`
	SkipStateUpdate bool `json:"skip_state_update,omitempty"`
	// FallbackReason says why the state is what it is when no rule matched:
	// herdr\'s default_known_agent_idle_fallback.
	FallbackReason string `json:"fallback_reason,omitempty"`

	// Rules is every rule of the manifest and what it did.
	Rules []RuleReport `json:"evaluated_rules"`
	// Screen is what detection read, so an explanation can be checked against
	// what was actually on the screen rather than what it was expected to be.
	Screen string `json:"screen,omitempty"`
	Title  string `json:"osc_title,omitempty"`
}

// RuleReport is one rule and what it saw.
type RuleReport struct {
	ID          string `json:"id"`
	Region      string `json:"region"`
	State       string `json:"state"`
	Priority    int    `json:"priority"`
	Matched     bool   `json:"matched"`
	RegionBytes int    `json:"region_bytes"`
}

// Explain runs the pane's manifest over its screen now and says what happened.
func (s *Server) Explain(id session.PaneID, withScreen bool) (Explanation, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return Explanation{}, err
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	out := Explanation{
		Pane:   uint64(id),
		Agent:  rt.shown.Agent,
		State:  rt.shown.State.String(),
		Source: "screen",
	}
	if rt.shown.Source != "" {
		// A hook is answering for this pane, so the screen is not what the
		// state came from — and saying which rule matched would be a lie.
		out.Source = "hook:" + rt.shown.Source
	}
	if rt.detector == nil {
		return out, fmt.Errorf("server: pane %d is not running an agent tend can recognise", id)
	}

	manifest := rt.detector.Manifest()
	if manifest == nil {
		return out, fmt.Errorf("server: pane %d has no manifest", id)
	}
	in := agent.Snapshot(rt.screen)
	result, rules := manifest.Explain(in)

	out.Rule, out.Region, out.Priority = result.RuleID, result.Region, result.Priority
	out.VisibleIdle, out.VisibleBlocker = result.VisibleIdle, result.VisibleBlocker
	out.VisibleWorking, out.SkipStateUpdate = result.VisibleWorking, result.SkipStateUpdate
	out.FallbackReason = result.FallbackReason
	for _, rule := range rules {
		out.Rules = append(out.Rules, RuleReport{
			ID: rule.RuleID, Region: rule.Region, State: rule.State.String(),
			Priority: rule.Priority, Matched: rule.Matched, RegionBytes: rule.RegionBytes,
		})
	}
	if withScreen {
		out.Screen = in.Screen
		out.Title = in.OSCTitle
	}
	return out, nil
}
