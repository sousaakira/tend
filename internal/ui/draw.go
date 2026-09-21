// Package ui draws a tend session.
//
// Drawing is pure: it takes a description of what to show and fills a grid of
// cells. Nothing here reads a socket, a terminal or a clock, so a whole
// screenful can be asserted in a test without any of those existing.
//
// The client owns this. A pane's contents arrive as its own terminal screen,
// and compositing them into one grid is presentation — which is why it lives
// on this side of the socket rather than in the server.
package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/sousaakira/tend/internal/config"
	"github.com/sousaakira/tend/internal/vt"
)

// Theme is every colour the interface uses, in one place so a change is one
// edit rather than a search.
type Theme struct {
	Border        vt.Style
	BorderFocused vt.Style
	Title         vt.Style
	TitleFocused  vt.Style

	Status      vt.Style
	StatusKey   vt.Style
	StatusAlert vt.Style
	// TabInactive is a tab not in view, which is the bar itself unless a
	// palette gives it a surface of its own.
	TabInactive vt.Style

	Overlay      vt.Style
	OverlayTitle vt.Style

	// A menu is drawn plainly and marks its selection by reversing it, which
	// is the other way round from the overlay. Reversing the whole panel and
	// then reversing the selected row inside it leaves nothing to reverse:
	// the mark has to be the thing the panel is not.
	Menu         vt.Style
	MenuTitle    vt.Style
	MenuSelected vt.Style

	Sidebar         vt.Style
	SidebarActive   vt.Style
	SidebarSelected vt.Style
	SidebarDetail   vt.Style
	// SidebarCursor is the navigation cursor's row, when a palette gives it
	// a background; the zero style leaves the row as it is and the cursor
	// is the › in the margin alone.
	SidebarCursor      vt.Style
	SidebarGroup       vt.Style
	SidebarGroupActive vt.Style

	Working vt.Style
	Blocked vt.Style
	Idle    vt.Style
	// Done is an agent that finished while nobody was looking: herdr's teal.
	Done    vt.Style
	Unknown vt.Style
	Exited  vt.Style
}

// DefaultTheme uses the terminal's own palette rather than fixed colours, so
// tend looks like the rest of the user's terminal instead of fighting it.
func DefaultTheme() Theme {
	dim := vt.Style{Attrs: vt.AttrDim}
	return Theme{
		Border:        dim,
		BorderFocused: vt.Style{FG: vt.IndexedColor(4)},
		Title:         dim,
		TitleFocused:  vt.Style{FG: vt.IndexedColor(4), Attrs: vt.AttrBold},

		Status:      vt.Style{Attrs: vt.AttrReverse},
		StatusKey:   vt.Style{Attrs: vt.AttrReverse | vt.AttrBold},
		StatusAlert: vt.Style{Attrs: vt.AttrReverse | vt.AttrBold, FG: vt.IndexedColor(1)},
		TabInactive: vt.Style{Attrs: vt.AttrReverse},

		Overlay:      vt.Style{Attrs: vt.AttrReverse},
		OverlayTitle: vt.Style{Attrs: vt.AttrReverse | vt.AttrBold},

		Menu:         vt.Style{},
		MenuTitle:    vt.Style{FG: vt.IndexedColor(4), Attrs: vt.AttrBold},
		MenuSelected: vt.Style{Attrs: vt.AttrReverse | vt.AttrBold},

		Sidebar:            vt.Style{},
		SidebarActive:      vt.Style{Attrs: vt.AttrBold},
		SidebarSelected:    vt.Style{Attrs: vt.AttrReverse | vt.AttrBold},
		SidebarDetail:      dim,
		SidebarGroup:       dim,
		SidebarGroupActive: vt.Style{FG: vt.IndexedColor(4), Attrs: vt.AttrBold},

		Working: vt.Style{FG: vt.IndexedColor(3)},
		Blocked: vt.Style{FG: vt.IndexedColor(1), Attrs: vt.AttrBold},
		Idle:    vt.Style{FG: vt.IndexedColor(2)},
		Done:    vt.Style{FG: vt.IndexedColor(6)},
		Unknown: dim,
		Exited:  dim,
	}
}

