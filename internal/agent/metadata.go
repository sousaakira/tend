package agent

import (
	"sort"
	"strings"
	"time"
)

// A hook can say more about an agent than what state it is in: what model it
// is using, how much context is left, what it calls itself, what to call the
// state it is in. herdr calls this metadata (`terminal/metadata.rs`), and its
// sidebar is mostly made of it.
//
// It is reported the same way state is — by source, in order, with a sequence
// — and for the same reason: several things may report about one pane, and the
// last one to arrive is not always the last one sent.
//
// Two things make it different from state. A value can be given a lifetime,
// because "23% of context left" is true for a minute and misleading for an
// hour; and it is presentation, so nothing here decides what an agent is
// doing, only what is written next to it.

// maxTokens is how many values one pane keeps. A hook reporting a new key
// every second would otherwise grow without end.
const maxTokens = 32

// maxMetadataSources is how many things may report about one pane.
const maxMetadataSources = 8

// MetadataReport is one thing a hook said about how to show a pane.
type MetadataReport struct {
	Source string
	Agent  string
	// Title and DisplayAgent replace what the pane is called and what the
	// agent is called. Empty means "no change"; the Clear fields remove.
	Title        string
	DisplayAgent string
	// StateLabels rename a state for this pane: "blocked" might be "waiting
	// for approval".
	StateLabels map[string]string
	// Tokens are values to show beside the agent. A nil value removes one.
	Tokens map[string]*string
	// TTL is how long the tokens in this report stay true. Zero means until
	// something replaces them.
	TTL time.Duration

	ClearTitle        bool
	ClearDisplayAgent bool
	ClearStateLabels  bool

	Seq *uint64
}

// metadataEntry is what one source has said.
type metadataEntry struct {
	agent        string
	title        string
	displayAgent string
	stateLabels  map[string]string
	reportedAt   time.Time
}

// token is one value and when it stops being true.
type token struct {
	value     string
	expiresAt time.Time
}

// Presentation is what to show about a pane, on top of its state.
type Presentation struct {
	Title        string            `json:"title,omitempty"`
	DisplayAgent string            `json:"display_agent,omitempty"`
	StateLabels  map[string]string `json:"state_labels,omitempty"`
	Tokens       []Token           `json:"tokens,omitempty"`
}

// Token is one value to show, with the key it was reported under.
type Token struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Empty reports whether there is nothing to show.
func (p Presentation) Empty() bool {
	return p.Title == "" && p.DisplayAgent == "" && len(p.StateLabels) == 0 && len(p.Tokens) == 0
}

// Label is the one-line summary a list shows: the agent, then its tokens.
func (p Presentation) Label(agent string) string {
	name := p.DisplayAgent
	if name == "" {
		name = agent
	}
	if len(p.Tokens) == 0 {
		return name
	}
	parts := make([]string, 0, len(p.Tokens)+1)
	if name != "" {
		parts = append(parts, name)
	}
	for _, t := range p.Tokens {
		parts = append(parts, t.Value)
	}
	return strings.Join(parts, " · ")
}

// ReportMetadata records what a source said, and reports whether it changed
// anything worth redrawing.
func (a *Arbiter) ReportMetadata(r MetadataReport, now time.Time) bool {
	if r.Source == "" {
		return false
	}
	if a.conflicts(r.Agent) {
		// About an agent that is not the one here: the same rule as a state
		// report, for the same reason.
		return false
	}
	if !a.acceptMetadata(r.Source, r.Seq) {
		return false
	}
	if a.metadata == nil {
		a.metadata = map[string]*metadataEntry{}
	}
	if _, known := a.metadata[r.Source]; !known && len(a.metadata) >= maxMetadataSources {
		return false
	}

	entry := a.metadata[r.Source]
	if entry == nil {
		entry = &metadataEntry{}
		a.metadata[r.Source] = entry
	}
	before := a.Presentation(now)

	entry.agent = r.Agent
	entry.reportedAt = now
	if r.ClearTitle {
		entry.title = ""
	}
	if r.Title != "" {
		entry.title = r.Title
	}
	if r.ClearDisplayAgent {
		entry.displayAgent = ""
	}
	if r.DisplayAgent != "" {
		entry.displayAgent = r.DisplayAgent
	}
	if r.ClearStateLabels {
		entry.stateLabels = nil
	}
	for state, label := range r.StateLabels {
		if entry.stateLabels == nil {
			entry.stateLabels = map[string]string{}
		}
		entry.stateLabels[state] = label
	}
	a.patchTokens(r, now)

	return !equalPresentation(before, a.Presentation(now))
}

