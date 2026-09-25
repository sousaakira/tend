package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/sousaakira/tend/internal/vt"
)

// Pull requests in the issues panel, as Orca shows them beside issues: the
// list with each one's branch, checks and review, and one whole — what it
// changes, whether it merges, the checks that failed, its text and its
// thread of comments and reviews.

// PREntry is one pull request in a list.
type PREntry struct {
	Number int
	Title  string
	// State is open, closed or merged; Draft a draft among the open.
	State  string
	Draft  bool
	Author string
	Labels []string
	Head   string
	Base   string
	// Review is GitHub's decision: APPROVED, CHANGES_REQUESTED,
	// REVIEW_REQUIRED, or empty.
	Review string
	// Pass, Fail and Pending count its checks.
	Pass, Fail, Pending int
	Updated             time.Time
	URL                 string
}

// PRCheck is one check on a pull request: pass, fail, pending or skipped.
type PRCheck struct {
	Name, State string
}

// PRDetailView is one pull request, over the list.
type PRDetailView struct {
	PREntry
	Body      string
	Created   time.Time
	Additions int
	Deletions int
	Files     int
	Mergeable string
	Checks    []PRCheck
	Thread    []IssueComment
	Loading   bool
	Scroll    int
}

// Buttons of a pull request.
const (
	IssueButtonMerge = "merge"
	IssueButtonReady = "ready"
)

func prButtons(p *PRDetailView) []IssueButton {
	out := []IssueButton{{ID: IssueButtonBack, Label: "[ Back ]"}}
	if p.State == "open" {
		out = append(out, IssueButton{ID: IssueButtonMerge, Label: "[ Merge ]"})
		if p.Draft {
			out = append(out, IssueButton{ID: IssueButtonReady, Label: "[ Ready ]"})
		}
	}
	out = append(out, IssueButton{ID: IssueButtonComment, Label: "[ Comment ]"})
	switch p.State {
	case "open":
		out = append(out, IssueButton{ID: IssueButtonState, Label: "[ Close PR ]"})
	case "closed":
		out = append(out, IssueButton{ID: IssueButtonState, Label: "[ Reopen ]"})
	}
	return append(out,
		IssueButton{ID: IssueButtonBrowser, Label: "[ In browser ]"},
		IssueButton{ID: IssueButtonClose, Label: "[ Close ]"})
}

// prState is how a pull request's state reads: draft for an open draft.
func prState(p PREntry) string {
	if p.State == "open" && p.Draft {
		return "draft"
	}
	return p.State
}

// prChecks is its checks in a few columns: ✗ and how many failed, else ●
// and how many run, else ✓, else nothing when it has none.
func prChecks(p PREntry) string {
	switch {
	case p.Fail > 0:
		return fmt.Sprintf("✗%d", p.Fail)
	case p.Pending > 0:
		return fmt.Sprintf("●%d", p.Pending)
	case p.Pass > 0:
		return "✓"
	}
	return ""
}

// prReview is GitHub's review decision, short.
func prReview(p PREntry) string {
	switch p.Review {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes"
	case "REVIEW_REQUIRED":
		return "review"
	}
	return ""
}

// prLineSpans is a pull request on one line, as an issue lists its own.
func prLineSpans(p PREntry, theme Theme) []notesSpan {
	spans := []notesSpan{{fmt.Sprintf("#%d ", p.Number), withBold(theme.NotesAccent)}, {prState(p), theme.NotesSub}}
	if c := prChecks(p); c != "" {
		spans = append(spans, notesSpan{" " + c, theme.NotesSub})
	}
	if r := prReview(p); r != "" {
		spans = append(spans, notesSpan{" " + r, theme.NotesSub})
	}
	return append(spans, notesSpan{"  " + p.Title, theme.Notes}, notesSpan{"  " + p.Head, theme.NotesSub})
}