// ThemeFrom builds the theme a configuration asks for: the named palette if
// there is one, then each colour the user set over it. An empty value keeps
// what was there, so a file naming one colour changes one colour.
func ThemeFrom(c config.Theme) Theme { return themeFrom(c, nil) }

// themeFrom is ThemeFrom with the overrides for one appearance laid over the
// rest, which only auto_switch picks.
func themeFrom(c config.Theme, mode map[string]string) Theme {
	t := DefaultTheme()
	name := c.Name
	if name == "" && (len(c.Custom.All) > 0 || len(mode) > 0) {
		// Overrides are of a palette's tokens, so there has to be a palette
		// under them: herdr's default one, as herdr has it.
		name = "catppuccin"
	}
	if p, ok := PaletteNamed(name); ok {
		t = t.withPalette(p.withCustom(c.Custom.All).withCustom(mode))
	}
	apply := func(target *vt.Style, value string) {
		if value == "" {
			return
		}
		parsed, ok := config.ParseColor(value)
		if !ok {
			return // rejected at load; nothing sensible to do here
		}
		if parsed.RGB {
			target.FG = vt.RGBColor(parsed.R, parsed.G, parsed.B)
		} else {
			target.FG = vt.IndexedColor(parsed.Index)
		}
		// A colour the user chose replaces dimming, which would fight it.
		target.Attrs &^= vt.AttrDim
	}
	apply(&t.Border, c.Border)
	apply(&t.BorderFocused, c.BorderFocused)
	apply(&t.Title, c.Border)
	apply(&t.TitleFocused, c.BorderFocused)
	apply(&t.Working, c.Working)
	apply(&t.Blocked, c.Blocked)
	apply(&t.Idle, c.Idle)
	return t
}

// ThemeFor is ThemeFrom for a terminal that is light or dark: with
// auto_switch on, the configured dark or light theme stands in for the name,
// as herdr's resolve_effective_theme does. Until the terminal has said which
// it is, it is taken to be dark, herdr's assumption too.
func ThemeFor(c config.Theme, light bool) Theme {
	if !c.AutoSwitch {
		return ThemeFrom(c)
	}
	mode := c.Custom.Dark
	c.Name = c.DarkName
	if c.Name == "" {
		c.Name = "catppuccin"
	}
	if light {
		c.Name, mode = c.LightName, c.Custom.Light
		if c.Name == "" {
			c.Name = "catppuccin-latte"
		}
	}
	return themeFrom(c, mode)
}

// withPalette colours a theme from a palette, using the tokens herdr uses
// for the same things: accent for what has focus, overlay0 for frames and
// secondary text, and yellow, red and green for working, blocked and idle
// (`ui/panes.rs`, `client/shell.rs` status_color).
//
// Dimming goes wherever a colour arrives. It is how the default theme says
// "secondary" without a colour, and on top of one it only makes the colour
// the palette chose into another one.
func (t Theme) withPalette(p Palette) Theme {
	fg := func(target *vt.Style, c vt.Color) {
		target.FG = c
		target.Attrs &^= vt.AttrDim
	}
	for _, s := range []*vt.Style{&t.BorderFocused, &t.TitleFocused, &t.MenuTitle, &t.SidebarGroupActive} {
		fg(s, p.Accent)
	}
	for _, s := range []*vt.Style{&t.Border, &t.Title, &t.SidebarDetail, &t.SidebarGroup, &t.Unknown, &t.Exited} {
		fg(s, p.Overlay0)
	}
	fg(&t.Working, p.Yellow)
	fg(&t.Blocked, p.Red)
	fg(&t.Idle, p.Green)
	fg(&t.Done, p.Teal)
	fg(&t.StatusAlert, p.Red)

	if p.PanelBG.IsDefault() {
		// A palette with no panel colour of its own (terminal) keeps the
		// reversed bars and panels: without a background there is nothing
		// else to set them apart from the panes.
		return t
	}
	// herdr's surfaces: bars and panels on panel_bg, what is chosen on the
	// accent in the panel's own colour (panel_contrast_fg), the entry in view
	// on active_row_bg (`client/shell/tabs.rs`, `overlays.rs`,
	// `agent_sidebar.rs`).
	contrast := p.PanelBG
	on := func(fg, bg vt.Color, attrs vt.Attr) vt.Style { return vt.Style{FG: fg, BG: bg, Attrs: attrs} }
	t.Status = on(p.Overlay1, p.PanelBG, 0)
	t.TabInactive = on(p.Overlay0, p.Surface0, 0)
	t.StatusKey = on(contrast, p.Accent, vt.AttrBold)
	t.StatusAlert = on(contrast, p.Red, vt.AttrBold)
	t.Overlay = on(p.Text, p.PanelBG, 0)
	t.OverlayTitle = on(p.Accent, p.PanelBG, vt.AttrBold)
	t.Menu = on(p.Text, p.PanelBG, 0)
	t.MenuTitle = on(p.Accent, p.PanelBG, vt.AttrBold)
	t.MenuSelected = on(contrast, p.Accent, vt.AttrBold)
	t.SidebarSelected = on(p.Text, p.ActiveRowBG, vt.AttrBold)
	if !p.SelectionBG.IsDefault() {
		t.SidebarCursor = on(p.Text, p.SelectionBG, 0)
	}
	if !p.SidebarBG.IsDefault() {
		t.Sidebar.BG = p.SidebarBG
	}
	return t
}

