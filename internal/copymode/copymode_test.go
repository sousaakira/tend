package copymode

import (
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// screen writes text into a fresh terminal and reads it back as copy mode sees
// it, so these tests go through the same grid the server has.
func screen(t *testing.T, cols, rows int, text string) ScreenText {
	t.Helper()
	s := vt.NewScreen(cols, rows, 100)
	if _, err := s.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	return FromScreen(s)
}

// TestWordMotionsMoveAsVimDoes: `w`, `b` and `e` are muscle memory. If they
// stop at the wrong place, copy mode is slower than the mouse it replaces.
func TestWordMotionsMoveAsVimDoes(t *testing.T) {
	text := screen(t, 40, 3, "foo.bar  baz\r\nqux")
	for _, c := range []struct {
		name   string
		from   Point
		motion string
		want   Point
	}{
		{"w stops at punctuation", Point{0, 0}, NextWordStart, Point{0, 3}},
		{"w leaves punctuation for the next word", Point{0, 3}, NextWordStart, Point{0, 4}},
		{"w skips the spaces", Point{0, 4}, NextWordStart, Point{0, 9}},
		{"w crosses a line break", Point{0, 9}, NextWordStart, Point{1, 0}},
		{"W takes punctuation as part of the word", Point{0, 0}, NextBigWordStart, Point{0, 9}},
		{"e lands on the last letter", Point{0, 0}, NextWordEnd, Point{0, 2}},
		{"E lands on the end of the big word", Point{0, 0}, NextBigWordEnd, Point{0, 6}},
		{"b goes back to the start of the word", Point{0, 10}, PreviousWordStart, Point{0, 9}},
		{"b crosses a line break", Point{1, 0}, PreviousWordStart, Point{0, 9}},
		{"B goes back over punctuation", Point{0, 9}, PreviousBigWordStart, Point{0, 0}},
		{"$ lands on the last character", Point{0, 0}, LineEnd, Point{0, 11}},
		{"^ lands on the first non-blank", Point{0, 5}, FirstNonBlank, Point{0, 0}},
	} {
		if got := Move(text, c.from, c.motion); got != c.want {
			t.Errorf("%s: %v from %v = %v, want %v", c.name, c.motion, c.from, got, c.want)
		}
	}
}

// TestParagraphMotionsStopAtBlankLines: `{` and `}` are how a long answer is
// crossed in a few presses instead of a few dozen.
func TestParagraphMotionsStopAtBlankLines(t *testing.T) {
	text := screen(t, 20, 6, "one\r\ntwo\r\n\r\nthree\r\nfour")
	if got := Move(text, Point{0, 0}, NextParagraph); got != (Point{2, 0}) {
		t.Errorf("} = %v, want the blank line", got)
	}
	if got := Move(text, Point{4, 1}, PreviousParagraph); got != (Point{2, 0}) {
		t.Errorf("{ = %v, want the blank line", got)
	}
}

// TestSearchFindsTheNearestMatchAndWraps covers `/`, `?`, `n` and `N`: literal,
// smart case, and round the end the way vim's wrapscan goes.
func TestSearchFindsTheNearestMatchAndWraps(t *testing.T) {
	text := screen(t, 30, 5, "error one\r\nfine\r\nError two\r\nerror three")

	m, total, ok := Search(text, "error", Point{0, 0}, Forward)
	if !ok || total != 3 || m.Start != (Point{2, 0}) {
		t.Errorf("forward = %+v, total %d, %v; want the one on row 2 of 3", m, total, ok)
	}
	// A capital makes it exact.
	if _, total, _ := Search(text, "Error", Point{0, 0}, Forward); total != 1 {
		t.Errorf("a capitalised query matched %d times, want 1", total)
	}
	// Backward from the top goes round to the bottom.
	m, _, _ = Search(text, "error", Point{0, 0}, Backward)
	if m.Start != (Point{3, 0}) {
		t.Errorf("backward from the top = %+v, want the last match", m)
	}
	// A literal search: a path is not a pattern.
	paths := screen(t, 30, 2, "see a.b and axb")
	if _, total, _ := Search(paths, "a.b", Point{0, 0}, Forward); total != 1 {
		t.Errorf("\"a.b\" matched %d times; a dot must mean a dot", total)
	}
}

// TestSearchFindsTextBrokenAcrossTheEdge: a path wrapped at the pane's right
// edge is one path. A search that treated the rows separately could not find
// it, and that is exactly what copy mode is used to find.
func TestSearchFindsTextBrokenAcrossTheEdge(t *testing.T) {
	text := screen(t, 10, 3, "/home/user/project")
	m, _, ok := Search(text, "user/proj", Point{0, 0}, Forward)
	if !ok {
		t.Fatal("a match across the wrap was not found")
	}
	if m.Start.Row != 0 || m.End.Row != 1 {
		t.Errorf("match = %+v, want it to start on row 0 and end on row 1", m)
	}
}
