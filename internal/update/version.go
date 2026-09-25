package update

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// StableManifest is where the stable channel's manifest is published: an
// asset of the latest GitHub release, which GitHub serves at a fixed URL
// that follows each new release. herdr publishes its manifest on its own
// site; tend has its releases on GitHub and nowhere else, and this needs
// nothing more to keep true.
const StableManifest = "https://github.com/auth-com-br/tend/releases/latest/download/latest.json"

// Version is a release's version, herdr's `Version`: major.minor.patch,
// with an optional leading v, and nothing else.
type Version struct{ Major, Minor, Patch int }

// ParseVersion reads a release's version. A development build's
// ("v0.3.0-3-g8e35891-dirty", "development build") is not one, and is
// refused: two of those have no order, and herdr's background check does
// not run from a build that is not a release either.
func ParseVersion(s string) (Version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || p == "" || p[0] == '+' {
			return Version{}, false
		}
		n[i] = v
	}
	return Version{n[0], n[1], n[2]}, true
}

// Less orders two versions, part by part.
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Newer reports whether a release is newer than the build running, as
// herdr's stable channel decides whether to offer one. Both must be release
// versions; anything else is not newer, so a development build is never
// told to "update" to the release it was built past.
func Newer(release, running string) bool {
	r, ok := ParseVersion(release)
	if !ok {
		return false
	}
	c, ok := ParseVersion(running)
	return ok && c.Less(r)
}

// NewManifest is the manifest for a release whose binaries are published
// at base (a URL ending in the release's directory), named as `make dist`
// names them: "tend-<os>-<arch>", with their checksums from sums, in
// SHA256SUMS form ("<hex>  <name>" per line).
func NewManifest(version, notes, base, sums string) ([]byte, error) {
	m := Manifest{Version: version, Notes: strings.TrimSpace(notes), Assets: map[string]string{}, SHA256: map[string]string{}}
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !strings.HasPrefix(fields[1], "tend-") {
			continue
		}
		name := fields[1]
		platform := strings.TrimPrefix(name, "tend-")
		os, arch, ok := strings.Cut(platform, "-")
		if !ok {
			continue
		}
		switch arch {
		case "amd64":
			arch = "x86_64"
		case "arm64":
			arch = "aarch64"
		}
		key := os + "-" + arch
		m.Assets[key] = strings.TrimSuffix(base, "/") + "/" + name
		m.SHA256[key] = fields[0]
	}
	if len(m.Assets) == 0 {
		return nil, fmt.Errorf("update: no tend-<os>-<arch> binaries in the checksums")
	}
	if m.Notes == "" {
		return nil, fmt.Errorf("update: a release needs notes, as herdr's manifest does")
	}
	return json.MarshalIndent(m, "", "  ")
}

// CheckLatest is herdr's background check: the release a manifest
// publishes, and whether it is newer than the build running. It does not
// download anything. A release without notes is refused, as herdr refuses
// one: the notes are what the user is shown about it.
func CheckLatest(url, running string) (Release, bool, error) {
	m, err := Fetch(url)
	if err != nil {
		return Release{}, false, err
	}
	if !Newer(m.Version, running) {
		return Release{}, false, nil
	}
	if strings.TrimSpace(m.Notes) == "" {
		return Release{}, false, fmt.Errorf("update: %s publishes %s without notes", url, m.Version)
	}
	r, err := ReleaseFor(m)
	if err != nil {
		return Release{}, false, err
	}
	return r, true, nil
}