// StateStyle picks the colour for an agent state.
func (t Theme) StateStyle(state string, running bool) vt.Style {
	if !running {
		return t.Exited
	}
	switch state {
	case "working":
		return t.Working
	case "blocked":
		return t.Blocked
	case "idle":
		return t.Idle
	case "done":
		return t.Done
	default:
		return t.Unknown
	}
}

// Rect is a region of the screen in cells.
type Rect struct {
	X, Y, Cols, Rows int
}

// Pane is one pane to draw.
type Pane struct {
	ID    uint64
	Rect  Rect
	Title string
	Agent string
	State string
	// Command is what the pane is running, used as a label until the pane
	// names itself. A pane called "pane" tells nobody anything.
	Command string

	// Screen is the pane's own terminal. Nil draws an empty frame, which is
	// what a pane looks like between being opened and its first output.
	Screen *vt.Screen

	Running bool
	Focused bool
}

// Tab is one tab in the bar across the top.
type Tab struct {
	ID     uint64
	Name   string
	Panes  int
	Active bool
	// Alert marks a tab holding an agent that needs an answer, so a blocked
	// agent in a tab you are not looking at is still visible.
	Alert bool
}

// Frame is everything to draw.
type Frame struct {
	Panes []Pane
	Tabs  []Tab
	// Status is what goes at the right end of the tab bar (ui.tab_bar_right),
	// and StatusSeparator what goes between two entries.
	Status          []StatusEntry
	StatusSeparator string
	// TabBarBottom puts the tab bar above the status bar rather than at the
	// top, and HideSingleTab leaves it out while the space has one tab:
	// herdr's tab_bar_position and hide_tab_bar_when_single_tab.
	TabBarBottom  bool
	HideSingleTab bool

	Session   string
	Workspace string
	Tab       string
	// Offline marks that the session is unreachable. It is shown rather than
	// hidden: a frozen screen with no explanation is the worst version of
	// this, because the user cannot tell it from an agent that has stopped.
	Offline bool
	// Scroll is how far back a pane is being read, and ScrollDepth how far it
	// could go. Both are shown: a view that says only "scrolled" leaves the
	// user unable to tell a glance back from being lost in a long history.
	Scroll      int
	ScrollDepth int
	// Copy marks copy mode, and CopyCursor is where its cursor is. The
	// terminal's own cursor is put there: it is the one mark every terminal
	// already draws, blinks and makes visible on any colour.
	Copy       bool
	CopyCursor *CopyCursor
	// Resize marks resize mode, so the status bar can say why h/j/k/l are not
	// reaching the pane.
	Resize bool
	// Zoomed marks that one pane is filling the area, so the status bar can
	// say so — a zoomed pane and a session with one pane look identical
	// otherwise.
	Zoomed bool

	// Message is shown in place of the pane list, for a moment, after an
	// action or an error.
	Message string
	// Alert marks Message as a problem rather than a confirmation.
	Alert bool
	// Prefix marks that the prefix key is armed and the next key is a command.
	Prefix bool

	// Waiting is how many agents anywhere in the session are blocked on an
	// answer, including ones in spaces this client is not looking at.
	//
	// It is on the status bar rather than only in the list, because the whole
	// reason to run tend is that the agent needing you is usually not the one
	// on screen — and the list can be folded, scrolled, or turned off.
	Waiting int

	// Sidebar shows the spaces and agents down the left edge, and SidebarRows
	// is what it holds.
	Sidebar bool
	// Spaces and Agents are the sidebar's two lists. They are separate
	// because they answer different questions and because one sharing the
	// other's scroll meant the spaces pushed the agents off the bottom.
	Spaces SidebarSection
	Agents SidebarSection
	// SidebarSplit is the line the divider sits on. Zero means the sidebar
	// decides; anything else is where the user dragged it to.
	SidebarSplit int
	// Navigating marks that the list has the keyboard.
	Navigating bool

	// Prompt and PromptText are a line being typed, such as a new name. When
	// Prompt is set it is drawn as a box over the middle of the screen: the
	// status bar is where tend says things, not where the user says them.
	Prompt     string
	PromptText string
	// PromptSelected marks the text as the seed, about to be replaced by the
	// next keystroke. It is drawn differently so that is visible rather than
	// surprising.
	PromptSelected bool

	// Overlay is a panel drawn over the middle of the screen, for things that
	// do not fit on one status line. A list of keys is the obvious case: there
	// are fourteen of them, and cramming those into a status bar means
	// truncating exactly the ones somebody was looking for.
	Overlay []string

	// Menu is a context menu, opened on the thing it acts on. Nil when none
	// is open.
	Menu *Menu

	// Selection is a range of text being marked in a pane, or nil.
	Selection *Selection
	// Highlight marks every visible match of a copy-mode search, or nil.
	Highlight *Highlight
}

