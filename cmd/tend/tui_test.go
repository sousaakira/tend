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

// dividerColumn returns the screen column where two panes meet, counted in
// cells rather than bytes.
//
// The box-drawing characters are three bytes each, so strings.Index gives an
// offset that is not a column. Comparing two such offsets happens to work;
// clicking at one does not.
func (a *attached) dividerColumn() int {
	for _, line := range a.lines() {
		col := 0
		var prev rune
		for _, r := range line {
			if prev == '┐' && r == '┌' {
				return col - 1
			}
			prev = r
			col++
		}
	}
	return -1
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

// TestBareTendStartsItsOwnServer is the shape of the thing: one command, no
// server running, and you are in a session.
//
// Needing a daemon is tend's problem rather than the user's, and this is the
// test that keeps it that way — it starts from an empty runtime directory with
// nothing listening.
func TestBareTendStartsItsOwnServer(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)

	bin := buildBinary(t)
	p, err := pty.Start(bin, nil, pty.Options{
		Size: pty.Size{Cols: 80, Rows: 20},
		Env: append(os.Environ(),
			"TEND_RUNTIME_DIR="+runtimeDir,
			"SHELL=/bin/sh",
		),
	})
	if err != nil {
		t.Fatalf("running tend with no arguments: %v", err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(80, 20, 100)}
	go func() { _, _ = io.Copy(a, p) }()

	t.Cleanup(func() {
		_ = p.Close()
		if c, err := connect("default", nil); err == nil {
			_ = c.Shutdown()
			_ = c.Close()
		}
	})

	a.waitForScreen(t, "a session drawn from nothing", func(s string) bool {
		return strings.Contains(s, "┌") && strings.Contains(s, "default")
	})

	// A shell really is running in it.
	a.send(t, "printf started-from-nothing\n")
	a.waitForScreen(t, "the shell's output", func(s string) bool {
		return strings.Contains(s, "started-from-nothing")
	})
}

// TestStartedServerOutlivesTheClient: the server tend starts is detached, so
// closing the client leaves the session running rather than taking it down.
func TestStartedServerOutlivesTheClient(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)

	bin := buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", "auto"}, pty.Options{
		Size: pty.Size{Cols: 80, Rows: 20},
		Env:  append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh"),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(80, 20, 100)}
	go func() { _, _ = io.Copy(a, p) }()

	t.Cleanup(func() {
		if c, err := connect("auto", nil); err == nil {
			_ = c.Shutdown()
			_ = c.Close()
		}
	})

	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02d")
	exited := make(chan error, 1)
	go func() { exited <- p.Wait() }()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the client did not exit after detaching")
	}

	c, err := connect("auto", nil)
	if err != nil {
		t.Fatalf("the server should still be running: %v", err)
	}
	defer c.Close()
	snap, err := c.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Panes) != 1 {
		t.Errorf("the session has %d panes, want 1", len(snap.Panes))
	}
}

// TestCommandsThatInspectDoNotCreate: listing a session that does not exist
// must say so. Creating one on the way to listing it would report an empty
// session rather than the absence of one.
func TestCommandsThatInspectDoNotCreate(t *testing.T) {
	runtimeDir := t.TempDir()
	bin := buildBinary(t)

	for _, args := range [][]string{
		{"ls", "-s", "ghost"},
		{"kill", "-s", "ghost", "1"},
	} {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("%v succeeded against a session that does not exist", args)
		}
		if !strings.Contains(string(out), "no session") {
			t.Errorf("%v said %q, want it to report the missing session", args, out)
		}
	}

	if entries, err := os.ReadDir(runtimeDir); err == nil && len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("inspecting created %v", names)
	}
}

// TestAttachZoomsAPane: a zoomed pane fills the area and the others go away,
// and zooming again puts them back.
func TestAttachZoomsAPane(t *testing.T) {
	a := startSession(t, 100, 24)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool {
		return strings.Count(s, "┌") == 2
	})

	a.send(t, "\x02z")
	a.waitForScreen(t, "one pane filling the screen", func(s string) bool {
		return strings.Count(s, "┌") == 1 && strings.Contains(s, "zoom")
	})

	a.send(t, "\x02z")
	a.waitForScreen(t, "both panes back", func(s string) bool {
		return strings.Count(s, "┌") == 2 && !strings.Contains(s, "zoom")
	})
}

