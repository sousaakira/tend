package update

import (
	"encoding/json"
	"testing"
)

// TestVersionsAreHerdrsThreeNumbers: a release's version is major.minor.patch
// with an optional v, compared number by number; a development build's is
// not a version. If it regresses, 0.10.0 sorts before 0.9.0, or a build made
// from a working tree is told to "update" back to the release it came from.
func TestVersionsAreHerdrsThreeNumbers(t *testing.T) {
	for _, s := range []string{"v0.3.0", "0.3.0", " v1.2.3 "} {
		if _, ok := ParseVersion(s); !ok {
			t.Errorf("%q is a version", s)
		}
	}
	for _, s := range []string{"development build", "v0.3.0-3-g8e35891-dirty", "0.3", "v0.3.0.1", "v0.x.0", "v0.3.+1"} {
		if _, ok := ParseVersion(s); ok {
			t.Errorf("%q is not a version", s)
		}
	}
	cases := []struct {
		release, running string
		newer            bool
	}{
		{"v0.4.0", "v0.3.0", true},
		{"v0.10.0", "v0.9.9", true},
		{"v1.0.0", "v0.99.99", true},
		{"v0.3.1", "v0.3.0", true},
		{"v0.3.0", "v0.3.0", false},
		{"v0.2.9", "v0.3.0", false},
		{"v0.4.0", "v0.3.0-3-g8e35891-dirty", false},
		{"v0.4.0", "development build", false},
		{"nonsense", "v0.3.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.release, c.running); got != c.newer {
			t.Errorf("Newer(%q, %q) = %v", c.release, c.running, got)
		}
	}
}

// TestAManifestIsMadeFromARelease: `make dist`'s checksums become a manifest
// in the shape Fetch reads, each binary under the platform name ReleaseFor
// looks up, and a release without notes is refused. If it regresses, the
// published manifest names no binary for anyone, and every check fails.
func TestAManifestIsMadeFromARelease(t *testing.T) {
	sums := "aaa  tend-linux-amd64\nbbb  tend-darwin-arm64\nccc  SHA256SUMS\n"
	raw, err := NewManifest("v0.4.0", "### New\n- things\n", "https://example.test/download/v0.4.0/", sums)
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Version != "v0.4.0" || m.Notes != "### New\n- things" {
		t.Errorf("version and notes: %+v", m)
	}
	if m.Assets["linux-x86_64"] != "https://example.test/download/v0.4.0/tend-linux-amd64" || m.SHA256["linux-x86_64"] != "aaa" {
		t.Errorf("linux: %+v", m)
	}
	if m.Assets["darwin-aarch64"] == "" || m.SHA256["darwin-aarch64"] != "bbb" || len(m.Assets) != 2 {
		t.Errorf("darwin, and nothing that is not a binary: %+v", m)
	}
	if _, err := NewManifest("v0.4.0", "  ", "https://x", sums); err == nil {
		t.Error("a release without notes is refused")
	}
}