// StatusRows is how many rows at the bottom the status bar occupies. Panes are
// laid out above it.
const StatusRows = 1

// TabRows is how many rows the tab bar takes for a given number of tabs.
//
// The bar is shown whenever there is a session, not only once there are two
// tabs. It carries the button that makes a new one, and a button that appears
// only after you already have what it creates is a button nobody finds.
//
// HideSingleTab is the setting for people who never use tabs and would
// rather have the row back, and it is off unless they ask for it.
func TabRows(f Frame) int {
	if n := len(f.Tabs); n > 0 && !(f.HideSingleTab && n == 1) {
		return 1
	}
	return 0
}

// TabBarRow is the row the tab bar is drawn on, or -1 when it is not shown.
// Drawing, hit-testing and the pane area all ask this one function.
func TabBarRow(f Frame, rows int) int {
	if TabRows(f) == 0 {
		return -1
	}
	if f.TabBarBottom {
		return rows - StatusRows - 1
	}
	return 0
}

// StatusEntry is one thing at the right of the tab bar. Accent is herdr's
// emphasis, which only ZOOM uses.
type StatusEntry struct {
	Text   string
	Accent bool
}

// minTabStrip is how much of the bar the tabs keep before the status gives
// way: herdr's MIN_TAB_STRIP_WIDTH, one short tab, the new-tab button and
// two scroll buttons. The tabs are what the bar is for.
const minTabStrip = 17

// StatusArea is where the right end of the tab bar is drawn, as a start
// column and a width, or a width of zero when there is none or no room.
func StatusArea(f Frame, cols int) (start, width int) {
	for i, e := range f.Status {
		if i > 0 {
			width += runewidth.StringWidth(f.StatusSeparator)
		}
		width += runewidth.StringWidth(e.Text)
	}
	if width == 0 {
		return cols, 0
	}
	// One column of space before it, as herdr leaves.
	if cols-SidebarGutter(f, cols)-(width+1) < minTabStrip {
		return cols, 0
	}
	return cols - width, width
}

// NewTabLabel is the button at the end of the bar.
const NewTabLabel = " + "

// TabSegment is where one entry of the tab bar was drawn, in columns.
//
// Drawing and hit-testing both work from these, so a click cannot land
// somewhere other than what it looks like it is on: there is one description
// of the layout, not two that have to agree.
type TabSegment struct {
	Tab uint64
	// New marks the button that creates a tab rather than selects one.
	New        bool
	Start, End int
}