// patchTokens applies a report's tokens, removing the ones set to nothing.
func (a *Arbiter) patchTokens(r MetadataReport, now time.Time) {
	if len(r.Tokens) == 0 {
		return
	}
	if a.tokens == nil {
		a.tokens = map[string]token{}
	}
	var expires time.Time
	if r.TTL > 0 {
		expires = now.Add(r.TTL)
	}
	for key, value := range r.Tokens {
		if value == nil {
			delete(a.tokens, key)
			continue
		}
		if _, known := a.tokens[key]; !known && len(a.tokens) >= maxTokens {
			continue
		}
		a.tokens[key] = token{value: *value, expiresAt: expires}
	}
}

// acceptMetadata orders metadata reports per source, apart from state.
func (a *Arbiter) acceptMetadata(source string, seq *uint64) bool {
	if a.metadataSeqs == nil {
		a.metadataSeqs = map[string]uint64{}
	}
	last, seen := a.metadataSeqs[source]
	if seq == nil {
		return true // a source that does not number them says so every time
	}
	if seen && *seq <= last {
		return false
	}
	a.metadataSeqs[source] = *seq
	return true
}

// Presentation is what to show about the pane now, with expired values gone.
func (a *Arbiter) Presentation(now time.Time) Presentation {
	out := Presentation{}
	if len(a.metadata) > 0 {
		// The most recent report of each thing wins, which is what a user
		// watching one pane expects when two things report about it.
		var newest time.Time
		for _, entry := range a.metadata {
			if entry.title != "" && !entry.reportedAt.Before(newest) {
				out.Title = entry.title
			}
			if entry.displayAgent != "" && !entry.reportedAt.Before(newest) {
				out.DisplayAgent = entry.displayAgent
			}
			for state, label := range entry.stateLabels {
				if out.StateLabels == nil {
					out.StateLabels = map[string]string{}
				}
				out.StateLabels[state] = label
			}
			if entry.reportedAt.After(newest) {
				newest = entry.reportedAt
			}
		}
	}

	if len(a.tokens) > 0 {
		live := make([]Token, 0, len(a.tokens))
		for key, t := range a.tokens {
			if !t.expiresAt.IsZero() && !now.Before(t.expiresAt) {
				continue // said for a while, and the while is over
			}
			live = append(live, Token{Key: key, Value: t.value})
		}
		// By key, because two values reported in one message arrive in a map
		// and have no order of their own. Anything else reshuffles the line
		// every time a hook repeats itself.
		sort.Slice(live, func(i, j int) bool { return live[i].Key < live[j].Key })
		out.Tokens = live
	}
	return out
}

// ExpireMetadata drops what has run out and reports whether anything went.
func (a *Arbiter) ExpireMetadata(now time.Time) bool {
	var gone bool
	for key, t := range a.tokens {
		if !t.expiresAt.IsZero() && !now.Before(t.expiresAt) {
			delete(a.tokens, key)
			gone = true
		}
	}
	return gone
}

// ClearMetadata forgets what a source said, or everything when unnamed.
func (a *Arbiter) ClearMetadata(source string) {
	if source == "" {
		a.metadata, a.tokens, a.metadataSeqs = nil, nil, nil
		return
	}
	delete(a.metadata, source)
	delete(a.metadataSeqs, source)
}

func equalPresentation(a, b Presentation) bool {
	if a.Title != b.Title || a.DisplayAgent != b.DisplayAgent {
		return false
	}
	if len(a.StateLabels) != len(b.StateLabels) || len(a.Tokens) != len(b.Tokens) {
		return false
	}
	for k, v := range a.StateLabels {
		if b.StateLabels[k] != v {
			return false
		}
	}
	for i := range a.Tokens {
		if a.Tokens[i] != b.Tokens[i] {
			return false
		}
	}
	return true
}
