package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/sousaakira/tend/internal/vt"
)

// The issues panel is tend's own, after Orca's task page: the GitHub issues
// of the project being worked in, a preset and a search over them, and one
// issue whole — its text and its thread — in the same panel. It is drawn as
// the sessions list is. What the keys do, and asking GitHub, is the
// client's; this draws and says where a click landed.

// IssueEntry is one issue in the list.
type IssueEntry struct {
	Number    int
	Title     string
	State     string
	Author    string
	Labels    []string
	Assignees []string
	Comments  int
	Updated   time.Time
	URL       string
}

// IssueComment is one comment in an issue's thread.
type IssueComment struct {
	Author  string
	Body    string
	Created time.Time
}

// IssueDetailView is one issue, over the list.
type IssueDetailView struct {
	IssueEntry
	Body    string
	Created time.Time
	Thread  []IssueComment
	Loading bool
	Scroll  int
}

// IssuesView is the panel while it is up.
type IssuesView struct {
	// Repo is owner/name, Remote the remote it came through, Remotes all
	// the GitHub ones the project has.
	Repo    string
	Remote  string
	Remotes []string
	// Filters are the presets' names and Filter the one in use.
	Filters []string
	Filter  int
	Query   string
	Issues  []IssueEntry
	Total   int
	Cursor  int
	Scroll  int
	Loading bool
	// Error is what stopped the list — no gh, no login, no remote — and
	// Message a line of what happened.
	Error   string
	Message string
	// Dir is the directory the repository was found from, said under the
	// list while nothing else is.
	Dir string
	Now time.Time
	// Detail is the issue open over the list, nil for the list.
	Detail *IssueDetailView
	// Compose is a comment or a new issue being written, nil when none is.
	Compose *IssueCompose
	// Confirm is a question waiting for its answer: "close" asks for the
	// reason an issue is closed, "reopen" whether to open it again.
	Confirm string
}

// IssueCompose is what is being written to GitHub.
type IssueCompose struct {
	// NewIssue is a new issue, with a title; otherwise a comment on the
	// issue open.
	NewIssue bool
	Title    string
	Body     string
	// InBody is where typing goes: the title or the text.
	InBody  bool
	Sending bool
}

// Buttons, by what a click on one does.
const (
	IssueButtonOpen    = "open"
	IssueButtonBack    = "back"
	IssueButtonStart   = "start"
	IssueButtonNew     = "new"
	IssueButtonComment = "comment"
	IssueButtonState   = "state"
	IssueButtonBrowser = "browser"
	IssueButtonClose   = "close"
)

// IssueButton is one button on the bottom line.
type IssueButton struct {
	ID    string
	Label string
	Rect
}

// issueButtons are the buttons the panel shows as it is: the list's, or the
// open issue's, and Close at the right of both.
func issueButtons(v *IssuesView) []IssueButton {
	if v.Detail != nil {
		state := "[ Close issue ]"
		if v.Detail.State == "closed" {
			state = "[ Reopen ]"
		}
		return []IssueButton{
			{ID: IssueButtonBack, Label: "[ Back ]"},
			{ID: IssueButtonStart, Label: "[ Start work ]"},
			{ID: IssueButtonComment, Label: "[ Comment ]"},
			{ID: IssueButtonState, Label: state},
			{ID: IssueButtonBrowser, Label: "[ In browser ]"},
			{ID: IssueButtonClose, Label: "[ Close ]"},
		}
	}
	return []IssueButton{
		{ID: IssueButtonOpen, Label: "[ Open ]"},
		{ID: IssueButtonStart, Label: "[ Start work ]"},
		{ID: IssueButtonNew, Label: "[ New issue ]"},
		{ID: IssueButtonBrowser, Label: "[ In browser ]"},
		{ID: IssueButtonClose, Label: "[ Close ]"},
	}
}

// IssueButtonAt is the button under a point, by its ID.
func IssueButtonAt(v *IssuesView, cols, rows, x, y int) (string, bool) {
	for _, b := range IssuesLayout(v, cols, rows).Buttons {
		if y == b.Y && x >= b.X && x < b.X+b.Cols {
			return b.ID, true
		}
	}
	return "", false
}

// composeRows is how many lines what is being written takes, over the
// hint: a rule, the title for a new issue, the text, and a blank.
const composeRows = 8

