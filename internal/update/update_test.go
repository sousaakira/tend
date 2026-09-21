package update

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serve publishes a manifest and a binary, so these tests go through real
// HTTP rather than a stand-in for it: what is under test is what happens with
// a server on the other end.
func serve(t *testing.T, version string, binary []byte, sum string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/tend", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(binary)
	})
	mux.HandleFunc("/latest.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"version":%q,"notes":"fixed things","assets":{%q:%q},"sha256":{%q:%q}}`,
			version, Platform(), base+"/tend", Platform(), sum)
	})
	srv := httptest.NewServer(mux)
	base = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

func sumOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// TestAnUpdateIsCheckedDownloadedVerifiedAndInstalled is the whole path. If it
// regresses, an update either does not happen or happens with the wrong bytes,
// and the second is much worse.
func TestAnUpdateIsCheckedDownloadedVerifiedAndInstalled(t *testing.T) {
	binary := []byte("#!/bin/sh\necho new\n")
	srv := serve(t, "b2222222", binary, sumOf(binary))

	m, err := Fetch(srv.URL + "/latest.json")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	release, err := ReleaseFor(m)
	if err != nil {
		t.Fatalf("ReleaseFor: %v", err)
	}
	if release.Version != "b2222222" || release.Notes != "fixed things" {
		t.Fatalf("release = %+v", release)
	}
	if !Differs(release, "a1111111") {
		t.Error("a different published build was not reported as different")
	}
	if Differs(release, "b2222222") {
		t.Error("the build that is running was reported as an update")
	}

	// Installed over a binary that is "running".
	dir := t.TempDir()
	target := filepath.Join(dir, "tend")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	downloaded, err := Download(release, target)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if filepath.Dir(downloaded) != dir {
		t.Errorf("the download landed in %s, not beside the binary it replaces", filepath.Dir(downloaded))
	}
	if err := Install(downloaded, target); err != nil {
		t.Fatalf("Install: %v", err)
	}
	after, err := os.ReadFile(target)
	if err != nil || string(after) != string(binary) {
		t.Fatalf("after installing, the binary is %q, %v", after, err)
	}
	if info, err := os.Stat(target); err != nil || info.Mode()&0o111 == 0 {
		t.Errorf("the installed binary is not executable: %v %v", info.Mode(), err)
	}
	// Nothing left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("the directory holds %d files after an install, want 1", len(entries))
	}
}

// TestADownloadThatDoesNotMatchIsThrownAway: the checksum is the only thing
// standing between a manifest and running whatever was served. A mismatch has
// to leave nothing executable behind.
func TestADownloadThatDoesNotMatchIsThrownAway(t *testing.T) {
	binary := []byte("not what the manifest says")
	srv := serve(t, "b2222222", binary, sumOf([]byte("something else")))

	m, err := Fetch(srv.URL + "/latest.json")
	if err != nil {
		t.Fatal(err)
	}
	release, _ := ReleaseFor(m)

	dir := t.TempDir()
	target := filepath.Join(dir, "tend")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Download(release, target); !errors.Is(err, ErrChecksum) {
		t.Fatalf("Download: %v, want ErrChecksum", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("a refused download left %d files behind", len(entries))
	}
	if after, _ := os.ReadFile(target); string(after) != "old" {
		t.Errorf("the binary was replaced by a download that did not match: %q", after)
	}
}

// TestAManifestWithNothingForThisMachineSaysSo, rather than downloading
// something built for another one.
func TestAManifestWithNothingForThisMachineSaysSo(t *testing.T) {
	if _, err := ReleaseFor(Manifest{
		Version: "x", Assets: map[string]string{"plan9-sparc": "http://example/x"},
	}); !errors.Is(err, ErrNoAsset) {
		t.Errorf("err = %v, want ErrNoAsset", err)
	}
	if _, err := Fetch(""); !errors.Is(err, ErrNoManifest) {
		t.Errorf("err = %v, want ErrNoManifest", err)
	}
}

// TestSomethingThatIsNotAManifestIsRefused: the URL is in a settings file and
// can point anywhere, including at a web page.
func TestSomethingThatIsNotAManifestIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	defer srv.Close()
	_, err := Fetch(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "not a manifest") {
		t.Errorf("err = %v", err)
	}
}
