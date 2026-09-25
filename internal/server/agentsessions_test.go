//go:build unix

package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/auth-com-br/tend/internal/agent"
)

// TestAConversationOpenInAPaneIsNotDeleted: the sessions list says which
// conversation is open in which pane, and a delete asked for one of those
// keeps it, saying why, while the others go. If it regresses, deleting old
// sessions deletes the file of an agent still writing to it.
func TestAConversationOpenInAPaneIsNotDeleted(t *testing.T) {
	claudeDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	const open, closed = "11111111-2222-3333-4444-555555555555", "66666666-7777-8888-9999-aaaaaaaaaaaa"
	folder := filepath.Join(claudeDir, "projects", "-work")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{open, closed} {
		line := `{"type":"user","cwd":"/work","message":{"role":"user","content":"hello"}}` + "\n"
		if err := os.WriteFile(filepath.Join(folder, id+".jsonl"), []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	s := newServer(t)
	_, pane := openTab(t, s, "exec sleep 60")
	if err := s.RememberAgentSession(pane, agent.PersistedSession{Agent: "claude", Session: agent.SessionRef{ID: open}}); err != nil {
		t.Fatal(err)
	}

	list, err := s.AgentSessions()
	if err != nil {
		t.Fatal(err)
	}
	panes := map[string]uint64{}
	for _, x := range list.Sessions {
		panes[x.ID] = x.Pane
	}
	if len(panes) != 2 || panes[open] != uint64(pane) || panes[closed] != 0 {
		t.Errorf("list: %+v", list.Sessions)
	}

	res := s.DeleteAgentSessions([]string{open, closed})
	if len(res.Deleted) != 1 || res.Deleted[0] != closed || res.Kept[open] == "" {
		t.Errorf("delete: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(folder, open+".jsonl")); err != nil {
		t.Errorf("the open one went: %v", err)
	}
	if _, err := os.Stat(filepath.Join(folder, closed+".jsonl")); !os.IsNotExist(err) {
		t.Errorf("the closed one is still there: %v", err)
	}
}

// TestASessionWhoseDirectoryCannotBeEnteredSaysSo: a conversation held in a
// directory that is gone, or that this user cannot enter (one made as root
// in /root), is listed with why it cannot be resumed here. If it regresses,
// resuming it fails as "fork/exec /bin/sh: permission denied", which says
// nothing about why.
func TestASessionWhoseDirectoryCannotBeEnteredSaysSo(t *testing.T) {
	claudeDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	here := t.TempDir()
	gone := filepath.Join(t.TempDir(), "deleted-project")
	folder := filepath.Join(claudeDir, "projects", "-x")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	for id, dir := range map[string]string{
		"11111111-2222-3333-4444-555555555555": here,
		"66666666-7777-8888-9999-aaaaaaaaaaaa": gone,
	} {
		line := `{"type":"user","cwd":"` + dir + `","message":{"role":"user","content":"hello"}}` + "\n"
		if err := os.WriteFile(filepath.Join(folder, id+".jsonl"), []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	list, err := newServer(t).AgentSessions()
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range list.Sessions {
		switch x.Dir {
		case here:
			if x.Unreachable != "" {
				t.Errorf("%s: %q", x.Dir, x.Unreachable)
			}
		case gone:
			if x.Unreachable != gone+" is gone" {
				t.Errorf("%s: %q", x.Dir, x.Unreachable)
			}
		}
	}
	if os.Geteuid() != 0 {
		if why := unreachable("/root/nowhere"); why != "/root/nowhere is not open to this user" && why != "/root/nowhere is gone" {
			t.Errorf("/root: %q", why)
		}
	}
}
