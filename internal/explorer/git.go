package explorer

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Everything the explorer knows about git it asks git for, in the porcelain
// formats git promises to keep stable. Parsing `git status` for people, or
// reading .git by hand, is how tools end up disagreeing with git about a
// rename or a conflict.

// Change is one path git has something to say about.
type Change struct {
	// Path is relative to the repository's top, with forward slashes, as git
	// writes it. Orig is where a rename came from.
	Path, Orig string
	// Index and Work are git's two status letters: what is staged, and what
	// is changed in the working tree but not staged. '.' or ' ' is nothing.
	Index, Work byte
}

// Staged reports whether some of the change is in the index.
func (c Change) Staged() bool { return c.Index != ' ' && c.Index != '?' && c.Index != '!' }

// Unstaged reports whether some of the change is only in the working tree.
func (c Change) Unstaged() bool { return c.Work != ' ' }

// Untracked is a file git has never been told about.
func (c Change) Untracked() bool { return c.Index == '?' }

// Conflicted is a path mid-merge, which git marks with a U on either side or
// with both sides adding or deleting.
func (c Change) Conflicted() bool {
	pair := string([]byte{c.Index, c.Work})
	return c.Index == 'U' || c.Work == 'U' || pair == "AA" || pair == "DD"
}

// Letter is the one letter the explorer shows for a path, as an editor's
// source-control view does: the working tree's news first, since that is
// what was touched most recently.
func (c Change) Letter() byte {
	switch {
	case c.Conflicted():
		return '!'
	case c.Untracked():
		return 'U'
	case c.Work != ' ':
		return c.Work
	default:
		return c.Index
	}
}

// Status is the repository as `git status` sees it.
type Status struct {
	Branch         string
	Upstream       string
	Ahead, Behind  int
	Changes        []Change
	byPath         map[string]Change
	dirtyDirs      map[string]byte
	detachedOrNone bool
}

// Of is the change at a path, relative to the repository's top.
func (s *Status) Of(path string) (Change, bool) {
	if s == nil {
		return Change{}, false
	}
	c, ok := s.byPath[path]
	return c, ok
}

// DirLetter is the letter a directory shows for the changes inside it, or 0.
func (s *Status) DirLetter(dir string) byte {
	if s == nil {
		return 0
	}
	return s.dirtyDirs[dir]
}

// parseStatus reads `git status --porcelain=v2 --branch -z`.
//
// Version 2 because it says the upstream and ahead/behind on header lines,
// and version 1 hides them in a string meant for people.
func parseStatus(out []byte) *Status {
	st := &Status{byPath: map[string]Change{}, dirtyDirs: map[string]byte{}}
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		line := string(fields[i])
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			st.Branch = strings.TrimPrefix(line, "# branch.head ")
			st.detachedOrNone = st.Branch == "(detached)"
		case strings.HasPrefix(line, "# branch.upstream "):
			st.Upstream = strings.TrimPrefix(line, "# branch.upstream ")
		case strings.HasPrefix(line, "# branch.ab "):
			for _, f := range strings.Fields(strings.TrimPrefix(line, "# branch.ab ")) {
				n, _ := strconv.Atoi(f[1:])
				if f[0] == '+' {
					st.Ahead = n
				} else {
					st.Behind = n
				}
			}
		case strings.HasPrefix(line, "1 "):
			// 1 XY sub mH mI mW hH hI path
			parts := strings.SplitN(line, " ", 9)
			if len(parts) == 9 {
				st.add(Change{Path: parts[8], Index: parts[1][0], Work: parts[1][1]})
			}
		case strings.HasPrefix(line, "2 "):
			// 2 XY sub mH mI mW hH hI Xscore path, then the original path as
			// the next NUL-separated field.
			parts := strings.SplitN(line, " ", 10)
			if len(parts) == 10 {
				c := Change{Path: parts[9], Index: parts[1][0], Work: parts[1][1]}
				if i+1 < len(fields) {
					c.Orig = string(fields[i+1])
					i++
				}
				st.add(c)
			}
		case strings.HasPrefix(line, "u "):
			// u XY sub m1 m2 m3 mW h1 h2 h3 path
			parts := strings.SplitN(line, " ", 11)
			if len(parts) == 11 {
				st.add(Change{Path: parts[10], Index: parts[1][0], Work: parts[1][1]})
			}
		case strings.HasPrefix(line, "? "):
			st.add(Change{Path: strings.TrimPrefix(line, "? "), Index: '?', Work: '?'})
		}
	}
	sort.Slice(st.Changes, func(a, b int) bool { return st.Changes[a].Path < st.Changes[b].Path })
	return st
}

