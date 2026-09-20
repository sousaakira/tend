package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/sousaakira/tend/internal/pty"
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
