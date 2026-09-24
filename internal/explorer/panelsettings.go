package explorer

import (
	"strconv"

	"github.com/sousaakira/tend/internal/vt"
)

// The panel's settings, opened by the gear on the activity bar, as
// herdr-sidebar's gear opens its own (`explorer_app.rs`, draw_settings):
// each setting with its value, enter or a click moving it to the next. They
// are the [files] rows of tend's settings screen, written to the same file,
// so either place shows what the other chose.

// settingOption is one value a setting can take: as shown, and as written
// to the settings file (TOML).
type settingOption struct{ label, toml string }

// panelSetting is one row: its key in [files], and what it can be.
type panelSetting struct {
	label, key string
	options    []settingOption
	// current is the option now chosen, as its toml.
	current func(Settings) string
	// later, when set, is said after a change: the panel cannot move or
	// resize the pane it is in, so a side or a width is how it opens next.
	later bool
}

var panelSettings = []panelSetting{
	{
		label: "icons", key: "icons",
		options: []settingOption{{"none", `"none"`}, {"nerd font", `"nerd"`}, {"emoji", `"emoji"`}},
		current: func(s Settings) string {
			if s.Icons == "" {
				return `"none"`
			}
			return strconv.Quote(s.Icons)
		},
	},
	{
		label: "open", key: "auto_open",
		options: []settingOption{{"in every tab", "true"}, {"when asked", "false"}},
		current: func(s Settings) string { return strconv.FormatBool(s.AutoOpen) },
	},
	{
		label: "follow", key: "follow",
		options: []settingOption{{"the pane beside", "true"}, {"stay put", "false"}},
		current: func(s Settings) string { return strconv.FormatBool(s.Follow) },
	},
	{
		label: "dotfiles", key: "hidden",
		options: []settingOption{{"shown", "false"}, {"hidden", "true"}},
		current: func(s Settings) string { return strconv.FormatBool(s.Hidden) },
	},
	{
		label: "side", key: "dock",
		options: []settingOption{{"right", `"right"`}, {"left", `"left"`}},
		current: func(s Settings) string {
			if s.Dock == "left" {
				return `"left"`
			}
			return `"right"`
		},
		later: true,
	},
	{
		label: "width", key: "width",
		options: []settingOption{{"28", "28"}, {"32", "32"}, {"40", "40"}, {"48", "48"}},
		current: func(s Settings) string { return strconv.Itoa(s.Width) },
		later:   true,
	},
}

// optionIndex is which of a setting's options is chosen, or -1 for a value
// that is none of them (a width typed into the file by hand).
func (p panelSetting) optionIndex(s Settings) int {
	now := p.current(s)
	for i, o := range p.options {
		if o.toml == now {
			return i
		}
	}
	return -1
}

// SetSettingsWriter gives the panel a way to write one of its settings:
// key in [files], value as TOML. Without one the gear's settings are shown
// and cannot be changed.
func (m *Model) SetSettingsWriter(fn func(key, value string) error) { m.writeSetting = fn }

func (m *Model) openSettings() {
	m.settingsCursor = 0
	m.mode = modeSettings
}

// cycleSetting moves a setting to its next value, writes it, and applies it
// here at once rather than at the settings file's next look.
func (m *Model) cycleSetting(i int) {
	p := panelSettings[i]
	if m.writeSetting == nil {
		m.say("settings cannot be written from here", true)
		return
	}
	next := p.options[(p.optionIndex(m.settings)+1)%len(p.options)]
	if err := m.writeSetting(p.key, next.toml); err != nil {
		m.say("settings: "+err.Error(), true)
		return
	}
	s := m.settings
	switch p.key {
	case "icons":
		s.Icons, _ = strconv.Unquote(next.toml)
	case "follow":
		s.Follow = next.toml == "true"
	case "auto_open":
		s.AutoOpen = next.toml == "true"
	case "hidden":
		s.Hidden = next.toml == "true"
	case "dock":
		s.Dock, _ = strconv.Unquote(next.toml)
	case "width":
		s.Width, _ = strconv.Atoi(next.toml)
	}
	m.Configure(s)
	if p.later {
		m.say(p.label+": "+next.label+", when the panel opens again", false)
	}
}

func (m *Model) settingsKey(k Key) {
	switch k.Name {
	case "esc", "q", "ctrl+c":
		m.mode = modeList
	case "up", "k":
		m.settingsCursor = (m.settingsCursor + len(panelSettings) - 1) % len(panelSettings)
	case "down", "j":
		m.settingsCursor = (m.settingsCursor + 1) % len(panelSettings)
	case "enter", "space", "right", "l":
		m.cycleSetting(m.settingsCursor)
	}
}

// settingsTop is the row the first setting is drawn on.
const settingsTop = 2

func (m *Model) settingsMouse(ev Mouse) {
	if !ev.Press {
		return
	}
	i := ev.Y - settingsTop
	if i < 0 || i >= len(panelSettings) {
		m.mode = modeList
		return
	}
	m.settingsCursor = i
	m.cycleSetting(i)
}

func (m *Model) drawSettings(g *vt.Grid) {
	put(g, 1, 0, "Settings", styleAccent, m.cols)
	for i, p := range panelSettings {
		y := settingsTop + i
		if y >= m.rows-2 {
			break
		}
		value := p.current(m.settings)
		if j := p.optionIndex(m.settings); j >= 0 {
			value = p.options[j].label
		}
		label, valueStyle := styleNormal, styleAccent
		if i == m.settingsCursor {
			fill(g, y, 0, m.cols, styleSel)
			label, valueStyle = styleSel, styleSel
		}
		put(g, 2, y, p.label, label, m.cols)
		put(g, max(m.cols-1-len([]rune(value)), 2+len(p.label)+1), y, value, valueStyle, m.cols)
	}
	if m.message != "" {
		style := styleDim
		if m.messageErr {
			style = styleErr
		}
		put(g, 1, m.rows-2, truncate(m.message, m.cols-2), style, m.cols)
	}
	put(g, 1, m.rows-1, truncate("enter change  esc", m.cols-2), styleDim, m.cols)
}