// IssuesGeometry is where the panel's parts are.
type IssuesGeometry struct {
	Box Rect
	// Filters are the presets' chips; Search the line typed into; List
	// the issues, one a line, under a line of column names.
	Filters []Rect
	Search  Rect
	List    Rect
	// Body is where an issue's text scrolls, in the detail.
	Body Rect
	// Compose is where what is being written goes, when it is.
	Compose Rect
	// Buttons are on the bottom line.
	Buttons []IssueButton
}

// IssuesLayout is the panel's geometry on a screen of cols by rows, for a
// view with these preset names.
func IssuesLayout(v *IssuesView, cols, rows int) IssuesGeometry {
	w, h := max(min(sessionsCols, cols-2), 0), max(min(sessionsRows, rows-2), 0)
	box := Rect{X: (cols - w) / 2, Y: (rows - h) / 2, Cols: w, Rows: h}
	g := IssuesGeometry{Box: box}
	x := box.X + 2
	for _, name := range v.Filters {
		label := " " + name + " "
		r := Rect{X: x, Y: box.Y + 2, Cols: runewidth.StringWidth(label), Rows: 1}
		g.Filters = append(g.Filters, r)
		x += r.Cols + 1
	}
	g.Search = Rect{X: box.X + 2, Y: box.Y + 3, Cols: max(box.Cols-4, 0), Rows: 1}
	// The column names at Y+5, the issues under them; a blank, a message,
	// a hint and the buttons at the bottom.
	g.List = Rect{X: box.X + 2, Y: box.Y + 6, Cols: max(box.Cols-4, 0), Rows: max(box.Rows-11, 0)}
	// The detail's header takes three lines and a blank.
	g.Body = Rect{X: box.X + 2, Y: box.Y + 6, Cols: max(box.Cols-5, 0), Rows: max(box.Rows-11, 0)}
	if v.Compose != nil {
		// What is being written takes the bottom of the list or the text,
		// which is still there over it to be read while writing.
		n := min(composeRows, max(g.List.Rows-3, 0))
		g.List.Rows -= n
		g.Body.Rows -= n
		g.Compose = Rect{X: box.X + 2, Y: g.List.Y + g.List.Rows, Cols: max(box.Cols-4, 0), Rows: n}
	}
	bottom := box.Y + box.Rows - 2
	bx := box.X + 2
	for _, b := range issueButtons(v) {
		b.Rect = Rect{X: bx, Y: bottom, Cols: runewidth.StringWidth(b.Label), Rows: 1}
		if b.ID == IssueButtonClose {
			b.X = box.X + box.Cols - 2 - b.Cols
		} else {
			bx += b.Cols + 1
		}
		g.Buttons = append(g.Buttons, b)
	}
	return g
}

// IssueAt is the issue on a line of the list, by its place in Issues.
func IssueAt(v *IssuesView, cols, rows, x, y int) (int, bool) {
	g := IssuesLayout(v, cols, rows)
	if v.Detail != nil || x < g.List.X || x >= g.List.X+g.List.Cols || y < g.List.Y || y >= g.List.Y+g.List.Rows {
		return 0, false
	}
	i := clampIssuesScroll(v, g) + y - g.List.Y
	return i, i < len(v.Issues)
}

// IssueFilterAt is the preset chip under a point.
func IssueFilterAt(v *IssuesView, cols, rows, x, y int) (int, bool) {
	for i, r := range IssuesLayout(v, cols, rows).Filters {
		if y == r.Y && x >= r.X && x < r.X+r.Cols {
			return i, true
		}
	}
	return 0, false
}

// IssuesScrollFor is the scroll that keeps the cursor in view.
func IssuesScrollFor(v *IssuesView, cols, rows int) int {
	g := IssuesLayout(v, cols, rows)
	top := clampIssuesScroll(v, g)
	if v.Cursor < top {
		top = v.Cursor
	}
	if g.List.Rows > 0 && v.Cursor >= top+g.List.Rows {
		top = v.Cursor - g.List.Rows + 1
	}
	return max(top, 0)
}

func clampIssuesScroll(v *IssuesView, g IssuesGeometry) int {
	return max(min(v.Scroll, len(v.Issues)-g.List.Rows), 0)
}

// ClampIssueScroll is the detail's scroll as it will be drawn, for the
// client to keep its own in step.
func ClampIssueScroll(v *IssuesView, cols, rows int, theme Theme) int {
	if v.Detail == nil {
		return 0
	}
	g := IssuesLayout(v, cols, rows)
	return max(min(v.Detail.Scroll, len(issueBody(v, g.Body.Cols, theme))-g.Body.Rows), 0)
}

