package detect

import (
	"strings"
	"testing"
)

func manifest(t *testing.T, src string) *Manifest {
	t.Helper()
	m, err := ParseManifest([]byte(src))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	return m
}

// --- rule selection --------------------------------------------------------

func TestDetectNoMatch(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "r1"
state = "working"
contains = ["busy"]
`)
	got := m.Detect(Input{Screen: "idle prompt"})
	if got.Matched {
		t.Errorf("got %+v, want no match", got)
	}
	if got.State != StateUnknown {
		t.Errorf("state = %v, want unknown", got.State)
	}
}

func TestDetectHighestPriorityWins(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "low"
state = "idle"
priority = 10
contains = ["prompt"]
[[rules]]
id = "high"
state = "working"
priority = 900
contains = ["prompt"]
`)
	got := m.Detect(Input{Screen: "a prompt here"})
	if got.RuleID != "high" || got.State != StateWorking {
		t.Errorf("got %+v, want the high rule to win", got)
	}
}

// TestDetectPriorityOrderIsIndependentOfFileOrder: a later rule with a higher
// priority still wins, so manifests need not be sorted.
func TestDetectPriorityOrderIsIndependentOfFileOrder(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "high"
state = "working"
priority = 900
contains = ["prompt"]
[[rules]]
id = "low"
state = "idle"
priority = 10
contains = ["prompt"]
`)
	if got := m.Detect(Input{Screen: "prompt"}); got.RuleID != "high" {
		t.Errorf("got %+v, want the high rule", got)
	}
}

// TestDetectTieGoesToTheEarlierRule pins the tie-break. Rule order is
// therefore meaningful, and reordering a manifest can change behaviour even
// when no priority changes.
func TestDetectTieGoesToTheEarlierRule(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "first"
state = "working"
priority = 100
contains = ["prompt"]
[[rules]]
id = "second"
state = "idle"
priority = 100
contains = ["prompt"]
`)
	if got := m.Detect(Input{Screen: "prompt"}); got.RuleID != "first" {
		t.Errorf("got %+v, want the first rule to hold the tie", got)
	}
}

func TestDetectFlags(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "r1"
state = "working"
visible_working = true
visible_idle = true
skip_state_update = true
contains = ["busy"]
`)
	got := m.Detect(Input{Screen: "busy"})
	if !got.VisibleWorking {
		t.Error("visible_working should be set for a working rule")
	}
	// A flag only applies when the rule's own state matches it.
	if got.VisibleIdle {
		t.Error("visible_idle must not be set for a working rule")
	}
	if !got.SkipStateUpdate {
		t.Error("skip_state_update should carry through")
	}
}

func TestExplainReportsEveryRule(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "r1"
state = "working"
contains = ["busy"]
[[rules]]
id = "r2"
state = "idle"
contains = ["done"]
`)
	res, evals := m.Explain(Input{Screen: "busy"})
	if len(evals) != 2 {
		t.Fatalf("got %d evaluations, want 2", len(evals))
	}
	if !evals[0].Matched || evals[1].Matched {
		t.Errorf("evaluations = %+v, want only the first to match", evals)
	}
	if evals[0].RegionBytes != len("busy") {
		t.Errorf("region bytes = %d, want %d", evals[0].RegionBytes, len("busy"))
	}
	if res.RuleID != "r1" {
		t.Errorf("result = %+v, want r1", res)
	}
}

// --- gates -----------------------------------------------------------------

func TestGateEmptyMatchesEverything(t *testing.T) {
	// A rule with no matcher is unconditional. That is a real pattern for a
	// low-priority default, so it must not be mistaken for a no-op.
	m := manifest(t, `
id = "demo"
[[rules]]
id = "always"
state = "idle"
priority = -100
`)
	if got := m.Detect(Input{Screen: "anything"}); !got.Matched {
		t.Errorf("got %+v, want an empty gate to match", got)
	}
}

