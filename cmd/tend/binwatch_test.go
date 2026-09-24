package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestABinaryIsNewOnceItHasSettled: the watch says nothing while the binary
// is the one started, nor at the first look at a new one, and says so once
// the new one has held still. If it regresses, a panel starts a binary the
// installer is still writing, or never notices an install at all.
func TestABinaryIsNewOnceItHasSettled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tend")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	w := &binaryWatch{path: path, started: info}
	if w.changed() {
		t.Fatal("the same binary is not new")
	}

	next := path + ".new"
	if err := os.WriteFile(next, []byte("new build"), 0o755); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-5 * time.Second)
	_ = os.Chtimes(next, past, past)
	if err := os.Rename(next, path); err != nil {
		t.Fatal(err)
	}
	if w.changed() {
		t.Error("the first look at a new binary waits")
	}
	if !w.changed() {
		t.Error("a settled new binary is new")
	}
}
