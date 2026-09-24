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
