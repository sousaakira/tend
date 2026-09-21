package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestAManifestIsCheckedBeforeItIsTrusted: a plugin is somebody else's file,
// and a mistake in it must be reported where it can be fixed rather than
// turning up as a missing action months later.
func TestAManifestIsCheckedBeforeItIsTrusted(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ManifestName, `
id = "good"
name = "Good"
version = "1.0.0"

[[actions]]
id = "hello"
title = "Say hello"
command = ["./hello.sh"]

[[events]]
on = "pane.opened"
command = ["./hello.sh"]

[[events]]
on = "moon.rose"
command = ["./hello.sh"]

[[panes]]
id = "side"
title = "Side"
placement = "popup"
command = ["./hello.sh"]
`, 0o600)

	m, warnings, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.ID != "good" || len(m.Actions) != 1 {
		t.Fatalf("manifest = %+v", m)
	}
	// An event this build has never heard of is a warning, not a refusal: a
	// plugin written for a newer tend should still do the rest of what it does.
	joined := strings.Join(warnings, "; ")
	if !strings.Contains(joined, "moon.rose") {
		t.Errorf("warnings = %q, want the unknown event named", joined)
	}
	// A popup has no equivalent here, and silently dropping the pane would
	// leave the user with a plugin whose button does nothing.
	if !strings.Contains(joined, "popup") {
		t.Errorf("warnings = %q, want the popup explained", joined)
	}
	if m.Panes[0].Placement != "split" {
		t.Errorf("placement = %q, want split", m.Panes[0].Placement)
	}

	for _, bad := range []string{
		"id = \"a\"\nversion = \"1\"\n[[actions]]\nid = \"x\"\ntitle = \"X\"\n",
		"id = \"a b\"\nversion = \"1\"\n",
		"id = \"a\"\n",
		"id = \"a\"\nversion = \"1\"\n[[panes]]\nid = \"p\"\ntitle = \"P\"\nplacement = \"sideways\"\ncommand = [\"x\"]\n",
	} {
		dir := t.TempDir()
		write(t, dir, ManifestName, bad, 0o600)
		if _, _, err := Load(dir); err == nil {
			t.Errorf("this manifest was accepted:\n%s", bad)
		}
	}

	if _, _, err := Load(t.TempDir()); err == nil {
		t.Error("a directory with no manifest was accepted as a plugin")
	}
}

// TestTheRegistryOutlivesTheServer: a plugin is linked once and expected to be
// there tomorrow.
func TestTheRegistryOutlivesTheServer(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ManifestName, "id = \"keeper\"\nname = \"Keeper\"\nversion = \"1.0.0\"\n", 0o600)
	path := filepath.Join(t.TempDir(), "plugins.json")

	r, err := OpenRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.List(); len(got) != 0 {
		t.Fatalf("a fresh registry has %d plugins", len(got))
	}
	if _, err := r.Link(dir); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if err := r.SetEnabled("keeper", false); err != nil {
		t.Fatal(err)
	}

	again, err := OpenRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := again.Get("keeper")
	if !ok || got.Root != dir || got.Enabled {
		t.Fatalf("after reopening: %+v, %v", got, ok)
	}
	if len(again.Enabled()) != 0 {
		t.Error("a disabled plugin came back enabled")
	}
	if err := again.Unlink("keeper"); err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Get("keeper"); ok {
		t.Error("unlink did not remove it")
	}
	// The directory is the user's; tend did not put it there and must not
	// remove it.
	if _, err := os.Stat(filepath.Join(dir, ManifestName)); err != nil {
		t.Errorf("unlink deleted the plugin's own files: %v", err)
	}
}

