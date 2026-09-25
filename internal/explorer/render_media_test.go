package explorer

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// writePNG is a w×h image, red on top and blue below.
func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{255, 0, 0, 255}
			if y >= h/2 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// TestAnImageIsDrawnInHalfBlocks: two pixels a cell, top as the colour and
// bottom as the background, scaled to fit and never enlarged. If it
// regresses, an image previews as "binary file".
func TestAnImageIsDrawnInHalfBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flag.png")
	writePNG(t, path, 40, 40)
	lines, w, h, err := renderImage(path, 20, 10)
	if err != nil || w != 40 || h != 40 {
		t.Fatalf("render: %v %dx%d", err, w, h)
	}
	if len(lines) != 10 || len(lines[0]) != 20 {
		t.Fatalf("scaled to %d rows of %d, want 10 of 20", len(lines), len(lines[0]))
	}
	top, bottom := lines[0][0].style, lines[9][0].style
	if lines[0][0].text != "▀" || top.FG != vt.RGBColor(255, 0, 0) || bottom.BG != vt.RGBColor(0, 0, 255) {
		t.Errorf("top %+v bottom %+v", top, bottom)
	}
	small, _, _, _ := renderImage(path, 200, 100)
	if len(small) != 20 || len(small[0]) != 40 {
		t.Errorf("a small image is not enlarged: %d×%d", len(small[0]), len(small))
	}

	p := NewPreview(path, 0, nil)
	text := previewText(p, 30, 14)
	if !strings.Contains(text, "40×40") || !strings.Contains(text, "▀") {
		t.Errorf("the preview:\n%s", text)
	}
}

// TestMarkdownReadsAsItReads: headings without their hashes, emphasis and
// code without their marks, links as their text, bullets, quotes, fences
// gone; m shows the source. If it regresses, a README previews as its
// source.
func TestMarkdownReadsAsItReads(t *testing.T) {
	src := []string{
		"# Title",
		"Some **bold** and *soft* and `code` and a [link](http://x).",
		"- one",
		"- [x] done",
		"1. first",
		"> quoted",
		"```go",
		"x := 1",
		"```",
		"---",
		"snake_case_name stays",
	}
	out := renderMarkdown(src, 20)
	flat := make([]string, len(out))
	for i, line := range out {
		for _, s := range line {
			flat[i] += s.text
		}
	}
	want := []string{"TITLE", "Some bold and soft and code and a link.", "• one", "☑ done", "1. first", "▎ quoted", "  go", "  x := 1"}
	for i, w := range want {
		if flat[i] != w {
			t.Errorf("line %d = %q, want %q", i, flat[i], w)
		}
	}
	if !strings.HasPrefix(flat[8], "───") || flat[9] != "snake_case_name stays" {
		t.Errorf("rule and snake_case: %q %q", flat[8], flat[9])
	}
	for _, s := range out[1] {
		if s.text == "bold" && s.style.Attrs&vt.AttrBold == 0 {
			t.Errorf("bold is not bold: %+v", s)
		}
		if s.text == "link" && s.style != mdLink {
			t.Errorf("a link is not a link: %+v", s)
		}
	}

	path := filepath.Join(t.TempDir(), "README.md")
	os.WriteFile(path, []byte(strings.Join(src, "\n")), 0o644)
	p := NewPreview(path, 0, nil)
	if text := previewText(p, 50, 16); !strings.Contains(text, "TITLE") || strings.Contains(text, "# Title") {
		t.Errorf("rendered by default:\n%s", text)
	}
	p.Key(Key{Name: "m"})
	if text := previewText(p, 50, 16); !strings.Contains(text, "# Title") {
		t.Errorf("m shows the source:\n%s", text)
	}
	if q := NewPreview(path, 3, nil); !q.raw {
		t.Error("opened at a line, markdown starts as source, where the line is")
	}
}