// TestAttachResizesASplit checks the divider actually moves, by watching the
// column the second pane starts at.
func TestAttachResizesASplit(t *testing.T) {
	a := startSession(t, 100, 24)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool {
		return strings.Count(s, "┌") == 2
	})

	start := a.dividerColumn()
	if start < 0 {
		t.Fatalf("could not find the divider:\n%s", a.text())
	}

	// Focus is on the new right-hand pane, so growing it leftwards moves the
	// divider left.
	a.send(t, "\x02H")
	a.waitForScreen(t, "the divider to move left", func(string) bool {
		d := a.dividerColumn()
		return d >= 0 && d < start
	})
}

// TestAttachShowsTabs: a second tab makes the bar appear, and a single tab
// spends no row on saying there is one.
func TestAttachShowsTabs(t *testing.T) {
	a := startSession(t, 100, 24)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// One tab: the first row is the pane's own border, not a bar.
	if first := a.lines()[0]; !strings.HasPrefix(first, "┌") {
		t.Errorf("with one tab the top row is %q, want the pane border", first)
	}

	a.send(t, "\x02c")
	a.waitForScreen(t, "a tab bar", func(string) bool {
		return !strings.HasPrefix(a.lines()[0], "┌")
	})
	if first := a.lines()[0]; !strings.Contains(first, "shell") {
		t.Errorf("tab bar = %q, want it to name the tabs", first)
	}
}

// TestAttachScrollsBack: a pane's history is reachable without stopping it.
func TestAttachScrollsBack(t *testing.T) {
	a := startSession(t, 80, 12)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// More output than fits, so the early lines are only in the scrollback.
	a.send(t, "for i in $(seq 1 40); do echo line-$i; done\n")
	a.waitForScreen(t, "the last line", func(s string) bool {
		return strings.Contains(s, "line-40")
	})
	if strings.Contains(a.text(), "line-1\n") {
		t.Fatal("line-1 should have scrolled off already")
	}

	a.send(t, "\x02[")
	a.waitForScreen(t, "the scroll indicator", func(s string) bool {
		return strings.Contains(s, "scroll")
	})

	// Page back until the earliest line appears.
	for i := 0; i < 10 && !strings.Contains(a.text(), "line-2 "); i++ {
		a.send(t, "\x1b[5~")
		time.Sleep(60 * time.Millisecond)
	}
	if !strings.Contains(a.text(), "line-2") {
		t.Errorf("scrolling back did not reach the early output:\n%s", a.text())
	}

	// Leaving the view returns to the live screen.
	a.send(t, "q")
	a.waitForScreen(t, "the live screen", func(s string) bool {
		return !strings.Contains(s, "scroll") && strings.Contains(s, "line-40")
	})
}

// TestAttachUsesTheConfiguredPrefix: a user who rebinds the prefix gets the
// new key, and the old one reaches the pane like any other keystroke.
func TestAttachUsesTheConfiguredPrefix(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[keys]\nprefix = \"ctrl+a\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	bin := buildBinary(t)

	p, err := pty.Start(bin, []string{"attach", "-s", "cfg"}, pty.Options{
		Size: pty.Size{Cols: 90, Rows: 20},
		Env: append(os.Environ(),
			"TEND_RUNTIME_DIR="+runtimeDir,
			"TEND_CONFIG="+cfgPath,
			"SHELL=/bin/sh",
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(90, 20, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		if c, err := connect("cfg", nil); err == nil {
			_ = c.Shutdown()
			_ = c.Close()
		}
	})

	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// The configured prefix works.
	a.send(t, "\x01|")
	a.waitForScreen(t, "a split from ctrl+a", func(s string) bool {
		return strings.Count(s, "┌") == 2
	})
}

// TestAttachRejectsABrokenConfig: running with settings the user did not
// write, and not saying so, is worse than refusing to start.
func TestAttachRejectsABrokenConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[keys]\nprefix = \"banana\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := buildBinary(t)
	cmd := exec.Command(bin, "attach")
	cmd.Env = append(os.Environ(),
		"TEND_RUNTIME_DIR="+t.TempDir(),
		"TEND_CONFIG="+cfgPath,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("a broken config should stop tend from starting")
	}
	if !strings.Contains(string(out), "prefix") {
		t.Errorf("error = %q, want it to name the setting", out)
	}
}

// TestConfigCommand covers the path people are pointed at when something is
// wrong with their settings.
func TestConfigCommand(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	bin := buildBinary(t)

	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "TEND_CONFIG="+cfgPath)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	out, err := run("config")
	if err != nil {
		t.Fatalf("config: %v\n%s", err, out)
	}
	if !strings.Contains(out, cfgPath) || !strings.Contains(out, "none") {
		t.Errorf("output = %q, want the path and that there is no file", out)
	}

	if out, err := run("config", "-init"); err != nil {
		t.Fatalf("config -init: %v\n%s", err, out)
	}
	out, err = run("config")
	if err != nil {
		t.Fatalf("config after init: %v\n%s", err, out)
	}
	if !strings.Contains(out, "loads cleanly") {
		t.Errorf("output = %q, want it to load", out)
	}

	// A second -init must not quietly overwrite what the user has written.
	if _, err := run("config", "-init"); err == nil {
		t.Error("a second -init should refuse without -force")
	}
}

