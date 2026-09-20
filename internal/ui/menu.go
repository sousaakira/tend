package ui

import (
	"github.com/mattn/go-runewidth"

	"github.com/sousaakira/tend/internal/vt"
)

// A context menu is how everything that is not a jump gets done with the
// mouse: closing a pane, renaming a space, splitting where you are pointing.
//
// It exists because the alternative is a key for each of those, and a key for
// each is a list nobody reads. The menu is opened on the thing it acts on, so
// what it will affect is never in question — there is no "current selection"
// to be wrong about.

// Menu action names. They are the commands the client dispatches, so a menu
// item and a key binding end up in the same place.
const (
	MenuClose      = "menu-close"
	MenuRename     = "menu-rename"
	MenuNewTab     = "menu-new-tab"
	MenuNewSpace   = "menu-new-space"
	MenuSplitRight = "menu-split-right"
	MenuSplitDown  = "menu-split-down"
	MenuZoom       = "menu-zoom"
	MenuGoTo       = "menu-go-to"
	MenuGroup      = "menu-group"
	MenuFold       = "menu-fold"
)

// MenuItem is one line of a menu.
type MenuItem struct {
	Label  string
	Action string
}

// Menu is a popup anchored at the point it was opened on, acting on whichever
// of Pane, Tab and Workspace is set.
type Menu struct {
	Title string
	Items []MenuItem
	// X and Y are where it was opened, which is its top-left corner unless
	// that would put it off the screen.
	X, Y int
	// Selected is the keyboard cursor. The mouse does not use it: clicking an
	// item runs that item, not whatever the cursor was on.
	Selected int

	Pane      uint64
	Tab       uint64
	Workspace uint64
	// Group names the group a group menu acts on, which has no identifier of
	// its own because it has no record of its own.
	Group string
}

// menuPadding is the space either side of a label inside the border.
const menuPadding = 2

// Rect is where the menu is drawn, clamped to the screen.
//
// A menu that hangs off the bottom of the terminal loses exactly the items
// added last, which are the destructive ones, so it is moved rather than
// clipped.
func (m Menu) Rect(cols, rows int) Rect {
	width := runewidth.StringWidth(m.Title)
	for _, item := range m.Items {
		width = max(width, runewidth.StringWidth(item.Label))
	}
	r := Rect{
		Cols: min(width+2*menuPadding+2, cols),
		Rows: min(len(m.Items)+2, rows),
	}
	r.X = min(max(m.X, 0), max(cols-r.Cols, 0))
	r.Y = min(max(m.Y, 0), max(rows-r.Rows, 0))
	return r
}

// MenuItemAt returns the item under a point, and whether the point is inside
// the menu at all.
//
// The two answers are separate on purpose: a click inside the border but not
// on an item must not fall through to whatever is behind the menu, or the
// menu would close and act on the pane underneath in one gesture.
func MenuItemAt(m Menu, cols, rows, x, y int) (item MenuItem, onItem, inside bool) {
	r := m.Rect(cols, rows)
	if x < r.X || x >= r.X+r.Cols || y < r.Y || y >= r.Y+r.Rows {
		return MenuItem{}, false, false
	}
	i := y - r.Y - 1
	if i < 0 || i >= len(m.Items) {
		return MenuItem{}, false, true
	}
	return m.Items[i], true, true
}

// drawMenu paints the popup over whatever is behind it.
func drawMenu(dst *vt.Grid, m Menu, theme Theme) {
	if len(m.Items) == 0 {
		return
	}
	r := m.Rect(dst.Cols(), dst.Rows())

	for y := r.Y; y < r.Y+r.Rows; y++ {
		row := dst.Line(y)
		if row == nil {
			continue
		}
		for x := r.X; x < r.X+r.Cols; x++ {
			row.SetCell(x, vt.Cell{R: ' ', Style: theme.Overlay, Width: 1})
		}
	}
	drawBox(dst, r, theme.OverlayTitle)
	if m.Title != "" {
		writeString(dst, r.X+menuPadding, r.Y, " "+truncate(m.Title, r.Cols-4)+" ", theme.OverlayTitle, r.X+r.Cols)
	}

	limit := r.X + r.Cols - 1
	for i, item := range m.Items {
		y := r.Y + 1 + i
		if y >= r.Y+r.Rows-1 {
			break
		}
		style := theme.Overlay
		if i == m.Selected {
			style = theme.OverlayTitle
		}
		// The whole row takes the cursor's styling, so the selection reads as
		// a band rather than as a word that changed weight.
		for x := r.X + 1; x < limit; x++ {
			if row := dst.Line(y); row != nil {
				row.SetCell(x, vt.Cell{R: ' ', Style: style, Width: 1})
			}
		}
		writeString(dst, r.X+menuPadding, y, truncate(item.Label, r.Cols-2*menuPadding), style, limit)
	}
}

// PaneMenu is what a right-click on a pane offers.
func PaneMenu(pane uint64, x, y int, closable bool) Menu {
	items := []MenuItem{
		{Label: "split right", Action: MenuSplitRight},
		{Label: "split down", Action: MenuSplitDown},
		{Label: "zoom", Action: MenuZoom},
		{Label: "rename tab", Action: MenuRename},
	}
	if closable {
		items = append(items, MenuItem{Label: "close pane", Action: MenuClose})
	}
	return Menu{Title: "pane", Items: items, X: x, Y: y, Pane: pane}
}

// TabMenu is what a right-click on the tab bar offers.
func TabMenu(tab uint64, x, y int) Menu {
	return Menu{
		Title: "tab",
		Items: []MenuItem{
			{Label: "new tab", Action: MenuNewTab},
			{Label: "rename", Action: MenuRename},
			{Label: "close tab", Action: MenuClose},
		},
		X: x, Y: y, Tab: tab,
	}
}

// SpaceMenu is what a right-click on a space offers.
func SpaceMenu(workspace uint64, x, y int) Menu {
	return Menu{
		Title: "space",
		Items: []MenuItem{
			{Label: "new space", Action: MenuNewSpace},
			{Label: "new tab", Action: MenuNewTab},
			{Label: "rename", Action: MenuRename},
			{Label: "group...", Action: MenuGroup},
			{Label: "close space", Action: MenuClose},
		},
		X: x, Y: y, Workspace: workspace,
	}
}

// GroupMenu is what a right-click on a group heading offers.
//
// It acts on the group as a whole, which is the set of spaces naming it: there
// is no group record to rename, so renaming one moves every member.
func GroupMenu(group string, folded bool, x, y int) Menu {
	fold := "fold"
	if folded {
		fold = "unfold"
	}
	return Menu{
		Title: "group",
		Items: []MenuItem{
			{Label: fold, Action: MenuFold},
			{Label: "new space here", Action: MenuNewSpace},
			{Label: "rename group", Action: MenuRename},
			{Label: "ungroup", Action: MenuClose},
		},
		X: x, Y: y, Group: group,
	}
}

// AgentMenu is what a right-click on an agent row offers. It acts on the
// pane the agent is running in, which is the only thing there is to act on.
func AgentMenu(pane, tab, workspace uint64, x, y int) Menu {
	return Menu{
		Title: "agent",
		Items: []MenuItem{
			{Label: "go to", Action: MenuGoTo},
			{Label: "rename tab", Action: MenuRename},
			{Label: "close pane", Action: MenuClose},
		},
		X: x, Y: y, Pane: pane, Tab: tab, Workspace: workspace,
	}
}
