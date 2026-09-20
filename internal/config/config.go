// Package config reads tend's settings file.
//
// Every setting has a working default, so the file is optional and a partial
// one is normal: a user who wants a different prefix key should write three
// lines, not a full configuration. An unreadable or invalid file is an error
// rather than a silent fallback — starting with settings the user did not
// write, and not saying so, is worse than refusing to start.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is everything tend can be told.
type Config struct {
	Keys   Keys   `toml:"keys"`
	Pane   Pane   `toml:"pane"`
	UI     UI     `toml:"ui"`
	Server Server `toml:"server"`
}

// Keys configures the keyboard.
type Keys struct {
	// Prefix arms a command. Written as "ctrl+b", or "none" to disable it —
	// which is only sensible when something else is providing the keys.
	Prefix string `toml:"prefix"`
}

// Pane configures new panes.
type Pane struct {
	// Shell is what a pane runs when no command is given. Empty follows
	// $SHELL.
	Shell []string `toml:"shell"`
	// Scrollback is how many lines of history a pane keeps.
	Scrollback int `toml:"scrollback"`
}

// UI configures the interface.
type UI struct {
	Mouse bool `toml:"mouse"`
	// Sidebar shows the spaces and agents down the left edge. On by default:
	// knowing which agent needs you is the reason to run tend, and a list
	// behind a keystroke is one most people never press.
	Sidebar bool `toml:"sidebar"`
	// Grouped lists agents under their tab rather than flat.
	Grouped bool  `toml:"grouped"`
	Theme   Theme `toml:"theme"`
}

// Theme names the colours. Each is a palette name, a number from 0 to 255, or
// a #rrggbb value; empty keeps the default.
type Theme struct {
	Border        string `toml:"border"`
	BorderFocused string `toml:"border_focused"`
	Working       string `toml:"working"`
	Blocked       string `toml:"blocked"`
	Idle          string `toml:"idle"`
}

// Server configures the session server.
type Server struct {
	// DetectInterval is how often panes are re-examined, as "150ms".
	DetectInterval string `toml:"detect_interval"`
}

// Defaults returns the configuration tend uses when told nothing.
func Defaults() Config {
	return Config{
		Keys:   Keys{Prefix: "ctrl+b"},
		Pane:   Pane{Scrollback: 5000},
		UI:     UI{Mouse: true, Sidebar: true},
		Server: Server{DetectInterval: "150ms"},
	}
}

// Path returns the settings file, whether or not it exists.
func Path() (string, error) {
	if override := os.Getenv("TEND_CONFIG"); override != "" {
		return override, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: finding the config directory: %w", err)
	}
	return filepath.Join(dir, "tend", "config.toml"), nil
}

// Load reads the settings file, or returns the defaults when there is none.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Defaults(), err
	}
	cfg, err := LoadFile(path)
	if os.IsNotExist(err) {
		return Defaults(), nil
	}
	return cfg, err
}

