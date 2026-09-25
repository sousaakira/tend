package ui

import (
	"fmt"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// The companies panel is where the session's companies are kept: herdr's
// user-defined Spaces (src/user_space.rs), which herdr has the data and the
// API for and no screen. It lists them, makes a new one, renames and deletes
// one, chooses which spaces a company holds, and switches the sidebar to
// one — the part that is tend's own: while a company is chosen, the sidebar
// shows its spaces and their agents only. It is drawn in the release notes
// panel's colours, as the sessions list is. What the keys do is the
// client's; this draws and says where a click landed.

// CompanyEntry is one line of the list. The first is always every space,
// with ID zero: choosing it is choosing no company.
type CompanyEntry struct {
	ID   uint64
	Name string
	// Spaces is how many spaces it holds; Waiting how many of their agents
	// want the user, which is said even when the company is not the one
	// shown — that is the point of saying it.
	Spaces  int
	Waiting int
}

// CompanySpace is one space while a company's spaces are being chosen.
type CompanySpace struct {
	ID     uint64
	Name   string
	Detail string
	In     bool
}

// CompanyNaming is what the name being typed is for.
type CompanyNaming uint8

const (
	CompanyNamingNone CompanyNaming = iota
	CompanyNamingNew
	CompanyNamingRename
)

// CompaniesView is the panel while it is up.
type CompaniesView struct {
	Entries []CompanyEntry
	// Active is the company the sidebar shows, zero for every space.
	Active uint64
	Cursor int
	Scroll int

	// Choosing is the company whose spaces are listed to be ticked, zero
	// while the companies are listed; Spaces the spaces, in the session's
	// order, and SpaceCursor the one the cursor is on.
	Choosing     uint64
	ChoosingName string
	Spaces       []CompanySpace
	SpaceCursor  int

	// Naming is while a name is typed, into Input.
	Naming CompanyNaming
	Input  string
	// Confirm is while a delete waits for its answer.
	Confirm bool
	Message string
}

// CompaniesGeometry is where the panel's parts are, for drawing and a
// click.
type CompaniesGeometry struct {
	Box  Rect
	List Rect
	// The buttons on the bottom line: while the companies are listed,
	// Switch, New, Rename, Spaces and Delete; while a company's spaces are
	// chosen, Done takes the first place. Close is always at the right.
	Buttons []Rect
	Close   Rect
}

const (
	companiesCols = 76
	companiesRows = 24
)

// CompanyButtons are the labels of the bottom line, in order, for the list
// and for a company's spaces.
var (
	CompanyButtons      = []string{"[ Switch ]", "[ New ]", "[ Rename ]", "[ Spaces ]", "[ Delete ]"}
	CompanySpaceButtons = []string{"[ Done ]"}
	companiesClose      = "[ Close ]"
)

// The buttons by what they do, as the places in Buttons.
const (
	CompanySwitch = iota
	CompanyNew
	CompanyRename
	CompanySpacesButton
	CompanyDelete
)

// CompaniesLayout is the panel's geometry on a screen of cols by rows.
func CompaniesLayout(v *CompaniesView, cols, rows int) CompaniesGeometry {
	w, h := min(companiesCols, cols-2), min(companiesRows, rows-2)
	w, h = max(w, 0), max(h, 0)
	box := Rect{X: (cols - w) / 2, Y: (rows - h) / 2, Cols: w, Rows: h}
	g := CompaniesGeometry{Box: box}
	// A title, a subtitle, a blank; the list; a blank, a message, a hint,
	// the buttons; inside a frame.
	g.List = Rect{X: box.X + 2, Y: box.Y + 3, Cols: max(box.Cols-4, 0), Rows: max(box.Rows-8, 0)}
	bottom := box.Y + box.Rows - 2
	labels := CompanyButtons
	if v != nil && v.Choosing != 0 {
		labels = CompanySpaceButtons
	}
	x := box.X + 2
	for _, label := range labels {
		r := Rect{X: x, Y: bottom, Cols: runewidth.StringWidth(label), Rows: 1}
		g.Buttons = append(g.Buttons, r)
		x += r.Cols + 1
	}
	cw := runewidth.StringWidth(companiesClose)
	g.Close = Rect{X: box.X + box.Cols - 2 - cw, Y: bottom, Cols: cw, Rows: 1}
	return g
}

// CompaniesLineAt is the line of the list at a point, by its place in the
// entries or, while a company's spaces are chosen, in the spaces.
func CompaniesLineAt(v *CompaniesView, cols, rows, x, y int) (int, bool) {
	g := CompaniesLayout(v, cols, rows)
	if !inside(g.List, x, y) {
		return 0, false
	}
	i := companiesTop(v, g) + y - g.List.Y
	return i, i < companiesLen(v)
}

// CompaniesScrollFor is the scroll that keeps the cursor in view.
func CompaniesScrollFor(v *CompaniesView, cols, rows int) int {
	g := CompaniesLayout(v, cols, rows)
	top, cursor := companiesTop(v, g), companiesCursor(v)
	if cursor < top {
		top = cursor
	}
	if g.List.Rows > 0 && cursor >= top+g.List.Rows {
		top = cursor - g.List.Rows + 1
	}
	return max(top, 0)
}

func companiesLen(v *CompaniesView) int {
	if v.Choosing != 0 {
		return len(v.Spaces)
	}
	return len(v.Entries)
}

func companiesCursor(v *CompaniesView) int {
	if v.Choosing != 0 {
		return v.SpaceCursor
	}
	return v.Cursor
}

func companiesTop(v *CompaniesView, g CompaniesGeometry) int {
	return max(min(v.Scroll, companiesLen(v)-g.List.Rows), 0)
}

func inside(r Rect, x, y int) bool {
	return x >= r.X && x < r.X+r.Cols && y >= r.Y && y < r.Y+r.Rows
}

// CompanyLabel is what the sidebar's heading says about the company shown:
// its name, cut to fit, or "all" when there is none but some exist.
func CompanyLabel(name string, any bool, width int) string {
	switch {
	case name != "":
		return "◆ " + truncate(name, max(width, 2))
	case any:
		return "◇ all"
	}
	return "◇"
}

func drawCompanies(dst *vt.Grid, v *CompaniesView, theme Theme) {
	g := CompaniesLayout(v, dst.Cols(), dst.Rows())
	box := g.Box
	for y := box.Y; y < box.Y+box.Rows; y++ {
		for x := box.X; x < box.X+box.Cols; x++ {
			setCell(dst, x, y, ' ', theme.Notes)
		}
	}
	drawBox(dst, box, theme.NotesAccent)
	drawCloseMark(dst, box, withBold(theme.NotesAccent))
	if box.Rows < 10 || box.Cols < 40 {
		return
	}
	right := box.X + box.Cols - 1
	listEnd := g.List.X + g.List.Cols
	title := "COMPANIES"
	subtitle := "what the sidebar shows: a company's spaces, or all of them"
	if v.Choosing != 0 {
		title = "SPACES OF " + v.ChoosingName
		subtitle = "ticked spaces are shown while this company is chosen"
	}
	title = truncate(title, box.Cols-4)
	writeString(dst, box.X+(box.Cols-runewidth.StringWidth(title))/2, box.Y+1, title, withBold(theme.NotesAccent), right)
	writeString(dst, box.X+2, box.Y+2, truncate(subtitle, box.Cols-4), theme.NotesSub, right)

	top := companiesTop(v, g)
	if v.Choosing != 0 {
		if len(v.Spaces) == 0 {
			writeString(dst, g.List.X, g.List.Y, "no spaces in this session yet", theme.NotesSub, listEnd)
		}
		for line := 0; line < g.List.Rows && top+line < len(v.Spaces); line++ {
			i := top + line
			s := v.Spaces[i]
			y := g.List.Y + line
			base, sub := theme.Notes, theme.NotesSub
			if i == v.SpaceCursor {
				base, sub = theme.NotesButton, theme.NotesButton
				fillLine(dst, g.List.X, listEnd, y, base)
			}
			mark := "[ ]"
			if s.In {
				mark = "[✓]"
			}
			x := writeString(dst, g.List.X, y, mark, withBold(base), listEnd)
			x = writeString(dst, x+1, y, truncate(s.Name, g.List.Cols/2), base, listEnd)
			if s.Detail != "" {
				writeString(dst, x+2, y, truncate(s.Detail, listEnd-x-2), sub, listEnd)
			}
		}
	} else {
		for line := 0; line < g.List.Rows && top+line < len(v.Entries); line++ {
			i := top + line
			e := v.Entries[i]
			y := g.List.Y + line
			base, sub, accent := theme.Notes, theme.NotesSub, theme.NotesAccent
			if i == v.Cursor {
				base, sub, accent = theme.NotesButton, theme.NotesButton, theme.NotesButton
				fillLine(dst, g.List.X, listEnd, y, base)
			}
			if e.ID == v.Active {
				writeString(dst, g.List.X, y, "●", withBold(accent), listEnd)
			}
			name, style := e.Name, base
			if e.ID == 0 {
				name, style = "all spaces", sub
				if i == v.Cursor {
					style = base
				}
			}
			info := fmt.Sprintf("%d space%s", e.Spaces, plural(e.Spaces))
			if e.Waiting > 0 {
				info = fmt.Sprintf("%d waiting · %s", e.Waiting, info)
			}
			infoX := listEnd - runewidth.StringWidth(info)
			writeString(dst, g.List.X+2, y, truncate(name, max(infoX-g.List.X-4, 4)), style, infoX)
			infoStyle := sub
			if e.Waiting > 0 {
				infoStyle = withBold(accent)
			}
			writeString(dst, infoX, y, info, infoStyle, listEnd)
		}
	}

	msgY, hintY := box.Y+box.Rows-4, box.Y+box.Rows-3
	switch {
	case v.Naming != CompanyNamingNone:
		label := "new company: "
		if v.Naming == CompanyNamingRename {
			label = "rename to: "
		}
		x := writeString(dst, box.X+2, msgY, label, theme.NotesSub, right)
		x = writeString(dst, x, msgY, truncateLeft(v.Input, box.Cols-6-runewidth.StringWidth(label)), withBold(theme.Notes), right)
		setCell(dst, x, msgY, ' ', theme.NotesButton)
		writeString(dst, box.X+2, hintY, "enter saves · esc cancels", theme.NotesSub, right)
	case v.Confirm:
		name := ""
		if v.Cursor < len(v.Entries) {
			name = v.Entries[v.Cursor].Name
		}
		writeString(dst, box.X+2, msgY, truncate(fmt.Sprintf("delete %s? its spaces stay, only the company goes", name), box.Cols-4), withBold(theme.Notes), right)
		writeString(dst, box.X+2, hintY, "enter deletes · esc cancels", theme.NotesSub, right)
	default:
		if v.Message != "" {
			writeString(dst, box.X+2, msgY, truncate(v.Message, box.Cols-4), theme.NotesSub, right)
		}
		hint := "↑↓ move · enter switch · n new · r rename · s spaces · d delete · esc"
		if v.Choosing != 0 {
			hint = "↑↓ move · space or enter ticks · esc back to the companies"
		}
		writeString(dst, box.X+2, hintY, truncate(hint, box.Cols-4), theme.NotesSub, right)
	}
	labels := CompanyButtons
	if v.Choosing != 0 {
		labels = CompanySpaceButtons
	}
	for i, r := range g.Buttons {
		writeString(dst, r.X, r.Y, labels[i], theme.NotesAccent, right)
	}
	writeString(dst, g.Close.X, g.Close.Y, companiesClose, theme.NotesAccent, right)
}

func fillLine(dst *vt.Grid, from, to, y int, style vt.Style) {
	for x := from; x < to; x++ {
		setCell(dst, x, y, ' ', style)
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
