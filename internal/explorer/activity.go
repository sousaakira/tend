package explorer

import (
	"github.com/mattn/go-runewidth"

	"github.com/sousaakira/tend/internal/vt"
)

// The activity bar is herdr-sidebar's (`explorer_app.rs`, draw_activity_bar):
// the three views as icons on the left, each in a chip, and a gear on the
// right that opens the panel's settings. It is three rows tall. The outer
// rows are blank except under the view shown, whose chip reaches into them
// by half a block, which makes it a tall button with room around it
// rather than a strip.
//
// It is drawn only with an icon theme set: without one there are no icons
// to put on it, and the header stays the one line of names it has always
// been.

// barRows is how many rows the activity bar adds over the one-line header
// it replaces: the list starts this much lower.
const barRows = 2

// barActive reports whether the activity bar is drawn: with icons on, in
// the modes that show the header, and in a pane tall enough to spare it.
func (m *Model) barActive() bool {
	if m.settings.Icons != IconsNerd && m.settings.Icons != IconsEmoji {
		return false
	}
	switch m.mode {
	case modeList, modeHelp, modeCommit, modePrompt:
	default:
		return false
	}
	return m.rows >= 4+barRows
}

// gearIcon is the settings glyph, herdr-sidebar's gear_icon.
func gearIcon(theme string) string {
	if theme == IconsEmoji {
		return "⚙"
	}
	return "\uf013" // nf-fa-cog
}

// barZone is one button on the bar: the columns it covers, [from, to).
type barZone struct{ from, to int }

// barLayout is where the bar's buttons are, the one description both
// drawing and a click read. Each view's chip is " icon  " with a Nerd Font
// — herdr-sidebar keeps a second cell for glyphs the non-Mono font draws
// two wide, so the chips are the same size either way — and " icon " with
// emoji, which are two wide already; a space before each. The gear is " ⚙ "
// against the right edge.
func (m *Model) barLayout() (views [3]barZone, gear barZone) {
	slack := ""
	if m.settings.Icons == IconsNerd {
		slack = " "
	}
	x := 0
	for i := range views {
		x++ // the space before each chip
		w := runewidth.StringWidth(" " + viewIcon(View(i), m.settings.Icons) + slack + " ")
		views[i] = barZone{x, x + w}
		x += w
	}
	gw := runewidth.StringWidth(" " + gearIcon(m.settings.Icons) + " ")
	gear = barZone{m.cols - gw, m.cols}
	return views, gear
}

// Chip colours: the view shown in the selection's, herdr-sidebar's
// DarkGray under the icon in the text's own colour; the others dimmed, as
// its idle buttons are.
var (
	styleChipOn  = styleSel
	styleChipCap = vt.Style{FG: styleSel.BG}
)

func (m *Model) drawActivityBar(g *vt.Grid) {
	views, gear := m.barLayout()
	for i, z := range views {
		on := View(i) == m.view
		style := styleDim
		if on {
			style = styleChipOn
			fill(g, 1, z.from, z.to, style)
			for x := z.from; x < z.to; x++ {
				put(g, x, 0, "▄", styleChipCap, m.cols)
				put(g, x, 2, "▀", styleChipCap, m.cols)
			}
		}
		put(g, z.from+1, 1, viewIcon(View(i), m.settings.Icons), style, gear.from)
	}
	put(g, gear.from+1, 1, gearIcon(m.settings.Icons), styleDim, m.cols)
}

// barClick acts on a press on the bar's rows: a view's chip switches to it,
// the gear opens the settings. It reports whether the press was the bar's.
func (m *Model) barClick(ev Mouse) bool {
	if ev.Y > barRows {
		return false
	}
	if !ev.Press || ev.Button != 0 || (m.mode != modeList && m.mode != modeHelp) {
		return true
	}
	views, gear := m.barLayout()
	if ev.X >= gear.from && ev.X < gear.to {
		m.openSettings()
		return true
	}
	for i, z := range views {
		if ev.X >= z.from && ev.X < z.to {
			m.mode = modeList
			if View(i) == ViewSearch {
				m.openContentSearch()
			} else {
				m.switchView(View(i))
			}
		}
	}
	return true
}
