package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/update"
)

// manifestServer publishes a manifest naming version, over real HTTP, and
// counts how often it is read.
func manifestServer(t *testing.T, version string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var reads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		fmt.Fprintf(w, `{"version":%q,"notes":"### New\n- a thing","assets":{%q:"http://x/tend"},"sha256":{%q:"00"}}`,
			version, update.Platform(), update.Platform())
	}))
	t.Cleanup(srv.Close)
	return srv, &reads
}

// TestTheServerFindsANewerReleaseAndSaysSo: a release build's server looks
// for a release as it starts, and on finding a newer one keeps its notes,
// tells every client, and shows it in the snapshot — then stops looking.
// If it regresses, nobody hears of a release until they go looking, or the
// server reads the manifest every half hour for ever after.
func TestTheServerFindsANewerReleaseAndSaysSo(t *testing.T) {
	manifest, reads := manifestServer(t, "v0.4.0")
	notes := filepath.Join(t.TempDir(), "release-notes.json")
	start := make(chan struct{})
	s, err := New(Config{
		Build: "v0.3.0", NotesPath: notes, UpdateInterval: 10 * time.Millisecond,
		UpdateSource: func() (string, bool) {
			<-start
			return manifest.URL, true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sub := s.Subscribe(16)
	defer sub.Close()
	close(start)

	select {
	case ev := <-sub.C:
		if ev.Kind != EventUpdateReady || ev.Title != "v0.4.0" || ev.Body != UpdateInstall {
			t.Fatalf("event: %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no update-ready event")
	}
	snap := s.Snapshot()
	if snap.Update == nil || snap.Update.Ready != "v0.4.0" || snap.Update.Notes != "v0.4.0" || snap.Update.Install == "" {
		t.Errorf("snapshot: %+v", snap.Update)
	}
	n, ok := s.ReleaseNotes()
	if !ok || !n.Newer || n.Body != "### New\n- a thing" {
		t.Errorf("notes: %+v %v", n, ok)
	}
	time.Sleep(100 * time.Millisecond)
	if got := reads.Load(); got != 1 {
		t.Errorf("the manifest was read %d times after a release was found", got)
	}
	if err := s.DismissReleaseNotes("v0.9.9"); err != ErrStaleNotes {
		t.Errorf("dismissing other notes: %v", err)
	}
}

// TestTheServerOffersNothingItShouldNot: the same release as the one
// running is not ready, a build that is not a release never looks, and a
// check turned off does not read the manifest. If it regresses, the
// status bar says "update ready" to someone already on it, or a check the
// user turned off goes on reaching out to the network.
func TestTheServerOffersNothingItShouldNot(t *testing.T) {
	for _, c := range []struct {
		name, build string
		enabled     bool
		wantReads   bool
	}{
		{"same release", "v0.4.0", true, true},
		{"development build", "v0.4.0-2-gabc-dirty", true, false},
		{"turned off", "v0.3.0", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			manifest, reads := manifestServer(t, "v0.4.0")
			s, err := New(Config{
				Build: c.build, NotesPath: filepath.Join(t.TempDir(), "n.json"), UpdateInterval: 10 * time.Millisecond,
				UpdateSource: func() (string, bool) { return manifest.URL, c.enabled },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			time.Sleep(100 * time.Millisecond)
			if snap := s.Snapshot(); snap.Update != nil {
				t.Errorf("nothing to offer: %+v", snap.Update)
			}
			if got := reads.Load() > 0; got != c.wantReads {
				t.Errorf("manifest read: %v", got)
			}
		})
	}
}
