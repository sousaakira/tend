//go:build unix

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/config"
)

// TestAShellCommandRunsWhereThePaneIs: a background command runs in the
// directory the pane is in now, told which pane it came from. If it
// regresses, a `git pull` bound to a key pulls whatever directory the server
// started in.
func TestAShellCommandRunsWhereThePaneIs(t *testing.T) {
	s := newServer(t)
	dir := t.TempDir()
	_, pane := openTab(t, s, "cd "+dir+" && exec sleep 30")
	waitFor(t, "the pane to be in its directory", func() bool {
		rt, _ := s.runtime(pane)
		return rt != nil && rt.pty.Cwd() == dir
	})

	out := filepath.Join(t.TempDir(), "out")
	if _, err := s.RunCommand(pane, config.CommandShell, "pwd > "+out+"; echo $TEND_ACTIVE_PANE_ID >> "+out); err != nil {
		t.Fatal(err)
	}
	var got string
	waitFor(t, "the command to run", func() bool {
		data, _ := os.ReadFile(out)
		got = string(data)
		return strings.Count(got, "\n") == 2
	})
	if want := dir + "\np_" + itoa(uint64(pane)) + "\n"; got != want {
		t.Errorf("the command wrote %q, want %q", got, want)
	}
}

// TestAPaneCommandClosesWhenItEnds: a command run in a pane of its own takes
// the pane with it when it finishes. If it regresses, every lazygit opened
// from a key leaves an "exited" frame to close by hand.
func TestAPaneCommandClosesWhenItEnds(t *testing.T) {
	s := newServer(t)
	_, from := openTab(t, s, "exec sleep 30")
	pane, err := s.RunCommand(from, config.CommandPane, "echo ran; read _")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the command's pane", func() bool {
		text, _ := s.ScreenText(pane)
		return strings.Contains(text, "ran")
	})
	if err := s.Write(pane, []byte("\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pane to close", func() bool {
		_, err := s.runtime(pane)
		return err != nil
	})
	if _, err := s.runtime(from); err != nil {
		t.Errorf("the pane it came from went too: %v", err)
	}
	if _, err := s.RunCommand(from, "window", "x"); err == nil {
		t.Error("an unknown type should be refused")
	}
	if _, err := s.RunCommand(from, config.CommandPluginAction, "nope.nothing"); err == nil {
		t.Error("a plugin action on a server without plugins should be refused")
	}
}
