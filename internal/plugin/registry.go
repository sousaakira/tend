package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The registry is the list of plugins this machine has, kept in a file so that
// a plugin survives the server it was linked into. herdr keeps the same list
// in `plugins.json` beside its config; so does tend.
//
// Linking records a directory rather than copying it. A plugin is usually a
// checkout the user is also editing, and a copy would be a second version that
// silently goes stale — herdr links, and the sidebar plugin in the owner's
// herdr is a linked checkout under ~/.config/herdr/plugins.

var (
	// ErrNotInstalled means no plugin by that id is linked.
	ErrNotInstalled = errors.New("plugin: not installed")
	// ErrAlreadyInstalled means one is, from somewhere else.
	ErrAlreadyInstalled = errors.New("plugin: already installed")
)

// Installed is one entry in the registry.
type Installed struct {
	Manifest
	// Root is the directory the manifest was read from. Commands are resolved
	// against it, which is what lets a manifest say "./target/release/thing".
	Root     string `json:"root"`
	Enabled  bool   `json:"enabled"`
	LinkedAt string `json:"linked_at,omitempty"`
	// Warnings are what was wrong with the manifest that was not bad enough to
	// refuse it. They are kept so `tend plugin list` can show them long after
	// the link, rather than only printing them once.
	Warnings []string `json:"warnings,omitempty"`
	// Source is where it was installed from, when that was GitHub; nil for a
	// plugin linked from a directory of the user's own.
	Source *Source `json:"source,omitempty"`
}

// Registry is the set of installed plugins, backed by a file.
type Registry struct {
	path string

	mu      sync.Mutex
	plugins map[string]*Installed
}