func TestGateContainsIsCaseInsensitive(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "r1"
state = "blocked"
contains = ["Do You Want To Proceed?"]
`)
	if got := m.Detect(Input{Screen: "do you want to proceed?"}); !got.Matched {
		t.Error("contains should ignore case")
	}
}

// TestGateRegexIsCaseSensitive is the counterpart: unlike contains, patterns
// mean exactly what they say unless they opt into (?i).
func TestGateRegexIsCaseSensitive(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "r1"
state = "blocked"
regex = ["^Proceed"]
`)
	if got := m.Detect(Input{Screen: "proceed"}); got.Matched {
		t.Error("regex should be case-sensitive")
	}
	if got := m.Detect(Input{Screen: "Proceed"}); !got.Matched {
		t.Error("regex should match the exact case")
	}
}

func TestGateListsAreConjunctive(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "r1"
state = "working"
contains = ["alpha", "beta"]
`)
	if got := m.Detect(Input{Screen: "alpha only"}); got.Matched {
		t.Error("every needle in contains must be present")
	}
	if got := m.Detect(Input{Screen: "alpha and beta"}); !got.Matched {
		t.Error("both needles present should match")
	}
}

// TestGateLineRegexMatchesPerLine: a line_regex anchored with ^ must match a
// line, not only the start of the region.
func TestGateLineRegexMatchesPerLine(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "r1"
state = "working"
line_regex = ["^esc to interrupt"]
`)
	if got := m.Detect(Input{Screen: "header\nesc to interrupt\nfooter"}); !got.Matched {
		t.Error("line_regex should match any single line")
	}
	if got := m.Detect(Input{Screen: "prefix esc to interrupt"}); got.Matched {
		t.Error("an anchored line_regex must not match mid-line")
	}
}

func TestGateAny(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "r1"
state = "working"
any = [
  { contains = ["alpha"] },
  { contains = ["beta"] },
]
`)
	for _, screen := range []string{"alpha", "beta", "alpha beta"} {
		if got := m.Detect(Input{Screen: screen}); !got.Matched {
			t.Errorf("any should match %q", screen)
		}
	}
	if got := m.Detect(Input{Screen: "gamma"}); got.Matched {
		t.Error("any should not match when no branch does")
	}
}

func TestGateAll(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "r1"
state = "working"
all = [
  { contains = ["alpha"] },
  { regex = ["b.ta"] },
]
`)
	if got := m.Detect(Input{Screen: "alpha beta"}); !got.Matched {
		t.Error("all branches satisfied should match")
	}
	if got := m.Detect(Input{Screen: "alpha only"}); got.Matched {
		t.Error("all requires every branch")
	}
}

// TestGateNot covers the negative guards manifests use to stop a working rule
// from firing while an approval prompt is on screen.
func TestGateNot(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "r1"
state = "working"
contains = ["running"]
not = [
  { contains = ["do you want to proceed?"] },
]
`)
	if got := m.Detect(Input{Screen: "running"}); !got.Matched {
		t.Error("should match without the guard text")
	}
	if got := m.Detect(Input{Screen: "running\ndo you want to proceed?"}); got.Matched {
		t.Error("the not guard should block the match")
	}
}

func TestGateNested(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "r1"
state = "working"
any = [
  { all = [ { contains = ["a"] }, { contains = ["b"] } ] },
  { contains = ["zzz"] },
]
`)
	if got := m.Detect(Input{Screen: "a b"}); !got.Matched {
		t.Error("nested all inside any should match")
	}
	if got := m.Detect(Input{Screen: "a"}); got.Matched {
		t.Error("the nested all needs both branches")
	}
	if got := m.Detect(Input{Screen: "zzz"}); !got.Matched {
		t.Error("the second any branch should still match")
	}
}

// --- regions ---------------------------------------------------------------

func TestRegionOSCFieldsAreSeparateFromTheScreen(t *testing.T) {
	// An agent can report progress in its title without drawing anything, so
	// the OSC regions must not read the grid.
	m := manifest(t, `
id = "demo"
[[rules]]
id = "title"
state = "working"
region = "osc_title"
contains = ["busy"]
`)
	if got := m.Detect(Input{Screen: "busy on screen"}); got.Matched {
		t.Error("an osc_title rule must not read the screen")
	}
	if got := m.Detect(Input{OSCTitle: "busy"}); !got.Matched {
		t.Error("an osc_title rule should read the title")
	}
}