// TabSegments lays out the bar across cols columns.
//
// The bar belongs to the panes, so it starts where they do rather than at the
// edge of the screen. The sidebar lists every space; the tabs list one space's
// tabs, and a bar running over the sidebar would read as though those tabs
// belonged to the whole session.
func TabSegments(f Frame, cols int) []TabSegment {
	var out []TabSegment
	x := SidebarGutter(f, cols)
	// The tabs stop where the status starts, and a column short of it.
	if start, width := StatusArea(f, cols); width > 0 {
		cols = start - 1
	}
	for _, tab := range f.Tabs {
		label := tabLabel(tab)
		width := runewidth.StringWidth(label)
		if x+width > cols {
			break
		}
		out = append(out, TabSegment{Tab: tab.ID, Start: x, End: x + width})
		x += width
	}
	if width := runewidth.StringWidth(NewTabLabel); x+width <= cols {
		out = append(out, TabSegment{New: true, Start: x, End: x + width})
	}
	return out
}

func tabLabel(tab Tab) string {
	label := tab.Name
	if label == "" {
		label = itoa(tab.ID)
	}
	return " " + label + " "
}

// TabAt reports what a click on the bar landed on.
func TabAt(f Frame, x, y, cols, rows int) (tab uint64, newTab bool, ok bool) {
	if row := TabBarRow(f, rows); row < 0 || y != row || x < SidebarGutter(f, cols) {
		return 0, false, false
	}
	for _, seg := range TabSegments(f, cols) {
		if x >= seg.Start && x < seg.End {
			return seg.Tab, seg.New, true
		}
	}
	return 0, false, false
}

// Draw fills dst with the frame.
//
// dst is cleared first: a frame describes the whole screen, so anything left
// from the last one is stale by definition. The painter that sends this to a
// terminal is what avoids redrawing the parts that did not change.
func Draw(dst *vt.Grid, f Frame, theme Theme) {
	dst.Clear(vt.DefaultStyle)

	drawTabs(dst, f, theme)
	drawSidebar(dst, f, theme)
	drawShowHandle(dst, f, theme)
	for _, p := range f.Panes {
		drawPane(dst, p, theme)
	}
	drawStatus(dst, f, theme)

	// Over the panes but under everything that floats: the selection marks
	// text that is already drawn.
	drawSelection(dst, f)
	drawHighlights(dst, f)

	// Last, so they sit over the panes rather than under them. The menu is
	// last of all: it is opened on top of whatever is already showing.
	if len(f.Overlay) > 0 {
		drawOverlay(dst, f.Overlay, theme)
	}
	if f.Menu != nil {
		drawMenu(dst, *f.Menu, theme)
	}
	// The field is last: it has the keyboard, so nothing may sit over it.
	drawPrompt(dst, f, theme)
}

// drawTabs draws the tab bar.
func drawTabs(dst *vt.Grid, f Frame, theme Theme) {
	y := TabBarRow(f, dst.Rows())
	if y < 0 {
		return
	}
	row := dst.Line(y)
	if row == nil {
		return
	}
	for x := SidebarGutter(f, dst.Cols()); x < dst.Cols(); x++ {
		row.SetCell(x, vt.Cell{R: ' ', Style: theme.Status, Width: 1})
	}

	byID := make(map[uint64]Tab, len(f.Tabs))
	for _, tab := range f.Tabs {
		byID[tab.ID] = tab
	}

	for _, seg := range TabSegments(f, dst.Cols()) {
		if seg.New {
			writeString(dst, seg.Start, y, NewTabLabel, theme.StatusKey, dst.Cols())
			continue
		}
		tab := byID[seg.Tab]
		style := theme.TabInactive
		if tab.Active {
			style = theme.StatusKey
		}
		if tab.Alert && !tab.Active {
			// A blocked agent in a tab you are not looking at is the one thing
			// the bar exists to tell you.
			style = theme.StatusAlert
		}
		writeString(dst, seg.Start, y, tabLabel(tab), style, dst.Cols())
	}
	drawTabBarStatus(dst, f, theme, y)
}

// drawTabBarStatus writes the right end of the tab bar.
func drawTabBarStatus(dst *vt.Grid, f Frame, theme Theme, y int) {
	x, width := StatusArea(f, dst.Cols())
	if width == 0 {
		return
	}
	for i, e := range f.Status {
		if i > 0 && f.StatusSeparator != "" {
			writeString(dst, x, y, f.StatusSeparator, theme.Status, dst.Cols())
			x += runewidth.StringWidth(f.StatusSeparator)
		}
		style := theme.Status
		if e.Accent {
			style = theme.StatusKey
		}
		writeString(dst, x, y, e.Text, style, dst.Cols())
		x += runewidth.StringWidth(e.Text)
	}
}

