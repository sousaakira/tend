package session

import (
	"os"
	"path/filepath"
	"strings"
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
