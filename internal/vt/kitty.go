package vt

import (
	"encoding/base64"
	"strconv"
	"strings"
)

// The kitty graphics protocol: a program in a pane says "here is an image"
// and "put it here", and the terminal draws it over the cells. tend sits in
// the middle of that — the pane's output goes through this emulator and what
// the user sees is redrawn from the grid — so an image has to be taken apart
// here and put back together by the client, against the outer terminal.
//
// herdr does the same (`kitty_graphics.rs`), on libghostty's image store. This
// is the store: what was transmitted, and where it was placed. Nothing here
// draws anything; a screen cannot, because the thing that can is the terminal
// the client is talking to.
//
// What is supported is what programs actually send: direct transmission
// (t=d) of RGB, RGBA or PNG data, in chunks, placed at the cursor. A file
// transfer (t=f, t=t, t=s) is refused rather than half-honoured — it would
// mean tend reading a path chosen by whatever is in the pane.

// KittyFormat is how an image's bytes are encoded.
type KittyFormat uint8

const (
	// KittyRGB is 24-bit, KittyRGBA 32-bit, KittyPNG a PNG file.
	KittyRGB  KittyFormat = 24
	KittyRGBA KittyFormat = 32
	KittyPNG  KittyFormat = 100
)

// maxImageBytes bounds one image. A pane's program could otherwise hand tend
// as much memory as it likes by transmitting and never placing.
const maxImageBytes = 16 << 20

// maxImages is how many images one screen keeps. Past that the oldest goes:
// a program that transmits without deleting — and they do — must not grow
// without end.
const maxImages = 64

// KittyImage is what was transmitted.
type KittyImage struct {
	ID     uint32
	Format KittyFormat
	// Width and Height are in pixels, as the sender declared them. A PNG
	// carries its own size and these are zero.
	Width, Height int
	Data          []byte
}

// KittyPlacement is one appearance of an image on the screen.
type KittyPlacement struct {
	ImageID uint32
	// ID is the placement's own id, so the sender can delete this one and
	// leave others of the same image alone.
	ID uint32
	// Row is absolute: history rows first, so an image moves up with the text
	// it was placed beside rather than staying where the screen was.
	Row int
	Col int
	// Cols and Rows are how many cells the sender asked it to cover. Zero
	// means the image's own size decides.
	Cols, Rows int
	// Z is the stacking order, which the client passes on.
	Z int
}

// kittyState is a screen's graphics: the images it holds and where they are.
type kittyState struct {
	images     map[uint32]*KittyImage
	order      []uint32
	placements []KittyPlacement
	// pending is a transfer arriving in chunks.
	pending *kittyTransfer
	// revision counts changes, so a client can tell whether it has the
	// current picture without comparing every image.
	revision uint64
}

// kittyTransfer is an image being sent in chunks.
type kittyTransfer struct {
	image KittyImage
	place bool
	spec  KittyPlacement
}

// KittyImages is every image the screen holds, oldest first.
func (s *Screen) KittyImages() []KittyImage {
	if s.kitty == nil {
		return nil
	}
	out := make([]KittyImage, 0, len(s.kitty.order))
	for _, id := range s.kitty.order {
		if img, ok := s.kitty.images[id]; ok {
			out = append(out, *img)
		}
	}
	return out
}

// KittyPlacements is where the images are, in the order they were placed.
func (s *Screen) KittyPlacements() []KittyPlacement {
	if s.kitty == nil {
		return nil
	}
	return append([]KittyPlacement(nil), s.kitty.placements...)
}

// KittyRevision changes whenever the images or placements do.
func (s *Screen) KittyRevision() uint64 {
	if s.kitty == nil {
		return 0
	}
	return s.kitty.revision
}

// apcGraphics handles a kitty graphics command. It returns false for an APC
// that is not one, which the caller discards as before.
func (s *Screen) apcGraphics(data []byte) bool {
	if len(data) == 0 || data[0] != 'G' {
		return false
	}
	control, payload, _ := strings.Cut(string(data[1:]), ";")
	keys := parseKittyKeys(control)

	if s.kitty == nil {
		s.kitty = &kittyState{images: map[uint32]*KittyImage{}}
	}
	k := s.kitty

	switch keys["a"] {
	case "", "t", "T":
		s.kittyTransmit(keys, payload)
	case "p":
		s.kittyPlace(keys)
	case "d":
		s.kittyDelete(keys)
	default:
		// A command this build does not do — animation, for instance — is
		// dropped. Answering it wrongly would be worse than not answering.
	}
	k.revision++
	return true
}

// kittyTransmit takes an image, or a chunk of one.
func (s *Screen) kittyTransmit(keys map[string]string, payload string) {
	k := s.kitty
	if t := keys["t"]; t != "" && t != "d" {
		// A file transfer names a path for tend to read. Whatever is in the
		// pane chose that path, so it is refused.
		k.pending = nil
		return
	}

	if k.pending == nil {
		k.pending = &kittyTransfer{
			image: KittyImage{
				ID:     uint32(kittyNum(keys, "i")),
				Format: kittyFormat(keys),
				Width:  kittyNum(keys, "s"),
				Height: kittyNum(keys, "v"),
			},
			place: keys["a"] == "T",
			spec:  placementFrom(keys),
		}
	}
	chunk, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		k.pending = nil
		return
	}
	if len(k.pending.image.Data)+len(chunk) > maxImageBytes {
		k.pending = nil
		return
	}
	k.pending.image.Data = append(k.pending.image.Data, chunk...)

	if keys["m"] == "1" {
		return // more chunks follow
	}

	transfer := k.pending
	k.pending = nil
	if len(transfer.image.Data) == 0 {
		return
	}
	s.storeKittyImage(transfer.image)
	if transfer.place {
		spec := transfer.spec
		spec.ImageID = transfer.image.ID
		s.addKittyPlacement(spec)
	}
}

