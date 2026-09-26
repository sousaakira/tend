package config

import (
	"strings"
	"testing"
)

// TestErrorSourcesAreTheOldServerAndTheAddedOnes: the server in [errors]
// is a source like those in [errors.sources], listed first, the added ones
// by name, one without a token left out, and names come from the host. If
// it regresses, a user who connected one server before loses it on
// upgrading, or a half-written source is offered and fails.
func TestErrorSourcesAreTheOldServerAndTheAddedOnes(t *testing.T) {
	cfg, err := parse(`[errors]
url = "https://glitchtip.rvx.dev.br"
token = "a"
source = "zeta"

[errors.sources.zeta]
url = "https://z.example.com"
token = "z"

[errors.sources.alpha]
url = "https://a.example.com"
token = "b"

[errors.sources.half]
url = "https://h.example.com"
`, "test")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range cfg.ErrorSources() {
		got = append(got, s.Name)
	}
	if strings.Join(got, " ") != "glitchtip-rvx-dev-br alpha zeta" || !cfg.ErrorSources()[0].Legacy || cfg.Errors.Source != "zeta" {
		t.Errorf("sources %v, %+v", got, cfg.ErrorSources())
	}
	for url, want := range map[string]string{"https://GlitchTip.Example.com:8000/x": "glitchtip-example-com-8000", "": "glitchtip"} {
		if got := ErrorSourceName(url); got != want {
			t.Errorf("%q named %q, want %q", url, got, want)
		}
	}
}

// TestRemovingASectionLeavesTheRestAsWritten: a table goes with its keys
// and the blank line before it; the tables around it, their comments and
// their order stay. If it regresses, removing one server rewrites or
// damages the user's settings file.
func TestRemovingASectionLeavesTheRestAsWritten(t *testing.T) {
	in := "# mine\n[errors]\nurl = \"x\"\n\n[errors.sources.a]\nurl = \"a\"\ntoken = \"t\"\n\n[issues]\n# keep\nagent = \"claude\"\n"
	want := "# mine\n[errors]\nurl = \"x\"\n\n[issues]\n# keep\nagent = \"claude\"\n"
	if got := DropSection(in, "errors.sources.a"); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	last := "[errors]\nurl = \"x\"\n\n[errors.sources.a]\nurl = \"a\"\n"
	if got := DropSection(last, "errors.sources.a"); got != "[errors]\nurl = \"x\"\n" {
		t.Errorf("the last table: %q", got)
	}
	if got := DropSection(in, "errors.sources.none"); got != in {
		t.Errorf("a table not there changed the file: %q", got)
	}
}
