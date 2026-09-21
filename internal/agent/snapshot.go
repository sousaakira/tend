// Package agent joins the terminal core to agent detection.
//
// It exists so neither side has to know the other: vt produces screen state
// without any idea what an agent is, detect consumes text without any idea
// where it came from, and this is the one place that turns one into the other.
package agent

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sousaakira/tend/internal/detect"
	"github.com/sousaakira/tend/internal/vt"
)

// Snapshot builds detection input from a terminal.
//
// The text is the active screen, which is always the bottom of the buffer.
// That is deliberate: a user scrolling back must not change what an agent
// appears to be doing, so detection never reads a scrolled viewport.
//
// Trailing blank rows are dropped. A terminal is mostly empty below the
// cursor, and leaving forty blank lines in would push every line-counted
// region past the content it was written to find.
func Snapshot(s *vt.Screen) detect.Input {
	return detect.Input{
		Screen:      ScreenText(s),
		OSCTitle:    s.Title(),
		OSCProgress: s.Progress(),
	}
}

// ScreenText renders the active screen as plain text, one line per row.
func ScreenText(s *vt.Screen) string {
	g := s.Grid()
	rows := g.Rows()

	last := -1
	for y := 0; y < rows; y++ {
		if g.Line(y).Text() != "" {
			last = y
		}
	}
	if last < 0 {
		return ""
	}

	var b strings.Builder
	for y := 0; y <= last; y++ {
		if y > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(g.Line(y).Text())
	}
	return b.String()
}

// Detector tracks one pane's agent state over time.
//
// Detection itself is stateless — a rule either matches the current screen or
// it does not. This adds the small amount of memory a caller actually needs:
// which state was last reported, and the rule that concluded it.
type Detector struct {
	manifest *detect.Manifest

	state  detect.State
	ruleID string
	known  bool
}

// NewDetector returns a detector for one agent.
func NewDetector(m *detect.Manifest) *Detector {
	return &Detector{manifest: m}
}

// Agent returns the manifest id.
func (d *Detector) Agent() string {
	if d.manifest == nil {
		return ""
	}
	return d.manifest.ID
}

// Manifest is the rules this detector is using, for anything that has to
// explain what they did.
func (d *Detector) Manifest() *detect.Manifest { return d.manifest }

// State returns the last state reported.
func (d *Detector) State() detect.State { return d.state }

// Rule returns the id of the rule behind the current state.
func (d *Detector) Rule() string { return d.ruleID }

// Update runs detection and reports whether the state changed.
//
// A rule marked skip_state_update is honoured here: it means the screen is
// ambiguous, so the previous conclusion stands rather than being replaced by
// a guess. That decision belongs at this level, not in the matcher.
func (d *Detector) Update(s *vt.Screen) (detect.Result, bool) {
	if d.manifest == nil {
		return detect.Result{}, false
	}
	res := d.manifest.Detect(Snapshot(s))

	if res.SkipStateUpdate {
		return res, false
	}
	if d.known && res.State == d.state {
		return res, false
	}
	d.state = res.State
	d.ruleID = res.RuleID
	d.known = true
	return res, true
}

// ResolveManifest picks the detection manifest for a command.
//
// An explicit name must exist, because naming an agent that is not there is a
// mistake worth reporting. An empty name is inferred from the command's own
// name, and a command matching nothing yields no manifest and no error: plenty
// of useful panes are not agents, and a shell should not fail to open because
// nobody wrote rules for it.
func ResolveManifest(c *detect.Catalog, explicit, command string) (*detect.Manifest, error) {
	if c == nil {
		return nil, errors.New("agent: no manifest catalog")
	}
	if explicit != "" {
		m, ok := c.Lookup(explicit)
		if !ok {
			return nil, fmt.Errorf("agent: unknown agent %q", explicit)
		}
		return m, nil
	}
	base := strings.TrimSuffix(filepath.Base(command), ".exe")
	if m, ok := c.Lookup(base); ok {
		return m, nil
	}
	return nil, nil
}
