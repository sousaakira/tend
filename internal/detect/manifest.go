// Package detect decides whether an agent is working, blocked or idle by
// matching declarative rules against a snapshot of its screen.
//
// Detection is decoupled by design: it reads text and nothing else. It never
// touches the parser, the PTY or viewport state. That is what makes it
// testable against fixed strings, and what stops a detection change from
// being able to break a terminal.
//
// Rules live in TOML manifests, one per agent, so supporting a new agent — or
// fixing one whose UI changed — is an edit to data rather than to code.
package detect

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// EngineVersion is the rule vocabulary this build understands. A manifest
// declaring a higher min_engine_version is rejected rather than
// half-evaluated: silently ignoring rules it depends on would report a
// confident wrong state.
//
// 1: the base vocabulary.
// 2: prompt-box and prompt-marker regions.
// 3: top_non_empty_lines.
const EngineVersion = 3

// State is what a rule concludes about the agent.
type State uint8

const (
	// StateUnknown is both "no rule matched" and the explicit "unknown" state
	// a rule may assert to stop lower-priority rules from guessing.
	StateUnknown State = iota
	StateIdle
	StateWorking
	StateBlocked
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateWorking:
		return "working"
	case StateBlocked:
		return "blocked"
	default:
		return "unknown"
	}
}

// StateFromString reads a state's name, for the client, which receives states
// over the wire as the words String produces.
func StateFromString(s string) State {
	state, err := parseState(s)
	if err != nil {
		return StateUnknown
	}
	return state
}

func parseState(s string) (State, error) {
	switch s {
	case "idle":
		return StateIdle, nil
	case "working":
		return StateWorking, nil
	case "blocked":
		return StateBlocked, nil
	case "unknown", "":
		return StateUnknown, nil
	}
	return StateUnknown, fmt.Errorf("unknown state %q", s)
}

// rawManifest mirrors the TOML on disk. It is separate from Manifest so that
// decoding stays a dumb mapping and every derived value — compiled patterns,
// parsed states — is produced by one validating step.
type rawManifest struct {
	ID               string    `toml:"id"`
	Version          string    `toml:"version"`
	MinEngineVersion int       `toml:"min_engine_version"`
	UpdatedAt        string    `toml:"updated_at"`
	Aliases          []string  `toml:"aliases"`
	Rules            []rawRule `toml:"rules"`
}

type rawRule struct {
	ID              string `toml:"id"`
	State           string `toml:"state"`
	Priority        int    `toml:"priority"`
	Region          string `toml:"region"`
	VisibleIdle     bool   `toml:"visible_idle"`
	VisibleBlocker  bool   `toml:"visible_blocker"`
	VisibleWorking  bool   `toml:"visible_working"`
	SkipStateUpdate bool   `toml:"skip_state_update"`

	rawGate
}

// rawGate is the matcher vocabulary, shared by a rule and by its nested gates.
type rawGate struct {
	All       []rawGate `toml:"all"`
	Any       []rawGate `toml:"any"`
	Not       []rawGate `toml:"not"`
	Contains  []string  `toml:"contains"`
	Regex     []string  `toml:"regex"`
	LineRegex []string  `toml:"line_regex"`
}

// Manifest is a compiled set of rules for one agent.
type Manifest struct {
	ID               string
	Version          string
	MinEngineVersion int
	Aliases          []string
	Rules            []Rule
}

// Rule is one compiled detection rule.
type Rule struct {
	ID       string
	State    State
	Priority int
	Region   string

	// VisibleIdle, VisibleBlocker and VisibleWorking mark a rule whose
	// evidence is visible on screen, as opposed to inferred. They apply only
	// when the rule's own state matches the flag.
	VisibleIdle    bool
	VisibleBlocker bool
	VisibleWorking bool

	// SkipStateUpdate reports a state without letting it overwrite the stored
	// one: the screen is ambiguous and the previous conclusion should stand.
	SkipStateUpdate bool

	gate gate
}

// gate is a compiled matcher tree.
type gate struct {
	all       []gate
	any       []gate
	not       []gate
	contains  []string // lowercased at compile time
	regex     []*regexp.Regexp
	lineRegex []*regexp.Regexp

	// usesContains is true when this gate or anything below it tests a
	// substring, and so needs a lowercased copy of the region. Most rules do
	// not, and lowercasing a screen per rule is real cost on a per-pane path.
	usesContains bool
}

