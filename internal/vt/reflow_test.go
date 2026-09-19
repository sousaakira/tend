package vt

import (
	"strings"
	"testing"
	"time"
)

// visible returns the screen's rows as text, trailing blank rows removed, so
// a reflow can be asserted as the text a person would see.
func visible(s *Screen) []string {
	g := s.Grid()
	out := make([]string, 0, g.Rows())
	for y := 0; y < g.Rows(); y++ {
		out = append(out, g.Line(y).Text())
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// allText returns history and viewport together, which is the whole of what
// reflow is responsible for preserving.
func allText(s *Screen) []string {
	g := s.MainGrid()
	var out []string
	for i := 0; i < g.HistoryLen(); i++ {
		out = append(out, g.HistoryLine(i).Text())
	}
	for y := 0; y < g.Rows(); y++ {
		out = append(out, g.Line(y).Text())
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// joined is the text with wrapping undone, which is what must survive a
// change of width: where the breaks fall is exactly what reflow is allowed to
// change.
func joined(s *Screen) string {
	g := s.MainGrid()
	total := g.HistoryLen() + g.Rows()
	rowAt := func(i int) *Row {
		if i < g.HistoryLen() {
			return g.HistoryLine(i)
		}
		return g.Line(i - g.HistoryLen())
	}

	var b strings.Builder
	for i := 0; i < total; i++ {
		row := rowAt(i)
		// A wrapped row's trailing space is content, not padding, so its full
		// width is taken. Row.Text trims, which is right for detection and
		// wrong for putting a wrapped line back together.
		if row.Wrapped() {
			b.WriteString(rowFullText(row))
			continue
		}
		b.WriteString(row.Text())
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// rowFullText is a row including its trailing blanks.
func rowFullText(r *Row) string {
	var b strings.Builder
	for x := 0; x < r.Len(); x++ {
		cell := r.Cell(x)
		if cell.IsContinuation() {
			continue
		}
		if cell.R == 0 {
			b.WriteByte(' ')
		} else {
			b.WriteRune(cell.R)
		}
		for _, mark := range r.Combining(x) {
			b.WriteRune(mark)
		}
	}
	return b.String()
}

func TestReflowNarrowingRewraps(t *testing.T) {
	s := NewScreen(20, 6, 100)
	s.Write([]byte("abcdefghijklmnopqrstuvwxyz")) // wraps once at 20

	if got := visible(s); len(got) != 2 || got[0] != "abcdefghijklmnopqrst" {
		t.Fatalf("before = %q", got)
	}

	s.Resize(10, 6)
	got := visible(s)
	want := []string{"abcdefghij", "klmnopqrst", "uvwxyz"}
	if len(got) != len(want) {
		t.Fatalf("after = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReflowWideningRejoins(t *testing.T) {
	s := NewScreen(10, 6, 100)
	s.Write([]byte("abcdefghijklmnopqrstuvwxyz")) // three rows at 10

	s.Resize(30, 6)
	got := visible(s)
	if len(got) != 1 || got[0] != "abcdefghijklmnopqrstuvwxyz" {
		t.Errorf("after widening = %q, want one joined row", got)
	}
}

// TestReflowRoundTrip is the property that matters: where the breaks fall is
// the only thing reflow may change.
func TestReflowRoundTrip(t *testing.T) {
	inputs := []string{
		"short",
		strings.Repeat("x", 250),
		"one\r\ntwo\r\nthree",
		"a long first line that will certainly wrap\r\nshort\r\n" + strings.Repeat("y", 90),
		"\x1b[31mcoloured and quite long so that it wraps around the edge\x1b[0m",
	}
	widths := []int{80, 20, 7, 120, 13, 40}

	for _, input := range inputs {
		s := NewScreen(80, 10, 500)
		s.Write([]byte(input))
		want := joined(s)

		for _, w := range widths {
			s.Resize(w, 10)
			if got := joined(s); got != want {
				t.Fatalf("input %q at width %d:\n got %q\nwant %q", trunc(input), w, got, want)
			}
		}
	}
}

func trunc(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

// TestReflowKeepsExplicitNewlines: a line that ended because the program wrote
// a newline must never be joined to the next, however much room appears.
func TestReflowKeepsExplicitNewlines(t *testing.T) {
	s := NewScreen(10, 6, 100)
	s.Write([]byte("one\r\ntwo\r\nthree"))

	s.Resize(60, 6)
	got := visible(s)
	want := []string{"one", "two", "three"}
	if len(got) != len(want) {
		t.Fatalf("after = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestReflowMovesTheCursorWithItsCharacter: the cursor's row and column both
// change, but the character it sits on does not.
func TestReflowMovesTheCursorWithItsCharacter(t *testing.T) {
	s := NewScreen(20, 6, 100)
	s.Write([]byte("abcdefghijklmnopqrstuvwxyz"))

	// The cursor is after 'z', on row 1 column 6.
	if c := s.Cursor(); c.X != 6 || c.Y != 1 {
		t.Fatalf("before = (%d,%d), want (6,1)", c.X, c.Y)
	}

	s.Resize(10, 6)
	// 26 characters at 10 wide puts the cursor after 'z' on row 2 column 6.
	if c := s.Cursor(); c.X != 6 || c.Y != 2 {
		t.Errorf("after = (%d,%d), want (6,2)", c.X, c.Y)
	}

	// Typing continues where the cursor is, which is the real test of it.
	s.Write([]byte("!"))
	if got := visible(s); got[2] != "uvwxyz!" {
		t.Errorf("row 2 = %q, want %q", got[2], "uvwxyz!")
	}
}

func TestReflowCursorStaysOnScreen(t *testing.T) {
	s := NewScreen(40, 5, 200)
	s.Write([]byte(strings.Repeat("filler line\r\n", 20) + "prompt> "))

	s.Resize(12, 5)
	c := s.Cursor()
	if c.Y < 0 || c.Y >= 5 {
		t.Errorf("cursor row = %d, outside a 5 row screen", c.Y)
	}
	s.Write([]byte("typed"))
	// The text may wrap at this width, so the check is against the unwrapped
	// content rather than against any one row.
	if !strings.Contains(joined(s), "prompt> typed") {
		t.Errorf("typing after a reflow did not land at the cursor:\n%q", visible(s))
	}
}

// TestReflowKeepsHistory: narrowing produces more rows than before, and the
// ones that no longer fit belong in the scrollback rather than nowhere.
func TestReflowKeepsHistory(t *testing.T) {
	s := NewScreen(40, 4, 200)
	s.Write([]byte("alpha\r\nbravo\r\ncharlie\r\ndelta\r\necho"))

	before := allText(s)
	s.Resize(10, 4)
	after := allText(s)

	if len(after) != len(before) {
		t.Fatalf("history changed length: %q then %q", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("line %d = %q, want %q", i, after[i], before[i])
		}
	}
}

func TestReflowPreservesStyles(t *testing.T) {
	s := NewScreen(20, 4, 100)
	s.Write([]byte("\x1b[31m" + strings.Repeat("r", 25) + "\x1b[0m"))

	s.Resize(10, 4)
	// Every 'r' keeps its colour wherever it landed.
	for y := 0; y < 3; y++ {
		row := s.Grid().Line(y)
		for x := 0; x < row.Len(); x++ {
			cell := row.Cell(x)
			if cell.R != 'r' {
				continue
			}
			if cell.Style.FG != IndexedColor(1) {
				t.Fatalf("cell (%d,%d) lost its colour", x, y)
			}
		}
	}
}

// TestReflowDoesNotSplitWideCharacters: half of a double-width character has
// nowhere to live, so it moves whole to the next row.
func TestReflowDoesNotSplitWideCharacters(t *testing.T) {
	s := NewScreen(20, 4, 100)
	s.Write([]byte(strings.Repeat("世", 12))) // 24 columns of content

	s.Resize(7, 4) // an odd width, so a naive split would cut one in half
	for y := 0; y < s.Grid().Rows(); y++ {
		row := s.Grid().Line(y)
		for x := 0; x < row.Len(); x++ {
			cell := row.Cell(x)
			if cell.Width == 2 && x+1 >= row.Len() {
				t.Fatalf("a wide character was left hanging at (%d,%d)", x, y)
			}
			if cell.IsContinuation() && x == 0 {
				t.Fatalf("row %d starts with an orphaned continuation cell", y)
			}
		}
	}
	if got := joined(s); !strings.Contains(got, strings.Repeat("世", 12)) {
		t.Errorf("content = %q, want the wide characters intact", got)
	}
}

func TestReflowKeepsCombiningMarks(t *testing.T) {
	s := NewScreen(12, 4, 100)
	s.Write([]byte(strings.Repeat("e\u0301", 10)))

	s.Resize(5, 4)
	if got := joined(s); !strings.Contains(got, "e\u0301e\u0301") {
		t.Errorf("content = %q, want the combining marks kept with their letters", got)
	}
}

// TestReflowLeavesTheAlternateScreenAlone: an application there owns every
// cell and redraws on the resize that is about to reach it, so rewrapping
// would fight a redraw that is already coming.
func TestReflowLeavesTheAlternateScreenAlone(t *testing.T) {
	s := NewScreen(20, 4, 100)
	s.Write([]byte("\x1b[?1049h\x1b[H" + strings.Repeat("z", 30)))

	s.Resize(10, 4)
	if !s.IsAlt() {
		t.Fatal("expected to still be on the alternate screen")
	}

	// Row by row, truncating and rewrapping can look identical. Counting the
	// characters tells them apart: rewrapping keeps all thirty, truncating
	// drops the ten that no longer fit on the first row.
	count := 0
	for y := 0; y < s.Grid().Rows(); y++ {
		count += strings.Count(s.Grid().Line(y).Text(), "z")
	}
	if count != 20 {
		t.Errorf("the alternate screen holds %d characters, want 20 — it was rewrapped", count)
	}
}

func TestReflowOnAnEmptyScreen(t *testing.T) {
	s := NewScreen(20, 5, 50)
	s.Resize(8, 5)
	if got := visible(s); len(got) != 0 {
		t.Errorf("an empty screen reflowed to %q", got)
	}
	if c := s.Cursor(); c.X != 0 || c.Y != 0 {
		t.Errorf("cursor = (%d,%d), want the origin", c.X, c.Y)
	}
}

func TestReflowToOneColumn(t *testing.T) {
	// A degenerate width must terminate and keep the text, however ugly.
	s := NewScreen(20, 6, 100)
	s.Write([]byte("hello"))
	s.Resize(1, 6)

	if got := joined(s); !strings.Contains(got, "h") {
		t.Errorf("content = %q, want the text kept", got)
	}
}

// TestReflowWideCharacterWiderThanTheScreen must terminate rather than loop
// forever trying to place something that cannot fit.
func TestReflowWideCharacterWiderThanTheScreen(t *testing.T) {
	s := NewScreen(20, 4, 50)
	s.Write([]byte("世界"))

	done := make(chan struct{})
	go func() {
		s.Resize(1, 4)
		close(done)
	}()
	select {
	case <-done:
	case <-timeoutAfterSeconds(5):
		t.Fatal("reflow did not terminate at a width smaller than a character")
	}
}

func BenchmarkReflow(b *testing.B) {
	content := strings.Repeat("a terminal line of moderate length that wraps sometimes\r\n", 200)
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		s := NewScreen(120, 40, 2000)
		s.Write([]byte(content))
		b.StartTimer()
		s.Resize(80, 40)
	}
}

// timeoutAfterSeconds is a small helper so the loop-termination test reads as
// what it is checking rather than as timer plumbing.
func timeoutAfterSeconds(n int) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		<-time.After(time.Duration(n) * time.Second)
		close(ch)
	}()
	return ch
}
