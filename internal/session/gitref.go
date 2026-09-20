package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Branch reads the checked-out branch of a git working tree, or "" when the
// directory is not one.
//
// It reads the files rather than running git. A sidebar redraws while the user
// is working, and spawning a process per space per refresh would be felt; the
// two files involved are small and their format has not changed in fifteen
// years. A detached head reports the short commit instead, because "" would
// read as "not a repository" and this very much is one.
func Branch(dir string) string {
	if dir == "" {
		return ""
	}
	gitDir, ok := resolveGitDir(dir)
	if !ok {
		return ""
	}

	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(head))

	if ref, ok := strings.CutPrefix(text, "ref: "); ok {
		return filepath.Base(ref)
	}
	// A detached head holds the commit itself.
	if len(text) >= 7 {
		return text[:7]
	}
	return ""
}

// resolveGitDir finds the git directory for a working tree, walking up to the
// repository root and following the pointer a worktree or submodule leaves.
func resolveGitDir(dir string) (string, bool) {
	for {
		candidate := filepath.Join(dir, ".git")
		info, err := os.Stat(candidate)
		switch {
		case err == nil && info.IsDir():
			return candidate, true

		case err == nil:
			// A worktree or submodule keeps a file pointing at the real one.
			data, err := os.ReadFile(candidate)
			if err != nil {
				return "", false
			}
			path, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
			if !ok {
				return "", false
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			return path, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// Tracking is how far a working tree has drifted from the branch it follows.
type Tracking struct {
	Ahead  int
	Behind int
}

// Empty reports whether there is nothing to say.
func (t Tracking) Empty() bool { return t.Ahead == 0 && t.Behind == 0 }

// AheadBehind counts the commits a working tree has that its upstream does
// not, and the other way round.
//
// This one runs git, unlike Branch. The answer needs the commit graph, and
// reaching it means decoding loose objects and packfiles and the index that
// finds them — a pile of code to arrive at what one command already knows, and
// wrong for every repository layout it did not anticipate. The cost is paid
// once per directory per cache period, not per redraw, and a repository that
// does not answer quickly is one whose exact numbers can wait.
func AheadBehind(dir string) Tracking {
	if dir == "" {
		return Tracking{}
	}
	if _, ok := resolveGitDir(dir); !ok {
		return Tracking{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), trackingTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "rev-list", "--count", "--left-right", "@{upstream}...HEAD")
	cmd.Dir = dir
	// A repository with no upstream is the common case for a new branch, and
	// git says so on stderr. It is not an error worth reporting: there is
	// simply nothing to be ahead or behind of.
	out, err := cmd.Output()
	if err != nil {
		return Tracking{}
	}

	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return Tracking{}
	}
	behind, err1 := strconv.Atoi(fields[0])
	ahead, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil {
		return Tracking{}
	}
	return Tracking{Ahead: ahead, Behind: behind}
}

// trackingTimeout bounds the one command this package runs. A repository on a
// slow or unreachable filesystem must not hold up a redraw.
const trackingTimeout = 2 * time.Second