// issueBody is the detail's scrolling part: the issue's text, then each
// comment under a rule with who wrote it and when.
func issueBody(v *IssuesView, width int, theme Theme) []notesLine {
	d := v.Detail
	var out []notesLine
	body := strings.TrimSpace(d.Body)
	if body == "" {
		out = append(out, notesLine{spans: []notesSpan{{" no description", theme.NotesSub}}, fill: theme.Notes})
	} else {
		out = append(out, markdownLines(body, width, theme)...)
	}
	for _, c := range d.Thread {
		head := fmt.Sprintf(" %s · %s ", c.Author, SessionAge(v.Now, c.Created))
		rule := strings.Repeat("─", max(width-runewidth.StringWidth(head)-2, 0))
		out = append(out, notesLine{fill: theme.Notes},
			notesLine{spans: []notesSpan{{"──", theme.NotesSub}, {head, withBold(theme.NotesAccent)}, {rule, theme.NotesSub}}, fill: theme.Notes})
		out = append(out, markdownLines(strings.TrimSpace(c.Body), width, theme)...)
	}
	return out
}

func drawIssues(dst *vt.Grid, v *IssuesView, theme Theme) {
	g := IssuesLayout(v, dst.Cols(), dst.Rows())
	box := g.Box
	for y := box.Y; y < box.Y+box.Rows; y++ {
		for x := box.X; x < box.X+box.Cols; x++ {
			setCell(dst, x, y, ' ', theme.Notes)
		}
	}
	drawBox(dst, box, theme.NotesAccent)
	if box.Rows < 14 || box.Cols < 50 {
		return
	}
	right := box.X + box.Cols - 1
	title := "GITHUB ISSUES"
	if v.Repo != "" {
		title += " · " + v.Repo
		if len(v.Remotes) > 1 {
			title += " (" + v.Remote + ")"
		}
	}
	writeString(dst, box.X+2, box.Y+1, truncate(title, box.Cols-24), withBold(theme.NotesAccent), right)
	if v.Detail != nil {
		drawIssueDetail(dst, v, g, theme)
		return
	}
	if v.Total > 0 || len(v.Issues) > 0 {
		count := fmt.Sprintf("%d of %d", len(v.Issues), v.Total)
		writeString(dst, right-1-runewidth.StringWidth(count), box.Y+1, count, theme.NotesSub, right)
	}
	for i, r := range g.Filters {
		style := theme.NotesSub
		if i == v.Filter {
			style = theme.NotesButton
		}
		writeString(dst, r.X, r.Y, " "+v.Filters[i]+" ", style, right)
	}
	end := g.Search.X + g.Search.Cols
	x := writeString(dst, g.Search.X, g.Search.Y, "search ", theme.NotesSub, end)
	if v.Query == "" {
		setCell(dst, x, g.Search.Y, ' ', theme.NotesButton)
		writeString(dst, x+2, g.Search.Y, "words, or GitHub's own: label:bug author:someone", theme.NotesSub, end)
	} else {
		x = writeString(dst, x, g.Search.Y, truncateLeft(v.Query, g.Search.Cols-9), withBold(theme.Notes), end)
		setCell(dst, x, g.Search.Y, ' ', theme.NotesButton)
	}

	// The columns: number, title, labels, author, age, comments.
	listEnd := g.List.X + g.List.Cols
	const numW, authorW, ageW, cmtW = 7, 14, 5, 4
	labelW := min(24, g.List.Cols/5)
	titleW := max(g.List.Cols-numW-labelW-authorW-ageW-cmtW-4, 10)
	cols := []int{g.List.X, g.List.X + numW}
	cols = append(cols, cols[1]+titleW+1)
	cols = append(cols, cols[2]+labelW+1)
	cols = append(cols, cols[3]+authorW+1)
	head := g.List.Y - 1
	for i, name := range []string{"#", "title", "labels", "author", "age"} {
		writeString(dst, cols[i], head, name, theme.NotesSub, listEnd)
	}
	writeString(dst, listEnd-2, head, "💬", theme.NotesSub, listEnd)

	switch {
	case v.Error != "":
		writeString(dst, g.List.X, g.List.Y, truncate(v.Error, g.List.Cols), withBold(theme.Notes), listEnd)
	case v.Loading && len(v.Issues) == 0:
		writeString(dst, g.List.X, g.List.Y, "asking GitHub…", theme.NotesSub, listEnd)
	case len(v.Issues) == 0:
		writeString(dst, g.List.X, g.List.Y, "no issues match", theme.NotesSub, listEnd)
	}
	top := clampIssuesScroll(v, g)
	for line := 0; line < g.List.Rows && top+line < len(v.Issues) && v.Error == ""; line++ {
		i := top + line
		e := v.Issues[i]
		y := g.List.Y + line
		base, sub, accent := theme.Notes, theme.NotesSub, theme.NotesAccent
		if i == v.Cursor {
			base = theme.NotesButton
			sub, accent = base, base
			for x := g.List.X; x < listEnd; x++ {
				setCell(dst, x, y, ' ', base)
			}
		}
		numStyle := accent
		if e.State == "closed" {
			numStyle = sub
		}
		writeString(dst, cols[0], y, fmt.Sprintf("%d", e.Number), numStyle, cols[1]-1)
		writeString(dst, cols[1], y, truncate(e.Title, titleW), base, cols[2]-1)
		writeString(dst, cols[2], y, truncate(strings.Join(e.Labels, ", "), labelW), sub, cols[3]-1)
		writeString(dst, cols[3], y, truncate(e.Author, authorW), sub, cols[4]-1)
		writeString(dst, cols[4], y, SessionAge(v.Now, e.Updated), sub, listEnd)
		if e.Comments > 0 {
			n := fmt.Sprintf("%d", e.Comments)
			writeString(dst, listEnd-runewidth.StringWidth(n), y, n, sub, listEnd)
		}
	}

	msg := v.Message
	if msg == "" {
		msg = v.Dir
	}
	if v.Loading && len(v.Issues) > 0 {
		msg = "asking GitHub…"
	}
	hint := "type to search · tab next filter · ↑↓ move · enter open · ctrl+n new issue · ctrl+o in browser · ctrl+r reload · esc"
	if len(v.Remotes) > 1 {
		hint += " · ctrl+t " + strings.Join(v.Remotes, "/")
	}
	drawIssuesFoot(dst, v, g, msg, hint, theme)
}

