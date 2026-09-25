package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// The release notes panel is herdr's (`ui/release_notes.rs`,
// `client/shell/overlays.rs`): a panel of at most 80 by 24 in the middle of
// the screen, the version and what the notes are — "update ready" for a
// release newer than the one running, "what's new in this release" for the
// one running — at its top with a close button, and the notes below as
// herdr draws markdown: a ### heading in capitals in the accent, a list item
// behind an accent bullet, `code` in the accent on its own surface, and a
// fenced block behind a bar in the gutter. It scrolls when the notes are
// longer than it, with a bar at the right edge saying where.

// ReleaseNotesView is the panel while it is up.
type ReleaseNotesView struct {
	Version string
	Body    string
	// Newer is "update ready": a release newer than the build running, and
	// Install is how to install it, said at the top of the body.
	Newer   bool
	Install string
	// Scroll is the first body line shown; the drawing clamps it.
	Scroll int
}

// notesMax is herdr's panel size.
const (
	notesMaxCols = 80
	notesMaxRows = 24
)

// NotesGeometry is where the panel and its parts are, the one description
// both drawing and a click read.
type NotesGeometry struct {
	Box Rect
	// Close is the " esc close " button; Update, beside it, " u update now ",
	// is there while the notes are of a release newer than the one running.
	// Body is where the notes scroll.
	Close  Rect
	Update Rect
	Body   Rect
}

// ReleaseNotesLayout is the panel's geometry on a screen of cols by rows.
// ok is false when there is too little room for more than the frame, as in
// herdr, which then draws the frame alone.
func ReleaseNotesLayout(cols, rows int) (NotesGeometry, bool) {
	w, h := min(notesMaxCols, cols), min(notesMaxRows, rows)
	box := Rect{X: (cols - w) / 2, Y: (rows - h) / 2, Cols: w, Rows: h}
	g := NotesGeometry{Box: box}
	if w-2 < 20 || h-2 < 8 {
		return g, false
	}
	const button = " esc close "
	bw := runewidth.StringWidth(button)
	g.Close = Rect{X: box.X + box.Cols - 2 - bw, Y: box.Y + 1, Cols: bw, Rows: 1}
	const update = " u update now "
	uw := runewidth.StringWidth(update)
	g.Update = Rect{X: g.Close.X - 1 - uw, Y: box.Y + 1, Cols: uw, Rows: 1}
	// Two header rows and a blank, then the body; the footer and a blank
	// above it at the bottom.
	g.Body = Rect{X: box.X + 1, Y: box.Y + 4, Cols: box.Cols - 3, Rows: box.Rows - 7}
	return g, true
}

// notesSpan is a run of text in one style.
type notesSpan struct {
	text  string
	style vt.Style
}

// notesLine is one line of the body, with the style the rest of its row
// takes (a fenced block's surface reaches the edge).
type notesLine struct {
	spans []notesSpan
	fill  vt.Style
}

// ReleaseNotesLines is how many body lines the notes make at a width, for
// scrolling to stop at the end.
func ReleaseNotesLines(v *ReleaseNotesView, width int, theme Theme) int {
	return len(notesBody(v, width, theme))
}

// notesBody lays the notes out, herdr's release_note_lines: the "update
// ready" preamble, then each markdown line, wrapped to the width.
func notesBody(v *ReleaseNotesView, width int, theme Theme) []notesLine {
	var out []notesLine
	if v.Newer {
		out = append(out,
			notesLine{spans: []notesSpan{{" ", theme.Notes}, {"●", theme.NotesAccent}, {" update ready", withBold(theme.Notes)}}, fill: theme.Notes},
		)
		// How to install it wraps as the notes do: it is a sentence, and
		// cut at the panel's edge it lost the command it names.
		out = append(out, wrapNotes(notesLine{spans: append([]notesSpan{{" ", theme.Notes}}, inlineCode(v.Install, theme)...), fill: theme.Notes}, width)...)
		out = append(out, notesLine{fill: theme.Notes})
	}
	return append(out, markdownLines(v.Body, width, theme)...)
}

// markdownLines is markdown as the notes draw it, wrapped to a width: the
// release notes' body, and an issue's text and comments.
func markdownLines(body string, width int, theme Theme) []notesLine {
	var out []notesLine
	fenced := false
	for _, raw := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(raw)
		var line notesLine
		switch {
		case strings.HasPrefix(trimmed, "```"):
			fenced = !fenced
			continue
		case fenced:
			line = notesLine{spans: []notesSpan{{"▏ ", theme.NotesAccent}, {raw, theme.NotesFence}}, fill: theme.NotesFence}
		case strings.HasPrefix(trimmed, "#"):
			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			line = notesLine{spans: []notesSpan{{" " + strings.ToUpper(heading), theme.NotesAccent}}, fill: theme.Notes}
		case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "):
			line = notesLine{spans: append([]notesSpan{{" ", theme.Notes}, {"•", theme.NotesAccent}, {" ", theme.Notes}},
				inlineCode(trimmed[2:], theme)...), fill: theme.Notes}
		default:
			line = notesLine{spans: append([]notesSpan{{" ", theme.Notes}}, inlineCode(raw, theme)...), fill: theme.Notes}
		}
		out = append(out, wrapNotes(line, width)...)
	}
	return out
}

