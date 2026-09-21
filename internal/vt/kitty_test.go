package vt

import (
	"encoding/base64"
	"strings"
	"testing"
)

// apc wraps a kitty graphics command the way a program sends it.
func apc(control, payload string) string {
	return "\x1b_G" + control + ";" + payload + "\x1b\\"
}

// TestAnImageIsKeptAndPlacedWhereTheCursorWas is the whole of what the
// emulator can do about graphics: remember what was sent and where it goes.
// The drawing belongs to the terminal the client talks to, and it cannot draw
// what this did not keep.
func TestAnImageIsKeptAndPlacedWhereTheCursorWas(t *testing.T) {
	s := NewScreen(20, 5, 10)
	data := base64.StdEncoding.EncodeToString([]byte("PNGDATA"))

	// Two lines first, so the placement is not at the top: an image goes
	// where the cursor is.
	if _, err := s.Write([]byte("one\r\ntwo\r\n" + apc("a=T,i=7,f=100,c=4,r=2", data))); err != nil {
		t.Fatal(err)
	}

	images := s.KittyImages()
	if len(images) != 1 || images[0].ID != 7 || string(images[0].Data) != "PNGDATA" {
		t.Fatalf("images = %+v", images)
	}
	if images[0].Format != KittyPNG {
		t.Errorf("format = %d, want PNG", images[0].Format)
	}

	placements := s.KittyPlacements()
	if len(placements) != 1 {
		t.Fatalf("placements = %+v", placements)
	}
	p := placements[0]
	if p.ImageID != 7 || p.Row != 2 || p.Col != 0 || p.Cols != 4 || p.Rows != 2 {
		t.Errorf("placement = %+v, want image 7 at row 2 over 4x2 cells", p)
	}
}

// TestAnImageArrivingInChunksIsAssembled: an image of any size arrives in
// pieces, and a terminal that took the first piece for the whole would show a
// quarter of a picture.
func TestAnImageArrivingInChunksIsAssembled(t *testing.T) {
	s := NewScreen(20, 5, 10)
	whole := base64.StdEncoding.EncodeToString([]byte("abcdefghij"))
	first, second := whole[:8], whole[8:]

	if _, err := s.Write([]byte(apc("a=t,i=1,f=24,m=1", first) + apc("m=0", second))); err != nil {
		t.Fatal(err)
	}
	images := s.KittyImages()
	if len(images) != 1 || string(images[0].Data) != "abcdefghij" {
		t.Fatalf("images = %+v", images)
	}
}

// TestPlacementsFollowTheTextTheyWerePlacedBeside: an image is anchored to a
// line, not to a row of the screen. Without this it stays put while the text
// scrolls out from under it.
func TestPlacementsFollowTheTextTheyWerePlacedBeside(t *testing.T) {
	s := NewScreen(20, 3, 50)
	data := base64.StdEncoding.EncodeToString([]byte("x"))
	if _, err := s.Write([]byte(apc("a=T,i=1,f=32", data))); err != nil {
		t.Fatal(err)
	}
	at := s.KittyPlacements()[0].Row

	// Enough output to push that line into the history.
	if _, err := s.Write([]byte(strings.Repeat("filler\r\n", 10))); err != nil {
		t.Fatal(err)
	}
	if got := s.KittyPlacements()[0].Row; got != at {
		t.Errorf("the placement moved to row %d; an absolute row does not change under scrolling", got)
	}
	if s.MainGrid().HistoryLen() == 0 {
		t.Fatal("nothing scrolled, so this test proved nothing")
	}
}

// TestDeletingImagesAndPlacements covers the part of the protocol a program
// uses to clean up after itself: without it, images accumulate for the life
// of the pane.
func TestDeletingImagesAndPlacements(t *testing.T) {
	s := NewScreen(20, 5, 10)
	data := base64.StdEncoding.EncodeToString([]byte("x"))
	if _, err := s.Write([]byte(
		apc("a=T,i=1,p=1,f=32", data) + apc("a=T,i=1,p=2,f=32", data) + apc("a=T,i=2,f=32", data),
	)); err != nil {
		t.Fatal(err)
	}
	if got := len(s.KittyPlacements()); got != 3 {
		t.Fatalf("%d placements before deleting", got)
	}

	// One placement of one image.
	if _, err := s.Write([]byte(apc("a=d,d=p,i=1,p=2", ""))); err != nil {
		t.Fatal(err)
	}
	if got := len(s.KittyPlacements()); got != 2 {
		t.Errorf("%d placements after deleting one", got)
	}

	// Every placement of an image, and the image itself in capitals.
	if _, err := s.Write([]byte(apc("a=d,d=I,i=1", ""))); err != nil {
		t.Fatal(err)
	}
	if got := len(s.KittyPlacements()); got != 1 {
		t.Errorf("%d placements after deleting image 1", got)
	}
	if got := len(s.KittyImages()); got != 1 {
		t.Errorf("%d images after freeing image 1", got)
	}
}

// TestAFileTransferIsRefused: t=f names a path for tend to read, chosen by
// whatever is running in the pane. Reading it would be doing as told by
// somebody else's program.
func TestAFileTransferIsRefused(t *testing.T) {
	s := NewScreen(20, 5, 10)
	path := base64.StdEncoding.EncodeToString([]byte("/etc/passwd"))
	if _, err := s.Write([]byte(apc("a=T,i=1,f=100,t=f", path))); err != nil {
		t.Fatal(err)
	}
	if got := len(s.KittyImages()); got != 0 {
		t.Errorf("a file transfer produced %d images", got)
	}
}

// TestAPCThatIsNotGraphicsIsStillDiscarded: the emulator took APC payloads and
// dropped them before this existed, and anything that is not a graphics
// command must go on being dropped rather than kept as an image.
func TestAPCThatIsNotGraphicsIsStillDiscarded(t *testing.T) {
	s := NewScreen(20, 5, 10)
	if _, err := s.Write([]byte("\x1b_Xsomething\x1b\\hello")); err != nil {
		t.Fatal(err)
	}
	if got := len(s.KittyImages()); got != 0 {
		t.Errorf("a non-graphics APC produced %d images", got)
	}
	if got := s.Grid().Line(0).Text(); got != "hello" {
		t.Errorf("the text after it was lost: %q", got)
	}
}
