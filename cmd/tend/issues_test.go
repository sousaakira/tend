//go:build unix

package main

import (
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/vt"
)

// TestTheIssuesPanelListsSearchesAndOpens: prefix+I lists the issues of the
// GitHub repository the pane's project is on, typing searches GitHub with
// it, tab moves to the next preset, and enter opens an issue with its
// thread; esc goes back to the list. The gh here is a script answering as
// GitHub does — the panel and what it asks are under test, not GitHub. If
// it regresses, the toolbar's Issues shows another project's issues, or
// none, or an issue without its comments.
func TestTheIssuesPanelListsSearchesAndOpens(t *testing.T) {
	project := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "git@github.com:acme/shop.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", project}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	bin := t.TempDir()
	asked := filepath.Join(bin, "asked")
	gh := `#!/bin/sh
printf '%s\n' "$*" >> ` + asked + `
if [ "$1" = issue ]; then
cat <<'JSON'
{"number":42,"title":"Checkout button is grey","state":"OPEN","url":"https://github.com/acme/shop/issues/42",
 "body":"The **checkout** button should be green.","author":{"login":"ana"},"labels":[{"name":"bug"}],"assignees":[],
 "comments":[{"author":{"login":"bo"},"body":"Confirmed on mobile.","createdAt":"2026-09-21T10:00:00Z"}],
 "createdAt":"2026-09-20T10:00:00Z","updatedAt":"2026-09-21T10:00:00Z"}
JSON
exit 0
fi
cat <<'JSON'
{"total_count":2,"items":[
 {"number":42,"title":"Checkout button is grey","state":"open","html_url":"https://github.com/acme/shop/issues/42","user":{"login":"ana"},"labels":[{"name":"bug"}],"assignees":[],"comments":1,"updated_at":"2026-09-21T10:00:00Z"},
 {"number":7,"title":"Footer links","state":"open","html_url":"https://github.com/acme/shop/issues/7","user":{"login":"bo"},"labels":[],"assignees":[],"comments":0,"updated_at":"2026-09-19T10:00:00Z"}]}
JSON
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(gh), 0o755); err != nil {
		t.Fatal(err)
	}

	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh", "PATH="+bin+":"+os.Getenv("PATH"))
	tend := buildBinary(t)
	p, err := pty.Start(tend, []string{"attach", "-s", "issues"}, pty.Options{Size: pty.Size{Cols: 120, Rows: 36}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(120, 36, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "issues") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "cd "+project+"\r", "the shell in the project", func(s string) bool { return strings.Contains(s, filepath.Base(project)) })

	a.send(t, "\x02I")
	a.waitForScreen(t, "the project's issues", func(s string) bool {
		return strings.Contains(s, "GITHUB ISSUES · acme/shop") && strings.Contains(s, "Checkout button is grey") && strings.Contains(s, "Footer links")
	})
	a.send(t, "label:bug")
	a.send(t, "\t")
	a.waitForScreen(t, "the next preset", func(s string) bool { return strings.Contains(s, "label:bug") })
	waitForFileContent(t, asked, url.QueryEscape("repo:acme/shop is:issue is:open assignee:@me label:bug"))

	a.send(t, "\r")
	a.waitForScreen(t, "the issue with its thread", func(s string) bool {
		return strings.Contains(s, "#42 open · opened by ana") && strings.Contains(s, "checkout button should be green") &&
			strings.Contains(s, "bo ·") && strings.Contains(s, "Confirmed on mobile.")
	})
	a.send(t, "\x1b")
	a.waitForScreen(t, "back to the list", func(s string) bool {
		return strings.Contains(s, "Footer links") && !strings.Contains(s, "Confirmed on mobile.")
	})
}

// TestAnIssueIsCommentedClosedAndWorkedOn: on an open issue, c writes a
// comment that goes to GitHub as written, x then n closes it as not
// planned, and w makes a worktree on a branch named after it, opens it as
// a space and starts the agent there told to complete the issue; w again
// goes to that worktree rather than making another. gh and claude are
// scripts saying what they were given; git is real. If it regresses, a
// comment is cut, an issue is closed for the wrong reason, or work on one
// issue starts twice.
func TestAnIssueIsCommentedClosedAndWorkedOn(t *testing.T) {
	project := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "first"},
		{"remote", "add", "origin", "https://github.com/acme/shop.git"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = project, gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	bin := t.TempDir()
	asked := filepath.Join(bin, "asked")
	gh := `#!/bin/sh
