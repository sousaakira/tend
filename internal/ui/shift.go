package ui

import "github.com/sousaakira/tend/internal/vt"

// A program that keeps its own scrollback — an agent, a pager, an editor —
// does not tell anyone when it scrolls. It repaints its window, and the only
// evidence that the text moved is that the same lines are somewhere else.
//
// A selection has to follow that move or it marks whichever text happens to
// land under it, which is not what the user dragged over. So the move is
// worked out from the two pictures: if most of what was on screen is still on
// screen a fixed number of rows away, the view scrolled by that much.

// ShiftRows is the most the text is believed to have moved in one step.
//
// A whole-screen change is a repaint, not a scroll — a different view, a
// cleared screen, a redrawn dialog — and reading it as a scroll would drag the
// selection somewhere arbitrary. Half a screen is more than any wheel notch
// produces and less than a new view.
const ShiftRows = 8

// minShiftMatches is how many lines must line up before a shift is believed.
// Two matching lines happen by accident in any output with repetition.
const minShiftMatches = 3

// DetectShift reports how far the text moved between two pictures of a screen,
// positive meaning down, and whether the answer is trustworthy.
//
// Only distinct, non-blank lines are counted. A screen of blank rows matches
// itself at every offset, and a screen of identical rows matches at several —
// neither says anything about where the text went.
func DetectShift(before, after []string) (int, bool) {
	if len(before) == 0 || len(before) != len(after) {
		return 0, false
	}

	bestShift, bestScore, runnerUp := 0, 0, 0
	for shift := -ShiftRows; shift <= ShiftRows; shift++ {
		if shift == 0 {
			continue
		}
		score := scoreShift(before, after, shift)
		switch {
		case score > bestScore:
			bestShift, bestScore, runnerUp = shift, score, bestScore
		case score > runnerUp:
			runnerUp = score
		}
	}

	if bestScore < minShiftMatches {
		return 0, false
	}
	// A tie is not an answer. Output with a repeating shape lines up equally
	// well at several offsets, and picking one of them would move the
	// selection somewhere the text did not go.
	if bestScore == runnerUp {
		return 0, false
	}
	return bestShift, true
}

// scoreShift counts the distinct lines that line up at one offset.
func scoreShift(before, after []string, shift int) int {
	seen := make(map[string]bool)
	score := 0
	for y := range after {
		from := y - shift
		if from < 0 || from >= len(before) {
			continue
		}
		line := after[y]
		if !meaningful(line) || line != before[from] || seen[line] {
			continue
		}
		seen[line] = true
		score++
	}
	return score
}

// meaningful reports whether a line says enough to be evidence. Blank lines
// and a rule of box-drawing match everywhere.
func meaningful(line string) bool {
	distinct := 0
	for _, r := range line {
		if r != ' ' && r != '─' && r != '│' && r != '┃' && r != '═' {
			distinct++
		}
	}
	return distinct >= 3
}

// ScreenLines reads a terminal's visible rows, which is what DetectShift
// compares.
func ScreenLines(screen *vt.Screen) []string {
	if screen == nil {
		return nil
	}
	grid := screen.Grid()
	out := make([]string, grid.Rows())
	for y := range out {
		if row := grid.Line(y); row != nil {
			out[y] = row.Text()
		}
	}
	return out
}
