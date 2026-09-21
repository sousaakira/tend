package config

import (
	"fmt"
	"strconv"
	"strings"
)

// A key that runs something the user wrote: herdr's [[keys.command]]
// (`config/keybinds.rs`).
//
//	[[keys.command]]
//	key = "g"                  # after the prefix; "prefix+g" means the same
//	type = "pane"              # shell, pane, popup or plugin_action
//	command = "lazygit"
//	description = "git"
//
// shell runs the command in the background, pane runs it in a pane of its
// own that closes when it exits, and plugin_action invokes an installed
// plugin's action by id. popup is accepted and runs as a pane: tend has no
// popups, and a config written for herdr should still do what it says.

// Command types, herdr's.
const (
	CommandShell        = "shell"
	CommandPane         = "pane"
	CommandPopup        = "popup"
	CommandPluginAction = "plugin_action"
)

// CommandKey is one [[keys.command]] entry.
type CommandKey struct {
	Key         string `toml:"key"`
	Command     string `toml:"command"`
	Type        string `toml:"type"`
	Description string `toml:"description,omitempty"`
	// Width and Height size a popup, as herdr's do: a number of cells, or a
	// percentage like "80%". Unset is half the area.
	Width  any `toml:"width,omitempty"`
	Height any `toml:"height,omitempty"`
}

// PopupSize is a popup width or height as written in the settings — a
// number of cells or a percentage — as the text the server reads.
func PopupSize(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.Itoa(int(x))
	case string:
		return x
	}
	return fmt.Sprint(v)
}

// Kind is the entry's type, with herdr's default.
func (c CommandKey) Kind() string {
	if c.Type == "" {
		return CommandShell
	}
	return c.Type
}

// KeyName is the key after the prefix. herdr writes prefix keys as
// "prefix+g"; tend's keys all follow the prefix, so the word is dropped.
func (c CommandKey) KeyName() string {
	return strings.TrimPrefix(strings.TrimSpace(c.Key), "prefix+")
}

// checkCommandKeys refuses entries that could never run. Whether the key is
// one tend can read after the prefix is the interface's to say, as it is for
// keys.bind.
func checkCommandKeys(keys []CommandKey) error {
	for i, c := range keys {
		where := fmt.Sprintf("keys.command[%d]", i)
		if strings.TrimSpace(c.Command) == "" {
			return fmt.Errorf("%s has no command", where)
		}
		if c.KeyName() == "" {
			return fmt.Errorf("%s has no key", where)
		}
		switch c.Kind() {
		case CommandShell, CommandPane, CommandPopup, CommandPluginAction:
		default:
			return fmt.Errorf("%s has type %q; use shell, pane, popup or plugin_action", where, c.Type)
		}
	}
	return nil
}
