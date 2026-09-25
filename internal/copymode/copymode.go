// Package copymode is where the cursor goes when somebody moves it through a
// pane's text with the keyboard: by word, to the end of the line, to the next
// blank line, to the next match of a search.
//
// It is herdr's (`pane/terminal.rs`: `RetainedTextBuffer`, `word_motion`,
// `paragraph_motion_target`, `search_window`), ported as a function of the
// text alone. The text is the whole of a pane's history and screen, addressed
// by absolute row: row 0 is the oldest line kept. The client holds the cursor
// and the selection; this answers where a motion lands, which needs the text,
// and the text is on the server.
package copymode

import (
	"strings"
	"unicode"

	"github.com/auth-com-br/tend/internal/vt"
)

// Point is a cell, by absolute row and column.
type Point struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// Text is a pane's rows, oldest first.
type Text interface {
	Rows() int
	Row(i int) *vt.Row
}

// ScreenText is a screen's history and visible rows as one sequence.
//
// Only the main screen has a history. A full-screen program on the alternate
// screen is read as it shows itself, which is what the user is looking at.
type ScreenText struct {
	grid    *vt.Grid
	history int
}

// FromScreen reads a screen. The caller holds whatever lock guards it for as
// long as the result is used.
func FromScreen(s *vt.Screen) ScreenText {
	grid := s.Grid()
	history := 0
	if grid == s.MainGrid() {
		history = grid.HistoryLen()
	}
	return ScreenText{grid: grid, history: history}
}

// Rows is how many rows there are, history included.
func (t ScreenText) Rows() int { return t.history + t.grid.Rows() }

// History is how many of those rows are above the screen.
func (t ScreenText) History() int { return t.history }

// Row returns one row, or nil past the end.
func (t ScreenText) Row(i int) *vt.Row {
	switch {
	case i < 0:
		return nil
	case i < t.history:
		return t.grid.HistoryLine(i)
	case i < t.Rows():
		return t.grid.Line(i - t.history)
	}
	return nil
}

// separators are the characters that end a word without being whitespace:
// herdr's `COPY_MODE_WORD_SEPARATORS`, which is vim's idea of punctuation for
// `w` and `b`.
const separators = "!\"#$%&'()*+,-./:;<=>?@[\\]^`{|}~"

type class uint8

const (
	space class = iota
	separator
	word
)

func classOf(r rune) class {
	switch {
	case r == 0 || unicode.IsSpace(r):
		return space
	case r < 0x80 && strings.ContainsRune(separators, r):
		return separator
	}
	return word
}

// atom is one character on the way through the text. A line that ends without
// wrapping contributes one atom with no position after its last cell: it is
// whitespace between words, which is what a line break is to a word motion,
// and it is not anywhere the cursor can land.
type atom struct {
	at    Point
	onRow bool
	class class
}

// atoms reads rows from, to (inclusive) into a stream.
func atoms(t Text, from, to int) []atom {
	var out []atom
	for r := max(from, 0); r <= to && r < t.Rows(); r++ {
		row := t.Row(r)
		if row == nil {
			continue
		}
		for c, cell := range row.Cells() {
			if cell.IsContinuation() {
				continue // the right half of a wide character
			}
			out = append(out, atom{at: Point{r, c}, onRow: true, class: classOf(cell.R)})
		}
		if !row.Wrapped() {
			out = append(out, atom{class: space})
		}
	}
	return out
}

// motionWindow is how far a word motion reads around the cursor. A word does
// not span a thousand lines, and reading the whole history for every keypress
// is the cost this avoids.
const motionWindow = 200

// Motion names, herdr's.
const (
	NextWordStart        = "next_word_start"
	PreviousWordStart    = "previous_word_start"
	NextWordEnd          = "next_word_end"
	NextBigWordStart     = "next_big_word_start"
	PreviousBigWordStart = "previous_big_word_start"
	NextBigWordEnd       = "next_big_word_end"
	LineEnd              = "line_end"
	FirstNonBlank        = "first_non_blank"
	PreviousParagraph    = "previous_paragraph"
	NextParagraph        = "next_paragraph"
)

