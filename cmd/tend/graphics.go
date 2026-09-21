package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/ui"
)

// An image a program drew in a pane has to be drawn again here, on the
// terminal this client is talking to. The pane's own terminal cannot draw it —
// it is a grid of cells in another process — and tend redraws the screen from
// that grid, so anything the image left behind would be painted over.
//
// So: the server keeps what was sent and where, this asks for it when the
// pane's revision moves, and after every paint it puts the visible ones on
// screen with the terminal's own graphics protocol. It is herdr's arrangement
// (`kitty_graphics.rs`), where images come out of a pane and go back as host
// placements with ids of tend's own.
//
// The terminal has to speak it. Kitty, Ghostty and WezTerm do; the rest get
// nothing, which is what they would have shown anyway.

// hostImageBase keeps tend's image ids away from any the outer terminal is
// already using for something else. herdr reserves a base for the same reason.
const hostImageBase = 990_000

// chunkBytes is how much base64 goes in one escape sequence. The protocol asks
// for 4096 or less; this is kitty's own suggestion.
const chunkBytes = 3072

// graphicsState is what this client has put on the outer terminal.
type graphicsState struct {
	// revision is the pane revision each pane's images were fetched at, and
	// cache what was fetched then.
	revision map[uint64]uint64
	cache    map[uint64]proto.PaneGraphicsResult
	// images maps a pane's image id to the id it was given here, and says
	// whether the bytes have been sent.
	sent map[imageKey]uint32
	// shown is what is on the screen now, so the next paint can tell what to
	// delete rather than clearing everything and flickering.
	shown map[placementKey]placedAt
	next  uint32
}

type imageKey struct {
	pane  uint64
	image uint32
}

type placementKey struct {
	pane      uint64
	image, id uint32
}

// placedAt is where a placement was drawn, in the outer terminal's cells.
type placedAt struct {
	x, y int
	// host is the id the outer terminal knows it by.
	host uint32
}

func newGraphicsState() *graphicsState {
	return &graphicsState{
		revision: map[uint64]uint64{},
		cache:    map[uint64]proto.PaneGraphicsResult{},
		sent:     map[imageKey]uint32{},
		shown:    map[placementKey]placedAt{},
		next:     hostImageBase,
	}
}

// graphicsSupported reports whether the terminal takes kitty graphics.
//
// Asked of the environment rather than the terminal itself: the protocol has a
// query, but it answers on the input stream, and tend's input stream is the
// user's keyboard going to a pane. Guessing wrong and staying silent is better
// than a stray reply typed into an agent.
func graphicsSupported(env func(string) string) bool {
	if env("TEND_GRAPHICS") == "off" {
		return false
	}
	if env("KITTY_WINDOW_ID") != "" {
		return true
	}
	switch env("TERM_PROGRAM") {
	case "ghostty", "WezTerm":
		return true
	}
	switch term := env("TERM"); {
	case term == "xterm-kitty", term == "xterm-ghostty":
		return true
	case strings.Contains(term, "wezterm"):
		return true
	}
	return false
}

// graphicsFor returns the state to draw images with, or nil for a terminal
// that cannot.
func graphicsFor(env func(string) string) *graphicsState {
	if !graphicsSupported(env) {
		return nil
	}
	return newGraphicsState()
}

// syncGraphics fetches what it needs and draws the images of the panes on
// screen. It runs after a paint, on the drawing goroutine.
func (t *tui) syncGraphics(frame ui.Frame) {
	if t.graphics == nil {
		return
	}

	// Read once, under the lock: the scroll view moves on another goroutine,
	// and these decide where every image goes.
	t.mu.Lock()
	scrollPane, scrollOffset := t.scrollPane, t.scrollOffset
	t.mu.Unlock()

	var out strings.Builder
	live := map[placementKey]bool{}

	for _, pane := range frame.Panes {
		if pane.Screen == nil {
			continue
		}
		result, ok := t.paneGraphics(pane.ID)
		if !ok || len(result.Placements) == 0 {
			continue
		}
		inner := ui.PaneInner(pane.Rect)
		// What the pane is showing: the rows on screen are the last ones of
		// history plus screen, unless this client is scrolled back.
		top := result.History
		if pane.ID == scrollPane {
			top -= scrollOffset
		}

		for _, p := range result.Placements {
			y := p.Row - top
			if y < 0 || y >= inner.Rows || p.Col >= inner.Cols {
				continue // above, below or off the side of what is drawn
			}
			key := placementKey{pane: pane.ID, image: p.ImageID, id: p.ID}
			at := placedAt{x: inner.X + p.Col, y: inner.Y + y}
			live[key] = true

			host, ready := t.hostImage(&out, pane.ID, p.ImageID, result.Images)
			if !ready {
				continue
			}
			at.host = host
			if was, drawn := t.graphics.shown[key]; drawn && was == at {
				continue // already there, at the same place
			}
			t.graphics.shown[key] = at
			writePlacement(&out, at, host, p)
		}
	}

	// Anything that was on screen and is not now: scrolled away, deleted by
	// the program, or in a pane that closed.
	for key, at := range t.graphics.shown {
		if live[key] {
			continue
		}
		delete(t.graphics.shown, key)
		fmt.Fprintf(&out, "\x1b_Ga=d,d=i,i=%d\x1b\\", at.host)
	}

	if out.Len() > 0 {
		_, _ = os.Stdout.WriteString(out.String())
	}
}

