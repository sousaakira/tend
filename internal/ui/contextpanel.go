package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/sousaakira/tend/internal/vt"
)

// The context panel is tend's own: what tools have captured (the server's
// context buffer), one item's parts under the list, and what to do with it —
// copy it, send it to the agent the space is working with, drop it. Drawn in
// the release notes panel's colours, as the agent manager is. It draws and
// says where a click landed; the rest is the client's.

// ContextEntry is one captured item as the panel lists it.
type ContextEntry struct {
	Kind    string
	Summary string
	// Parts are its details under headings: URL, SELECTED ELEMENT, TEXT, ...
	Parts []ContextPart
}

// ContextPart is one heading and what is under it.
type ContextPart struct {
	Heading string
	Value   string
}

// ContextView is the panel while it is up.
type ContextView struct {
	Items  []ContextEntry
	Cursor int
	// Target is where Send to Agent types, as its frame names it ("1
	// claude"); empty when there is no pane to send to.
	Target  string
	Loading bool
	// ClearArmed is set after a first Clear: the second one empties it.
	ClearArmed bool
	Message    string
}

// ContextButton is one of the panel's buttons.
type ContextButton int

const (
	ContextCopy ContextButton = iota
	ContextSend
	ContextSendAll
	ContextRemove
	ContextClear
	ContextClose
)

var contextButtonLabels = [...]string{"[ Copy ]", "[ Send ]", "[ Send all ]", "[ Remove ]", "[ Clear ]", "[ Close ]"}

// ContextGeometry is where the panel's parts are.
type ContextGeometry struct {
	Box Rect
	// List is the items, one a line; Detail the chosen one's parts.
	List, Detail Rect
	Buttons      [6]Rect
}

const (
	contextCols     = 76
	contextMaxRows  = 28
	contextListRows = 7
)

// ContextLayout is the panel's geometry on a screen of cols by rows.
func ContextLayout(v *ContextView, cols, rows int) ContextGeometry {
	w, h := min(contextCols, cols), min(contextMaxRows, rows)
	box := Rect{X: (cols - w) / 2, Y: (rows - h) / 2, Cols: w, Rows: h}
	g := ContextGeometry{Box: box}
	listRows := min(max(len(v.Items), 1), contextListRows)
	g.List = Rect{X: box.X + 2, Y: box.Y + 3, Cols: box.Cols - 4, Rows: listRows}
	// A rule, then the detail down to the target line, the message and the
	// buttons at the bottom.
	top := g.List.Y + listRows + 1
	g.Detail = Rect{X: box.X + 2, Y: top, Cols: box.Cols - 4, Rows: max(box.Y+box.Rows-4-top, 0)}
	x, y := box.X+2, box.Y+box.Rows-2
	for i, label := range contextButtonLabels {
		w := runewidth.StringWidth(label)
		if ContextButton(i) == ContextClose {
			x = box.X + box.Cols - 2 - w
		}
		g.Buttons[i] = Rect{X: x, Y: y, Cols: w, Rows: 1}
		x += w + 1
	}
	return g
}

// contextListTop is the first item the list shows, so the cursor is in it.
func contextListTop(v *ContextView, rows int) int {
	return max(min(v.Cursor-rows+1, len(v.Items)-rows), 0)
}

// ContextItemAt is the item on a line of the list, if one is.
func ContextItemAt(v *ContextView, cols, rows, x, y int) (int, bool) {
	g := ContextLayout(v, cols, rows)
	if x < g.List.X || x >= g.List.X+g.List.Cols || y < g.List.Y || y >= g.List.Y+g.List.Rows {
		return 0, false
	}
	i := contextListTop(v, g.List.Rows) + y - g.List.Y
	return i, i < len(v.Items)
}

// ContextButtonAt is the button under a point, if one is.
func ContextButtonAt(v *ContextView, cols, rows, x, y int) (ContextButton, bool) {
	g := ContextLayout(v, cols, rows)
	for i, r := range g.Buttons {
		if y == r.Y && x >= r.X && x < r.X+r.Cols {
			return ContextButton(i), true
		}
	}
	return 0, false
}

