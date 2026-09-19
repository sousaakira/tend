package detect

import (
	"strings"
	"testing"
)

// TestBundledManifestsLoad is the load-bearing test of this package.
//
// The manifests are inherited from herdr, where they were calibrated against
// 22 real agent UIs and their patterns compiled with Rust's regex crate. The
// premise of reusing them is that both that crate and Go's regexp are RE2 —
// no lookaround, no backreferences, the same \x{...} and (?m) syntax. If that
// premise is wrong, it is wrong here, loudly, rather than at runtime on a
// pane nobody is watching.
func TestBundledManifestsLoad(t *testing.T) {
	c, err := Bundled()
	if err != nil {
		t.Fatalf("loading bundled manifests: %v", err)
	}
	ids := c.IDs()
	if len(ids) < 20 {
		t.Fatalf("loaded %d manifests (%v), want the full bundled set", len(ids), ids)
	}

	rules := 0
	patterns := 0
	for _, m := range c.Manifests() {
		if m.ID == "" {
			t.Error("a manifest has no id")
		}
		if len(m.Rules) == 0 {
			t.Errorf("manifest %q has no rules", m.ID)
		}
		if m.MinEngineVersion > EngineVersion {
			t.Errorf("manifest %q needs engine %d", m.ID, m.MinEngineVersion)
		}
		for _, r := range m.Rules {
			rules++
			patterns += countPatterns(&r.gate)
		}
	}
	t.Logf("loaded %d manifests, %d rules, %d compiled patterns", len(ids), rules, patterns)
	if patterns == 0 {
		t.Error("no patterns compiled — the manifests did not carry over")
	}
}

func countPatterns(g *gate) int {
	n := len(g.regex) + len(g.lineRegex)
	for i := range g.all {
		n += countPatterns(&g.all[i])
	}
	for i := range g.any {
		n += countPatterns(&g.any[i])
	}
	for i := range g.not {
		n += countPatterns(&g.not[i])
	}
	return n
}

// TestBundledManifestsAreAddressable checks the ids and aliases agents will
// actually be looked up by.
func TestBundledManifestsAreAddressable(t *testing.T) {
	c, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"claude", "codex", "cursor", "gemini", "opencode"} {
		if _, ok := c.Lookup(want); !ok {
			t.Errorf("no manifest for %q; have %v", want, c.IDs())
		}
	}
	// Aliases resolve, and lookup ignores case.
	if m, ok := c.Lookup("claude-code"); !ok || m.ID != "claude" {
		t.Errorf("alias claude-code resolved to %v (ok=%v), want claude", m, ok)
	}
	if _, ok := c.Lookup("CLAUDE"); !ok {
		t.Error("lookup should be case-insensitive")
	}
	if _, ok := c.Lookup("no-such-agent"); ok {
		t.Error("lookup of an unknown agent should fail")
	}
}

// TestBundledCatalogIsCached guards the parse-once contract: manifests are
// compiled on first use and reused, not reparsed per detection.
func TestBundledCatalogIsCached(t *testing.T) {
	a, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Error("Bundled returned a different catalog on the second call")
	}
}

// --- parsing ---------------------------------------------------------------

func mustParse(t *testing.T, src string) *Manifest {
	t.Helper()
	m, err := ParseManifest([]byte(src))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	return m
}

func parseErr(t *testing.T, src string) string {
	t.Helper()
	_, err := ParseManifest([]byte(src))
	if err == nil {
		t.Fatal("expected an error")
	}
	return err.Error()
}

func TestParseMinimal(t *testing.T) {
	m := mustParse(t, `
id = "demo"
[[rules]]
id = "r1"
state = "working"
contains = ["busy"]
`)
	if m.ID != "demo" || len(m.Rules) != 1 {
		t.Fatalf("manifest = %+v", m)
	}
	r := m.Rules[0]
	if r.State != StateWorking {
		t.Errorf("state = %v, want working", r.State)
	}
	// An omitted region defaults to the whole snapshot.
	if r.Region != RegionWholeRecent {
		t.Errorf("region = %q, want %q", r.Region, RegionWholeRecent)
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	// A typo must fail loudly rather than silently disabling the rule it was
	// meant to configure.
	got := parseErr(t, `
id = "demo"
[[rules]]
id = "r1"
state = "working"
containz = ["busy"]
`)
	if !strings.Contains(got, "unknown field") {
		t.Errorf("error = %q, want it to name the unknown field", got)
	}
}

func TestParseRejectsBadState(t *testing.T) {
	got := parseErr(t, `
id = "demo"
[[rules]]
id = "r1"
state = "thinking"
`)
	if !strings.Contains(got, "thinking") {
		t.Errorf("error = %q, want it to name the bad state", got)
	}
}

func TestParseRejectsBadRegion(t *testing.T) {
	got := parseErr(t, `
id = "demo"
[[rules]]
id = "r1"
region = "bottom_non_empty_lines"
`)
	if !strings.Contains(got, "unknown region") {
		t.Errorf("error = %q, want an unknown-region error", got)
	}
}

func TestParseRejectsBadRegex(t *testing.T) {
	got := parseErr(t, `
id = "demo"
[[rules]]
id = "r1"
regex = ["([unclosed"]
`)
	if !strings.Contains(got, "regex") {
		t.Errorf("error = %q, want a regex error", got)
	}
}

func TestParseRejectsDuplicateRuleID(t *testing.T) {
	got := parseErr(t, `
id = "demo"
[[rules]]
id = "r1"
[[rules]]
id = "r1"
`)
	if !strings.Contains(got, "duplicate") {
		t.Errorf("error = %q, want a duplicate-id error", got)
	}
}

func TestParseRejectsMissingIDs(t *testing.T) {
	if got := parseErr(t, `[[rules]]
id = "r1"
`); !strings.Contains(got, "no id") {
		t.Errorf("error = %q, want a missing-manifest-id error", got)
	}
	if got := parseErr(t, `
id = "demo"
[[rules]]
state = "idle"
`); !strings.Contains(got, "no id") {
		t.Errorf("error = %q, want a missing-rule-id error", got)
	}
}

// TestParseRejectsFutureEngineVersion: half-evaluating a manifest that needs
// rules this build lacks would report a confident wrong state.
func TestParseRejectsFutureEngineVersion(t *testing.T) {
	got := parseErr(t, `
id = "demo"
min_engine_version = 99
[[rules]]
id = "r1"
`)
	if !strings.Contains(got, "engine version") {
		t.Errorf("error = %q, want an engine-version error", got)
	}
}

// TestParseRejectsRegionNewerThanDeclaredEngine: a manifest must declare the
// version its regions need, or an older engine would read the wrong text.
func TestParseRejectsRegionNewerThanDeclaredEngine(t *testing.T) {
	got := parseErr(t, `
id = "demo"
min_engine_version = 1
[[rules]]
id = "r1"
region = "top_non_empty_lines(3)"
`)
	if !strings.Contains(got, "min_engine_version") {
		t.Errorf("error = %q, want a version requirement error", got)
	}
	// Declaring the right version accepts it.
	mustParse(t, `
id = "demo"
min_engine_version = 3
[[rules]]
id = "r1"
region = "top_non_empty_lines(3)"
`)
}

func TestCatalogRejectsNameCollision(t *testing.T) {
	c := &Catalog{byName: map[string]*Manifest{}}
	if err := c.add(&Manifest{ID: "a", Aliases: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.add(&Manifest{ID: "b", Aliases: []string{"x"}}); err == nil {
		t.Error("a second manifest claiming alias x should be rejected")
	}
}
