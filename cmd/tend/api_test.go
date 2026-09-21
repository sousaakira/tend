//go:build unix

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sousaakira/tend/internal/api"
	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/transport"
	"github.com/sousaakira/tend/internal/vt"
)

// TestServeOffersTheAutomationSocket is the hooks path end to end: a real
// server starts the JSON socket beside the session one, every pane inherits
// the env a hook reads, and a report over that socket changes what the pane
// is shown as. If it regresses, integrations have nowhere to speak and the
// arbiter never hears from them.
func TestServeOffersTheAutomationSocket(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")

	bin := buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", "hooks"}, pty.Options{
		Size: pty.Size{Cols: 80, Rows: 20},
		Env:  env,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(80, 20, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stopSession(t, "hooks")
	})
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	apiPath, err := transport.APISocketPath("hooks")
	if err != nil {
		t.Fatal(err)
	}
	conn := dialAPISocket(t, apiPath)
	defer conn.Close()
	r := bufio.NewReader(conn)

	reply := callAPI(t, conn, r, `{"id":"1","method":"ping"}`)
	result, _ := reply["result"].(map[string]any)
	if result["type"] != "pong" || result["protocol"] == nil {
		t.Fatalf("ping = %v", reply)
	}

	list := callAPI(t, conn, r, `{"id":"list","method":"pane.list"}`)
	panes, _ := list["result"].(map[string]any)["panes"].([]any)
	if len(panes) == 0 {
		t.Fatalf("pane.list = %v", list)
	}
	info, _ := panes[0].(map[string]any)
	paneID, _ := info["pane_id"].(string)
	if paneID == "" {
		t.Fatalf("pane.list entry = %v", info)
	}

	// Markers keep the values off the pane border characters that otherwise
	// glue themselves to whatever is printed at the edge of the frame.
	a.send(t, "printf '<<%s|%s>>\\n' \"$TEND_ENV\" \"$TEND_PANE_ID\"\n")
	a.waitForScreen(t, "pane env", func(s string) bool {
		return strings.Contains(s, "<<"+api.EnvMarkerValue+"|"+paneID+">>")
	})

	report := fmt.Sprintf(
		`{"id":"2","method":"pane.report_agent","params":{"pane_id":%q,"source":"test-hook","agent":"deploy-bot","state":"blocked","message":"approve?","seq":1}}`,
		paneID,
	)
	if reply = callAPI(t, conn, r, report); reply["error"] != nil {
		t.Fatalf("report = %v", reply)
	}
	got := callAPI(t, conn, r, fmt.Sprintf(
		`{"id":"3","method":"pane.get","params":{"pane_id":%q}}`, paneID,
	))
	info, _ = got["result"].(map[string]any)["pane"].(map[string]any)
	if info["agent"] != "deploy-bot" || info["agent_state"] != "blocked" || info["message"] != "approve?" {
		t.Errorf("after report, pane = %v", info)
	}

	// The socket path the pane sees must be this session's automation socket,
	// not the client protocol one. A hook that dials the wrong file gets a
	// framed protocol it cannot speak. Compared in the shell: the path wraps
	// on screen and border glyphs break a substring match.
	a.send(t, "case \"$TEND_SOCKET_PATH\" in */api/hooks.sock) printf 'sock-ok\\n';; *) printf 'sock-bad\\n';; esac\n")
	a.waitForScreen(t, "socket path", func(s string) bool {
		return strings.Contains(s, "sock-ok")
	})
}

