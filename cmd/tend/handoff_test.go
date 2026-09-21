//go:build unix

package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/transport"
	"github.com/sousaakira/tend/internal/vt"
)

// TestHandoffReplacesTheServerUnderARunningShell is the feature end to end,
// with the real binary on both sides: a server is replaced by another process
// while a client is attached, and the shell in the pane is the same shell
// afterwards. If it regresses, picking up a new build goes back to meaning
// losing whatever every agent in the session was doing.
func TestHandoffReplacesTheServerUnderARunningShell(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")

	bin := buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", "relay"}, pty.Options{
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
		stopSession(t, "relay")
	})
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// The shell names itself. Written so the command line as typed does not
	// match: only the output has digits after the dash.
	first := regexp.MustCompile(`first-(\d+)`)
	a.send(t, "echo first-$$\n")
	var pid string
	a.waitForScreen(t, "the shell's pid", func(s string) bool {
		if m := first.FindStringSubmatch(s); m != nil {
			pid = m[1]
			return true
		}
		return false
	})

	cmd := exec.Command(bin, "handoff", "-s", "relay")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tend handoff: %v\n%s", err, out)
	}

	// Two servers have announced themselves in the one log: the one that was
	// started, and the one that took over from it.
	log, err := os.ReadFile(filepath.Join(runtimeDir, "relay.log"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(log), "listening on"); n != 2 {
		t.Fatalf("%d servers announced themselves, want 2; the log says:\n%s", n, log)
	}

	// The client lost its connection and found the new server by itself, and
	// what it types reaches the shell that was there all along. sendUntil,
	// because keys typed while it is reconnecting go nowhere.
	a.sendUntil(t, "echo second-$$\n", "the same shell to answer through the new server",
		func(s string) bool { return strings.Contains(s, "second-"+pid) })

	// What the old server had on the screen came across with the pane.
	if !strings.Contains(a.text(), "first-"+pid) {
		t.Errorf("the screen lost what was on it before the handoff:\n%s", a.text())
	}

	// The automation socket is handed over with the session one. A replacement
	// that forgot it would leave every hook talking to a dead file until the
	// next pane spawn reinvented the env — and existing agents would stay dark.
	apiPath, err := transport.APISocketPath("relay")
	if err != nil {
		t.Fatal(err)
	}
	conn := dialAPISocket(t, apiPath)
	defer conn.Close()
	r := bufio.NewReader(conn)
	if reply := callAPI(t, conn, r, `{"id":"h","method":"ping"}`); reply["error"] != nil {
		t.Errorf("automation socket after handoff: %v", reply)
	}
}

// TestNoticePromisesOnlyWhatTheServerCanDo: the notice is where somebody
// decides whether to press r with an agent mid-task. Telling them everything
// keeps running when the server can only restart loses their work on tend's
// word; warning them of a loss that will not happen keeps them on an old
// server for nothing.
func TestNoticePromisesOnlyWhatTheServerCanDo(t *testing.T) {
	with := strings.Join(staleServerOverlay(mismatch{build: "old", handoff: true}, "new", "work"), "\n")
	if !strings.Contains(with, "keeps running") || !strings.Contains(with, "tend handoff -s work") {
		t.Errorf("a server that can hand off should be offered one:\n%s", with)
	}
	if strings.Contains(with, "-server") {
		t.Errorf("and should not be pointed at the command that ends its programs:\n%s", with)
	}

	without := strings.Join(staleServerOverlay(mismatch{build: "old"}, "new", "work"), "\n")
	if !strings.Contains(without, "restart it now") || strings.Contains(without, "keeps running") {
		t.Errorf("a server that cannot hand off must say what a restart costs:\n%s", without)
	}
}

// TestHandoffRunsTheBinaryThatAskedForIt: a tend installed somewhere other
// than where the server was started from — a remote attach puts one in
// ~/.local/bin beside an older one in /usr/local/bin — hands off to itself.
// If it regresses, the handoff succeeds, every program survives, and the
// server that comes back is the old build again, with none of the fixes the
// install was for.
func TestHandoffRunsTheBinaryThatAskedForIt(t *testing.T) {
	if _, err := os.Stat("/proc/self/exe"); err != nil {
		t.Skip("needs /proc to see which binary the server runs")
	}
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")

	old := buildBinary(t)
	start := exec.Command(old, "new", "-s", "moved", "--", "/bin/sh")
	start.Env = env
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("tend new: %v\n%s", err, out)
	}
	t.Cleanup(func() { stopSession(t, "moved") })

	// The same build at another path: what differs is only where it lives.
	elsewhere := filepath.Join(t.TempDir(), "tend")
	data, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(elsewhere, data, 0o755); err != nil {
		t.Fatal(err)
	}
	handoff := exec.Command(elsewhere, "handoff", "-s", "moved")
	handoff.Env = env
	if out, err := handoff.CombinedOutput(); err != nil {
		t.Fatalf("tend handoff: %v\n%s", err, out)
	}

	if got := serverExecutable(t, "moved"); got != elsewhere {
		t.Errorf("the server after the handoff runs %s, want %s", got, elsewhere)
	}
}

// serverExecutable is the binary the session's server process runs, found by
// its command line in /proc.
func serverExecutable(t *testing.T, session string) string {
	t.Helper()
	want := "\x00serve\x00-s\x00" + session + "\x00"
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil || !strings.Contains(string(cmdline), want) {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err == nil {
			return exe
		}
	}
	t.Fatalf("no server process for session %q", session)
	return ""
}