printf '%s\n' "$*" >> ` + asked + `
case "$1 $2" in
"issue view") cat <<'JSON'
{"number":42,"title":"Checkout button is grey","state":"OPEN","url":"https://github.com/acme/shop/issues/42",
 "body":"Make it green.","author":{"login":"ana"},"labels":[],"assignees":[],"comments":[],
 "createdAt":"2026-09-20T10:00:00Z","updatedAt":"2026-09-21T10:00:00Z"}
JSON
;;
"issue comment") cat > ` + asked + `.comment ;;
"issue close"|"issue reopen") ;;
"issue create") cat > ` + asked + `.new; echo https://github.com/acme/shop/issues/42 ;;
*) cat <<'JSON'
{"total_count":1,"items":[{"number":42,"title":"Checkout button is grey","state":"open","html_url":"https://github.com/acme/shop/issues/42","user":{"login":"ana"},"labels":[],"assignees":[],"comments":0,"updated_at":"2026-09-21T10:00:00Z"}]}
JSON
;;
esac
`
	claude := "#!/bin/sh\necho \"claude was given: $*\"\necho \"on: $(git branch --show-current)\"\nexec sleep 60\n"
	for name, body := range map[string]string{"gh": gh, "claude": claude} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	worktrees := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "tend.toml")
	settings := quietSettings + "\n[worktrees]\ndirectory = \"" + worktrees + "\"\n\n[issues]\nagent = \"claude\"\n"
	if err := os.WriteFile(cfg, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}

	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", cfg)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "SHELL=/bin/sh", "PATH="+bin+":"+os.Getenv("PATH"))
	tend := buildBinary(t)
	p, err := pty.Start(tend, []string{"attach", "-s", "work"}, pty.Options{Size: pty.Size{Cols: 120, Rows: 36}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(120, 36, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "work") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "cd "+project+"\r", "the shell in the project", func(s string) bool { return strings.Contains(s, "shop") })

	a.send(t, "\x02I")
	a.waitForScreen(t, "the list", func(s string) bool { return strings.Contains(s, "Checkout button is grey") })
	a.send(t, "\r")
	a.waitForScreen(t, "the issue", func(s string) bool { return strings.Contains(s, "Make it green.") })

	a.send(t, "c")
	a.waitForScreen(t, "the comment box", func(s string) bool { return strings.Contains(s, "comment on #42") })
	a.send(t, "It's \"fixed\" on main.\nSee the PR.")
	a.send(t, "\r")
	a.waitForScreen(t, "it commented", func(s string) bool { return strings.Contains(s, "commented on #42") })
	waitForFileContent(t, asked+".comment", "It's \"fixed\" on main.\nSee the PR.")

	a.send(t, "x")
	a.waitForScreen(t, "the question", func(s string) bool { return strings.Contains(s, "close #42?") })
	a.send(t, "n")
	a.waitForScreen(t, "it closed", func(s string) bool { return strings.Contains(s, "closed #42 as not planned") })
	waitForFileContent(t, asked, "issue close 42 --repo acme/shop --reason not planned")

	// A new issue, from the list: title, tab, text, enter; it opens.
	a.send(t, "\x1b")
	a.waitForScreen(t, "back to the list", func(s string) bool { return strings.Contains(s, "[ New issue ]") })
	a.send(t, "\x0e")
	a.waitForScreen(t, "the new issue box", func(s string) bool { return strings.Contains(s, "new issue in acme/shop") })
	a.send(t, "Footer overlaps")
	a.send(t, "\t")
	a.send(t, "On **mobile**.")
	a.send(t, "\r")
	a.waitForScreen(t, "it filed and opened it", func(s string) bool {
		return strings.Contains(s, "filed #42") && strings.Contains(s, "Make it green.")
	})
	waitForFileContent(t, asked, "issue create --repo acme/shop --title Footer overlaps --body-file -")
	waitForFileContent(t, asked+".new", "On **mobile**.")

	a.send(t, "w")
	a.waitForScreen(t, "the agent on the issue's branch", func(s string) bool {
		return !strings.Contains(s, "GITHUB ISSUES") &&
			strings.Contains(s, "claude was given: Complete https://github.com/acme/shop/issues/42") &&
			strings.Contains(s, "on: issue-42-checkout-button-is-grey")
	})

	// Again: the worktree is there, and it is gone to.
	a.send(t, "\x02I")
	// Not by the title: the space made for the issue is named after it.
	a.waitForScreen(t, "the list again", func(s string) bool { return strings.Contains(s, "GITHUB ISSUES") && strings.Contains(s, "1 of 1") })
	a.send(t, "\r")
	a.waitForScreen(t, "the issue again", func(s string) bool { return strings.Contains(s, "Make it green.") })
	a.send(t, "w")
	a.waitForScreen(t, "the worktree gone to", func(s string) bool {
		return strings.Contains(s, "#42 already has a worktree: issue-42-checkout-button-is-grey")
	})
	out, _ := exec.Command("git", "-C", project, "worktree", "list").Output()
	if n := strings.Count(string(out), "[issue-42"); n != 1 {
		t.Errorf("worktrees for #42: %d\n%s", n, out)
	}
}

// TestPullRequestsAreListedOpenedAndMerged: → shows the project's pull
// requests, their checks filled in after the list; enter opens one with
// what it changes, and m then s merges it by squash; an issue shows the
// pull request GitHub links to it, and p opens it. gh is a script answering
// as GitHub does. If it regresses, the list waits on every check of every
// pull request, a merge is made the wrong way, or an issue never shows the
// work done on it.
func TestPullRequestsAreListedOpenedAndMerged(t *testing.T) {
	project := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "git@github.com:acme/shop.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", project}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	bin := t.TempDir()
	asked := filepath.Join(bin, "asked")
	gh := `#!/bin/sh
