// Package plugin is the host for things installed beside tend that extend it.
//
// A plugin is a directory with a `tend-plugin.toml` in it. The manifest says
// what to build, what to run at startup, what commands the user can invoke,
// what to run when something happens in the session, and what panes it offers.
// Everything a plugin does to the session it does through the automation
// socket, which is the same door a script uses — in herdr's words, the entire
// CLI is the plugin API, and this package adds no second one.
//
// The shape is herdr's (`app/api/plugins/manifest.rs`), with `herdr` in the
// names replaced. A manifest written for herdr is one rename away from working
// here, which is the point: the panel in the owner's herdr is a plugin, not
// part of herdr.
package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

// ManifestName is the file that makes a directory a plugin.
const ManifestName = "tend-plugin.toml"

// Limits on what a manifest may name. They are herdr's, and they exist so that
// a plugin cannot make the session's own lists unreadable.
const (
	maxIDChars     = 120
	maxTitleChars  = 120
	maxCommandArgs = 64
)

// ErrNoManifest means the directory holds no plugin.
var ErrNoManifest = errors.New("plugin: no " + ManifestName + " in that directory")

// Manifest is a plugin as its author declared it.
type Manifest struct {
	ID          string `toml:"id" json:"id"`
	Name        string `toml:"name" json:"name"`
	Version     string `toml:"version" json:"version"`
	Description string `toml:"description,omitempty" json:"description,omitempty"`
	// MinVersion is the oldest tend this plugin says it works with. It is
	// recorded and reported, not enforced: tend's builds are git descriptions
	// with no ordering, so there is nothing to compare against.
	MinVersion string   `toml:"min_tend_version,omitempty" json:"min_tend_version,omitempty"`
	Platforms  []string `toml:"platforms,omitempty" json:"platforms,omitempty"`

	Build   []Step   `toml:"build,omitempty" json:"build,omitempty"`
	Startup []Step   `toml:"startup,omitempty" json:"startup,omitempty"`
	Actions []Action `toml:"actions,omitempty" json:"actions,omitempty"`
	Events  []Hook   `toml:"events,omitempty" json:"events,omitempty"`
	Panes   []Pane   `toml:"panes,omitempty" json:"panes,omitempty"`
}

// Step is a command with no name of its own: a build, or something to run when
// the server starts.
type Step struct {
	Platforms []string `toml:"platforms,omitempty" json:"platforms,omitempty"`
	Command   []string `toml:"command" json:"command"`
}

// Action is a command the user can invoke by name.
type Action struct {
	ID          string   `toml:"id" json:"id"`
	Title       string   `toml:"title" json:"title"`
	Description string   `toml:"description,omitempty" json:"description,omitempty"`
	Platforms   []string `toml:"platforms,omitempty" json:"platforms,omitempty"`
	Command     []string `toml:"command" json:"command"`
}

// Hook is a command run when something happens in the session.
type Hook struct {
	On        string   `toml:"on" json:"on"`
	Platforms []string `toml:"platforms,omitempty" json:"platforms,omitempty"`
	Command   []string `toml:"command" json:"command"`
}

// Pane is a pane the plugin offers to open.
type Pane struct {
	ID          string   `toml:"id" json:"id"`
	Title       string   `toml:"title" json:"title"`
	Description string   `toml:"description,omitempty" json:"description,omitempty"`
	Platforms   []string `toml:"platforms,omitempty" json:"platforms,omitempty"`
	// Placement is "split" or "tab". herdr also has popups, which tend has no
	// equivalent for yet.
	Placement string   `toml:"placement,omitempty" json:"placement,omitempty"`
	Command   []string `toml:"command" json:"command"`
}

// Events a hook may name. A manifest naming anything else is kept, and the
// unknown name is reported as a warning: a plugin written for a newer tend
// should still install and do the rest of what it does.
var knownEvents = map[string]bool{
	"pane.opened":       true,
	"pane.closed":       true,
	"pane.exited":       true,
	"agent.state":       true,
	"pane.clipboard":    true,
	"pane.focused":      true,
	"tab.focused":       true,
	"tab.created":       true,
	"workspace.focused": true,
	"workspace.created": true,
}

// KnownEvents is the set a hook may wait for, for anything that has to list
// them.
func KnownEvents() []string {
	out := make([]string, 0, len(knownEvents))
	for name := range knownEvents {
		out = append(out, name)
	}
	return out
}

// Load reads a plugin's manifest from its directory.
//
// Warnings are things worth telling the user that are not reasons to refuse
// the plugin: an event nobody has heard of, a platform list that excludes this
// machine. An error is a manifest that cannot be acted on at all.
func Load(root string) (Manifest, []string, error) {
	path := filepath.Join(root, ManifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Manifest{}, nil, fmt.Errorf("%w: %s", ErrNoManifest, root)
		}
		return Manifest{}, nil, err
	}
	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return Manifest{}, nil, fmt.Errorf("plugin: reading %s: %w", path, err)
	}
	warnings, err := m.check()
	return m, warnings, err
}