// ParseManifest compiles a manifest from TOML.
//
// Unknown fields are an error. A manifest is data that ships separately from
// the binary, so a typo in a field name must fail loudly instead of quietly
// disabling the rule it was meant to configure.
func ParseManifest(data []byte) (*Manifest, error) {
	var raw rawManifest
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, k := range undecoded {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("unknown field(s): %s", strings.Join(keys, ", "))
	}

	if raw.ID == "" {
		return nil, fmt.Errorf("manifest has no id")
	}
	if raw.MinEngineVersion > EngineVersion {
		return nil, fmt.Errorf("manifest %q needs engine version %d, this build understands %d",
			raw.ID, raw.MinEngineVersion, EngineVersion)
	}

	m := &Manifest{
		ID:               raw.ID,
		Version:          raw.Version,
		MinEngineVersion: raw.MinEngineVersion,
		Aliases:          raw.Aliases,
		Rules:            make([]Rule, 0, len(raw.Rules)),
	}

	seen := make(map[string]struct{}, len(raw.Rules))
	for i, rr := range raw.Rules {
		if rr.ID == "" {
			return nil, fmt.Errorf("rule %d in %q has no id", i, raw.ID)
		}
		if _, dup := seen[rr.ID]; dup {
			return nil, fmt.Errorf("duplicate rule id %q in %q", rr.ID, raw.ID)
		}
		seen[rr.ID] = struct{}{}

		state, err := parseState(rr.State)
		if err != nil {
			return nil, fmt.Errorf("rule %q in %q: %w", rr.ID, raw.ID, err)
		}
		region := rr.Region
		if region == "" {
			region = RegionWholeRecent
		}
		if err := validateRegion(region, raw.MinEngineVersion); err != nil {
			return nil, fmt.Errorf("rule %q in %q: %w", rr.ID, raw.ID, err)
		}
		g, err := compileGate(rr.rawGate)
		if err != nil {
			return nil, fmt.Errorf("rule %q in %q: %w", rr.ID, raw.ID, err)
		}

		m.Rules = append(m.Rules, Rule{
			ID:              rr.ID,
			State:           state,
			Priority:        rr.Priority,
			Region:          region,
			VisibleIdle:     rr.VisibleIdle,
			VisibleBlocker:  rr.VisibleBlocker,
			VisibleWorking:  rr.VisibleWorking,
			SkipStateUpdate: rr.SkipStateUpdate,
			gate:            g,
		})
	}
	return m, nil
}

func compileGate(raw rawGate) (gate, error) {
	var g gate
	var err error

	if g.all, err = compileGates(raw.All); err != nil {
		return g, err
	}
	if g.any, err = compileGates(raw.Any); err != nil {
		return g, err
	}
	if g.not, err = compileGates(raw.Not); err != nil {
		return g, err
	}

	if len(raw.Contains) > 0 {
		g.contains = make([]string, len(raw.Contains))
		for i, needle := range raw.Contains {
			// Lowered once here so matching only lowers the region text.
			g.contains[i] = strings.ToLower(needle)
		}
	}
	if g.regex, err = compilePatterns(raw.Regex); err != nil {
		return g, err
	}
	if g.lineRegex, err = compilePatterns(raw.LineRegex); err != nil {
		return g, err
	}

	g.usesContains = len(g.contains) > 0 ||
		anyUsesContains(g.all) || anyUsesContains(g.any) || anyUsesContains(g.not)
	return g, nil
}

func anyUsesContains(gates []gate) bool {
	for i := range gates {
		if gates[i].usesContains {
			return true
		}
	}
	return false
}

func compileGates(raws []rawGate) ([]gate, error) {
	if len(raws) == 0 {
		return nil, nil
	}
	out := make([]gate, len(raws))
	for i, raw := range raws {
		g, err := compileGate(raw)
		if err != nil {
			return nil, err
		}
		out[i] = g
	}
	return out, nil
}

func compilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	out := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		// Manifests are written in Rust's regex dialect; see dialect.go.
		re, err := regexp.Compile(translatePattern(p))
		if err != nil {
			return nil, fmt.Errorf("regex %q: %w", p, err)
		}
		out[i] = re
	}
	return out, nil
}

// matches reports whether the gate holds for the region text.
//
// An empty gate matches. Every list is conjunctive except any, which is
// disjunctive and only applies when non-empty, and not, which fails the gate
// if any of its branches match. The ordering here is cheapest-first:
// substring tests before regexes, and the nested trees last.
func (g *gate) matches(text, lowerText string) bool {
	for _, needle := range g.contains {
		if !strings.Contains(lowerText, needle) {
			return false
		}
	}
	for _, re := range g.regex {
		if !re.MatchString(text) {
			return false
		}
	}
	for _, re := range g.lineRegex {
		if !matchesAnyLine(re, text) {
			return false
		}
	}
	for i := range g.all {
		if !g.all[i].matches(text, lowerText) {
			return false
		}
	}
	if len(g.any) > 0 {
		ok := false
		for i := range g.any {
			if g.any[i].matches(text, lowerText) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for i := range g.not {
		if g.not[i].matches(text, lowerText) {
			return false
		}
	}
	return true
}

// matchesAnyLine reports whether re matches at least one line of text. It
// walks the text in place rather than allocating a slice of lines: this runs
// per rule × per pane.
func matchesAnyLine(re *regexp.Regexp, text string) bool {
	for len(text) > 0 {
		line := text
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			line, text = text[:i], text[i+1:]
		} else {
			text = ""
		}
		line = strings.TrimSuffix(line, "\r")
		if re.MatchString(line) {
			return true
		}
	}
	return false
}
