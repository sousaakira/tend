package detect

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

// Input is the snapshot detection reads.
//
// Screen must come from the bottom of the terminal buffer, never from the
// user-visible viewport: a user scrolling back would otherwise change what the
// agent appears to be doing. OSCTitle and OSCProgress come from the terminal's
// own fields rather than the grid, since an agent can report progress without
// drawing anything.
type Input struct {
	Screen      string
	OSCTitle    string
	OSCProgress string
}

// Result is what detection concluded.
type Result struct {
	// Matched reports whether any rule fired. When false, State is idle —
	// herdr's default for a known agent no rule speaks for — FallbackReason
	// says so, and the rule fields are empty.
	Matched        bool
	State          State
	FallbackReason string

	RuleID   string
	Priority int
	Region   string

	// VisibleIdle, VisibleBlocker and VisibleWorking mark a conclusion drawn
	// from evidence actually on screen. They are set only when the matched
	// rule both carries the flag and concluded the matching state.
	VisibleIdle    bool
	VisibleBlocker bool
	VisibleWorking bool

	// SkipStateUpdate asks the caller to report this state without storing it:
	// the screen was ambiguous and the previous conclusion should stand.
	SkipStateUpdate bool
}

// RuleEvaluation is the per-rule detail behind a Result, for explaining a
// detection rather than performing one.
type RuleEvaluation struct {
	RuleID      string
	Region      string
	State       State
	Priority    int
	Matched     bool
	RegionBytes int
}

// Detect evaluates the manifest against the snapshot.
//
// Every rule is evaluated; the highest priority that matched wins, and a tie
// goes to the earlier rule in the file. Rule order is therefore meaningful and
// reordering a manifest can change behaviour even when priorities do not.
func (m *Manifest) Detect(in Input) Result {
	best, _ := m.evaluate(in, nil)
	return m.result(best)
}

// Explain evaluates the manifest and reports what every rule did. It is for
// diagnosis; Detect is the hot path.
func (m *Manifest) Explain(in Input) (Result, []RuleEvaluation) {
	evals := make([]RuleEvaluation, 0, len(m.Rules))
	best, evals := m.evaluate(in, evals)
	return m.result(best), evals
}

// KnownAgentIdleFallback is why a known agent with no rule matching is idle:
// herdr's DEFAULT_KNOWN_AGENT_IDLE_FALLBACK. An agent's screen at rest is
// the one thing its rules usually do not describe — they describe working
// and waiting — so "recognised, and nothing to say" means it is at its
// prompt. tend used to call that unknown, and OpenCode sitting at its
// prompt was never idle.
const KnownAgentIdleFallback = "default_known_agent_idle_fallback"

func (m *Manifest) result(best int) Result {
	if best < 0 {
		return Result{State: StateIdle, FallbackReason: KnownAgentIdleFallback}
	}
	r := &m.Rules[best]
	return Result{
		Matched:         true,
		State:           r.State,
		RuleID:          r.ID,
		Priority:        r.Priority,
		Region:          r.Region,
		VisibleIdle:     r.VisibleIdle && r.State == StateIdle,
		VisibleBlocker:  r.VisibleBlocker && r.State == StateBlocked,
		VisibleWorking:  r.VisibleWorking && r.State == StateWorking,
		SkipStateUpdate: r.SkipStateUpdate,
	}
}

// evaluate returns the index of the winning rule, or -1. When evals is
// non-nil it is appended to with one entry per rule.
func (m *Manifest) evaluate(in Input, evals []RuleEvaluation) (int, []RuleEvaluation) {
	snap := newSnapshot(in)
	cache := regionCache{snap: &snap}
	best := -1

	for i := range m.Rules {
		r := &m.Rules[i]
		text, lower := cache.get(r.Region, r.gate.usesContains)
		matched := r.gate.matches(text, lower)

		if evals != nil {
			evals = append(evals, RuleEvaluation{
				RuleID:      r.ID,
				Region:      r.Region,
				State:       r.State,
				Priority:    r.Priority,
				Matched:     matched,
				RegionBytes: len(text),
			})
		}
		if !matched {
			continue
		}
		// Strictly greater wins, so an earlier rule holds a tie.
		if best >= 0 && m.Rules[best].Priority >= r.Priority {
			continue
		}
		best = i
	}
	return best, evals
}

// regionCache memoises region text across the rules of one detection. Rules
// share regions heavily — whole_recent alone covers a third of them — and
// lowercasing a screen for every rule is the kind of per-pane cost that adds
// up across a workspace.
type regionCache struct {
	snap    *snapshot
	entries []regionEntry
}

type regionEntry struct {
	spec     string
	text     string
	lower    string
	hasLower bool
}

func (c *regionCache) get(spec string, needLower bool) (text, lower string) {
	for i := range c.entries {
		e := &c.entries[i]
		if e.spec != spec {
			continue
		}
		if needLower && !e.hasLower {
			e.lower = strings.ToLower(e.text)
			e.hasLower = true
		}
		return e.text, e.lower
	}
	e := regionEntry{spec: spec, text: c.snap.region(spec)}
	if needLower {
		e.lower = strings.ToLower(e.text)
		e.hasLower = true
	}
	c.entries = append(c.entries, e)
	return e.text, e.lower
}

// --- bundled catalog -------------------------------------------------------

//go:embed manifests/*.toml
var bundledFS embed.FS

// Catalog is a set of manifests addressable by id or alias.
type Catalog struct {
	manifests []*Manifest
	byName    map[string]*Manifest
}

var (
	bundledOnce sync.Once
	bundled     *Catalog
	bundledErr  error
)

// Bundled returns the manifests compiled into the binary. It parses them once
// and reuses the result; a failure here is a build-time mistake, since the
// manifests ship with the binary.
func Bundled() (*Catalog, error) {
	bundledOnce.Do(func() {
		bundled, bundledErr = LoadFS(bundledFS, "manifests")
	})
	return bundled, bundledErr
}

// LoadFS parses every .toml file in dir, which is how both the bundled
// manifests and a user's override directory are read.
func LoadFS(fsys fs.FS, dir string) (*Catalog, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	c := &Catalog{byName: make(map[string]*Manifest)}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		name := path.Join(dir, entry.Name())
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		m, err := ParseManifest(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if err := c.add(m); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
	}
	sort.Slice(c.manifests, func(i, j int) bool {
		return c.manifests[i].ID < c.manifests[j].ID
	})
	return c, nil
}

func (c *Catalog) add(m *Manifest) error {
	names := append([]string{m.ID}, m.Aliases...)
	for _, name := range names {
		key := strings.ToLower(name)
		if existing, dup := c.byName[key]; dup {
			return fmt.Errorf("name %q already claimed by manifest %q", name, existing.ID)
		}
		c.byName[key] = m
	}
	c.manifests = append(c.manifests, m)
	return nil
}

// Lookup finds a manifest by id or alias, case-insensitively.
func (c *Catalog) Lookup(name string) (*Manifest, bool) {
	m, ok := c.byName[strings.ToLower(name)]
	return m, ok
}

// Manifests returns every manifest, ordered by id.
func (c *Catalog) Manifests() []*Manifest { return c.manifests }

// IDs returns the manifest ids, ordered.
func (c *Catalog) IDs() []string {
	out := make([]string, len(c.manifests))
	for i, m := range c.manifests {
		out[i] = m.ID
	}
	return out
}
