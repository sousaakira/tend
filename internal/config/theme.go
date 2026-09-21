package config

import "strings"

// ThemeNames are the palettes tend ships, in the order a list shows them.
// They are herdr's (`config/theme.rs` THEME_NAMES), so a name that works in
// one works in the other.
var ThemeNames = []string{
	"catppuccin",
	"catppuccin-latte",
	"terminal",
	"tokyo-night",
	"tokyo-night-day",
	"dracula",
	"nord",
	"gruvbox",
	"gruvbox-light",
	"one-dark",
	"one-light",
	"solarized",
	"solarized-light",
	"kanagawa",
	"kanagawa-lotus",
	"rose-pine",
	"rose-pine-dawn",
	"vesper",
}

// themeAliases are the other spellings herdr accepts for the same palettes.
// People write the name they know the theme by, and "tokyonight" or
// "catppuccin-mocha" refusing to load would be a pedantry nobody asked for.
var themeAliases = map[string]string{
	"catppuccin-mocha": "catppuccin",
	"latte":            "catppuccin-latte",
	"light":            "catppuccin-latte",
	"tokyonight":       "tokyo-night",
	"tokyo-day":        "tokyo-night-day",
	"tokyonight-day":   "tokyo-night-day",
	"gruvbox-dark":     "gruvbox",
	"onedark":          "one-dark",
	"onelight":         "one-light",
	"solarized-dark":   "solarized",
	"lotus":            "kanagawa-lotus",
	"rosepine":         "rose-pine",
	"rosepine-dawn":    "rose-pine-dawn",
	"dawn":             "rose-pine-dawn",
}

// CanonicalTheme is the name a theme is known by, from any spelling of it:
// case, spaces and underscores do not matter, as in herdr. It reports false
// for a name that is no theme.
func CanonicalTheme(name string) (string, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.NewReplacer(" ", "-", "_", "-").Replace(n)
	if alias, ok := themeAliases[n]; ok {
		return alias, true
	}
	for _, known := range ThemeNames {
		if n == known {
			return known, true
		}
	}
	return "", false
}