func drawContextPanel(dst *vt.Grid, v *ContextView, theme Theme) {
	g := ContextLayout(v, dst.Cols(), dst.Rows())
	box := g.Box
	for y := box.Y; y < box.Y+box.Rows; y++ {
		for x := box.X; x < box.X+box.Cols; x++ {
			setCell(dst, x, y, ' ', theme.Notes)
		}
	}
	drawBox(dst, box, theme.NotesAccent)
	drawCloseMark(dst, box, withBold(theme.NotesAccent))
	if box.Rows < 12 || box.Cols < 40 {
		return
	}
	right := box.X + box.Cols - 1
	title := "CONTEXT"
	writeString(dst, box.X+(box.Cols-len(title))/2, box.Y+1, title, withBold(theme.NotesAccent), right)
	end := g.List.X + g.List.Cols

	switch {
	case v.Loading:
		writeString(dst, g.List.X, g.List.Y, "reading the context…", theme.NotesSub, end)
	case len(v.Items) == 0:
		writeString(dst, g.List.X, g.List.Y, "Nothing captured yet.", theme.Notes, end)
		lines := []string{
			"Add things with `tend context add`, with Add to context",
			"on a file in the files panel, or from any tool over the",
			"automation socket (context.add).",
		}
		for i, l := range lines {
			writeString(dst, g.Detail.X, g.Detail.Y+i, strings.ReplaceAll(l, "`", ""), theme.NotesSub, end)
		}
	}
	top := contextListTop(v, g.List.Rows)
	for line := 0; line < g.List.Rows && top+line < len(v.Items); line++ {
		i := top + line
		it := v.Items[i]
		y := g.List.Y + line
		style, kindStyle := theme.Notes, theme.NotesAccent
		if i == v.Cursor {
			style, kindStyle = theme.NotesButton, theme.NotesButton
			for x := g.List.X; x < end; x++ {
				setCell(dst, x, y, ' ', style)
			}
		}
		x := writeString(dst, g.List.X+1, y, "["+it.Kind+"]", kindStyle, end)
		writeString(dst, x+1, y, truncate(it.Summary, end-x-2), style, end)
	}
	// The rule between the list and the one item's parts.
	for x := box.X + 1; x < right; x++ {
		setCell(dst, x, g.List.Y+g.List.Rows, '─', theme.NotesSub)
	}
	if v.Cursor >= 0 && v.Cursor < len(v.Items) {
		y := g.Detail.Y
		for _, p := range v.Items[v.Cursor].Parts {
			// A heading only with room for a line of what it heads: one
			// alone at the bottom reads as a part that is empty.
			if y+1 >= g.Detail.Y+g.Detail.Rows {
				break
			}
			writeString(dst, g.Detail.X, y, p.Heading, theme.NotesAccent, end)
			y++
			for _, l := range strings.Split(p.Value, "\n") {
				if y >= g.Detail.Y+g.Detail.Rows {
					break
				}
				writeString(dst, g.Detail.X, y, truncate(l, g.Detail.Cols), theme.Notes, end)
				y++
			}
			y++
		}
	}

	infoY := box.Y + box.Rows - 3
	info, infoStyle := "", theme.NotesSub
	switch {
	case v.ClearArmed:
		info, infoStyle = "Clear again empties the whole context", withBold(theme.Notes)
	case v.Message != "":
		info = v.Message
	case v.Target != "":
		info = "Send types it into " + v.Target + " · c copy · s send · S send all · x remove"
	default:
		info = "no pane to send to in this tab"
	}
	writeString(dst, box.X+2, infoY, truncate(info, box.Cols-4), infoStyle, right)
	for i, r := range g.Buttons {
		style := theme.NotesAccent
		if (ContextButton(i) == ContextSend || ContextButton(i) == ContextSendAll) && v.Target == "" || len(v.Items) == 0 && ContextButton(i) != ContextClose {
			style = theme.NotesSub
		}
		writeString(dst, r.X, r.Y, contextButtonLabels[i], style, right)
	}
}
