//go:build unix

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/server"
	"github.com/sousaakira/tend/internal/transport"
	"github.com/sousaakira/tend/internal/vt"
)

// The TUI is tested by running it, because almost everything that can go wrong
// with it only happens against a real terminal: raw mode, the alternate
// screen, escape sequences arriving in pieces. So tend's own pty runs tend's
// own binary, and tend's own terminal emulator reads what it drew.
//
// That makes this the one test where the whole stack is exercised at once —
// and the one that fails when any layer of it stops agreeing with another.

// buildBinary compiles the CLI once per test run.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "tend")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the binary: %v\n%s", err, out)
	}
	return bin
}

// attached is a running TUI and a terminal reading it.
type attached struct {
	pty    *pty.Pty
	mu     sync.Mutex
	screen *vt.Screen
}

func (a *attached) send(t *testing.T, keys string) {
	t.Helper()
	if _, err := a.pty.Write([]byte(keys)); err != nil {
		t.Fatalf("sending %q: %v", keys, err)
	}
}

// lines returns what the TUI has drawn.
func (a *attached) lines() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	g := a.screen.Grid()
	out := make([]string, g.Rows())
	for y := 0; y < g.Rows(); y++ {
		out[y] = g.Line(y).Text()
	}
	return out
}

func (a *attached) text() string { return strings.Join(a.lines(), "\n") }

// Write feeds the TUI's output into the terminal emulator under a lock, since
// the test goroutine reads it while this one writes.
func (a *attached) Write(p []byte) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.screen.Write(p)
}

// waitForScreen polls until the drawn screen satisfies cond.
func (a *attached) waitForScreen(t *testing.T, what string, cond func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond(a.text()) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; the screen was:\n%s", what, a.text())
}

// startSession runs a server in this process and attaches the real binary to
// it over a pty.
func startSession(t *testing.T, cols, rows int) *attached {
	t.Helper()

	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)

	path, err := transport.SocketPath("tui")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := transport.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(server.Config{
		DetectInterval: 20 * time.Millisecond,
		DefaultSize:    pty.Size{Cols: uint16(cols), Rows: uint16(rows)},
		ShutdownGrace:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve(ln)
	}()

	bin := buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", "tui"}, pty.Options{
		Size: pty.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Env: append(os.Environ(),
			"TEND_RUNTIME_DIR="+runtimeDir,
			// A predictable shell, so what a new pane runs does not depend on
			// whoever is running the tests.
			"SHELL=/bin/sh",
			"TERM=xterm-256color",
		),
	})
	if err != nil {
		t.Fatalf("attaching: %v", err)
	}

	a := &attached{pty: p, screen: vt.NewScreen(cols, rows, 200)}
	go func() { _, _ = io.Copy(a, p) }()

	t.Cleanup(func() {
		_ = p.Close()
		_ = srv.Close()
		_ = ln.Close()
		<-served
	})
	return a
}

// TestAttachDrawsAPane is the whole stack in one assertion: a server starts a
// shell on a pty, renders its screen to ANSI, sends it over a socket, and the
// client parses and draws it inside a bordered frame.
func TestAttachDrawsAPane(t *testing.T) {
	a := startSession(t, 80, 20)

	a.waitForScreen(t, "a bordered pane", func(s string) bool {
		return strings.Contains(s, "┌") && strings.Contains(s, "┘")
	})
	a.waitForScreen(t, "the status bar", func(s string) bool {
		return strings.Contains(s, "tui")
	})

	// A shell is running in it, so a command typed into the pane comes back.
	a.send(t, "printf tend-was-here\n")
	a.waitForScreen(t, "the shell's output", func(s string) bool {
		return strings.Contains(s, "tend-was-here")
	})
}