printf '%s\n' "$*" >> ` + asked + `
case "$1 $2" in
"pr list")
  case "$*" in
  *statusCheckRollup*) echo '[{"number":9,"statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"}]}]' ;;
  *) echo '[{"number":9,"title":"Make checkout green","state":"OPEN","isDraft":false,"author":{"login":"bo"},"labels":[],"headRefName":"issue-42-checkout","baseRefName":"main","updatedAt":"2026-09-21T10:00:00Z","url":"https://github.com/acme/shop/pull/9","reviewDecision":"APPROVED"}]' ;;
  esac ;;
"pr view") cat <<'JSON'
{"number":9,"title":"Make checkout green","state":"OPEN","isDraft":false,"author":{"login":"bo"},"labels":[],"headRefName":"issue-42-checkout",
 "baseRefName":"main","updatedAt":"2026-09-21T10:00:00Z","url":"https://github.com/acme/shop/pull/9","reviewDecision":"APPROVED",
 "statusCheckRollup":[{"__typename":"CheckRun","name":"lint","status":"COMPLETED","conclusion":"FAILURE"}],
 "body":"Fixes the colour.","createdAt":"2026-09-20T10:00:00Z","additions":10,"deletions":2,"changedFiles":1,"mergeable":"MERGEABLE",
 "comments":[],"latestReviews":[{"author":{"login":"ana"},"body":"Looks good.","state":"APPROVED","submittedAt":"2026-09-21T09:00:00Z"}]}
JSON
;;
"pr merge") ;;
"api graphql") echo '{"data":{"repository":{"issue":{"closedByPullRequestsReferences":{"nodes":[{"number":9}]}}}}}' ;;
"issue view") cat <<'JSON'
{"number":42,"title":"Checkout button is grey","state":"OPEN","url":"https://github.com/acme/shop/issues/42","body":"Make it green.",
 "author":{"login":"ana"},"labels":[],"assignees":[],"comments":[],"createdAt":"2026-09-20T10:00:00Z","updatedAt":"2026-09-21T10:00:00Z"}