// storeKittyImage keeps an image, forgetting the oldest when there are too
// many.
func (s *Screen) storeKittyImage(img KittyImage) {
	k := s.kitty
	if _, exists := k.images[img.ID]; !exists {
		k.order = append(k.order, img.ID)
	}
	k.images[img.ID] = &img

	for len(k.order) > maxImages {
		oldest := k.order[0]
		k.order = k.order[1:]
		delete(k.images, oldest)
		k.placements = dropPlacements(k.placements, func(p KittyPlacement) bool {
			return p.ImageID == oldest
		})
	}
}

// kittyPlace puts an image on the screen at the cursor.
func (s *Screen) kittyPlace(keys map[string]string) {
	spec := placementFrom(keys)
	spec.ImageID = uint32(kittyNum(keys, "i"))
	if _, ok := s.kitty.images[spec.ImageID]; !ok {
		return // nothing to place
	}
	s.addKittyPlacement(spec)
}

// addKittyPlacement records where an image is, in absolute rows so that it
// travels with the text.
func (s *Screen) addKittyPlacement(spec KittyPlacement) {
	grid := s.Grid()
	cursor := s.Cursor()
	spec.Row = cursor.Y
	if grid == s.MainGrid() {
		spec.Row += grid.HistoryLen()
	}
	spec.Col = cursor.X

	// One placement per (image, placement id), as the protocol has it: the
	// same pair again moves it rather than drawing twice.
	s.kitty.placements = dropPlacements(s.kitty.placements, func(p KittyPlacement) bool {
		return p.ImageID == spec.ImageID && p.ID == spec.ID
	})
	s.kitty.placements = append(s.kitty.placements, spec)
}

// kittyDelete removes placements, and images when asked in capitals.
func (s *Screen) kittyDelete(keys map[string]string) {
	k := s.kitty
	what := keys["d"]
	if what == "" {
		what = "a"
	}
	// Capitals mean "and free the image itself", which is the only part of
	// the delete grammar that changes what is kept rather than what is shown.
	free := what[0] >= 'A' && what[0] <= 'Z'
	id := uint32(kittyNum(keys, "i"))

	switch strings.ToLower(what) {
	case "a":
		k.placements = nil
		if free {
			k.images, k.order = map[uint32]*KittyImage{}, nil
		}
	case "i":
		k.placements = dropPlacements(k.placements, func(p KittyPlacement) bool {
			return p.ImageID == id
		})
		if free {
			delete(k.images, id)
			k.order = dropIDs(k.order, id)
		}
	case "p":
		placement := uint32(kittyNum(keys, "p"))
		k.placements = dropPlacements(k.placements, func(p KittyPlacement) bool {
			return p.ImageID == id && p.ID == placement
		})
	}
}

// ClearKittyBelow forgets placements a clearing of the screen wiped out.
//
// A program that clears the screen expects its images to go with it: they were
// drawn over cells that no longer say what they said.
func (s *Screen) ClearKittyBelow(fromRow int) {
	if s.kitty == nil {
		return
	}
	before := len(s.kitty.placements)
	s.kitty.placements = dropPlacements(s.kitty.placements, func(p KittyPlacement) bool {
		return p.Row >= fromRow
	})
	if len(s.kitty.placements) != before {
		s.kitty.revision++
	}
}

func dropPlacements(list []KittyPlacement, match func(KittyPlacement) bool) []KittyPlacement {
	out := list[:0]
	for _, p := range list {
		if !match(p) {
			out = append(out, p)
		}
	}
	return out
}

func dropIDs(list []uint32, id uint32) []uint32 {
	out := list[:0]
	for _, candidate := range list {
		if candidate != id {
			out = append(out, candidate)
		}
	}
	return out
}

// placementFrom reads the placement keys of a command.
func placementFrom(keys map[string]string) KittyPlacement {
	return KittyPlacement{
		ID:   uint32(kittyNum(keys, "p")),
		Cols: kittyNum(keys, "c"),
		Rows: kittyNum(keys, "r"),
		Z:    kittyNum(keys, "z"),
	}
}

func kittyFormat(keys map[string]string) KittyFormat {
	switch kittyNum(keys, "f") {
	case 24:
		return KittyRGB
	case 100, 0:
		return KittyPNG
	}
	return KittyRGBA
}

// parseKittyKeys reads the comma-separated key=value control data.
func parseKittyKeys(control string) map[string]string {
	keys := make(map[string]string, 8)
	for _, pair := range strings.Split(control, ",") {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		keys[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return keys
}

func kittyNum(keys map[string]string, key string) int {
	n, err := strconv.Atoi(keys[key])
	if err != nil {
		return 0
	}
	return n
}