// drawIssuesFoot is what both views share at the bottom: what is being
// written, the message, the hint — or the question waiting — and the
// buttons.
func drawIssuesFoot(dst *vt.Grid, v *IssuesView, g IssuesGeometry, msg, hint string, theme Theme) {
	box := g.Box
	right := box.X + box.Cols - 1
	if v.Compose != nil {
		drawIssueCompose(dst, v, g, theme)
		hint = "enter sends · ctrl+j new line · esc cancels"
		if v.Compose.NewIssue {
			hint = "tab title/text · enter in the text files it · ctrl+j new line · esc cancels"
		}
	}
	switch v.Confirm {
	case "close":
		msg, hint = fmt.Sprintf("close #%d?", v.Detail.Number), "enter as completed · n as not planned · esc cancels"
	case "reopen":
		msg, hint = fmt.Sprintf("reopen #%d?", v.Detail.Number), "enter reopens · esc cancels"
	}
	msgY, hintY := box.Y+box.Rows-4, box.Y+box.Rows-3
	if msg != "" {
		style := theme.NotesSub
		if v.Confirm != "" {
			style = withBold(theme.Notes)
		}
		writeString(dst, box.X+2, msgY, truncate(msg, box.Cols-4), style, right)
	}
	writeString(dst, box.X+2, hintY, truncate(hint, box.Cols-4), theme.NotesSub, right)
	for _, b := range g.Buttons {
		writeString(dst, b.X, b.Y, b.Label, theme.NotesAccent, right)
	}
}