func drawPRList(dst *vt.Grid, v *IssuesView, g IssuesGeometry, theme Theme) {
	box := g.Box
	right := box.X + box.Cols - 1
	if len(v.PRs) > 0 {
		count := fmt.Sprintf("%d", len(v.PRs))
		writeString(dst, right-1-runewidth.StringWidth(count), box.Y+1, count, theme.NotesSub, right)
	}
	drawIssueSearchAndFilters(dst, v, g, theme)

	listEnd := g.List.X + g.List.Cols
	const numW, authorW, ageW, checkW, reviewW = 7, 14, 5, 6, 9
	branchW := min(28, g.List.Cols/5)
	titleW := max(g.List.Cols-numW-branchW-authorW-ageW-checkW-reviewW-6, 10)
	cols := []int{g.List.X, g.List.X + numW}
	for _, w := range []int{titleW, branchW, authorW, ageW, checkW} {
		cols = append(cols, cols[len(cols)-1]+w+1)
	}
	for i, name := range []string{"#", "title", "branch", "author", "age", "checks", "review"} {
		writeString(dst, cols[i], g.List.Y-1, name, theme.NotesSub, listEnd)
	}
	switch {
	case v.Error != "":
		writeString(dst, g.List.X, g.List.Y, truncate(v.Error, g.List.Cols), withBold(theme.Notes), listEnd)
	case v.Loading && len(v.PRs) == 0:
		writeString(dst, g.List.X, g.List.Y, "asking GitHub…", theme.NotesSub, listEnd)
	case len(v.PRs) == 0:
		writeString(dst, g.List.X, g.List.Y, "no pull requests match", theme.NotesSub, listEnd)
	}
	top := clampIssuesScroll(v, g)
	for line := 0; line < g.List.Rows && top+line < len(v.PRs) && v.Error == ""; line++ {
		i := top + line
		p := v.PRs[i]
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
		if p.State != "open" {
			numStyle = sub
		}
		title := p.Title
		if p.Draft && p.State == "open" {
			title = "[draft] " + title
		} else if p.State == "merged" {
			title = "[merged] " + title
		}
		writeString(dst, cols[0], y, fmt.Sprintf("%d", p.Number), numStyle, cols[1]-1)
		writeString(dst, cols[1], y, truncate(title, titleW), base, cols[2]-1)
		writeString(dst, cols[2], y, truncateLeft(p.Head, branchW), sub, cols[3]-1)
		writeString(dst, cols[3], y, truncate(p.Author, authorW), sub, cols[4]-1)
		writeString(dst, cols[4], y, SessionAge(v.Now, p.Updated), sub, cols[5]-1)
		checkStyle := sub
		if p.Fail > 0 && i != v.Cursor {
			checkStyle = withBold(theme.Notes)
		}
		writeString(dst, cols[5], y, prChecks(p), checkStyle, cols[6]-1)
		writeString(dst, cols[6], y, prReview(p), sub, listEnd)
	}

	msg := v.Message
	if msg == "" {
		msg = v.Dir
	}
	if v.Loading && len(v.PRs) > 0 {
		msg = "asking GitHub…"
	}
	hint := "type to search · tab next filter · ←→ issues/PRs · ↑↓ move · enter open · ctrl+o in browser · ctrl+r reload · esc"
	if len(v.Remotes) > 1 {
		hint += " · ctrl+t " + strings.Join(v.Remotes, "/")
	}
	drawIssuesFoot(dst, v, g, msg, hint, theme)
}

// prBody is a pull request's scrolling part: its failing and running
// checks, then its text and thread.
func prBody(v *IssuesView, width int, theme Theme) []notesLine {
	p := v.PR
	var out []notesLine
	var failing, running []string
	for _, c := range p.Checks {
		switch c.State {
		case "fail":
			failing = append(failing, c.Name)
		case "pending":
			running = append(running, c.Name)
		}
	}
	if len(failing) > 0 {
		out = append(out, notesLine{spans: []notesSpan{{" FAILING CHECKS", theme.NotesAccent}}, fill: theme.Notes})
		for _, name := range failing {
			out = append(out, notesLine{spans: []notesSpan{{" ✗ ", withBold(theme.Notes)}, {name, theme.Notes}}, fill: theme.Notes})
		}
		out = append(out, notesLine{fill: theme.Notes})
	}
	if len(running) > 0 {
		out = append(out, notesLine{spans: []notesSpan{{fmt.Sprintf(" ● %d running: ", len(running)), theme.NotesSub},
			{truncate(strings.Join(running, ", "), max(width-16, 10)), theme.NotesSub}}, fill: theme.Notes},
			notesLine{fill: theme.Notes})
	}
	return append(out, threadLines(v, p.Body, p.Thread, width, theme)...)
}

