package plugin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Installing a plugin from GitHub: herdr's `plugin install owner/repo[/subdir]`
// (`cli/plugin.rs`). The repository is cloned, the plugin in it shown and
// agreed to, built, and then moved to a directory tend manages — the user's
// own checkouts are what `link` is for.
//
// Only the shorthand, as herdr takes it: a URL, an ssh address or anything
// with a colon is refused, so what is installed is always a GitHub repository
// named in the plainest way there is.

// GithubBaseEnv overrides where GitHub is, for a mirror — and for the tests,
// which must not need the network.
const GithubBaseEnv = "TEND_GITHUB_URL"

// GithubSource is owner/repo, and the directory in it the plugin is in.
type GithubSource struct {
	Owner, Repo, Subdir string
}

// ParseGithubSource reads owner/repo[/subdir...].
func ParseGithubSource(value string) (GithubSource, error) {
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "git@") || strings.Contains(value, ":") {
		return GithubSource{}, errors.New("plugin install accepts only owner/repo[/subdir] shorthand")
	}
	parts := strings.Split(value, "/")
	if len(parts) < 2 {
		return GithubSource{}, errors.New("usage: tend plugin install <owner>/<repo>[/subdir...]")
	}
	for i, part := range parts {
		label := "subdir"
		switch i {
		case 0:
			label = "owner"
		case 1:
			label = "repo"
		}
		if err := checkSegment(label, part); err != nil {
			return GithubSource{}, err
		}
	}
	return GithubSource{Owner: parts[0], Repo: parts[1], Subdir: strings.Join(parts[2:], "/")}, nil
}

func checkSegment(label, value string) error {
	if value == "" || value == "." || value == ".." {
		return fmt.Errorf("GitHub %s is invalid: %q", label, value)
	}
	for _, r := range value {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '.'
		if !ok {
			return fmt.Errorf("GitHub %s contains invalid characters: %s", label, value)
		}
	}
	return nil
}

// String is the source as it was written.
func (g GithubSource) String() string {
	if g.Subdir == "" {
		return g.Owner + "/" + g.Repo
	}
	return g.Owner + "/" + g.Repo + "/" + g.Subdir
}

// RemoteURL is where to clone from.
func (g GithubSource) RemoteURL() string {
	base := strings.TrimSuffix(os.Getenv(GithubBaseEnv), "/")
	if base == "" {
		base = "https://github.com"
	}
	return base + "/" + g.Owner + "/" + g.Repo + ".git"
}

// Source is where an installed plugin came from, kept so it can be found by
// owner/repo again and its files removed with it.
type Source struct {
	Kind        string `json:"kind"`
	Owner       string `json:"owner,omitempty"`
	Repo        string `json:"repo,omitempty"`
	Subdir      string `json:"subdir,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Commit      string `json:"commit,omitempty"`
	ManagedPath string `json:"managed_path,omitempty"`
}

// Checkout clones the source into dir at ref (a branch, a tag or a commit;
// the default branch when empty), and says which commit it got.
func Checkout(src GithubSource, ref, dir string) (string, error) {
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	}
	if _, err := git("clone", "--quiet", src.RemoteURL(), dir); err != nil {
		return "", err
	}
	if ref != "" {
		if _, err := git("-C", dir, "checkout", "--quiet", ref); err != nil {
			return "", err
		}
	}
	return git("-C", dir, "rev-parse", "HEAD")
}
