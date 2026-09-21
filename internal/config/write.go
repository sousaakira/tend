package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Changing one setting from inside tend must not rewrite the settings file.
//
// A file the user wrote is a file they will read again: their comments, the
// order they put things in, the sections they left empty on purpose. Marshalling
// the whole configuration back would lose all of it, so a change is an edit —
// the one line that holds the key, replaced in place, and everything else left
// exactly as it was. herdr does this too (`config/io.rs::upsert_section_raw`),
// and this is that function.

// ErrNoConfig means there is no settings file to edit yet.
var ErrNoConfig = errors.New("config: no settings file")

// Set writes one key in one section of the settings file.
//
// value is TOML as it should appear after the equals sign: Quote for a string,
// "true" for a boolean. A section that is not in the file is added at the end;
// a key that is not in its section is added to it.
func Set(section, key, value string) error {
	path, err := Path()
	if err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// No file yet: start from the documented default, so what is written
		// lands in a file that explains itself rather than a bare fragment.
		content = []byte(Example)
	}

	updated := Upsert(string(content), section, key, value)
	if _, err := parse(updated, path); err != nil {
		// The edit produced something tend itself would refuse to start with.
		// Writing it would lock the user out of their own settings.
		return fmt.Errorf("config: refusing to write %s.%s: %w", section, key, err)
	}
	return writeFile(path, updated)
}

// Upsert returns content with section.key set to value, leaving the rest of
// the file — comments, order, spacing — as it was.
func Upsert(content, section, key, value string) string {
	header := "[" + section + "]"
	assignment := key + " = " + value

	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	out := make([]string, 0, len(lines)+3)
	found, inserted := false, false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) != header {
			out = append(out, line)
			continue
		}

		found = true
		out = append(out, line)
		// Walk this section until the next one, replacing the key if it is
		// there and adding it at the end of the section if it is not.
		for i++; i < len(lines); i++ {
			current := strings.TrimSpace(lines[i])
			if strings.HasPrefix(current, "[") && strings.HasSuffix(current, "]") {
				if !inserted {
					out = append(out, assignment)
					inserted = true
				}
				i--
				break
			}
			if isAssignment(current, key) {
				out = append(out, assignment)
				inserted = true
				continue
			}
			out = append(out, lines[i])
		}
	}

	switch {
	case !found:
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, header, assignment)
	case !inserted:
		out = append(out, assignment)
	}
	return strings.Join(out, "\n") + "\n"
}

// isAssignment reports whether a line assigns key, allowing for the spacing
// people actually write: "key = x", "key=x", "key\t= x".
func isAssignment(line, key string) bool {
	rest, ok := strings.CutPrefix(line, key)
	if !ok {
		return false
	}
	rest = strings.TrimLeft(rest, " \t")
	return strings.HasPrefix(rest, "=")
}

// Quote renders a string as TOML.
func Quote(s string) string { return strconv.Quote(s) }

// Bool renders a boolean as TOML.
func Bool(b bool) string { return strconv.FormatBool(b) }

// writeFile replaces the settings file in one step, so a crash halfway cannot
// leave a settings file that will not parse.
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tend-*.toml")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	return os.Rename(name, path)
}
