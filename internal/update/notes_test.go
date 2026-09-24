package update

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNotesAreUpdateReadyThenWhatsNew: notes saved for a newer release read
// as newer — an update ready — until that release is the one running, when
// the same file is what is new in it; dismissing marks them read only then,
// and never removes them. If it regresses, the notes vanish before anyone
// could read them, or an update ready disappears when it is looked at.
func TestNotesAreUpdateReadyThenWhatsNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release-notes.json")
	if _, _, ok := LoadNotes(path, "v0.3.0"); ok {
		t.Fatal("no file, no notes")
	}
	if err := SaveNotes(path, "v0.4.0", "### New  \n- a thing\t\n\n"); err != nil {
		t.Fatal(err)
	}
	n, newer, ok := LoadNotes(path, "v0.3.0")
	if !ok || !newer || n.Body != "### New\n- a thing" || !n.ShowOnStartup {
		t.Errorf("an update ready, trimmed: %+v %v %v", n, newer, ok)
	}
	if err := DismissNotes(path, "v0.3.0"); err != nil {
		t.Fatal(err)
	}
	if n, _, _ := LoadNotes(path, "v0.3.0"); !n.ShowOnStartup {
		t.Error("an update ready is not dismissed")
	}

	n, newer, ok = LoadNotes(path, "v0.4.0")
	if !ok || newer {
		t.Errorf("once running, what is new: %v %v", newer, ok)
	}
	if err := DismissNotes(path, "v0.4.0"); err != nil {
		t.Fatal(err)
	}
	if n, _, ok := LoadNotes(path, "v0.4.0"); !ok || n.ShowOnStartup {
		t.Errorf("read, and kept: %+v %v", n, ok)
	}

	if err := SaveNotes(path, "v0.5.0", "  \n "); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("empty notes remove the file")
	}
}

// TestTheCheckOffersOnlyANewerReleaseWithNotes: a published release newer
// than the one running is offered, the same or an older one is not, and a
// build that is not a release is never offered one. If it regresses, every
// check says an update is ready, or none ever does.
func TestTheCheckOffersOnlyANewerReleaseWithNotes(t *testing.T) {
	srv := serve(t, "v0.4.0", []byte("bin"), sumOf([]byte("bin")))
	r, ok, err := CheckLatest(srv.URL+"/latest.json", "v0.3.0")
	if err != nil || !ok || r.Version != "v0.4.0" || r.Notes != "fixed things" {
		t.Errorf("newer: %+v %v %v", r, ok, err)
	}
	for _, running := range []string{"v0.4.0", "v0.5.0", "v0.3.0-2-gabc", "development build"} {
		if _, ok, err := CheckLatest(srv.URL+"/latest.json", running); ok || err != nil {
			t.Errorf("not offered to %s: %v %v", running, ok, err)
		}
	}
}