// drawOverlay puts a panel in the middle of the screen.
//
// A list too long for the window is laid out in two columns rather than being
// cut off. Cutting it drops the entries at the end, which for a key list is
// the half nobody has memorised yet.
func drawOverlay(dst *vt.Grid, lines []string, theme Theme) {
	if len(lines) == 0 {
		return
	}
	title, body := lines[0], lines[1:]

	columns := [][]string{body}
	if len(lines)+4 > dst.Rows() && len(body) > 1 {
		half := (len(body) + 1) / 2
		columns = [][]string{body[:half], body[half:]}
	}

	const gap = 2
	height := 0
	width := runewidth.StringWidth(title)
	inner := 0
	for _, col := range columns {
		height = max(height, len(col))
		colWidth := 0
		for _, line := range col {
			colWidth = max(colWidth, runewidth.StringWidth(line))
		}
		if inner > 0 {
			inner += gap
		}
		inner += colWidth
	}
	width = max(width, inner)

	box := Rect{Cols: min(width+6, dst.Cols()), Rows: min(height+5, dst.Rows())}
	box.X = (dst.Cols() - box.Cols) / 2
	box.Y = (dst.Rows() - box.Rows) / 2

	// Fill first: an overlay that lets the pane behind it show through is
	// unreadable, whatever it says.
	for y := box.Y; y < box.Y+box.Rows; y++ {
		row := dst.Line(y)
		if row == nil {
			continue
		}
		for x := box.X; x < box.X+box.Cols; x++ {
			row.SetCell(x, vt.Cell{R: ' ', Style: theme.Overlay, Width: 1})
		}
	}
	drawBox(dst, box, theme.OverlayTitle)

	limit := box.X + box.Cols - 2
	writeString(dst, box.X+3, box.Y+1, truncate(title, box.Cols-6), theme.OverlayTitle, limit)

	x := box.X + 3
	for _, col := range columns {
		colWidth := 0
		for _, line := range col {
			colWidth = max(colWidth, runewidth.StringWidth(line))
		}
		for i, line := range col {
			y := box.Y + 3 + i
			if y >= box.Y+box.Rows-1 {
				break
			}
			writeString(dst, x, y, truncate(line, limit-x), theme.Overlay, limit)
		}
		x += colWidth + gap
	}
}

// CursorPosition returns where the terminal's cursor belongs: inside the
// focused pane, at that pane's own cursor. Typing has to appear where the user
// is looking, which means the real cursor follows the focused pane's.
//
// While a field is up it belongs in the field instead, for the same reason:
// that is where the keystrokes are going.
func CursorPosition(f Frame, cols, rows int) (x, y int, visible bool) {
	if x, y, ok := PromptCursor(f, cols, rows); ok {
		return x, y, true
	}
	if c := f.CopyCursor; c != nil {
		for _, p := range f.Panes {
			if p.ID != c.Pane {
				continue
			}
			inner := innerRect(p.Rect)
			if c.X < 0 || c.Y < 0 || c.X >= inner.Cols || c.Y >= inner.Rows {
				return 0, 0, false
			}
			return inner.X + c.X, inner.Y + c.Y, true
		}
		return 0, 0, false
	}
	for _, p := range f.Panes {
		if !p.Focused || p.Screen == nil {
			continue
		}
		inner := innerRect(p.Rect)
		if inner.Cols <= 0 || inner.Rows <= 0 {
			return 0, 0, false
		}
		cur := p.Screen.Cursor()
		if cur.X >= inner.Cols || cur.Y >= inner.Rows {
			return 0, 0, false
		}
		return inner.X + cur.X, inner.Y + cur.Y, p.Screen.Modes().CursorVisible && p.Running
	}
	return 0, 0, false
}

// PaneInner is the area inside a pane's border, for a client drawing over a
// pane rather than into the frame — an image, which the terminal draws and
// this package cannot.
func PaneInner(r Rect) Rect { return innerRect(r) }