// OpenRegistry reads the registry at path, creating nothing. A missing file is
// an empty registry, which is the ordinary case.
func OpenRegistry(path string) (*Registry, error) {
	r := &Registry{path: path, plugins: map[string]*Installed{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return r, nil
		}
		return nil, err
	}
	var file struct {
		Plugins []*Installed `json:"plugins"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		// A damaged registry is reported, not obeyed. Refusing to start over
		// it would make one bad file into a session nobody can run.
		return r, fmt.Errorf("plugin: ignoring %s: %w", path, err)
	}
	for _, p := range file.Plugins {
		if p != nil && p.ID != "" {
			r.plugins[p.ID] = p
		}
	}
	return r, nil
}

// List returns the installed plugins, in a stable order.
func (r *Registry) List() []Installed {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Installed, 0, len(r.plugins))
	for _, p := range r.plugins {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns one plugin.
func (r *Registry) Get(id string) (Installed, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.plugins[id]
	if !ok {
		return Installed{}, false
	}
	return *p, true
}

// Enabled returns the plugins that are installed and turned on, in order.
func (r *Registry) Enabled() []Installed {
	var out []Installed
	for _, p := range r.List() {
		if p.Enabled {
			out = append(out, p)
		}
	}
	return out
}

// Link records a plugin directory. The manifest is read now: a directory that
// is not a plugin should fail here, where somebody is watching, rather than at
// the next server start.
func (r *Registry) Link(dir string) (Installed, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return Installed{}, err
	}
	manifest, warnings, err := Load(root)
	if err != nil {
		return Installed{}, err
	}

	r.mu.Lock()
	if existing, ok := r.plugins[manifest.ID]; ok && existing.Root != root {
		r.mu.Unlock()
		return Installed{}, fmt.Errorf("%w: %s is linked from %s", ErrAlreadyInstalled, manifest.ID, existing.Root)
	}
	entry := &Installed{
		Manifest: manifest, Root: root, Enabled: true,
		LinkedAt: time.Now().UTC().Format(time.RFC3339), Warnings: warnings,
	}
	r.plugins[manifest.ID] = entry
	r.mu.Unlock()

	if err := r.save(); err != nil {
		return Installed{}, err
	}
	return *entry, nil
}

// SetSource records where an installed plugin came from.
func (r *Registry) SetSource(id string, src Source) error {
	r.mu.Lock()
	entry, ok := r.plugins[id]
	if ok {
		entry.Source = &src
	}
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotInstalled, id)
	}
	return r.save()
}

// ByGithubSource is the plugin installed from owner/repo[/subdir], if one is.
func (r *Registry) ByGithubSource(g GithubSource) (Installed, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.plugins {
		s := p.Source
		if s != nil && s.Kind == "github" && s.Owner == g.Owner && s.Repo == g.Repo && s.Subdir == g.Subdir {
			return *p, true
		}
	}
	return Installed{}, false
}

// Reload re-reads an installed plugin's manifest, for a plugin the user has
// edited since linking it.
func (r *Registry) Reload(id string) (Installed, error) {
	r.mu.Lock()
	entry, ok := r.plugins[id]
	if !ok {
		r.mu.Unlock()
		return Installed{}, fmt.Errorf("%w: %s", ErrNotInstalled, id)
	}
	root, enabled := entry.Root, entry.Enabled
	r.mu.Unlock()

	manifest, warnings, err := Load(root)
	if err != nil {
		return Installed{}, err
	}
	if manifest.ID != id {
		return Installed{}, fmt.Errorf("plugin: %s now calls itself %s; unlink it and link it again", id, manifest.ID)
	}

	r.mu.Lock()
	entry.Manifest, entry.Warnings, entry.Enabled = manifest, warnings, enabled
	updated := *entry
	r.mu.Unlock()

	if err := r.save(); err != nil {
		return Installed{}, err
	}
	return updated, nil
}

// Unlink forgets a plugin. The directory is left alone: it is the user's, and
// tend did not put it there.
func (r *Registry) Unlink(id string) error {
	r.mu.Lock()
	if _, ok := r.plugins[id]; !ok {
		r.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrNotInstalled, id)
	}
	delete(r.plugins, id)
	r.mu.Unlock()
	return r.save()
}

// SetEnabled turns a plugin on or off without forgetting where it is.
func (r *Registry) SetEnabled(id string, enabled bool) error {
	r.mu.Lock()
	entry, ok := r.plugins[id]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrNotInstalled, id)
	}
	entry.Enabled = enabled
	r.mu.Unlock()
	return r.save()
}

// save writes the registry, replacing the file in one step so that a crash
// mid-write cannot leave half a list behind.
func (r *Registry) save() error {
	r.mu.Lock()
	list := make([]*Installed, 0, len(r.plugins))
	for _, p := range r.plugins {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	data, err := json.MarshalIndent(struct {
		Plugins []*Installed `json:"plugins"`
	}{list}, "", "  ")
	r.mu.Unlock()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(r.path), ".plugins-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	return os.Rename(name, r.path)
}

// Action finds an action by id, in whichever enabled plugin has it. An id that
// two plugins share is reported rather than guessed at.
func (r *Registry) Action(id string) (Installed, Action, error) {
	var (
		found  Installed
		action Action
		count  int
	)
	for _, p := range r.Enabled() {
		for _, a := range p.Actions {
			if a.ID == id && platformAllowed(a.Platforms) {
				found, action, count = p, a, count+1
			}
		}
	}
	switch count {
	case 0:
		return Installed{}, Action{}, fmt.Errorf("plugin: no enabled plugin has an action called %q", id)
	case 1:
		return found, action, nil
	}
	return Installed{}, Action{}, fmt.Errorf("plugin: %d plugins have an action called %q", count, id)
}

// Pane finds a pane offered by a plugin. The name may be "plugin:pane" or just
// the pane's id when only one plugin offers it.
func (r *Registry) Pane(name string) (Installed, Pane, error) {
	pluginID, paneID, qualified := strings.Cut(name, ":")
	if !qualified {
		paneID = name
	}
	var (
		found Installed
		pane  Pane
		count int
	)
	for _, p := range r.Enabled() {
		if qualified && p.ID != pluginID {
			continue
		}
		for _, candidate := range p.Panes {
			if candidate.ID == paneID && platformAllowed(candidate.Platforms) {
				found, pane, count = p, candidate, count+1
			}
		}
	}
	switch count {
	case 0:
		return Installed{}, Pane{}, fmt.Errorf("plugin: no enabled plugin offers a pane called %q", name)
	case 1:
		return found, pane, nil
	}
	return Installed{}, Pane{}, fmt.Errorf("plugin: %d plugins offer a pane called %q; say which one as plugin:pane", count, name)
}

// Hooks returns the commands to run for an event, with the plugin each came
// from.
func (r *Registry) Hooks(event string) []HookRun {
	var out []HookRun
	for _, p := range r.Enabled() {
		for _, h := range p.Events {
			if h.On == event && platformAllowed(h.Platforms) {
				out = append(out, HookRun{Plugin: p, Hook: h})
			}
		}
	}
	return out
}

// HookRun is one hook and the plugin it belongs to.
type HookRun struct {
	Plugin Installed
	Hook   Hook
}