// drawIssueCompose draws what is being written, wrapped, with the cursor
// at the end of the field typing goes to.
func drawIssueCompose(dst *vt.Grid, v *IssuesView, g IssuesGeometry, theme Theme) {
	c, r := v.Compose, g.Compose
	if r.Rows < 3 {
		return
	}
	end := r.X + r.Cols
	for y := r.Y; y < r.Y+r.Rows; y++ {
		for x := r.X; x < end; x++ {
			setCell(dst, x, y, ' ', theme.Notes)
		}
	}
	head := " comment "
	if v.Detail != nil {
		head = fmt.Sprintf(" comment on #%d ", v.Detail.Number)
	}
	if c.NewIssue {
		head = " new issue in " + v.Repo + " "
	}
	if c.Sending {
		head += "· sending… "
	}
	writeString(dst, r.X, r.Y, "──"+head+strings.Repeat("─", max(r.Cols-runewidth.StringWidth(head)-2, 0)), theme.NotesAccent, end)
	y := r.Y + 1
	if c.NewIssue {
		style := theme.Notes
		if !c.InBody {
			style = withBold(theme.Notes)
		}
		x := writeString(dst, r.X, y, "title ", theme.NotesSub, end)
		x = writeString(dst, x, y, truncateLeft(c.Title, r.Cols-8), style, end)
		if !c.InBody {
			setCell(dst, x, y, ' ', theme.NotesButton)
		}
		y++
	}
	// The text, wrapped at the width; its last lines when it is longer
	// than the room, so the cursor stays in sight.
	var lines []string
	for _, para := range strings.Split(c.Body, "\n") {
		for {
			if runewidth.StringWidth(para) <= r.Cols-1 {
				lines = append(lines, para)
				break
			}
			cut := runewidth.Truncate(para, r.Cols-1, "")
			lines = append(lines, cut)
			para = para[len(cut):]
		}
	}
	room := r.Y + r.Rows - y
	if len(lines) > room {
		lines = lines[len(lines)-room:]
	}
	for i, line := range lines {
		x := writeString(dst, r.X, y+i, line, theme.Notes, end)
		if i == len(lines)-1 && (c.InBody || !c.NewIssue) {
			setCell(dst, x, y+i, ' ', theme.NotesButton)
		}
	}
	if c.Body == "" && (c.InBody || !c.NewIssue) {
		writeString(dst, r.X+2, y, "markdown works; it goes as written", theme.NotesSub, end)
	}
}

func drawIssueDetail(dst *vt.Grid, v *IssuesView, g IssuesGeometry, theme Theme) {
	d := v.Detail
	box := g.Box
	right := box.X + box.Cols - 1
	state := d.State
	stateStyle := withBold(theme.NotesAccent)
	if state == "closed" {
		stateStyle = withBold(theme.NotesSub)
	}
	x := writeString(dst, box.X+2, box.Y+2, fmt.Sprintf("#%d ", d.Number), withBold(theme.NotesAccent), right)
	x = writeString(dst, x, box.Y+2, state, stateStyle, right)
	meta := ""
	if d.Author != "" {
		meta = " · opened by " + d.Author
		if !d.Created.IsZero() {
			meta += " " + SessionAge(v.Now, d.Created) + " ago"
		}
	}
	if len(d.Assignees) > 0 {
		meta += " · assigned to " + strings.Join(d.Assignees, ", ")
	}
	writeString(dst, x, box.Y+2, truncate(meta, right-x-1), theme.NotesSub, right)
	writeString(dst, box.X+2, box.Y+3, truncate(d.Title, box.Cols-4), withBold(theme.Notes), right)
	if len(d.Labels) > 0 {
		writeString(dst, box.X+2, box.Y+4, truncate(strings.Join(d.Labels, " · "), box.Cols-4), theme.NotesAccent, right)
	}

	if d.Loading {
		writeString(dst, g.Body.X, g.Body.Y, "reading the issue…", theme.NotesSub, g.Body.X+g.Body.Cols)
	} else {
		lines := issueBody(v, g.Body.Cols, theme)
		top := max(min(d.Scroll, len(lines)-g.Body.Rows), 0)
		for i := 0; i < g.Body.Rows && top+i < len(lines); i++ {
			y := g.Body.Y + i
			lx := g.Body.X
			for _, sp := range lines[top+i].spans {
				lx = writeString(dst, lx, y, sp.text, sp.style, g.Body.X+g.Body.Cols)
			}
		}
		if len(lines) > g.Body.Rows && g.Body.Rows > 0 {
			thumb := max(g.Body.Rows*g.Body.Rows/len(lines), 1)
			at := (g.Body.Rows - thumb) * top / max(len(lines)-g.Body.Rows, 1)
			for i := 0; i < g.Body.Rows; i++ {
				r, style := '│', theme.NotesSub
				if i >= at && i < at+thumb {
					r, style = '▐', theme.NotesAccent
				}
				setCell(dst, g.Body.X+g.Body.Cols, g.Body.Y+i, r, style)
			}
		}
	}

	toggle := "x close"
	if d.State == "closed" {
		toggle = "x reopen"
	}
	drawIssuesFoot(dst, v, g, v.Message, "↑↓ scroll · w start work · c comment · "+toggle+" · o in browser · esc back", theme)
}