JSON
;;
*) echo '{"total_count":1,"items":[{"number":42,"title":"Checkout button is grey","state":"open","html_url":"https://github.com/acme/shop/issues/42","user":{"login":"ana"},"labels":[],"assignees":[],"comments":0,"updated_at":"2026-09-21T10:00:00Z"}]}' ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(gh), 0o755); err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh", "PATH="+bin+":"+os.Getenv("PATH"))
	tend := buildBinary(t)
	p, err := pty.Start(tend, []string{"attach", "-s", "pulls"}, pty.Options{Size: pty.Size{Cols: 120, Rows: 36}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(120, 36, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "pulls") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "cd "+project+"\r", "the shell in the project", func(s string) bool { return strings.Contains(s, filepath.Base(project)) })

	a.send(t, "\x02I")
	a.waitForScreen(t, "the issues", func(s string) bool { return strings.Contains(s, "GITHUB ISSUES") && strings.Contains(s, "1 of 1") })
	a.send(t, "\x1b[C")
	a.waitForScreen(t, "the pull requests, checks filled in", func(s string) bool {
		return strings.Contains(s, "GITHUB PULL REQUESTS") && strings.Contains(s, "Make checkout green") &&
			strings.Contains(s, "✗1") && strings.Contains(s, "approved")
	})
	a.send(t, "\r")
	a.waitForScreen(t, "the pull request", func(s string) bool {
		return strings.Contains(s, "#9 open · by bo · issue-42-checkout → main") && strings.Contains(s, "+10 −2 in 1 files") &&
			strings.Contains(s, "✗ lint") && strings.Contains(s, "ana · approved") && strings.Contains(s, "Looks good.")
	})
	a.send(t, "m")
	a.waitForScreen(t, "the question", func(s string) bool { return strings.Contains(s, "merge #9 into main?") })
	a.send(t, "s")
	a.waitForScreen(t, "it merged", func(s string) bool { return strings.Contains(s, "merged #9 (squash)") })
	waitForFileContent(t, asked, "pr merge 9 --repo acme/shop --squash")

	a.send(t, "\x1b")
	a.waitForScreen(t, "back to the list", func(s string) bool { return strings.Contains(s, "[ Open ] [ In browser ]") })
	a.send(t, "\x1b[D")
	a.waitForScreen(t, "the issues again", func(s string) bool { return strings.Contains(s, "GITHUB ISSUES") && strings.Contains(s, "1 of 1") })
	a.send(t, "\r")
	a.waitForScreen(t, "the issue with its pull request", func(s string) bool {
		return strings.Contains(s, "PULL REQUESTS") && strings.Contains(s, "#9 open") && strings.Contains(s, "Make checkout green")
	})
	a.send(t, "p")
	a.waitForScreen(t, "the pull request, from the issue", func(s string) bool { return strings.Contains(s, "#9 open · by bo") })
	a.send(t, "\x1b")
	a.waitForScreen(t, "back to the issue", func(s string) bool { return strings.Contains(s, "Make it green.") })
}