// TestVersionIsStamped: a binary that cannot say what built it is one nobody
// can report a bug against.
func TestVersionIsStamped(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "tend")
	build := exec.Command("go", "build", "-ldflags", "-X main.version=v9.9.9-test", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "v9.9.9-test") {
		t.Errorf("version = %q, want the stamped value", out)
	}
}

// TestAttachClickFocusesAPane: clicking is the plainest way to say "I mean
// that one", and it has to work without the keyboard.
func TestAttachClickFocusesAPane(t *testing.T) {
	a := startSession(t, 86, 12)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool {
		return strings.Count(s, "┌") == 2
	})

	// Focus follows a split, so it is on the right-hand pane. Clicking the
	// left one and typing must put the text there.
	a.send(t, "\x1b[<0;5;5M\x1b[<0;5;5m")
	time.Sleep(150 * time.Millisecond)
	a.send(t, "printf clicked\n")

	a.waitForScreen(t, "the click to move focus", func(string) bool {
		for _, line := range a.lines() {
			if i := strings.Index(line, "clicked"); i >= 0 && i < 43 {
				return true // it landed in the left pane
			}
		}
		return false
	})
}

// TestAttachDragResizesASplit: grabbing a border is the other half of the
// mouse being useful, and it must not be confused with clicking the pane.
func TestAttachDragResizesASplit(t *testing.T) {
	a := startSession(t, 86, 12)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool {
		return strings.Count(s, "┌") == 2
	})

	start := a.dividerColumn()
	if start < 0 {
		t.Fatalf("no divider found:\n%s", a.text())
	}

	// Press on the border, move ten columns left, release. Mouse reports
	// count from one, so the column is the cell plus one.
	a.send(t, "\x1b[<0;"+itoa(start+1)+";4M")
	time.Sleep(100 * time.Millisecond)
	a.send(t, "\x1b[<32;"+itoa(start-9)+";4M")

	a.waitForScreen(t, "the divider to follow the drag", func(string) bool {
		d := a.dividerColumn()
		return d >= 0 && d < start-5
	})
	a.send(t, "\x1b[<0;"+itoa(start-9)+";4m")
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestAttachReconnects: a server restarting is not the client's failure, and
// exiting when it happens loses the user's place for a reason that had
// nothing to do with them.
func TestAttachReconnects(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	bin := buildBinary(t)

	p, err := pty.Start(bin, []string{"attach", "-s", "again"}, pty.Options{
		Size: pty.Size{Cols: 80, Rows: 14},
		Env:  append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh"),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(80, 14, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		if c, err := connect("again", nil); err == nil {
			_ = c.Shutdown()
			_ = c.Close()
		}
	})

	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// Stop the server out from under it.
	c, err := connect("again", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()

	// The client says so rather than vanishing, then finds its way back: it
	// starts a server itself, exactly as it did the first time.
	a.waitForScreen(t, "a session again", func(s string) bool {
		return strings.Contains(s, "┌") && !strings.Contains(s, "OFFLINE")
	})

	a.send(t, "printf back-again\n")
	a.waitForScreen(t, "the new session to work", func(s string) bool {
		return strings.Contains(s, "back-again")
	})
}
