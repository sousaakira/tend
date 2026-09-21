//go:build unix

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sousaakira/tend/internal/detect"
	"github.com/sousaakira/tend/internal/pty"
	"github.com/sousaakira/tend/internal/vt"
	"testing"
	"time"
)

// TestAnAgentThatStopsIsAnnounced is why tend exists: the agent that needs you
// is usually not the one on screen. If it regresses, the only sign is a number
// on the status bar that nobody is looking at.
func TestAnAgentThatStopsIsAnnounced(t *testing.T) {
	a := startSession(t, 100, 16)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// A second pane, so the agent is not the one focused: what is on screen is
	// deliberately not announced.
	prog := fakeAgentBin(t, "claude", `printf 'esc to interrupt\n'; sleep 1; printf 'Do you want to proceed?\n  1. Yes\n'; cat`)
	a.sendUntil(t, prog+"\n", "the agent working", func(s string) bool {
		return strings.Contains(s, "esc to interrupt")
	})
	a.send(t, "\x02|")
	a.waitForScreen(t, "the second pane", func(s string) bool { return strings.Count(s, "┌") == 2 })

	a.waitForScreen(t, "the notice that it needs answering", func(s string) bool {
		return strings.Contains(s, "claude needs attention")
	})
}

// TestOnlyStoppingIsWorthSaying covers herdr's rule and the cooldown tend
// adds: an agent flickers between working and idle while a tool runs, and a
// notification a second is worse than none.
func TestOnlyStoppingIsWorthSaying(t *testing.T) {
	t0 := time.Now()
	for _, c := range []struct {
		name     string
		previous paneNotice
		state    detect.State
		at       time.Time
		kind     int
		want     bool
	}{
		{"blocked from working", paneNotice{state: detect.StateWorking, at: t0}, detect.StateBlocked, t0, announceBlocked, true},
		{"finished", paneNotice{state: detect.StateWorking, at: t0}, detect.StateIdle, t0, announceFinished, true},
		{"idle that was never working", paneNotice{state: detect.StateIdle, at: t0}, detect.StateIdle, t0, announceNothing, false},
		{"still working", paneNotice{state: detect.StateWorking, at: t0}, detect.StateWorking, t0, announceNothing, false},
		{"blocked again, too soon", paneNotice{state: detect.StateBlocked, at: t0}, detect.StateBlocked, t0.Add(time.Second), announceNothing, false},
		{"blocked again, much later", paneNotice{state: detect.StateWorking, at: t0}, detect.StateBlocked, t0.Add(time.Hour), announceBlocked, true},
	} {
		kind, ok := worthAnnouncing(c.previous, c.state, c.at)
		if ok != c.want || (c.want && kind != c.kind) {
			t.Errorf("%s: worthAnnouncing = %d, %v; want %d, %v", c.name, kind, ok, c.kind, c.want)
		}
	}
}

// TestReloadingSettingsAppliesThemWithoutARestart: the alternative is
// restarting the server, which closes every pane — a heavy price for a value
// in a file.
func TestReloadingSettingsAppliesThemWithoutARestart(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(configPath, []byte("[ui]\nsidebar = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withConfig(t, configPath)

	a := startSessionIn(t, 100, 16, t.TempDir())
	a.waitForScreen(t, "the sidebar", func(s string) bool { return strings.Contains(s, "spaces") })

	// Edited from outside, as somebody editing their settings would.
	if err := os.WriteFile(configPath, []byte("[ui]\nsidebar = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.send(t, "\x02R")
	a.waitForScreen(t, "the sidebar to go", func(s string) bool {
		return !strings.Contains(s, "spaces") && strings.Contains(s, "settings reloaded")
	})

	// A broken file says so and changes nothing.
	if err := os.WriteFile(configPath, []byte("[ui]\nsidebar = \"yes please\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.send(t, "\x02R")
	a.waitForScreen(t, "the complaint", func(s string) bool {
		// The message is the parser's, and it begins with the file it is
		// about; the rest is as long as the path and gets cut.
		return strings.Contains(s, "config:")
	})
	if strings.Contains(a.text(), "spaces") {
		t.Error("a settings file that will not parse changed the interface anyway")
	}
}

// TestAnythingCanTellTheUserSomething: a script that finished, an agent's
// hook, a plugin — they all have something to say and no screen to say it on.
// The session has one.
func TestAnythingCanTellTheUserSomething(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	bin := buildBinary(t)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")

	// A real session, because the notification goes over the automation
	// socket, which only a real server opens.
	p, err := pty.Start(bin, []string{"attach", "-s", "say"}, pty.Options{
		Size: pty.Size{Cols: 100, Rows: 14}, Env: env,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(100, 14, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "say") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	cmd := exec.Command(bin, "notify", "-s", "say", "the build finished", "12 tests, no failures")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tend notify: %v\n%s", err, out)
	}

	a.waitForScreen(t, "the notice", func(s string) bool {
		return strings.Contains(s, "the build finished")
	})
}
