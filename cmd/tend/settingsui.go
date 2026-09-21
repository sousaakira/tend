package main

import (
	"strings"

	"github.com/sousaakira/tend/internal/api"
	"github.com/sousaakira/tend/internal/config"
	sessionpkg "github.com/sousaakira/tend/internal/session"
)

// The settings screen: herdr's `prefix+s`, a list of the few settings worth
// changing without opening an editor, each changed where it is seen.
//
// It edits the settings file rather than holding its own copy — a change made
// here is a change somebody will find next time they read the file — and then
// reloads, which is the same path `ctrl+b R` takes. So nothing here has a
// second way of applying a setting that could disagree with the first.
//
// It is not the whole file. The file is the whole file, and it is commented;
// this is the handful of things people change while using tend.

// settingRow is one line in the screen.
type settingRow struct {
	label string
	// section and key name the setting in the file.
	section, key string
	// choices are what it can be, as shown and as written.
	choices []settingChoice
	// value reads the current one out of a configuration.
	value func(config.Config) string
}

type settingChoice struct {
	label string
	// toml is the value as it goes into the file.
	toml string
}

// settingRows is the screen, in order.
var settingRows = []settingRow{
	{
		label: "sidebar", section: "ui", key: "sidebar",
		choices: []settingChoice{{"on", "true"}, {"off", "false"}},
		value:   func(c config.Config) string { return config.Bool(c.UI.Sidebar) },
	},
	{
		label: "agent list", section: "ui", key: "grouped",
		choices: []settingChoice{{"flat", "false"}, {"grouped", "true"}},
		value:   func(c config.Config) string { return config.Bool(c.UI.Grouped) },
	},
	{
		label: "mouse", section: "ui", key: "mouse",
		choices: []settingChoice{{"on", "true"}, {"off", "false"}},
		value:   func(c config.Config) string { return config.Bool(c.UI.Mouse) },
	},
	{
		label: "notifications", section: "notify", key: "toasts",
		choices: []settingChoice{{"status line", `"tend"`}, {"terminal", `"terminal"`}, {"off", `"off"`}},
		value:   func(c config.Config) string { return config.Quote(c.Toasts()) },
	},
	{
		label: "notify focused pane", section: "notify", key: "focused",
		choices: []settingChoice{{"no", "false"}, {"yes", "true"}},
		value:   func(c config.Config) string { return config.Bool(c.Notify.Focused) },
	},
	{
		label: "sound", section: "sound", key: "enabled",
		choices: []settingChoice{{"off", "false"}, {"on", "true"}},
		value:   func(c config.Config) string { return config.Bool(c.Sound.Enabled) },
	},
	{
		label: "keep the session", section: "server", key: "persist",
		choices: []settingChoice{{"yes", "true"}, {"no", "false"}},
		value:   func(c config.Config) string { return config.Bool(c.Server.Persist) },
	},
}

// settingsState is the screen while it is open.
type settingsState struct {
	row int
}

// openSettings shows the screen, or closes it if it is already up.
func (t *tui) openSettings() error {
	t.mu.Lock()
	open := t.settings != nil
	if open {
		t.settings = nil
		t.overlay = nil
	} else {
		t.settings = &settingsState{}
	}
	t.dirty = true
	t.mu.Unlock()
	if !open {
		t.drawSettings()
	}
	t.wakeUp()
	return nil
}

// settingsUp reports whether the screen has the keyboard.
func (t *tui) settingsUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.settings != nil
}

// settingsKeys drives the screen. Every key is its own while it is up.
func (t *tui) settingsKeys(data []byte) (bool, error) {
	for _, key := range splitKeys(data) {
		switch key {
		case "\x1b", "q", "\x03", "s":
			t.mu.Lock()
			t.settings, t.overlay = nil, nil
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
			return true, nil
		case "k", "\x1b[A":
			t.moveSetting(-1)
		case "j", "\x1b[B":
			t.moveSetting(1)
		case "h", "\x1b[D":
			if err := t.changeSetting(-1); err != nil {
				return true, err
			}
		case "l", "\x1b[C", "\r", " ":
			if err := t.changeSetting(1); err != nil {
				return true, err
			}
		}
	}
	return true, nil
}

func (t *tui) moveSetting(delta int) {
	t.mu.Lock()
	if t.settings != nil {
		t.settings.row = (t.settings.row + delta + len(settingRows)) % len(settingRows)
	}
	t.mu.Unlock()
	t.drawSettings()
}

// changeSetting moves one setting to the next choice, writes it, and reloads.
func (t *tui) changeSetting(delta int) error {
	t.mu.Lock()
	if t.settings == nil {
		t.mu.Unlock()
		return nil
	}
	row := settingRows[t.settings.row]
	current := row.value(t.config)
	t.mu.Unlock()

	at := 0
	for i, choice := range row.choices {
		if choice.toml == current {
			at = i
		}
	}
	next := row.choices[(at+delta+len(row.choices))%len(row.choices)]

	if err := config.Set(row.section, row.key, next.toml); err != nil {
		t.setMessage(err.Error(), true)
		return nil
	}
	// Through the same reload the key uses, so a setting cannot be applied
	// here in a way that differs from applying it from the file.
	if err := t.reloadSettings(); err != nil {
		return err
	}
	t.setMessage(row.label+": "+next.label, false)
	t.drawSettings()
	return nil
}

// drawSettings renders the screen into the overlay panel.
func (t *tui) drawSettings() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.settings == nil {
		return
	}

	lines := []string{"settings", ""}
	for i, row := range settingRows {
		current := row.value(t.config)
		var shown []string
		for _, choice := range row.choices {
			label := choice.label
			if choice.toml == current {
				label = "[" + label + "]"
			}
			shown = append(shown, label)
		}
		marker := "  "
		if i == t.settings.row {
			marker = "▸ "
		}
		lines = append(lines, marker+pad(row.label, 20)+strings.Join(shown, " "))
	}
	path, _ := config.Path()
	lines = append(lines,
		"",
		"j k  move     h l  change     q  close",
		"written to "+path,
	)
	t.overlay = lines
	t.dirty = true
}

// pad is the column padding the overlay uses, spelled here because ui's is
// not exported and this is the only other place that needs it.
func pad(s string, width int) string {
	for len([]rune(s)) < width {
		s += " "
	}
	return s
}

// newWorktreeHere makes a worktree for the repository the focused space is in
// and opens it as a space of its own: herdr's prefix+shift+g.
//
// Through the automation socket, which is where worktrees live, rather than a
// second implementation over the client protocol. It takes seconds — git has
// to check the files out — so it runs off the input goroutine and says how it
// went when it is done.
func (t *tui) newWorktreeHere() error {
	t.mu.Lock()
	workspace := t.workspace
	session := t.session
	t.mu.Unlock()
	if workspace == 0 {
		return nil
	}

	t.setMessage("making a worktree…", false)
	go func() {
		result, err := apiCall(session, api.MethodWorktreeCreate, map[string]any{
			"workspace_id": api.WorkspaceID(sessionpkg.WorkspaceID(workspace)),
		}, true)
		if err != nil {
			t.setMessage(err.Error(), true)
			return
		}
		wt, _ := result["worktree"].(map[string]any)
		t.setMessage("worktree "+text(wt["branch"])+" opened", false)
		if err := t.refresh(); err != nil {
			t.setMessage(err.Error(), true)
		}
	}()
	return nil
}