func drawPRDetail(dst *vt.Grid, v *IssuesView, g IssuesGeometry, theme Theme) {
	p := v.PR
	box := g.Box
	right := box.X + box.Cols - 1
	stateStyle := withBold(theme.NotesAccent)
	if p.State != "open" || p.Draft {
		stateStyle = withBold(theme.NotesSub)
	}
	x := writeString(dst, box.X+2, box.Y+2, fmt.Sprintf("#%d ", p.Number), withBold(theme.NotesAccent), right)
	x = writeString(dst, x, box.Y+2, prState(p.PREntry), stateStyle, right)
	meta := ""
	if p.Author != "" {
		meta = " · by " + p.Author
	}
	meta += " · " + p.Head + " → " + p.Base
	writeString(dst, x, box.Y+2, truncate(meta, right-x-1), theme.NotesSub, right)
	writeString(dst, box.X+2, box.Y+3, truncate(p.Title, box.Cols-4), withBold(theme.Notes), right)
	var status []string
	// What it changes first: branch names run long, and this is not to be
	// cut off behind one.
	if p.Files > 0 {
		status = append(status, fmt.Sprintf("+%d −%d in %d files", p.Additions, p.Deletions, p.Files))
	}
	if r := prReview(p.PREntry); r != "" {
		status = append(status, map[string]string{"approved": "approved", "changes": "changes requested", "review": "review required"}[r])
	}
	switch p.Mergeable {
	case "MERGEABLE":
		if p.State == "open" {
			status = append(status, "can merge")
		}
	case "CONFLICTING":
		status = append(status, "has conflicts")
	}
	if n := p.Pass + p.Fail + p.Pending; n > 0 {
		status = append(status, fmt.Sprintf("checks ✓%d ✗%d ●%d", p.Pass, p.Fail, p.Pending))
	}
	if len(p.Labels) > 0 {
		status = append(status, strings.Join(p.Labels, ", "))
	}
	writeString(dst, box.X+2, box.Y+4, truncate(strings.Join(status, " · "), box.Cols-4), theme.NotesAccent, right)

	if p.Loading {
		writeString(dst, g.Body.X, g.Body.Y, "reading the pull request…", theme.NotesSub, g.Body.X+g.Body.Cols)
	} else {
		drawScrolled(dst, g.Body, prBody(v, g.Body.Cols, theme), p.Scroll, theme)
	}
	keys := "↑↓ scroll · c comment · o in browser · w its worktree · esc back"
	if p.State == "open" {
		keys = "↑↓ scroll · m merge · c comment · x close · o in browser · w its worktree · esc back"
		if p.Draft {
			keys = "↑↓ scroll · r ready · m merge · c comment · x close · o in browser · esc back"
		}
	} else if p.State == "closed" {
		keys = "↑↓ scroll · x reopen · c comment · o in browser · esc back"
	}
	drawIssuesFoot(dst, v, g, v.Message, keys, theme)
}

// drawScrolled draws lines into r from scroll, with a bar at its right edge
// when they are more than it holds.
func drawScrolled(dst *vt.Grid, r Rect, lines []notesLine, scroll int, theme Theme) {
	top := max(min(scroll, len(lines)-r.Rows), 0)
	for i := 0; i < r.Rows && top+i < len(lines); i++ {
		lx := r.X
		for _, sp := range lines[top+i].spans {
			lx = writeString(dst, lx, r.Y+i, sp.text, sp.style, r.X+r.Cols)
		}
	}
	if len(lines) > r.Rows && r.Rows > 0 {
		thumb := max(r.Rows*r.Rows/len(lines), 1)
		at := (r.Rows - thumb) * top / max(len(lines)-r.Rows, 1)
		for i := 0; i < r.Rows; i++ {
			ch, style := '│', theme.NotesSub
			if i >= at && i < at+thumb {
				ch, style = '▐', theme.NotesAccent
			}
			setCell(dst, r.X+r.Cols, r.Y+i, ch, style)
		}
	}
}