// TestAnIssuesLabelsAndTitleAreEdited: on an open issue, l lists the
// repository's labels with the issue's marked, enter marks and unmarks,
// ctrl+s sends only what changed; e changes the title. gh is a script
// answering as GitHub does. If it regresses, saving labels resends every
// label, or a title edit is lost.
func TestAnIssuesLabelsAndTitleAreEdited(t *testing.T) {
	project := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "git@github.com:acme/shop.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", project}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	bin := t.TempDir()
	asked := filepath.Join(bin, "asked")
	gh := `#!/bin/sh
printf '%s\n' "$*" >> ` + asked + `
case "$*" in
*labels?per_page*) printf 'bug\nenhancement\nhelp wanted\n' ;;
"issue edit"*) ;;
"issue view"*) cat <<'JSON'
{"number":42,"title":"Checkout button is grey","state":"OPEN","url":"https://github.com/acme/shop/issues/42","body":"Make it green.",
 "author":{"login":"ana"},"labels":[{"name":"bug"}],"assignees":[],"comments":[],"createdAt":"2026-09-20T10:00:00Z","updatedAt":"2026-09-21T10:00:00Z"}
JSON
;;
"api graphql"*) echo '{"data":{"repository":{"issue":{"closedByPullRequestsReferences":{"nodes":[]}}}}}' ;;
"pr list"*) echo '[]' ;;
*) echo '{"total_count":1,"items":[{"number":42,"title":"Checkout button is grey","state":"open","html_url":"https://github.com/acme/shop/issues/42","user":{"login":"ana"},"labels":[{"name":"bug"}],"assignees":[],"comments":0,"updated_at":"2026-09-21T10:00:00Z"}]}' ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(gh), 0o755); err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh", "PATH="+bin+":"+os.Getenv("PATH"))
	tend := buildBinary(t)
	p, err := pty.Start(tend, []string{"attach", "-s", "edit"}, pty.Options{Size: pty.Size{Cols: 120, Rows: 36}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(120, 36, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "edit") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "cd "+project+"\r", "the shell in the project", func(s string) bool { return strings.Contains(s, filepath.Base(project)) })

	a.send(t, "\x02I")
	a.waitForScreen(t, "the list", func(s string) bool { return strings.Contains(s, "GITHUB ISSUES") && strings.Contains(s, "1 of 1") })
	a.send(t, "\r")
	a.waitForScreen(t, "the issue", func(s string) bool { return strings.Contains(s, "Make it green.") })

	a.send(t, "l")
	a.waitForScreen(t, "the labels, the issue's marked", func(s string) bool {
		return strings.Contains(s, "[✓] bug") && strings.Contains(s, "[ ] enhancement") && strings.Contains(s, "[ ] help wanted")
	})
	a.send(t, "\r") // unmark bug
	a.send(t, "help")
	a.waitForScreen(t, "filtered", func(s string) bool {
		return strings.Contains(s, "[ ] help wanted") && !strings.Contains(s, "enhancement")
	})
	a.send(t, "\r") // mark help wanted
	a.waitForScreen(t, "marked", func(s string) bool { return strings.Contains(s, "[✓] help wanted") })
	a.send(t, "\x13")
	a.waitForScreen(t, "saved", func(s string) bool { return strings.Contains(s, "#42: labels saved") })
	waitForFileContent(t, asked, "issue edit 42 --repo acme/shop --add-label help wanted --remove-label bug")

	a.send(t, "e")
	a.waitForScreen(t, "the title box", func(s string) bool { return strings.Contains(s, "title of #42") })
	a.send(t, "\x15Checkout button should be green")
	a.send(t, "\r")
	a.waitForScreen(t, "the title saved", func(s string) bool { return strings.Contains(s, "#42: title saved") })
	waitForFileContent(t, asked, "issue edit 42 --repo acme/shop --title Checkout button should be green")
}

// TestAFolderOfRepositoriesListsThemAll: from a folder that is not a
// repository, with two under it, the panel lists both repositories' issues
// in one search, each with its repository; ctrl+t lists one of them; an
// issue's work starts in its own repository's checkout; and a new issue
// goes to the repository ctrl+t picks in its box. gh and claude are
// scripts; git is real. If it regresses, a project made of several
// repositories has "no GitHub remote", or work on an issue of one starts
// in another.
func TestAFolderOfRepositoriesListsThemAll(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "shop")
	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	for _, name := range []string{"api", "web"} {
		dir := filepath.Join(folder, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "first"},
			{"remote", "add", "origin", "git@github.com:acme/" + name + ".git"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir, cmd.Env = dir, gitEnv
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}
	bin := t.TempDir()
	asked := filepath.Join(bin, "asked")
	gh := `#!/bin/sh
printf '%s\n' "$*" >> ` + asked + `
case "$*" in
"issue create"*) cat > /dev/null; echo https://github.com/acme/web/issues/9 ;;
"issue view 7"*) echo '{"number":7,"title":"Checkout is slow","state":"OPEN","url":"https://github.com/acme/web/issues/7","body":"Slow.","author":{"login":"ana"},"labels":[],"assignees":[],"comments":[],"createdAt":"2026-09-20T10:00:00Z","updatedAt":"2026-09-21T10:00:00Z"}' ;;
"issue view"*) echo '{"number":9,"title":"New","state":"OPEN","url":"https://github.com/acme/web/issues/9","body":"","author":{"login":"me"},"labels":[],"assignees":[],"comments":[],"createdAt":"2026-09-21T10:00:00Z","updatedAt":"2026-09-21T10:00:00Z"}' ;;
"api graphql"*) echo '{"data":{"repository":{"issue":{"closedByPullRequestsReferences":{"nodes":[]}}}}}' ;;
"pr list"*) echo '[]' ;;
*) echo '{"total_count":2,"items":[
 {"number":7,"title":"Checkout is slow","state":"open","html_url":"https://github.com/acme/web/issues/7","repository_url":"https://api.github.com/repos/acme/web","user":{"login":"ana"},"labels":[],"assignees":[],"comments":0,"updated_at":"2026-09-21T10:00:00Z"},
 {"number":3,"title":"Rate limit the login","state":"open","html_url":"https://github.com/acme/api/issues/3","repository_url":"https://api.github.com/repos/acme/api","user":{"login":"bo"},"labels":[],"assignees":[],"comments":0,"updated_at":"2026-09-20T10:00:00Z"}]}' ;;