func TestRegionBottomNonEmptyLines(t *testing.T) {
	m := manifest(t, `
id = "demo"
[[rules]]
id = "r1"
state = "working"
region = "bottom_non_empty_lines(2)"
contains = ["target"]
`)
	// "target" is the third non-empty line from the bottom, so it is out of
	// range of a two-line region.
	out := m.Detect(Input{Screen: "target\nb\nc"})
	if out.Matched {
		t.Error("bottom_non_empty_lines(2) should not reach the third line up")
	}
	if got := m.Detect(Input{Screen: "x\ntarget\nc"}); !got.Matched {
		t.Error("bottom_non_empty_lines(2) should reach the second line up")
	}
}

func TestRegionBottomNonEmptyLinesSkipsBlanks(t *testing.T) {
	// Blank lines do not count towards the budget, but they stay in the text.
	s := newSnapshot(Input{Screen: "a\n\n\nb\nc\n"})
	if got := s.bottomNonEmptyLines(2); got != "b\nc\n" {
		t.Errorf("region = %q, want %q", got, "b\nc\n")
	}
	if got := s.bottomNonEmptyLines(3); got != "a\n\n\nb\nc\n" {
		t.Errorf("region = %q, want the whole screen", got)
	}
	// Asking for more than exist yields everything from the first non-empty.
	if got := s.bottomNonEmptyLines(99); got != "a\n\n\nb\nc\n" {
		t.Errorf("region = %q, want the whole screen", got)
	}
}

func TestRegionBottomNonEmptyLinesOnBlankScreen(t *testing.T) {
	s := newSnapshot(Input{Screen: "\n\n\n"})
	if got := s.bottomNonEmptyLines(3); got != "" {
		t.Errorf("region = %q, want empty", got)
	}
}

func TestRegionTopNonEmptyLines(t *testing.T) {
	s := newSnapshot(Input{Screen: "\na\nb\nc\n"})
	if got := s.topNonEmptyLines(2); got != "\na\nb\n" {
		t.Errorf("region = %q, want %q", got, "\na\nb\n")
	}
}

func TestRegionBottomLines(t *testing.T) {
	s := newSnapshot(Input{Screen: "a\nb\nc\nd"})
	if got := s.bottomLines(2); got != "c\nd" {
		t.Errorf("region = %q, want %q", got, "c\nd")
	}
	if got := s.bottomLines(99); got != "a\nb\nc\nd" {
		t.Errorf("region = %q, want everything", got)
	}
}

