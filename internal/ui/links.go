package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// Links in a pane are herdr's (`app/actions.rs`, url_span_at_column and
// trim_url_edges): a run of text from http:// or https:// to the next
// space, with trailing punctuation left out — a sentence's full stop, a
// quote, and a closing bracket unless the URL opened one itself, so
// "(see https://x.org/a(b))" is https://x.org/a(b). Only web URLs: a
// file:// or a custom scheme is not something a click should hand to a
// browser.

// LinkSpan is a URL on a row: its columns, from Start up to and including
// End, and the URL.
type LinkSpan struct {
	Start, End int
	URL        string
}

type linkCell struct {
	r          rune
	start, end int // columns the character covers
}

// LinkAt is the URL covering column col of a row of text, if one does.
func LinkAt(row string, col int) (LinkSpan, bool) {
	var cells []linkCell
	next := 0
	for _, r := range row {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			continue // a combining mark sits on the letter before it
		}
		cells = append(cells, linkCell{r: r, start: next, end: next + w - 1})
		next += w
	}
	clicked := -1
	for i, c := range cells {
		if col >= c.start && col <= c.end {
			clicked = i
		}
	}
	if clicked < 0 {
		return LinkSpan{}, false
	}
	text := func(from, to int) string {
		var b strings.Builder
		for _, c := range cells[from : to+1] {
			b.WriteRune(c.r)
		}
		return b.String()
	}
	for start := 0; start < len(cells); {
		rest := text(start, min(start+7, len(cells)-1))
		if !strings.HasPrefix(rest, "http://") && !strings.HasPrefix(rest, "https://") {
			start++
			continue
		}
		end := start
		for end+1 < len(cells) && cells[end+1].r != ' ' && cells[end+1].r != '\t' {
			end++
		}
		if clicked >= start && clicked <= end {
			for end >= start && trimTrailing(cells, start, end) {
				end--
			}
			url := text(start, end)
			if end < start || clicked > end || url == "http://" || url == "https://" {
				return LinkSpan{}, false
			}
			return LinkSpan{Start: cells[start].start, End: cells[end].end, URL: url}, true
		}
		start = end + 1
	}
	return LinkSpan{}, false
}

// trimTrailing is herdr's should_trim_trailing_url_cell.
func trimTrailing(cells []linkCell, start, end int) bool {
	switch cells[end].r {
	case '"', '\'', '`', '.', ',', ';', ':', '!', '?':
		return true
	case ')':
		return !balanced(cells, start, end, '(', ')')
	case ']':
		return !balanced(cells, start, end, '[', ']')
	case '}':
		return !balanced(cells, start, end, '{', '}')
	}
	return false
}

// balanced is whether the closer at end closes an opener inside the URL.
func balanced(cells []linkCell, start, end int, open, close rune) bool {
	depth := 0
	for _, c := range cells[start:end] {
		switch c.r {
		case open:
			depth++
		case close:
			depth--
		}
	}
	return depth > 0
}

// LinkHover is a link under the pointer with ctrl held, drawn underlined
// so the click about to be made says what it will open.
type LinkHover struct {
	Pane       uint64
	Row        int
	Start, End int
}

func drawLinkHover(dst *vt.Grid, f Frame) {
	h := f.LinkHover
	if h == nil {
		return
	}
	for _, p := range f.Panes {
		if p.ID != h.Pane {
			continue
		}
		inner := innerRect(p.Rect)
		if h.Row < 0 || h.Row >= inner.Rows {
			return
		}
		row := dst.Line(inner.Y + h.Row)
		if row == nil {
			return
		}
		for x := h.Start; x <= h.End && x < inner.Cols; x++ {
			cell := row.Cell(inner.X + x)
			cell.Style.Attrs |= vt.AttrUnderline
			cell.Style.Underline = vt.UnderlineSingle
			row.SetCell(inner.X+x, cell)
		}
	}
}