// Move returns where a motion from a point lands. A motion with nowhere to go
// stays put, as it does in vim.
func Move(t Text, from Point, motion string) Point {
	switch motion {
	case LineEnd:
		return Point{from.Row, lastCharacterCol(t.Row(from.Row))}
	case FirstNonBlank:
		return Point{from.Row, firstNonBlankCol(t.Row(from.Row))}
	case PreviousParagraph:
		return paragraph(t, from, -1)
	case NextParagraph:
		return paragraph(t, from, 1)
	}

	stream := atoms(t, from.Row-motionWindow, from.Row+motionWindow)
	current := -1
	for i, a := range stream {
		if a.onRow && a.at == from {
			current = i
			break
		}
	}
	if current < 0 {
		// The cursor is on a cell the stream skipped — the right half of a
		// wide character. Move from the half that is in it.
		for i, a := range stream {
			if a.onRow && a.at.Row == from.Row && a.at.Col < from.Col {
				current = i
			}
		}
		if current < 0 {
			return from
		}
	}

	var target int
	switch motion {
	case NextWordStart:
		target = nextStart(stream, current, false)
	case NextBigWordStart:
		target = nextStart(stream, current, true)
	case PreviousWordStart:
		target = previousStart(stream, current, false)
	case PreviousBigWordStart:
		target = previousStart(stream, current, true)
	case NextWordEnd:
		target = nextEnd(stream, current, false)
	case NextBigWordEnd:
		target = nextEnd(stream, current, true)
	default:
		return from
	}
	if target < 0 {
		return from
	}
	return target2point(stream, target, motion, from)
}

// same reports whether two atoms belong to one word. A big word is anything
// that is not whitespace; a small one also breaks between letters and
// punctuation.
func same(a, b class, big bool) bool {
	if big {
		return (a == space) == (b == space)
	}
	return a == b
}

func nextStart(s []atom, i int, big bool) int {
	cur := s[i].class
	j := i + 1
	if cur != space {
		for j < len(s) && same(s[j].class, cur, big) {
			j++
		}
	}
	for j < len(s) && s[j].class == space {
		j++
	}
	return forward(s, j)
}

func previousStart(s []atom, i int, big bool) int {
	j := i - 1
	for j >= 0 && s[j].class == space {
		j--
	}
	if j < 0 {
		return -1
	}
	cls := s[j].class
	for j > 0 && same(s[j-1].class, cls, big) {
		j--
	}
	return backward(s, j)
}

func nextEnd(s []atom, i int, big bool) int {
	j := i + 1
	for j < len(s) && s[j].class == space {
		j++
	}
	if j >= len(s) {
		return -1
	}
	cls := s[j].class
	for j+1 < len(s) && same(s[j+1].class, cls, big) {
		j++
	}
	return backward(s, j)
}

// forward and backward find the nearest atom that is a place, skipping the
// line breaks, which are not.
func forward(s []atom, i int) int {
	for ; i < len(s); i++ {
		if s[i].onRow {
			return i
		}
	}
	return -1
}

func backward(s []atom, i int) int {
	for ; i >= 0; i-- {
		if s[i].onRow {
			return i
		}
	}
	return -1
}

func target2point(s []atom, i int, _ string, from Point) Point {
	if i < 0 || i >= len(s) || !s[i].onRow {
		return from
	}
	return s[i].at
}

// paragraph is `{` and `}`: the nearest blank row above or below. herdr looks
// no further than a thousand rows, and neither does this.
func paragraph(t Text, from Point, direction int) Point {
	for d := 1; d < 1000; d++ {
		r := from.Row + d*direction
		if r < 0 || r >= t.Rows() {
			break
		}
		if blank(t.Row(r)) {
			return Point{r, 0}
		}
	}
	return from
}