// inlineCode splits text on backticks, the code in its own style without
// them, and outside the code on **, what is between in bold. herdr draws
// ** as it is; tend's release notes are the GitHub release's text, which
// uses it, so it is drawn rather than shown.
func inlineCode(text string, theme Theme) []notesSpan {
	var spans []notesSpan
	for i, part := range strings.Split(text, "`") {
		if part == "" {
			continue
		}
		if i%2 == 1 {
			spans = append(spans, notesSpan{part, theme.NotesCode})
			continue
		}
		for j, run := range strings.Split(part, "**") {
			if run == "" {
				continue
			}
			style := theme.Notes
			if j%2 == 1 {
				style = withBold(style)
			}
			spans = append(spans, notesSpan{run, style})
		}
	}
	return spans
}

func withBold(s vt.Style) vt.Style {
	s.Attrs |= vt.AttrBold
	return s
}

// wrapNotes breaks a line at the width, at a space when there is one, the
// rest going on under the text's first column.
func wrapNotes(line notesLine, width int) []notesLine {
	if width <= 4 {
		return []notesLine{line}
	}
	var out []notesLine
	cur := notesLine{fill: line.fill}
	used := 0
	for _, sp := range line.spans {
		for _, word := range splitKeepSpaces(sp.text) {
			w := runewidth.StringWidth(word)
			if used+w > width && used > 0 && strings.TrimSpace(word) != "" {
				out = append(out, cur)
				cur = notesLine{fill: line.fill, spans: []notesSpan{{"   ", line.fill}}}
				used = 3
			}
			if used == 3 && len(out) > 0 && strings.TrimSpace(word) == "" && len(cur.spans) == 1 {
				continue // no space at the start of a continued line
			}
			cur.spans = append(cur.spans, notesSpan{word, sp.style})
			used += w
		}
	}
	return append(out, cur)
}

// splitKeepSpaces splits text into words and the spaces between them.
func splitKeepSpaces(text string) []string {
	var out []string
	start := 0
	for i := 1; i <= len(text); i++ {
		if i == len(text) || (text[i] == ' ') != (text[i-1] == ' ') {
			out = append(out, text[start:i])
			start = i
		}
	}
	return out
}

// drawReleaseNotes paints the panel.
func drawReleaseNotes(dst *vt.Grid, v *ReleaseNotesView, theme Theme) {
	g, ok := ReleaseNotesLayout(dst.Cols(), dst.Rows())
	box := g.Box
	for y := box.Y; y < box.Y+box.Rows; y++ {
		for x := box.X; x < box.X+box.Cols; x++ {
			setCell(dst, x, y, ' ', theme.Notes)
		}
	}
	drawBox(dst, box, theme.NotesAccent)
	drawCloseMark(dst, box, withBold(theme.NotesAccent))
	if !ok {
		return
	}
	right := box.X + box.Cols - 1
	writeString(dst, box.X+2, box.Y+1, truncate("v"+strings.TrimPrefix(v.Version, "v"), g.Update.X-box.X-3), withBold(theme.Notes), g.Update.X)
	subtitle := "what's new in this release"
	if v.Newer {
		subtitle = "update ready"
	}
	writeString(dst, box.X+2, box.Y+2, truncate(subtitle, box.Cols-4), theme.NotesSub, right)
	writeString(dst, g.Close.X, g.Close.Y, " esc close ", theme.NotesButton, right)
	if v.Newer {
		// tend's own: herdr says how to update; this does it, asked.
		writeString(dst, g.Update.X, g.Update.Y, " u update now ", theme.NotesButton, right)
	}

	lines := notesBody(v, g.Body.Cols, theme)
	top := clampNotesScroll(v.Scroll, len(lines), g.Body.Rows)
	for i := 0; i < g.Body.Rows && top+i < len(lines); i++ {
		y := g.Body.Y + i
		line := lines[top+i]
		for x := g.Body.X; x < g.Body.X+g.Body.Cols; x++ {
			setCell(dst, x, y, ' ', line.fill)
		}
		x := g.Body.X
		for _, sp := range line.spans {
			x = writeString(dst, x, y, sp.text, sp.style, g.Body.X+g.Body.Cols)
		}
	}
	if len(lines) > g.Body.Rows {
		// The bar: the track in overlay0's dimness, the thumb over it.
		thumb := max(g.Body.Rows*g.Body.Rows/len(lines), 1)
		at := (g.Body.Rows - thumb) * top / max(len(lines)-g.Body.Rows, 1)
		for i := 0; i < g.Body.Rows; i++ {
			style := theme.NotesSub
			r := '│'
			if i >= at && i < at+thumb {
				r = '▐'
				style = theme.NotesAccent
			}
			setCell(dst, g.Body.X+g.Body.Cols, g.Body.Y+i, r, style)
		}
	}

	footer := []notesSpan{{" scroll ", theme.NotesSub}, {"wheel ↑↓", theme.NotesAccent}, {"  ·  ", theme.NotesSub},
		{"close", theme.NotesSub}, {" esc / enter ", theme.NotesAccent}}
	x := box.X + 1
	for _, sp := range footer {
		x = writeString(dst, x, box.Y+box.Rows-2, sp.text, sp.style, right)
	}
}

// clampNotesScroll keeps the first line shown within the notes.
func clampNotesScroll(scroll, lines, rows int) int {
	return max(min(scroll, lines-rows), 0)
}

// ClampNotesScroll is the scroll the panel will show, for the client to
// keep its own in step (so scrolling back up from past the end is at once).
func ClampNotesScroll(v *ReleaseNotesView, cols, rows int, theme Theme) int {
	g, ok := ReleaseNotesLayout(cols, rows)
	if !ok {
		return 0
	}
	return clampNotesScroll(v.Scroll, ReleaseNotesLines(v, g.Body.Cols, theme), g.Body.Rows)
}
