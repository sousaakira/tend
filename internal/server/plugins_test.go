package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sousaakira/tend/internal/plugin"
	"github.com/sousaakira/tend/internal/pty"
)

// pluginDir writes a plugin whose hooks record what they were told, so a test
// can assert on what actually ran rather than on the host's own bookkeeping.
func pluginDir(t *testing.T, manifest string) (root, log string) {
	t.Helper()
	root = t.TempDir()
	log = filepath.Join(root, "ran.log")
	script := "#!/bin/sh\nprintf '%s %s %s\\n' \"$1\" \"$TEND_PLUGIN_EVENT\" \"$TEND_PLUGIN_ROOT\" >>" + log + "\n"
	if err := os.WriteFile(filepath.Join(root, "hook.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, plugin.ManifestName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, log
}

func hostFor(t *testing.T, root string) *Plugins {
	t.Helper()
	reg, err := plugin.OpenRegistry(filepath.Join(t.TempDir(), "plugins.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Link(root); err != nil {
		t.Fatalf("link: %v", err)
	}
	return &Plugins{Registry: reg, ConfigDir: t.TempDir(), StateDir: t.TempDir()}
}

func waitForFile(t *testing.T, path, contains string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), contains) {
			return string(b)
		}
		time.Sleep(20 * time.Millisecond)
	}
	b, _ := os.ReadFile(path)
	t.Fatalf("timed out waiting for %q in %s; it holds:\n%s", contains, path, b)
	return ""
}

// TestAPluginsHooksRunOnSessionEvents is the whole point of the host: a plugin
// that asked to hear about panes hears about them, with enough context to act.
func TestAPluginsHooksRunOnSessionEvents(t *testing.T) {
	root, log := pluginDir(t, `
id = "recorder"
name = "recorder"
version = "0.1.0"

[[startup]]
command = ["./hook.sh", "startup-ran"]

[[events]]
on = "pane.opened"
command = ["./hook.sh", "opened"]
`)
	host := hostFor(t, root)
	s, err := build(Config{
		DetectInterval: 10 * time.Millisecond,
		DefaultSize:    pty.Size{Cols: 80, Rows: 24},
		Plugins:        host,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.startLoops()
	t.Cleanup(func() { _ = s.Close() })

	s.RunStartupPlugins()
	waitForFile(t, log, "startup-ran")

	// Opening a pane publishes from under the server lock. If the host reached
	// for that lock to describe the event, this call would never return — it
	// did, before the notification was moved off the publisher's goroutine.
	done := make(chan struct{})
	go func() {
		defer close(done)
		ws, _ := s.NewWorkspace("main")
		_, _, _ = s.NewTab(ws, "t", shell("sleep 5"))
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("opening a pane never returned; the plugin host is holding the server lock")
	}

	text := waitForFile(t, log, "opened")
	if !strings.Contains(text, "pane.opened") {
		t.Errorf("the hook was not told which event it was: %s", text)
	}
	if !strings.Contains(text, root) {
		t.Errorf("the hook was not told where its plugin is: %s", text)
	}
}

// TestASlowPluginDoesNotPileUp: an event hook runs on things that happen
// constantly. Without a limit, one slow hook becomes a process per event until
// the machine gives out.
func TestASlowPluginDoesNotPileUp(t *testing.T) {
	root, _ := pluginDir(t, `
id = "slow"
name = "slow"
version = "0.1.0"

[[events]]
on = "agent.state"
command = ["/bin/sh", "-c", "sleep 30"]
`)
	host := hostFor(t, root)
	s, err := build(Config{DefaultSize: pty.Size{Cols: 80, Rows: 24}, Plugins: host})
	if err != nil {
		t.Fatal(err)
	}
	// Closing is part of what is under test: a hook still running must not
	// hold the session open. It did, for the full half minute of its sleep,
	// because the command's output pipe outlived the process that was killed.
	t.Cleanup(func() {
		closed := make(chan struct{})
		go func() { _ = s.Close(); close(closed) }()
		select {
		case <-closed:
		case <-time.After(10 * time.Second):
			t.Error("closing the server waited for a hook that had been killed")
		}
	})

	for i := 0; i < hookLimit*3; i++ {
		s.notifyPlugins(Event{Kind: EventPaneState, Pane: 1})
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		host.mu.Lock()
		running := host.running["slow"]
		host.mu.Unlock()
		if running > hookLimit {
			t.Fatalf("%d hooks are running at once, the limit is %d", running, hookLimit)
		}
		if running == hookLimit {
			return // the limit held while more events arrived
		}
		time.Sleep(20 * time.Millisecond)
	}
}
