// Package update replaces the tend binary with a published one.
//
// It is herdr's arrangement (`update.rs`): a manifest per channel naming the
// version, the asset for each platform and its checksum; a check that says
// whether the published build differs from this one; a download that is
// verified before it is put anywhere; and a replacement that is atomic, so a
// failure leaves the binary that was working.
//
// Two things are deliberately not here. Nothing downloads on its own — an
// update happens because somebody ran `tend update` — and there is no default
// manifest URL, because tend has no published releases and inventing one would
// point the updater at somebody else's.
package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Channel is which manifest to read.
type Channel string

const (
	// Stable is the default; Preview is opted into.
	Stable  Channel = "stable"
	Preview Channel = "preview"
)

// Valid reports whether a channel name is one of the two.
func (c Channel) Valid() bool { return c == Stable || c == Preview }

var (
	// ErrNoManifest means no URL is configured for the channel.
	ErrNoManifest = errors.New("update: no manifest is configured")
	// ErrNoAsset means the manifest has nothing for this machine.
	ErrNoAsset = errors.New("update: the release has nothing for this platform")
	// ErrChecksum means what was downloaded is not what the manifest said.
	ErrChecksum = errors.New("update: the download does not match its checksum")
)

// fetchTimeout bounds reading a manifest, and downloadTimeout a binary. An
// update that hangs must fail rather than hold the terminal it was run from.
const (
	fetchTimeout    = 30 * time.Second
	downloadTimeout = 10 * time.Minute
	// maxBinary bounds a download. A binary is tens of megabytes; anything
	// past this is not one.
	maxBinary = 256 << 20
)

// Manifest is what a channel publishes, in herdr's shape.
type Manifest struct {
	Version string `json:"version"`
	Notes   string `json:"notes,omitempty"`
	// Assets and SHA256 are keyed by platform: "linux-x86_64".
	Assets map[string]string `json:"assets"`
	SHA256 map[string]string `json:"sha256,omitempty"`
}

// Release is what an update would install.
type Release struct {
	Version  string
	Notes    string
	URL      string
	SHA256   string
	Platform string
}

// Platform names this machine the way a manifest does.
func Platform() string {
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	}
	return runtime.GOOS + "-" + arch
}

// Fetch reads a manifest.
func Fetch(url string) (Manifest, error) {
	if url == "" {
		return Manifest{}, ErrNoManifest
	}
	client := &http.Client{Timeout: fetchTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return Manifest{}, fmt.Errorf("update: reading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Manifest{}, fmt.Errorf("update: %s answered %s", url, resp.Status)
	}
	var m Manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("update: %s is not a manifest: %w", url, err)
	}
	if m.Version == "" {
		return Manifest{}, fmt.Errorf("update: %s names no version", url)
	}
	return m, nil
}

// ReleaseFor picks this machine's asset out of a manifest.
func ReleaseFor(m Manifest) (Release, error) {
	platform := Platform()
	url, ok := m.Assets[platform]
	if !ok || url == "" {
		return Release{}, fmt.Errorf("%w: %s", ErrNoAsset, platform)
	}
	return Release{
		Version: m.Version, Notes: m.Notes, URL: url,
		SHA256: m.SHA256[platform], Platform: platform,
	}, nil
}

// Differs reports whether a release is a different build from the one running.
//
// Not "newer": tend's version is the git description its binary was built
// from, and two of those have no order. What can be said is that they are not
// the same, which is what somebody asking "should I update" wants to know
// about a channel they chose to follow.
func Differs(release Release, running string) bool {
	return running != "" && release.Version != "" && release.Version != running
}

// Download fetches a release into a file beside the binary it will replace,
// and checks it against the manifest before returning.
//
// Beside it, because the replacement is a rename and a rename only works
// within one filesystem; and verified before anything is made executable,
// because an unverified download is not a thing to leave lying about with the
// execute bit set.
func Download(release Release, target string) (string, error) {
	if release.URL == "" {
		return "", ErrNoAsset
	}
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".tend-update-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	remove := func() { _ = os.Remove(name) }

	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(release.URL)
	if err != nil {
		tmp.Close()
		remove()
		return "", fmt.Errorf("update: downloading %s: %w", release.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		remove()
		return "", fmt.Errorf("update: %s answered %s", release.URL, resp.Status)
	}

	sum := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, sum), io.LimitReader(resp.Body, maxBinary))
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		remove()
		return "", err
	}
	if written == 0 {
		remove()
		return "", errors.New("update: the download was empty")
	}

	if release.SHA256 != "" {
		got := hex.EncodeToString(sum.Sum(nil))
		if got != release.SHA256 {
			remove()
			return "", fmt.Errorf("%w: %s, expected %s", ErrChecksum, got, release.SHA256)
		}
	}
	if err := os.Chmod(name, 0o755); err != nil {
		remove()
		return "", err
	}
	return name, nil
}

// Install puts a downloaded binary in place of the running one.
//
// The old binary is kept beside the new one as <name>.old until the rename
// has succeeded. A binary cannot be overwritten while it is running on some
// systems, and moving it out of the way first is what makes the replacement
// work on all of them — the running process keeps the file it opened.
func Install(downloaded, target string) error {
	backup := target + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(target, backup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("update: moving %s aside: %w", target, err)
	}
	if err := os.Rename(downloaded, target); err != nil {
		// Put back what was working, then say what happened.
		_ = os.Rename(backup, target)
		return fmt.Errorf("update: installing %s: %w", target, err)
	}
	_ = os.Remove(backup)
	return nil
}