func blank(row *vt.Row) bool {
	if row == nil {
		return true
	}
	for _, c := range row.Cells() {
		if !c.IsContinuation() && !c.IsBlank() && !unicode.IsSpace(c.R) {
			return false
		}
	}
	return true
}

func firstNonBlankCol(row *vt.Row) int {
	if row == nil {
		return 0
	}
	for c, cell := range row.Cells() {
		if !cell.IsContinuation() && !cell.IsBlank() && !unicode.IsSpace(cell.R) {
			return c
		}
	}
	return 0
}

func lastCharacterCol(row *vt.Row) int {
	if row == nil {
		return 0
	}
	last := 0
	for c, cell := range row.Cells() {
		if !cell.IsContinuation() && !cell.IsBlank() {
			last = c
		}
	}
	return last
}

// Direction of a search.
const (
	Forward  = "forward"
	Backward = "backward"
)

// Match is one occurrence of a search.
type Match struct {
	Start Point `json:"start"`
	End   Point `json:"end"`
}

// maxQuery bounds a search, as herdr does: a query is typed by a person.
const maxQuery = 4096

// Search finds the nearest occurrence of query from a point, and how many
// there are in all.
//
// It is literal, not a pattern — a search typed into copy mode is almost
// always a path or an error message, both full of characters a pattern would
// take as syntax. It ignores case unless the query has a capital in it, which
// is what vim's smartcase does and what a person expects from lowercase.
// Wrapped rows are searched as the one line they are, so a path broken across
// the edge of the pane is still found.
func Search(t Text, query string, from Point, direction string) (found Match, total int, ok bool) {
	if query == "" || len(query) > maxQuery {
		return Match{}, 0, false
	}
	fold := !hasUpper(query)
	needle := []rune(query)
	if fold {
		needle = []rune(strings.ToLower(query))
	}

	var matches []Match
	for _, line := range lines(t) {
		hay := line.runes
		if fold {
			hay = []rune(strings.ToLower(string(line.runes)))
		}
		for i := 0; i+len(needle) <= len(hay); i++ {
			if equalRunes(hay[i:i+len(needle)], needle) {
				matches = append(matches, Match{
					Start: line.at[i], End: line.at[i+len(needle)-1],
				})
			}
		}
	}
	if len(matches) == 0 {
		return Match{}, 0, false
	}

	after := func(m Match) bool {
		return m.Start.Row > from.Row || (m.Start.Row == from.Row && m.Start.Col > from.Col)
	}
	before := func(m Match) bool {
		return m.Start.Row < from.Row || (m.Start.Row == from.Row && m.Start.Col < from.Col)
	}

	if direction == Backward {
		for i := len(matches) - 1; i >= 0; i-- {
			if before(matches[i]) {
				return matches[i], len(matches), true
			}
		}
		// Past the top, round to the bottom, as vim's wrapscan does.
		return matches[len(matches)-1], len(matches), true
	}
	for _, m := range matches {
		if after(m) {
			return m, len(matches), true
		}
	}
	return matches[0], len(matches), true
}

// line is a logical line: rows joined across soft wraps, each rune remembering
// the cell it came from.
type line struct {
	runes []rune
	at    []Point
}

func lines(t Text) []line {
	var (
		out []line
		cur line
	)
	for r := 0; r < t.Rows(); r++ {
		row := t.Row(r)
		if row == nil {
			continue
		}
		for c, cell := range row.Cells() {
			if cell.IsContinuation() {
				continue
			}
			ch := cell.R
			if ch == 0 {
				ch = ' '
			}
			cur.runes = append(cur.runes, ch)
			cur.at = append(cur.at, Point{r, c})
		}
		if row.Wrapped() {
			continue
		}
		out = append(out, cur)
		cur = line{}
	}
	if len(cur.runes) > 0 {
		out = append(out, cur)
	}
	return out
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func equalRunes(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
