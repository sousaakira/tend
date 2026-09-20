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