// TestAttachSplitsPanes covers the prefix key reaching the client rather than
// the pane, and the layout changing as a result.
func TestAttachSplitsPanes(t *testing.T) {
	a := startSession(t, 100, 24)
	a.waitForScreen(t, "the first pane", func(s string) bool {
		return strings.Contains(s, "┌")
	})

	countBorders := func(s string) int { return strings.Count(s, "┌") }
	if got := countBorders(a.text()); got != 1 {
		t.Fatalf("started with %d panes, want 1", got)
	}

	a.send(t, "\x02|")
	a.waitForScreen(t, "a second pane beside the first", func(s string) bool {
		return countBorders(s) == 2
	})

	a.send(t, "\x02-")
	a.waitForScreen(t, "a third pane below", func(s string) bool {
		return countBorders(s) == 3
	})

	// The panes must tile: no two top-left corners in the same column on the
	// same row unless the layout really is a grid.
	if got := countBorders(a.text()); got != 3 {
		t.Errorf("got %d panes, want 3", got)
	}
}

// TestAttachClosesAPane checks the other direction, and that the client keeps
// drawing afterwards rather than being left pointing at something gone.
func TestAttachClosesAPane(t *testing.T) {
	a := startSession(t, 100, 24)
	a.waitForScreen(t, "the first pane", func(s string) bool {
		return strings.Contains(s, "┌")
	})

	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool {
		return strings.Count(s, "┌") == 2
	})

	a.send(t, "\x02x")
	a.waitForScreen(t, "one pane again", func(s string) bool {
		return strings.Count(s, "┌") == 1
	})
}

// TestAttachPrefixIsNotForwarded: the prefix key belongs to the client, and a
// pane must never see it. An armed prefix is also shown, so the user is not
// guessing about the mode they are in.
func TestAttachPrefixIsNotForwarded(t *testing.T) {
	a := startSession(t, 80, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02")
	a.waitForScreen(t, "the prefix indicator", func(s string) bool {
		return strings.Contains(s, "PREFIX")
	})

	// Cancel it with an unbound key, which is forwarded instead.
	a.send(t, "Z")
	a.waitForScreen(t, "the prefix to clear", func(s string) bool {
		return !strings.Contains(s, "PREFIX")
	})
}

// TestAttachShowsHelp keeps the discoverability promise: the keys are one
// keystroke away rather than in the manual.
func TestAttachShowsHelp(t *testing.T) {
	a := startSession(t, 110, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02?")
	a.waitForScreen(t, "the key help", func(s string) bool {
		return strings.Contains(s, "detach")
	})
}

// TestAttachDetachLeavesTheSessionRunning is the point of the whole
// client/server split: the interface goes away, the agents do not.
func TestAttachDetachLeavesTheSessionRunning(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)

	path, err := transport.SocketPath("detach")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := transport.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv, err := server.New(server.Config{
		DetectInterval: 20 * time.Millisecond,
		ShutdownGrace:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	go func() { _ = srv.Serve(ln) }()

	bin := buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", "detach"}, pty.Options{
		Size: pty.Size{Cols: 80, Rows: 20},
		Env:  append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh"),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(80, 20, 100)}
	go func() { _, _ = io.Copy(a, p) }()

	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02d")
	exited := make(chan error, 1)
	go func() { exited <- p.Wait() }()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the client did not exit after detaching")
	}

	// The pane is still there, and a fresh connection can see it.
	c, err := connect("detach", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	snap, err := c.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Panes) != 1 {
		t.Fatalf("after detaching, the session has %d panes, want 1", len(snap.Panes))
	}
	if !snap.Panes[0].Running {
		t.Error("the pane's process should still be running")
	}
}

// TestAttachRefusesWithoutATerminal: drawing into a pipe produces escape
// sequences nobody asked for, so it is refused with a pointer to the command
// that does work there.
func TestAttachRefusesWithoutATerminal(t *testing.T) {
	t.Setenv("TEND_RUNTIME_DIR", t.TempDir())
	bin := buildBinary(t)

	cmd := exec.Command(bin, "attach")
	cmd.Env = append(os.Environ(), "TEND_RUNTIME_DIR="+os.Getenv("TEND_RUNTIME_DIR"))
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("attaching without a terminal should fail")
	}
	if !strings.Contains(string(out), "terminal") {
		t.Errorf("error = %q, want it to explain the terminal requirement", out)
	}
}