// check validates a manifest and collects its warnings.
func (m *Manifest) check() ([]string, error) {
	if err := checkID("id", m.ID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(m.Name) == "" {
		m.Name = m.ID
	}
	if strings.TrimSpace(m.Version) == "" {
		return nil, errors.New("plugin: the manifest has no version")
	}

	var warnings []string
	if !platformAllowed(m.Platforms) {
		warnings = append(warnings, fmt.Sprintf(
			"this plugin declares itself for %s, and this is %s",
			strings.Join(m.Platforms, ", "), runtime.GOOS))
	}

	for i, step := range m.Build {
		if err := checkCommand(fmt.Sprintf("build %d", i+1), step.Command); err != nil {
			return nil, err
		}
	}
	for i, step := range m.Startup {
		if err := checkCommand(fmt.Sprintf("startup %d", i+1), step.Command); err != nil {
			return nil, err
		}
	}

	seen := map[string]bool{}
	for i, a := range m.Actions {
		if err := checkID(fmt.Sprintf("action %d", i+1), a.ID); err != nil {
			return nil, err
		}
		if seen[a.ID] {
			return nil, fmt.Errorf("plugin: two actions are called %q", a.ID)
		}
		seen[a.ID] = true
		if strings.TrimSpace(a.Title) == "" {
			m.Actions[i].Title = a.ID
		}
		if len(a.Title) > maxTitleChars {
			m.Actions[i].Title = a.Title[:maxTitleChars]
		}
		if err := checkCommand("action "+a.ID, a.Command); err != nil {
			return nil, err
		}
	}

	for _, h := range m.Events {
		if err := checkCommand("event "+h.On, h.Command); err != nil {
			return nil, err
		}
		if !knownEvents[h.On] {
			warnings = append(warnings, fmt.Sprintf(
				"this build has no event called %q, so that hook never runs", h.On))
		}
	}

	seen = map[string]bool{}
	for i, p := range m.Panes {
		if err := checkID(fmt.Sprintf("pane %d", i+1), p.ID); err != nil {
			return nil, err
		}
		if seen[p.ID] {
			return nil, fmt.Errorf("plugin: two panes are called %q", p.ID)
		}
		seen[p.ID] = true
		if strings.TrimSpace(p.Title) == "" {
			m.Panes[i].Title = p.ID
		}
		switch p.Placement {
		case "", "split":
			m.Panes[i].Placement = "split"
		case "tab":
		case "popup":
			m.Panes[i].Placement = "split"
			warnings = append(warnings, fmt.Sprintf(
				"pane %q asks for a popup, which tend has no equivalent for; it opens as a split", p.ID))
		default:
			return nil, fmt.Errorf("plugin: pane %q asks for placement %q, which is not split or tab", p.ID, p.Placement)
		}
		if err := checkCommand("pane "+p.ID, p.Command); err != nil {
			return nil, err
		}
	}
	return warnings, nil
}

// checkID refuses a name that could not be typed, referred to, or shown.
func checkID(what, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("plugin: %s has no id", what)
	}
	if len(id) > maxIDChars {
		return fmt.Errorf("plugin: %s has an id of %d characters, the limit is %d", what, len(id), maxIDChars)
	}
	for _, r := range id {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '.'
		if !ok {
			return fmt.Errorf("plugin: %s has an id with %q in it; use letters, digits, - . _", what, r)
		}
	}
	return nil
}

func checkCommand(what string, command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("plugin: %s has no command", what)
	}
	if len(command) > maxCommandArgs {
		return fmt.Errorf("plugin: %s has %d arguments, the limit is %d", what, len(command), maxCommandArgs)
	}
	if strings.TrimSpace(command[0]) == "" {
		return fmt.Errorf("plugin: %s starts with an empty argument", what)
	}
	return nil
}

// platformAllowed reports whether a list of platforms includes this one. An
// empty list means every platform, which is what most manifests say.
func platformAllowed(platforms []string) bool {
	if len(platforms) == 0 {
		return true
	}
	for _, p := range platforms {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case runtime.GOOS:
			return true
		case "macos":
			if runtime.GOOS == "darwin" {
				return true
			}
		case "unix":
			if runtime.GOOS != "windows" {
				return true
			}
		}
	}
	return false
}

// ForThisPlatform picks the entries that apply here.
func ForThisPlatform[T any](items []T, platforms func(T) []string) []T {
	var out []T
	for _, item := range items {
		if platformAllowed(platforms(item)) {
			out = append(out, item)
		}
	}
	return out
}