func dialAPISocket(t *testing.T, path string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
		if err == nil {
			return conn
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("automation socket %s never came up; log:\n%s", path, readServerLog(filepath.Dir(filepath.Dir(path)), "hooks"))
	return nil
}

func callAPI(t *testing.T, conn net.Conn, r *bufio.Reader, line string) map[string]any {
	t.Helper()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintln(conn, line); err != nil {
		t.Fatal(err)
	}
	reply, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("no reply to %s: %v", line, err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(reply), &out); err != nil {
		t.Fatalf("reply %q: %v", reply, err)
	}
	return out
}

func readServerLog(runtimeDir, name string) string {
	b, err := os.ReadFile(filepath.Join(runtimeDir, name+".log"))
	if err != nil {
		return err.Error()
	}
	return string(b)
}

// TestTheCommandsScriptsUse drives a session the way a script or another agent
// does — from outside, with no client attached — and checks the answers are
// what a shell can act on. If it regresses, automation is unusable without
// somebody sitting in front of the session.
func TestTheCommandsScriptsUse(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	bin := buildBinary(t)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")

	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// No session yet: the failure has to say so rather than hang or panic.
	if out, err := run("agent", "list", "-s", "cli"); err == nil {
		t.Fatalf("agent list against nothing succeeded: %s", out)
	} else if !strings.Contains(out, "not running") {
		t.Errorf("the error does not explain itself: %s", out)
	}

	if out, err := run("new", "-s", "cli", "--", "/bin/sh"); err != nil {
		t.Fatalf("tend new: %v\n%s", err, out)
	}
	t.Cleanup(func() { stopSession(t, "cli") })

	// A pane is there, and can be typed into and read back.
	out, err := run("pane", "list", "-s", "cli")
	if err != nil || !strings.Contains(out, `"p_1"`) {
		t.Fatalf("pane list = %q, %v", out, err)
	}
	if out, err := run("pane", "send-text", "-s", "cli", "-submit", "p_1", "printf from-the-cli"); err != nil {
		t.Fatalf("send-text: %v\n%s", err, out)
	}
	if out, err := run("pane", "wait", "-s", "cli", "-contains", "from-the-cli", "-timeout", "10s", "p_1"); err != nil {
		t.Fatalf("pane wait: %v\n%s", err, out)
	}
	out, err = run("pane", "read", "-s", "cli", "p_1")
	if err != nil || !strings.Contains(out, "from-the-cli") {
		t.Fatalf("pane read = %q, %v", out, err)
	}

	// A wait that runs out has to fail the command, so `&&` in a script means
	// what it looks like.
	if out, err := run("pane", "wait", "-s", "cli", "-contains", "never-printed", "-timeout", "300ms", "p_1"); err == nil {
		t.Errorf("a wait that timed out succeeded: %s", out)
	}

	// And the generic door reaches methods with no command of their own.
	out, err = run("api", "-s", "cli", "session.snapshot")
	if err != nil || !strings.Contains(out, `"session_snapshot"`) {
		t.Fatalf("tend api = %q, %v", out, err)
	}
}

// TestAPluginExtendsTheSession is the plugin host end to end with the real
// binary: a directory with a manifest is linked, its hook runs when a pane
// opens, its action runs when invoked, and the pane it offers really runs its
// own command. If it regresses, nothing installed beside tend can extend it.
func TestAPluginExtendsTheSession(t *testing.T) {
	runtimeDir := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", filepath.Join(configDir, "tend.toml"))
	bin := buildBinary(t)
	env := append(os.Environ(),
		"TEND_RUNTIME_DIR="+runtimeDir,
		"TEND_CONFIG="+filepath.Join(configDir, "tend.toml"),
		"SHELL=/bin/sh",
	)
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	root := t.TempDir()
	manifest := `
id = "demo"
name = "Demo"
version = "0.1.0"

[[events]]
on = "pane.opened"
command = ["./record.sh", "opened"]

[[actions]]
id = "greet"
title = "Greet"
command = ["./record.sh", "greet"]

[[panes]]
id = "side"
title = "Demo pane"
command = ["/bin/sh", "-c", "printf i-am-the-plugin-pane\\n; sleep 30"]
`
	if err := os.WriteFile(filepath.Join(root, "tend-plugin.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	record := "#!/bin/sh\nprintf '%s %s\\n' \"$1\" \"$TEND_PLUGIN_CONTEXT_JSON\" >>\"$TEND_PLUGIN_ROOT/ran.log\"\nif [ \"$1\" = greet ]; then printf 'the-action-ran\\n'; fi\n"
	if err := os.WriteFile(filepath.Join(root, "record.sh"), []byte(record), 0o755); err != nil {
		t.Fatal(err)
	}

	if out, err := run("plugin", "link", root); err != nil {
		t.Fatalf("plugin link: %v\n%s", err, out)
	}
	if out, err := run("plugin", "list"); err != nil || !strings.Contains(out, "demo") {
		t.Fatalf("plugin list = %q, %v", out, err)
	}

	if out, err := run("new", "-s", "plug", "--", "/bin/sh"); err != nil {
		t.Fatalf("tend new: %v\n%s", err, out)
	}
	t.Cleanup(func() { stopSession(t, "plug") })

	// The hook ran when the pane opened, and was told where.
	log := filepath.Join(root, "ran.log")
	deadline := time.Now().Add(10 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(log); err == nil && strings.Contains(string(b), "opened") {
			text = string(b)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(text, `"pane_id":"p_1"`) {
		t.Fatalf("the pane.opened hook did not run with a pane in its context; the log holds:\n%s", text)
	}

	// An action's output comes back to whoever invoked it.
	out, err := run("plugin", "run", "-s", "plug", "greet")
	if err != nil || !strings.Contains(out, "the-action-ran") {
		t.Fatalf("plugin run = %q, %v", out, err)
	}

	// And a pane the plugin offers runs the plugin's own command.
	if out, err := run("plugin", "open", "-s", "plug", "side"); err != nil {
		t.Fatalf("plugin open: %v\n%s", err, out)
	}
	for time.Now().Before(deadline.Add(10 * time.Second)) {
		out, _ := run("pane", "read", "-s", "plug", "p_2")
		if strings.Contains(out, "i-am-the-plugin-pane") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("the pane the plugin opened never ran its command")
}

// TestAWorktreeBecomesASpace is worktrees end to end with the real binary and
// real git: a checkout made for an agent, opened as a space in the
// repository's group, found in the list, and removed with its space. If it
// regresses, two agents on one project go back to sharing a checkout.
func TestAWorktreeBecomesASpace(t *testing.T) {
	runtimeDir := t.TempDir()
	configDir := t.TempDir()
	worktrees := t.TempDir()
	cfg := filepath.Join(configDir, "tend.toml")
	if err := os.WriteFile(cfg, []byte("[worktrees]\ndirectory = \""+worktrees+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", cfg)
	bin := buildBinary(t)

	repo := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"commit", "-q", "--allow-empty", "-m", "first"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = repo, gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "SHELL=/bin/sh")
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env, cmd.Dir = env, repo
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := run("new", "-s", "wt", "--", "/bin/sh"); err != nil {
		t.Fatalf("tend new: %v\n%s", err, out)
	}
	t.Cleanup(func() { stopSession(t, "wt") })

	out, err := run("worktree", "create", "-s", "wt", "-json", "feature/login")
	if err != nil {
		t.Fatalf("worktree create: %v\n%s", err, out)
	}
	want := filepath.Join(worktrees, "project", "feature-login")
	if !strings.Contains(out, want) {
		t.Fatalf("the worktree is not where the settings say: %s", out)
	}
	if _, err := os.Stat(filepath.Join(want, ".git")); err != nil {
		t.Fatalf("no checkout at %s: %v", want, err)
	}

	// Open as a space, filed under the repository.
	out, err = run("api", "-s", "wt", "workspace.list")
	if err != nil || !strings.Contains(out, `"group":"project"`) || !strings.Contains(out, want) {
		t.Fatalf("the new space is not in the repository's group: %s %v", out, err)
	}

	out, err = run("worktree", "list", "-s", "wt")
	if err != nil || !strings.Contains(out, "feature/login") || !strings.Contains(out, "w_2") {
		t.Fatalf("worktree list = %q, %v", out, err)
	}

	// A change in it makes an unforced remove refuse, and keep the space.
	if err := os.WriteFile(filepath.Join(want, "draft"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := run("worktree", "remove", "-s", "wt", "w_2"); err == nil || !strings.Contains(out, "worktree_dirty") {
		t.Fatalf("removing a dirty worktree = %q, %v; want a refusal", out, err)
	}
	if out, _ := run("api", "-s", "wt", "workspace.list"); !strings.Contains(out, "w_2") {
		t.Fatal("a refused removal closed the space anyway")
	}

	if out, err := run("worktree", "remove", "-s", "wt", "-force", "w_2"); err != nil {
		t.Fatalf("forced remove: %v\n%s", err, out)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Error("the checkout is still there after removing it")
	}
	if out, _ := run("api", "-s", "wt", "workspace.list"); strings.Contains(out, "w_2") {
		t.Error("the space is still open after its worktree was removed")
	}
}

// TestFollowingASessionsEventsFromTheShell: a script that reacts to a session
// reads this. If it regresses, the only way to follow a session is to poll it.
func TestFollowingASessionsEventsFromTheShell(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	bin := buildBinary(t)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")

	if out, err := exec.Command(bin, "new", "-s", "ev", "--", "/bin/sh").CombinedOutput(); err != nil {
		t.Fatalf("tend new: %v\n%s", err, out)
	}
	// The command above ran with the test's own environment; the session it
	// made is in the temporary runtime directory because of t.Setenv.
	t.Cleanup(func() { stopSession(t, "ev") })

	watch := exec.Command(bin, "events", "-s", "ev", "-kinds", "pane.opened")
	watch.Env = env
	out, err := watch.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watch.Process.Kill(); _ = watch.Wait() }()

	lines := bufio.NewScanner(out)
	// Give the subscription a moment to be in place before making an event.
	time.Sleep(300 * time.Millisecond)
	if out, err := exec.Command(bin, "new", "-s", "ev", "--", "/bin/sh").CombinedOutput(); err != nil {
		t.Fatalf("second pane: %v\n%s", err, out)
	}

	done := make(chan string, 1)
	go func() {
		for lines.Scan() {
			if strings.Contains(lines.Text(), "pane.opened") {
				done <- lines.Text()
				return
			}
		}
		done <- ""
	}()
	select {
	case line := <-done:
		if !strings.Contains(line, `"pane_id"`) {
			t.Errorf("the event does not name the pane: %q", line)
		}
	case <-time.After(15 * time.Second):
		t.Error("no event arrived on the stream")
	}
}

// TestSavingAndRebuildingALayoutFromTheShell: setting up a session by hand
// every morning is the thing a layout file exists to stop.
func TestSavingAndRebuildingALayoutFromTheShell(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	bin := buildBinary(t)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := run("new", "-s", "lay", "--", "/bin/sh"); err != nil {
		t.Fatalf("tend new: %v\n%s", err, out)
	}
	t.Cleanup(func() { stopSession(t, "lay") })
	if out, err := run("api", "-s", "lay", "pane.split",
		`{"pane_id":"p_1","direction":"down","command":["/bin/sh","-c","sleep 30"]}`); err != nil {
		t.Fatalf("split: %v\n%s", err, out)
	}

	file := filepath.Join(t.TempDir(), "layout.json")
	if out, err := run("layout", "save", "-s", "lay", "-o", file); err != nil {
		t.Fatalf("layout save: %v\n%s", err, out)
	}
	saved, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), `"tree"`) || !strings.Contains(string(saved), "sleep 30") {
		t.Fatalf("the saved layout does not describe the tab:\n%s", saved)
	}

	if out, err := run("layout", "apply", "-s", "lay", file, "-name", "rebuilt"); err != nil {
		t.Fatalf("layout apply: %v\n%s", err, out)
	}
	out, err := run("api", "-s", "lay", "session.snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, `"pane_id"`) < 4 {
		t.Errorf("the rebuilt arrangement is not there:\n%s", out)
	}
}