// add records a change and marks every directory above it. v2 writes '.'
// for "nothing" where v1 wrote a space; the rest of the explorer speaks v1.
func (s *Status) add(c Change) {
	if c.Index == '.' {
		c.Index = ' '
	}
	if c.Work == '.' {
		c.Work = ' '
	}
	// An untracked directory is listed once with a trailing slash; the tree
	// wants the directory itself.
	c.Path = strings.TrimSuffix(c.Path, "/")
	s.Changes = append(s.Changes, c)
	s.byPath[c.Path] = c
	letter := c.Letter()
	for dir := filepath.ToSlash(filepath.Dir(c.Path)); dir != "." && dir != "/"; dir = filepath.ToSlash(filepath.Dir(dir)) {
		// A conflict inside outranks everything else, then any change.
		if prev := s.dirtyDirs[dir]; prev != '!' {
			s.dirtyDirs[dir] = letter
		}
	}
}

// Git runs git in one repository. The zero value is not a repository.
type Git struct {
	// Top is the repository's top directory, or "" when the explorer's
	// directory is not in one.
	Top string
}

// FindRepo is the repository dir is in, if it is in one.
func FindRepo(dir string) Git {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return Git{}
	}
	return Git{Top: strings.TrimSpace(string(out))}
}

// ErrNoRepo is what a git action says outside a repository.
var ErrNoRepo = errors.New("not in a git repository")

// run runs git in the repository and returns what it printed, or its own
// explanation of why not.
func (g Git) run(args ...string) ([]byte, error) {
	if g.Top == "" {
		return nil, ErrNoRepo
	}
	cmd := exec.Command("git", append([]string{"-C", g.Top}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			// git's first line is the reason; the rest is advice.
			return out, errors.New(strings.SplitN(msg, "\n", 2)[0])
		}
		return out, err
	}
	return out, nil
}

// Status asks git for the repository's status. Ignored files are not
// listed: in a project with a node_modules they are most of the disk, and
// the tree marks them itself by asking about the entries it shows.
func (g Git) Status() (*Status, error) {
	out, err := g.run("status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	return parseStatus(out), nil
}

// Ignored reports which of the given paths (relative to the top) git
// ignores, in one call for a whole directory's entries.
func (g Git) Ignored(paths []string) map[string]bool {
	out := map[string]bool{}
	if g.Top == "" || len(paths) == 0 {
		return out
	}
	cmd := exec.Command("git", "-C", g.Top, "check-ignore", "-z", "--stdin")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	// check-ignore exits 1 when nothing matched, which is not a failure.
	res, _ := cmd.Output()
	for _, p := range bytes.Split(res, []byte{0}) {
		if len(p) > 0 {
			out[string(p)] = true
		}
	}
	return out
}

// Diff is the change to one path as a unified diff: what is staged, or
// what is not. An untracked file is shown whole, as added.
func (g Git) Diff(c Change, staged bool) (string, error) {
	switch {
	case c.Untracked():
		// --no-index exits 1 when the files differ, which they always do.
		out, _ := g.run("diff", "--no-color", "--no-index", "--", "/dev/null", c.Path)
		return string(out), nil
	case staged:
		out, err := g.run("diff", "--no-color", "--cached", "-M", "--", c.Path)
		return string(out), err
	default:
		out, err := g.run("diff", "--no-color", "--", c.Path)
		return string(out), err
	}
}

// Stage adds a path to the index, deletions included.
func (g Git) Stage(path string) error {
	_, err := g.run("add", "-A", "--", path)
	return err
}

// StageAll adds everything, as `git add -A`.
func (g Git) StageAll() error {
	_, err := g.run("add", "-A")
	return err
}

// Unstage takes a path out of the index and leaves the file alone. A
// repository with no commit yet has no HEAD to reset to, and there the only
// way out of the index is rm --cached.
func (g Git) Unstage(path string) error {
	if _, err := g.run("reset", "-q", "--", path); err != nil {
		if _, err2 := g.run("rm", "-q", "--cached", "--", path); err2 != nil {
			return err
		}
	}
	return nil
}

// Discard puts a tracked file back as the index has it. Untracked files are
// refused: there is no copy of them anywhere, and deleting one is a decision
// for a command line, not a key next to the one that stages it.
func (g Git) Discard(c Change) error {
	if c.Untracked() {
		return fmt.Errorf("%s is untracked; git has no copy to put back", c.Path)
	}
	_, err := g.run("checkout", "--", c.Path)
	return err
}

// Commit commits what is staged.
func (g Git) Commit(message string) error {
	_, err := g.run("commit", "-q", "-m", message)
	return err
}

// Files is every file a search can find: tracked, and untracked but not
// ignored, which is what "the project" means to the person searching it.
func (g Git) Files() ([]string, error) {
	out, err := g.run("ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var files []string
	seen := map[string]bool{}
	for _, p := range bytes.Split(out, []byte{0}) {
		// A file deleted but not staged is still "cached"; it cannot be
		// opened, and listing it twice (it is also "other" after a rename)
		// helps nobody.
		if len(p) > 0 && !seen[string(p)] {
			seen[string(p)] = true
			files = append(files, string(p))
		}
	}
	return files, nil
}
