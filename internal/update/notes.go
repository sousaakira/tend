package update

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
)

// Release notes are kept as herdr keeps them (`release_notes.rs`): one file,
// release-notes.json beside the settings file, holding the notes of the
// newest release tend has heard of. They are saved when a check finds a
// release, before it is installed, so the same file is "update ready" while
// this build is older than it and "what's new" once the new build runs.
// Nothing deletes it: the notes stay there to be read again.

// Notes are one release's notes.
type Notes struct {
	Version string `json:"version"`
	Body    string `json:"body"`
	// ShowOnStartup is herdr's field, kept in the file's shape; herdr
	// writes it and clears it on dismiss, and opens nothing because of it.
	ShowOnStartup bool `json:"show_on_startup"`
}

// SaveNotes writes a release's notes, replacing what was there. Notes with
// nothing in them remove the file, as herdr's do.
func SaveNotes(path, version, body string) error {
	body = normalizeNotes(body)
	if body == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeNotes(path, Notes{Version: version, Body: body, ShowOnStartup: true})
}

// LoadNotes reads the notes kept, and whether they are of a release newer
// than the build running — "update ready" rather than "what's new". ok is
// false when there are none.
func LoadNotes(path, running string) (n Notes, newer, ok bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Notes{}, false, false
	}
	// herdr's legacy files have no show_on_startup; it is true for them.
	n.ShowOnStartup = true
	if json.Unmarshal(raw, &n) != nil || n.Version == "" || normalizeNotes(n.Body) == "" {
		return Notes{}, false, false
	}
	return n, Newer(n.Version, running), true
}

// DismissNotes marks the notes read, which herdr does only for the notes of
// the release running: an "update ready" stays until it is installed.
func DismissNotes(path, running string) error {
	n, newer, ok := LoadNotes(path, running)
	if !ok || newer || n.Version != running {
		return nil
	}
	n.ShowOnStartup = false
	return writeNotes(path, n)
}

// writeNotes replaces the file whole, through a temporary one beside it, so
// a reader never sees half of it.
func writeNotes(path string, n Notes) error {
	raw, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp." + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// normalizeNotes trims each line's end and the whole, herdr's normalize_body.
func normalizeNotes(body string) string {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