// paneGraphics returns a pane's images, fetching them when the pane says they
// changed. The revision is in the snapshot, so a pane with no images — which
// is nearly all of them — costs nothing.
func (t *tui) paneGraphics(pane uint64) (proto.PaneGraphicsResult, bool) {
	t.mu.Lock()
	var revision uint64
	for _, p := range t.snap.Panes {
		if p.ID == pane {
			revision = p.Graphics
		}
	}
	cached, have := t.graphics.cache[pane]
	t.mu.Unlock()

	if revision == 0 {
		return proto.PaneGraphicsResult{}, false
	}
	if have && t.graphics.revision[pane] == revision {
		return cached, true
	}

	result, err := t.client.PaneGraphics(pane)
	if err != nil {
		return proto.PaneGraphicsResult{}, false
	}
	t.mu.Lock()
	t.graphics.cache[pane] = result
	t.graphics.revision[pane] = revision
	t.mu.Unlock()
	return result, true
}

// hostImage makes sure the outer terminal has the image, and returns the id it
// knows it by.
func (t *tui) hostImage(out *strings.Builder, pane uint64, image uint32, images []proto.GraphicsImage) (uint32, bool) {
	key := imageKey{pane: pane, image: image}
	if host, ok := t.graphics.sent[key]; ok {
		return host, true
	}
	var found *proto.GraphicsImage
	for i := range images {
		if images[i].ID == image {
			found = &images[i]
		}
	}
	if found == nil || len(found.Data) == 0 {
		return 0, false
	}

	t.graphics.next++
	host := t.graphics.next
	t.graphics.sent[key] = host
	writeImage(out, host, *found)
	return host, true
}

// writeImage transmits an image to the outer terminal, in chunks.
func writeImage(out *strings.Builder, host uint32, img proto.GraphicsImage) {
	encoded := base64.StdEncoding.EncodeToString(img.Data)
	first := true
	for len(encoded) > 0 {
		chunk := encoded
		if len(chunk) > chunkBytes {
			chunk = chunk[:chunkBytes]
		}
		encoded = encoded[len(chunk):]
		more := 0
		if len(encoded) > 0 {
			more = 1
		}
		if first {
			// q=2 asks the terminal not to answer: its reply would arrive on
			// the input stream, which here is the user's keyboard on its way
			// to a pane.
			fmt.Fprintf(out, "\x1b_Ga=t,t=d,q=2,i=%d,f=%d", host, img.Format)
			if img.Width > 0 && img.Height > 0 {
				fmt.Fprintf(out, ",s=%d,v=%d", img.Width, img.Height)
			}
			fmt.Fprintf(out, ",m=%d;%s\x1b\\", more, chunk)
			first = false
			continue
		}
		fmt.Fprintf(out, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
	}
}

// writePlacement puts an image on the screen where the pane has it.
func writePlacement(out *strings.Builder, at placedAt, host uint32, p proto.GraphicsPlacement) {
	// The cursor is moved because a placement lands where the cursor is, and
	// then put back: this runs between paints, and the next paint would
	// otherwise draw from wherever the image left it.
	fmt.Fprintf(out, "\x1b7\x1b[%d;%dH", at.y+1, at.x+1)
	fmt.Fprintf(out, "\x1b_Ga=p,q=2,i=%d,p=%d,C=1", host, placementID(p))
	if p.Cols > 0 {
		fmt.Fprintf(out, ",c=%d", p.Cols)
	}
	if p.Rows > 0 {
		fmt.Fprintf(out, ",r=%d", p.Rows)
	}
	if p.Z != 0 {
		fmt.Fprintf(out, ",z=%d", p.Z)
	}
	out.WriteString("\x1b\\\x1b8")
}

// placementID is the id this client gives a placement. Zero is "the only one",
// which two placements of one image would fight over.
func placementID(p proto.GraphicsPlacement) uint32 {
	if p.ID == 0 {
		return 1
	}
	return p.ID
}

// clearGraphics takes every image this client put on the terminal off it,
// which is what leaving looks like from the terminal's side.
func (t *tui) clearGraphics() {
	if t.graphics == nil || len(t.graphics.shown) == 0 {
		return
	}
	t.graphics.shown = map[placementKey]placedAt{}
	_, _ = os.Stdout.WriteString("\x1b_Ga=d,d=A\x1b\\")
}