// TestSplitLinesMatchesRustSemantics: the manifests were calibrated against
// Rust's str::lines, where a trailing newline terminates the last line rather
// than starting an empty one. Go's strings.Split does the opposite, and the
// difference shifts every line-counted region by one.
func TestSplitLinesMatchesRustSemantics(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a\n", []string{"a"}},
		{"a\nb", []string{"a", "b"}},
		{"a\n\n", []string{"a", ""}},
		{"a\r\nb", []string{"a", "b"}},
		{"\n", []string{""}},
	}
	for _, c := range cases {
		got, _ := splitLines(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitLines(%q) = %q, want %q", c.in, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("splitLines(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestIsHorizontalRule(t *testing.T) {
	cases := map[string]bool{
		"":            false,
		"─":           true, // a lone rule char with nothing after it
		"───":         true,
		"────── tail": true,  // three or more may carry a label
		"─ tail":      false, // too short to be a border
		"plain text":  false,
		"  ─────  ":   true,
		"-----":       false, // ASCII hyphens are not box drawing
	}
	for line, want := range cases {
		if got := isHorizontalRule(line); got != want {
			t.Errorf("isHorizontalRule(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestRegionPromptBox(t *testing.T) {
	screen := strings.Join([]string{
		"output above",
		"────────────",
		"typed text",
		"────────────",
		"hint below",
	}, "\n")
	s := newSnapshot(Input{Screen: screen})

	if got := s.promptBoxBody(); got != "typed text\n" {
		t.Errorf("promptBoxBody = %q, want %q", got, "typed text\n")
	}
	if got := s.abovePromptBox(); got != "output above\n" {
		t.Errorf("abovePromptBox = %q, want %q", got, "output above\n")
	}
	if got := lastNonEmptyLine(s.abovePromptBox()); got != "output above" {
		t.Errorf("lastNonEmptyAboveBox = %q", got)
	}
}

func TestRegionPromptBoxWithoutBorders(t *testing.T) {
	// With fewer than two borders there is no box; the region falls back to
	// the whole screen rather than to nothing.
	s := newSnapshot(Input{Screen: "just output\n────────────\ntyped"})
	if got := s.abovePromptBox(); got != "just output\n────────────\ntyped" {
		t.Errorf("abovePromptBox = %q, want the whole screen", got)
	}
	if got := s.promptBoxBody(); got != "" {
		t.Errorf("promptBoxBody = %q, want empty", got)
	}
}

func TestRegionAfterLastHorizontalRule(t *testing.T) {
	s := newSnapshot(Input{Screen: "a\n───\nb\n───\nc"})
	if got := s.afterLastHorizontalRule(); got != "c" {
		t.Errorf("region = %q, want %q", got, "c")
	}
	s = newSnapshot(Input{Screen: "no rules here"})
	if got := s.afterLastHorizontalRule(); got != "no rules here" {
		t.Errorf("region = %q, want the whole screen", got)
	}
}

func TestRegionPromptMarkers(t *testing.T) {
	s := newSnapshot(Input{Screen: "output\n› typed here"})
	if _, ok := s.currentPromptIndex(); !ok {
		t.Fatal("expected a current prompt")
	}
	if got := s.afterLastPromptMarker(); got != "" {
		t.Errorf("afterLastPromptMarker = %q, want empty", got)
	}
	if got := s.beforeCurrentPromptMarker(); got != "output\n" {
		t.Errorf("beforeCurrentPromptMarker = %q", got)
	}

	// A block marker after the prompt means the agent has moved on, so that
	// prompt is history rather than the current one.
	s = newSnapshot(Input{Screen: "› typed here\n• running a tool"})
	if _, ok := s.currentPromptIndex(); ok {
		t.Error("a block marker after the prompt should retire it")
	}
}

// --- integration with the bundled manifests --------------------------------

// TestBundledClaudeTitleSpinner is a smoke test against real shipped data: an
// agent that reports a spinner in its window title is working. It uses the OSC
// title rule because that is the part of a manifest least likely to churn.
func TestBundledClaudeTitleSpinner(t *testing.T) {
	c, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	m, ok := c.Lookup("claude")
	if !ok {
		t.Fatal("no claude manifest")
	}

	got := m.Detect(Input{OSCTitle: "⠁ Thinking"})
	if got.State != StateWorking {
		t.Errorf("state = %v (rule %q), want working", got.State, got.RuleID)
	}
	if !got.VisibleWorking {
		t.Error("a spinner in the title is visible evidence of working")
	}

	if got := m.Detect(Input{OSCTitle: "some-project"}); got.State == StateWorking {
		t.Errorf("a plain title should not read as working (rule %q)", got.RuleID)
	}
}

// --- performance -----------------------------------------------------------

func BenchmarkDetect(b *testing.B) {
	c, err := Bundled()
	if err != nil {
		b.Fatal(err)
	}
	m, ok := c.Lookup("claude")
	if !ok {
		b.Fatal("no claude manifest")
	}
	in := Input{
		Screen: strings.Repeat("some terminal output line here\n", 40) +
			"> \n  Try \"fix the build\"\n",
		OSCTitle: "my-project",
	}
	b.ReportAllocs()
	for b.Loop() {
		m.Detect(in)
	}
}