esac
`
	claude := "#!/bin/sh\necho \"claude in: $(basename \"$(git rev-parse --show-toplevel)\") on $(git branch --show-current)\"\nexec sleep 60\n"
	for name, body := range map[string]string{"gh": gh, "claude": claude} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	worktrees := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(cfg, []byte(quietSettings+"\n[worktrees]\ndirectory = \""+worktrees+"\"\n\n[issues]\nagent = \"claude\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", cfg)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "SHELL=/bin/sh", "PATH="+bin+":"+os.Getenv("PATH"))
	tend := buildBinary(t)
	p, err := pty.Start(tend, []string{"attach", "-s", "folder"}, pty.Options{Size: pty.Size{Cols: 120, Rows: 36}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(120, 36, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "folder") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "cd "+folder+"\r", "the shell in the folder", func(s string) bool { return strings.Contains(s, "shop") })

	a.send(t, "\x02I")
	a.waitForScreen(t, "both repositories' issues", func(s string) bool {
		return strings.Contains(s, "GITHUB ISSUES · 2 repositories") && strings.Contains(s, " all ") &&
			strings.Contains(s, "Checkout is slow") && strings.Contains(s, "Rate limit the login") && strings.Contains(s, "repository")
	})
	waitForFileContent(t, asked, url.QueryEscape("repo:acme/api repo:acme/web is:issue is:open"))

	// A new issue from all of them: the first, then ctrl+t to the other.
	a.send(t, "\x0e")
	a.waitForScreen(t, "the new issue box", func(s string) bool { return strings.Contains(s, "new issue in acme/api (ctrl+t another)") })
	a.send(t, "\x14")
	a.waitForScreen(t, "the other repository", func(s string) bool { return strings.Contains(s, "new issue in acme/web") })
	a.send(t, "Slow search\r\r")
	waitForFileContent(t, asked, "issue create --repo acme/web --title Slow search")
	a.waitForScreen(t, "it filed", func(s string) bool { return strings.Contains(s, "filed #9") })
	a.send(t, "\x1b")

	// One of them: ctrl+t goes from all to the first, api.
	a.waitForScreen(t, "the list", func(s string) bool { return strings.Contains(s, "[ New issue ]") })
	a.send(t, "\x14")
	a.waitForScreen(t, "api alone", func(s string) bool { return strings.Contains(s, "GITHUB ISSUES · acme/api") })
	waitForFileContent(t, asked, url.QueryEscape("repo:acme/api is:issue is:open"))
	a.send(t, "\x14\x14") // web, then all again
	a.waitForScreen(t, "all again", func(s string) bool { return strings.Contains(s, "GITHUB ISSUES · 2 repositories") })

	// Work on web's issue starts in web's checkout.
	a.waitForScreen(t, "the list again", func(s string) bool { return strings.Contains(s, "Checkout is slow") })
	a.send(t, "\r")
	a.waitForScreen(t, "the issue, with its repository", func(s string) bool { return strings.Contains(s, "web #7 open") })
	a.send(t, "w")
	a.waitForScreen(t, "claude in web's worktree", func(s string) bool {
		return strings.Contains(s, "claude in: issue-7-checkout-is-slow on issue-7-checkout-is-slow")
	})
	out, _ := exec.Command("git", "-C", filepath.Join(folder, "web"), "worktree", "list").Output()
	if !strings.Contains(string(out), "[issue-7-checkout-is-slow]") {
		t.Errorf("web's worktrees:\n%s", out)
	}
	if out, _ := exec.Command("git", "-C", filepath.Join(folder, "api"), "worktree", "list").Output(); strings.Contains(string(out), "issue-7") {
		t.Errorf("the work started in api:\n%s", out)
	}
}
