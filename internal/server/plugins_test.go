//go:build unix

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
	script := "#!/bin/sh\nprintf '%s %s %s %s\\n' \"$1\" \"$TEND_PLUGIN_EVENT\" \"$TEND_PLUGIN_ROOT\" \"$TEND_PLUGIN_CONTEXT_JSON\" >>" + log + "\n"
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
	// Told by herdr's name, although the manifest used tend's older one.
	if !strings.Contains(text, "pane.created") {
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

// TestPluginsHearAboutFocusAndCreation: the sidebar plugin the owner runs in
// herdr hooks on pane.focused, tab.created, workspace.created and
// workspace.focused. Without those events it installs and never runs.
func TestPluginsHearAboutFocusAndCreation(t *testing.T) {
	root, log := pluginDir(t, `
id = "watcher"
name = "watcher"
version = "0.1.0"

[[events]]
on = "workspace.created"
command = ["./hook.sh", "ws-created"]

[[events]]
on = "tab.created"
command = ["./hook.sh", "tab-created"]

[[events]]
on = "pane.focused"
command = ["./hook.sh", "pane-focused"]

[[events]]
on = "workspace.focused"
command = ["./hook.sh", "ws-focused"]
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

	ws, err := s.NewWorkspace("main")
	if err != nil {
		t.Fatal(err)
	}
	waitForFile(t, log, "ws-created")

	_, pane, err := s.NewTab(ws, "t", shell("sleep 5"))
	if err != nil {
		t.Fatal(err)
	}
	waitForFile(t, log, "tab-created")

	// Focus is the client's, so it is reported rather than assumed.
	s.FocusPane(pane, 0)
	text := waitForFile(t, log, "pane-focused")
	if !strings.Contains(text, "ws-focused") {
		t.Errorf("the space was not reported as focused:\n%s", text)
	}
	if !strings.Contains(text, `"tab_id":"t_1"`) {
		t.Errorf("a hook was not told which tab:\n%s", text)
	}

	// The same pane again says nothing: a hook that runs on every keystroke
	// that moves focus inside one pane is a hook that runs constantly.
	before := text
	s.FocusPane(pane, pane)
	time.Sleep(200 * time.Millisecond)
	if after, _ := os.ReadFile(log); len(after) != len(before) {
		t.Errorf("focusing the same pane again ran the hooks:\n%s", after)
	}
}

// TestAPluginsRunsAreLogged is herdr's plugin.log.list: every run a plugin
// makes — here, a hook that fails — is kept with its status, exit code and
// output. If it regresses, a hook that stops working fails in silence.
func TestAPluginsRunsAreLogged(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fail.sh"), []byte("#!/bin/sh\necho went-wrong\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, plugin.ManifestName), []byte(`
id = "loud"
name = "loud"
version = "0.1.0"

[[events]]
on = "pane.created"
command = ["./fail.sh"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	host := hostFor(t, root)
	cfg := handoffConfig()
	cfg.Plugins = host
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	openTab(t, s, "sleep 30")

	var entry PluginLogEntry
	waitFor(t, "the hook's run in the log", func() bool {
		logs := host.Log("loud", 10)
		if len(logs) == 0 || logs[len(logs)-1].Status == "running" {
			return false
		}
		entry = logs[len(logs)-1]
		return true
	})
	if entry.Status != "failed" || entry.ExitCode == nil || *entry.ExitCode != 3 ||
		!strings.Contains(entry.Output, "went-wrong") || entry.Event != "pane.created" {
		t.Errorf("log entry = %+v", entry)
	}
	if len(host.Log("someone-else", 10)) != 0 {
		t.Error("a filter by plugin should leave other plugins' runs out")
	}
}