// innerRect is the area inside a pane's border.
func innerRect(r Rect) Rect {
	return Rect{X: r.X + 1, Y: r.Y + 1, Cols: r.Cols - 2, Rows: r.Rows - 2}
}

// InnerSize is the terminal size a pane of this rect can hold. The client
// resizes panes to it, so the server's terminal matches what is drawn.
func InnerSize(r Rect) (cols, rows int) {
	inner := innerRect(r)
	return max(inner.Cols, 1), max(inner.Rows, 1)
}

func drawPane(dst *vt.Grid, p Pane, theme Theme) {
	if p.Rect.Cols < 2 || p.Rect.Rows < 2 {
		// Too small for a border and anything inside it. Drawing half a frame
		// looks like a glitch; drawing nothing looks like a small pane.
		return
	}

	border := theme.Border
	title := theme.Title
	if p.Focused {
		border = theme.BorderFocused
		title = theme.TitleFocused
	}
	drawBox(dst, p.Rect, border)
	drawPaneTitle(dst, p, title, theme)

	if p.Screen == nil {
		return
	}
	blitScreen(dst, innerRect(p.Rect), p.Screen)
}

// drawPaneTitle writes the label into the top border.
func drawPaneTitle(dst *vt.Grid, p Pane, style vt.Style, theme Theme) {
	label := p.Title
	if label == "" {
		label = p.Agent
	}
	if label == "" {
		label = p.Command
	}
	if label == "" {
		label = "pane"
	}

	prefix := " " + itoa(p.ID) + " "
	text := prefix + label + " "

	// The state marker carries its own colour, so it is written separately
	// rather than folded into the title's style.
	marker := stateMarker(p.State, p.Running)

	avail := p.Rect.Cols - 4
	if avail < 1 {
		return
	}
	text = truncate(text, avail-runewidth.StringWidth(marker))

	x := p.Rect.X + 1
	x = writeString(dst, x, p.Rect.Y, text, style, p.Rect.X+p.Rect.Cols-1)
	writeString(dst, x, p.Rect.Y, marker, theme.StateStyle(p.State, p.Running), p.Rect.X+p.Rect.Cols-1)
}

func stateMarker(state string, running bool) string {
	if !running {
		return "exited "
	}
	switch state {
	case "working":
		return "● "
	case "blocked":
		return "▲ "
	case "idle":
		return "○ "
	default:
		return ""
	}
}

// blitScreen copies a pane's terminal into the grid.
func blitScreen(dst *vt.Grid, area Rect, screen *vt.Screen) {
	if area.Cols <= 0 || area.Rows <= 0 {
		return
	}
	src := screen.Grid()

	for y := 0; y < area.Rows && y < src.Rows(); y++ {
		srcRow := src.Line(y)
		dstRow := dst.Line(area.Y + y)
		if srcRow == nil || dstRow == nil {
			continue
		}
		for x := 0; x < area.Cols && x < srcRow.Len(); x++ {
			cell := srcRow.Cell(x)
			// A wide character whose second half falls outside the area would
			// be drawn with nothing to occupy the column it needs, so it is
			// replaced rather than clipped in half.
			if cell.Width == 2 && x+1 >= area.Cols {
				cell = vt.Cell{R: ' ', Style: cell.Style, Width: 1}
			}
			dstRow.SetCell(area.X+x, cell)
			for _, mark := range srcRow.Combining(x) {
				dstRow.AddCombining(area.X+x, mark)
			}
		}
	}
}

// drawBox draws a single-line frame around the rect.
func drawBox(dst *vt.Grid, r Rect, style vt.Style) {
	right := r.X + r.Cols - 1
	bottom := r.Y + r.Rows - 1

	for x := r.X + 1; x < right; x++ {
		setCell(dst, x, r.Y, '─', style)
		setCell(dst, x, bottom, '─', style)
	}
	for y := r.Y + 1; y < bottom; y++ {
		setCell(dst, r.X, y, '│', style)
		setCell(dst, right, y, '│', style)
	}
	setCell(dst, r.X, r.Y, '┌', style)
	setCell(dst, right, r.Y, '┐', style)
	setCell(dst, r.X, bottom, '└', style)
	setCell(dst, right, bottom, '┘', style)
}

