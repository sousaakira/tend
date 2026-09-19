package detect

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestTranslatePattern(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"untouched", `^\s*foo\b`, `^\s*foo\b`},
		{"no escapes at all", `abc[0-9]+`, `abc[0-9]+`},
		{"short codepoint", `[\u2800-\u28FF]`, `[\x{2800}-\x{28FF}]`},
		{"braced codepoint", `[\u{fe0e}\u{fe0f}]?`, `[\x{fe0e}\x{fe0f}]?`},
		{"long codepoint", `\U0001F600`, `\x{0001F600}`},
		{"braced long codepoint", `\U{1F600}`, `\x{1F600}`},
		{"property alias", `\p{Alphabetic}+`, `\p{L}+`},
		{"negated property alias", `\P{Alphabetic}`, `\P{L}`},
		{"short property alias", `\p{alpha}`, `\p{L}`},
		{"go property untouched", `\p{L}\pL\p{Han}`, `\p{L}\pL\p{Han}`},
		{"go hex untouched", `\x{2800}\x41`, `\x{2800}\x41`},
		// A literal backslash must not be read as opening an escape.
		{"escaped backslash", `\\u2800`, `\\u2800`},
		{"escaped backslash then real escape", `\\\u2800`, `\\\x{2800}`},
		// Malformed input is passed through for Go's compiler to reject.
		{"bad hex", `\uZZZZ`, `\uZZZZ`},
		{"truncated", `\u28`, `\u28`},
		{"unterminated brace", `\u{2800`, `\u{2800`},
		{"unknown property", `\p{Emoji}`, `\p{Emoji}`},
		{"trailing backslash", `abc\`, `abc\`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := translatePattern(c.in); got != c.want {
				t.Errorf("translatePattern(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestTranslatedPatternsBehave checks that translation preserves meaning, not
// just syntax: the braille range and the letter class must still match the
// text the manifests were calibrated against.
func TestTranslatedPatternsBehave(t *testing.T) {
	cases := []struct {
		pattern string
		match   []string
		reject  []string
	}{
		{
			pattern: `^\s*[\u2800-\u28FF]+\s+\p{Alphabetic}+\w*ing\b`,
			match:   []string{"⠁ thinking", "  ⠋⠙ working", "⠿ generating more"},
			reject:  []string{"thinking", "⠁ 123ing", "* thinking"},
		},
		{
			pattern: `^⚠[\u{fe0e}\u{fe0f}]?(?:\s|$)`,
			match:   []string{"⚠ needs input", "⚠", "⚠️ ready"},
			reject:  []string{"x⚠ ", "⚠x"},
		},
	}
	for _, c := range cases {
		re, err := regexp.Compile(translatePattern(c.pattern))
		if err != nil {
			t.Fatalf("compile %q: %v", c.pattern, err)
		}
		for _, s := range c.match {
			if !re.MatchString(s) {
				t.Errorf("%q should match %q", c.pattern, s)
			}
		}
		for _, s := range c.reject {
			if re.MatchString(s) {
				t.Errorf("%q should not match %q", c.pattern, s)
			}
		}
	}
}

// TestBundledPatternsCompileAfterTranslation walks every pattern in every
// bundled manifest. It is the regression guard for syncing manifests from
// upstream: if a future manifest uses a dialect construct the translator does
// not cover, this names the file, the rule and the pattern rather than letting
// detection quietly go dead for that agent.
func TestBundledPatternsCompileAfterTranslation(t *testing.T) {
	entries, err := fs.ReadDir(bundledFS, "manifests")
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	translated := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		data, err := fs.ReadFile(bundledFS, "manifests/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		var raw rawManifest
		if _, err := toml.Decode(string(data), &raw); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		for _, r := range raw.Rules {
			for _, p := range rawPatterns(r.rawGate) {
				total++
				out := translatePattern(p)
				if out != p {
					translated++
				}
				if _, err := regexp.Compile(out); err != nil {
					t.Errorf("%s rule %q: %q does not compile after translation: %v",
						e.Name(), r.ID, p, err)
				}
			}
		}
	}
	if total == 0 {
		t.Fatal("no patterns found")
	}
	t.Logf("%d patterns, %d needed dialect translation", total, translated)
}

func rawPatterns(g rawGate) []string {
	out := append([]string{}, g.Regex...)
	out = append(out, g.LineRegex...)
	for _, n := range g.All {
		out = append(out, rawPatterns(n)...)
	}
	for _, n := range g.Any {
		out = append(out, rawPatterns(n)...)
	}
	for _, n := range g.Not {
		out = append(out, rawPatterns(n)...)
	}
	return out
}