// TestACommandRunsAgainstItsOwnDirectory: a manifest says "./bin/thing" and
// means its own file. Resolving that on PATH would run whatever happened to
// share the name, or nothing at all.
func TestACommandRunsAgainstItsOwnDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ManifestName, "id = \"runner\"\nname = \"Runner\"\nversion = \"1\"\n", 0o600)
	write(t, dir, "say.sh", "#!/bin/sh\nprintf 'root=%s id=%s action=%s ctx=%s\\n' \"$TEND_PLUGIN_ROOT\" \"$TEND_PLUGIN_ID\" \"$TEND_PLUGIN_ACTION_ID\" \"$TEND_PLUGIN_CONTEXT_JSON\"\n", 0o755)

	r, err := OpenRegistry(filepath.Join(t.TempDir(), "p.json"))
	if err != nil {
		t.Fatal(err)
	}
	installed, err := r.Link(dir)
	if err != nil {
		t.Fatal(err)
	}

	result := Run(context.Background(), Invocation{
		Plugin: installed, Command: []string{"./say.sh"}, ActionID: "greet",
		Context: Context{PaneID: "p_3"},
	})
	if result.ExitCode != 0 || result.Err != "" {
		t.Fatalf("result = %+v", result)
	}
	for _, want := range []string{"root=" + dir, "id=runner", "action=greet", `"pane_id":"p_3"`} {
		if !strings.Contains(result.Output, want) {
			t.Errorf("output %q is missing %q", result.Output, want)
		}
	}

	// A command that is not there fails as itself, not as a shell's complaint.
	missing := Run(context.Background(), Invocation{Plugin: installed, Command: []string{"./gone.sh"}})
	if missing.ExitCode == 0 || !strings.Contains(missing.Err, "gone.sh") {
		t.Errorf("a missing command gave %+v", missing)
	}
	// A file that is there and not executable says so, rather than "exec
	// format error".
	write(t, dir, "data.txt", "not a program\n", 0o644)
	notExec := Run(context.Background(), Invocation{Plugin: installed, Command: []string{"./data.txt"}})
	if !strings.Contains(notExec.Err, "not executable") {
		t.Errorf("a non-executable file gave %+v", notExec)
	}
}

// TestACommandThatHangsIsKilled, and the wait on it ends with it — a hook that
// forks something long-lived must not hold the session open.
func TestACommandThatHangsIsKilled(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ManifestName, "id = \"hang\"\nname = \"Hang\"\nversion = \"1\"\n", 0o600)
	r, _ := OpenRegistry(filepath.Join(t.TempDir(), "p.json"))
	installed, err := r.Link(dir)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan Result, 1)
	go func() {
		done <- Run(context.Background(), Invocation{
			Plugin:  installed,
			Command: []string{"/bin/sh", "-c", "sleep 60 & sleep 60"},
			Timeout: 300 * time.Millisecond,
		})
	}()
	select {
	case result := <-done:
		if !result.TimedOut {
			t.Errorf("result = %+v, want a timeout", result)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the wait outlived the command it was waiting for")
	}
}

// TestLinkingBuildsThePlugin: a plugin linked from a checkout has not been
// built yet, so the binary its manifest points at does not exist. Without
// this, every action it offers fails later, somewhere else.
func TestLinkingBuildsThePlugin(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ManifestName, `
id = "builds"
name = "builds"
version = "1"

[[build]]
command = ["/bin/sh", "-c", "printf 'built\n' > made.txt; printf 'compiling…\n'"]
`, 0o600)

	r, err := OpenRegistry(filepath.Join(t.TempDir(), "p.json"))
	if err != nil {
		t.Fatal(err)
	}
	installed, err := r.Link(dir)
	if err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := Build(installed, nil, &out); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if made, err := os.ReadFile(filepath.Join(dir, "made.txt")); err != nil || string(made) != "built\n" {
		t.Errorf("the build did not run in the plugin's own directory: %q, %v", made, err)
	}
	// Watched rather than swallowed: a compiler's progress is the point of
	// standing there while it runs.
	if !strings.Contains(out.String(), "compiling…") {
		t.Errorf("the build's output was not passed on:\n%s", out.String())
	}
}

// TestABuildThatFailsIsReported, with what it printed — the reason is in the
// compiler's own words and nowhere else.
func TestABuildThatFailsIsReported(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ManifestName, `
id = "broken"
name = "broken"
version = "1"

[[build]]
command = ["/bin/sh", "-c", "printf 'no such crate\n' >&2; exit 3"]
`, 0o600)

	r, _ := OpenRegistry(filepath.Join(t.TempDir(), "p.json"))
	installed, err := r.Link(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err = Build(installed, nil, &out)
	if err == nil {
		t.Fatal("a build that exited 3 was reported as working")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("the error does not name the plugin: %v", err)
	}
	if !strings.Contains(out.String(), "no such crate") {
		t.Errorf("what the build said was lost:\n%s", out.String())
	}
}

// TestGithubSourcesAreHerdrsShorthandOnly: owner/repo[/subdir] and nothing
// else, so what is installed is always a GitHub repository named plainly.
func TestGithubSourcesAreHerdrsShorthandOnly(t *testing.T) {
	src, err := ParseGithubSource("alexarthurs/herdr-sidebar/plugins/herdr-sidebar")
	if err != nil || src.Owner != "alexarthurs" || src.Repo != "herdr-sidebar" || src.Subdir != "plugins/herdr-sidebar" {
		t.Errorf("parsed %+v, %v", src, err)
	}
	for _, bad := range []string{
		"https://github.com/a/b", "git@github.com:a/b.git", "a", "a/../b", "a/b/../c", "a/b c",
	} {
		if _, err := ParseGithubSource(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}