// LoadFile reads a specific settings file.
//
// Unknown keys are an error. A misspelled setting that is silently ignored is
// a setting the user believes is in effect, which is the most confusing way
// for configuration to fail.
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Defaults(), err
	}

	cfg := Defaults()
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return Defaults(), fmt.Errorf("config: %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, k := range undecoded {
			keys = append(keys, k.String())
		}
		return Defaults(), fmt.Errorf("config: %s: unknown setting(s): %s",
			path, strings.Join(keys, ", "))
	}
	if err := cfg.validate(); err != nil {
		return Defaults(), fmt.Errorf("config: %s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) validate() error {
	if _, err := c.PrefixKey(); err != nil {
		return err
	}
	if _, err := c.DetectInterval(); err != nil {
		return err
	}
	if c.Pane.Scrollback < 0 {
		return fmt.Errorf("pane.scrollback is %d; it cannot be negative", c.Pane.Scrollback)
	}
	for name, value := range map[string]string{
		"ui.theme.border":         c.UI.Theme.Border,
		"ui.theme.border_focused": c.UI.Theme.BorderFocused,
		"ui.theme.working":        c.UI.Theme.Working,
		"ui.theme.blocked":        c.UI.Theme.Blocked,
		"ui.theme.idle":           c.UI.Theme.Idle,
	} {
		if value == "" {
			continue
		}
		if _, ok := ParseColor(value); !ok {
			return fmt.Errorf("%s: %q is not a colour; use a name, 0-255, or #rrggbb", name, value)
		}
	}
	return nil
}

// PrefixKey returns the prefix as the byte it produces, or zero when disabled.
func (c Config) PrefixKey() (byte, error) {
	name := strings.ToLower(strings.TrimSpace(c.Keys.Prefix))
	switch name {
	case "":
		return 0x02, nil
	case "none", "off":
		return 0, nil
	}

	rest, ok := strings.CutPrefix(name, "ctrl+")
	if !ok || len(rest) != 1 {
		return 0, fmt.Errorf("keys.prefix: %q is not a key; use \"ctrl+<letter>\" or \"none\"", c.Keys.Prefix)
	}
	letter := rest[0]
	if letter < 'a' || letter > 'z' {
		return 0, fmt.Errorf("keys.prefix: %q is not a letter", rest)
	}
	// Ctrl+A is 0x01 and the alphabet follows from there.
	return letter - 'a' + 1, nil
}

// DetectInterval returns how often panes are re-examined.
func (c Config) DetectInterval() (time.Duration, error) {
	if c.Server.DetectInterval == "" {
		return 150 * time.Millisecond, nil
	}
	d, err := time.ParseDuration(c.Server.DetectInterval)
	if err != nil {
		return 0, fmt.Errorf("server.detect_interval: %w", err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("server.detect_interval must be positive, got %s", c.Server.DetectInterval)
	}
	return d, nil
}

// Shell returns the command a pane runs when given none.
func (c Config) Shell() []string {
	if len(c.Pane.Shell) > 0 {
		return c.Pane.Shell
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return []string{sh}
	}
	return []string{"/bin/sh"}
}

// Scrollback returns how many lines of history a pane keeps.
func (c Config) Scrollback() int {
	if c.Pane.Scrollback <= 0 {
		return 5000
	}
	return c.Pane.Scrollback
}

// Color is a parsed colour, in the terminal's own terms.
type Color struct {
	// RGB is set for a #rrggbb value.
	RGB     bool
	R, G, B uint8
	// Index is the palette entry otherwise.
	Index uint8
}

// namedColors are the sixteen every terminal has. They are used rather than
// fixed RGB so that tend follows the palette the user already chose.
var namedColors = map[string]uint8{
	"black": 0, "red": 1, "green": 2, "yellow": 3,
	"blue": 4, "magenta": 5, "cyan": 6, "white": 7,
	"brightblack": 8, "gray": 8, "grey": 8,
	"brightred": 9, "brightgreen": 10, "brightyellow": 11,
	"brightblue": 12, "brightmagenta": 13, "brightcyan": 14, "brightwhite": 15,
}

// ParseColor reads a colour written as a name, a palette number, or #rrggbb.
func ParseColor(value string) (Color, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return Color{}, false
	}

	if hex, ok := strings.CutPrefix(value, "#"); ok {
		if len(hex) != 6 {
			return Color{}, false
		}
		n, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return Color{}, false
		}
		return Color{RGB: true, R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n)}, true
	}

	if idx, ok := namedColors[value]; ok {
		return Color{Index: idx}, true
	}

	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || n > 255 {
		return Color{}, false
	}
	return Color{Index: uint8(n)}, true
}

// Example is a commented settings file, written by "tend config --init" so
// that the options are discoverable without a manual.
const Example = `# tend settings. Every value here is the default; delete what you do not change.

[keys]
# The key that arms a command. "ctrl+<letter>", or "none" to disable it.
prefix = "ctrl+b"

[pane]
# What a pane runs when no command is given. Empty follows $SHELL.
# shell = ["/bin/zsh"]

# How many lines of history a pane keeps.
scrollback = 5000

[ui]
# Click to focus a pane, drag a divider to resize, scroll to look back.
mouse = true

# Show the spaces and agents down the left edge.
sidebar = true

# List agents under their tab rather than flat.
grouped = false

[ui.theme]
# A palette name, a number from 0 to 255, or "#rrggbb".
# border = "brightblack"
# border_focused = "blue"
# working = "yellow"
# blocked = "red"
# idle = "green"

[server]
# How often panes are re-examined for agent state.
detect_interval = "150ms"
`