func drawStatus(dst *vt.Grid, f Frame, theme Theme) {
	y := dst.Rows() - StatusRows
	if y < 0 {
		return
	}
	row := dst.Line(y)
	if row == nil {
		return
	}
	// Fill the whole row first, so the bar reads as one band rather than as
	// text floating on the pane above it.
	for x := 0; x < dst.Cols(); x++ {
		row.SetCell(x, vt.Cell{R: ' ', Style: theme.Status, Width: 1})
	}

	limit := dst.Cols()
	x := 0

	if f.Offline {
		x = writeString(dst, x, y, " OFFLINE ", theme.StatusAlert, limit)
	}
	if f.Prefix {
		x = writeString(dst, x, y, " PREFIX ", theme.StatusAlert, limit)
	}
	if f.Navigating {
		x = writeString(dst, x, y, " NAVIGATE ", theme.StatusKey, limit)
	}
	if f.Waiting > 0 {
		x = writeString(dst, x, y, " "+itoa(uint64(f.Waiting))+" waiting ", theme.StatusAlert, limit)
	}

	left := " " + f.Session
	if f.Workspace != "" {
		left += " · " + f.Workspace
	}
	if f.Tab != "" {
		left += " · " + f.Tab
	}
	if f.Zoomed {
		left += " · zoom"
	}
	if f.Resize {
		left += " · resize (hjkl, esc)"
	}
	if f.Copy {
		left += " · copy " + itoa(uint64(f.Scroll)) + "/" + itoa(uint64(f.ScrollDepth))
	} else if f.Scroll > 0 {
		left += " · scroll " + itoa(uint64(f.Scroll)) + "/" + itoa(uint64(f.ScrollDepth))
	}
	x = writeString(dst, x, y, left+"  ", theme.StatusKey, limit)

	if f.Message != "" {
		style := theme.Status
		if f.Alert {
			style = theme.StatusAlert
		}
		writeString(dst, x, y, f.Message, style, limit)
		return
	}

	for _, p := range f.Panes {
		label := itoa(p.ID)
		if p.Agent != "" {
			label += ":" + p.Agent
		}
		style := theme.Status
		if p.Focused {
			style = theme.StatusKey
		}
		x = writeString(dst, x, y, " "+label, style, limit)
		x = writeString(dst, x, y, " "+strings.TrimSpace(stateMarker(p.State, p.Running)), theme.Status, limit)
		if x >= limit {
			break
		}
	}
}

// --- small helpers ---------------------------------------------------------

func setCell(dst *vt.Grid, x, y int, r rune, style vt.Style) {
	row := dst.Line(y)
	if row == nil || x < 0 || x >= row.Len() {
		return
	}
	row.SetCell(x, vt.Cell{R: r, Style: style, Width: 1})
}

// writeString draws text and returns the column after it. Wide characters
// take the two columns they need, so the caller's next write does not land on
// top of one.
func writeString(dst *vt.Grid, x, y int, text string, style vt.Style, limit int) int {
	row := dst.Line(y)
	if row == nil {
		return x
	}
	for _, r := range text {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			row.AddCombining(x-1, r)
			continue
		}
		if x+w > limit || x+w > row.Len() {
			break
		}
		row.SetCell(x, vt.Cell{R: r, Style: style, Width: uint8(w)})
		for i := 1; i < w; i++ {
			row.SetCell(x+i, vt.Cell{Style: style, Width: 0})
		}
		x += w
	}
	return x
}

// truncate shortens text to fit a number of columns, counting the width each
// rune actually occupies rather than the number of runes.
func truncate(text string, cols int) string {
	if cols <= 0 {
		return ""
	}
	if runewidth.StringWidth(text) <= cols {
		return text
	}
	var b strings.Builder
	used := 0
	for _, r := range text {
		w := runewidth.RuneWidth(r)
		if used+w > cols-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	b.WriteRune('…')
	return b.String()
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// fill paints a run of blank cells on one row, which is how a selection band
// is drawn under text that is written over it afterwards.
func fill(dst *vt.Grid, y, from, to int, style vt.Style) {
	row := dst.Line(y)
	if row == nil {
		return
	}
	for x := max(from, 0); x < min(to, dst.Cols()); x++ {
		row.SetCell(x, vt.Cell{R: ' ', Style: style, Width: 1})
	}
}
