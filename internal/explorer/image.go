package explorer

import (
	"image"
	"image/color"
	_ "image/gif" // decoders for image.Decode
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/sousaakira/tend/internal/vt"
)

// An image in the preview is drawn with the upper half block, one cell
// holding two pixels: the top one as the character's colour, the bottom as
// its background. It is what text-art tools do in their plainest mode, and
// it needs nothing of the terminal but 24-bit colour — no graphics
// protocol, which tend passes on only to terminals that have one. The
// decoders are the standard library's: PNG, JPEG and GIF.

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true}

// isImage is whether the preview draws a file as a picture.
func isImage(path string) bool { return imageExts[strings.ToLower(filepath.Ext(path))] }

// maxImagePixels refuses images whose decoding would take the panel's
// memory for a thumbnail: 40 megapixels is a large photograph.
const maxImagePixels = 40_000_000

// renderImage scales an image to fit cols by rows cells (two pixel rows a
// cell), keeping its proportions, and returns a line of spans per row and
// the image's size.
func renderImage(path string, cols, rows int) (lines [][]span, w, h int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, 0, 0, err
	}
	if cfg.Width*cfg.Height > maxImagePixels {
		return nil, cfg.Width, cfg.Height, errImageTooLarge
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, 0, 0, err
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	w, h = b.Dx(), b.Dy()
	if w == 0 || h == 0 || cols <= 0 || rows <= 0 {
		return nil, w, h, nil
	}
	// The target in pixels: cols wide, 2*rows tall, never enlarged.
	scale := min(float64(cols)/float64(w), float64(2*rows)/float64(h), 1)
	tw, th := max(int(float64(w)*scale), 1), max(int(float64(h)*scale), 1)
	if th%2 == 1 {
		th++
	}
	sample := func(x, y int) color.RGBA {
		// The average of the source pixels this target pixel covers, so a
		// scaled-down image is not a scattering of its individual pixels.
		x0, x1 := b.Min.X+x*w/tw, b.Min.X+max((x+1)*w/tw, x*w/tw+1)
		y0, y1 := b.Min.Y+y*h/th, b.Min.Y+max((y+1)*h/th, y*h/th+1)
		stepX, stepY := max((x1-x0)/4, 1), max((y1-y0)/4, 1)
		var r, g, bl, a, n uint32
		for yy := y0; yy < y1 && yy < b.Max.Y; yy += stepY {
			for xx := x0; xx < x1 && xx < b.Max.X; xx += stepX {
				cr, cg, cb, ca := img.At(xx, yy).RGBA()
				r, g, bl, a, n = r+cr, g+cg, bl+cb, a+ca, n+1
			}
		}
		if n == 0 {
			return color.RGBA{}
		}
		// Transparent pixels over black, as a dark terminal shows them.
		return color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), uint8(a / n >> 8)}
	}
	for y := 0; y < th; y += 2 {
		var line []span
		for x := 0; x < tw; x++ {
			top, bottom := sample(x, y), sample(x, y+1)
			line = append(line, span{"▀", vt.Style{
				FG: vt.RGBColor(top.R, top.G, top.B),
				BG: vt.RGBColor(bottom.R, bottom.G, bottom.B),
			}})
		}
		lines = append(lines, line)
	}
	return lines, w, h, nil
}

type imageError string

func (e imageError) Error() string { return string(e) }

const errImageTooLarge = imageError("image too large to preview")
