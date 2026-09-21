package agent

import (
	"strings"
	"testing"
)

// TestEachAgentIsResumedItsOwnWay: every agent spells "continue that
// conversation" differently, and the wrong flag starts a fresh one — which
// looks like the resume worked until the agent has no idea what you are
// talking about.
func TestEachAgentIsResumedItsOwnWay(t *testing.T) {
	for _, c := range []struct {
		agent string
		ref   SessionRef
		want  string
	}{
		{"claude", SessionRef{ID: "abc"}, "claude --resume abc"},
		{"codex", SessionRef{ID: "abc"}, "codex resume abc"},
		{"copilot", SessionRef{ID: "abc"}, "copilot --resume=abc"},
		{"omp", SessionRef{Path: "/tmp/s.jsonl"}, "omp --resume=/tmp/s.jsonl"},
		{"pi", SessionRef{Path: "/tmp/s.jsonl"}, "pi --session /tmp/s.jsonl"},
		{"cursor", SessionRef{ID: "abc"}, "cursor-agent --resume abc"},
		{"agy", SessionRef{ID: "abc"}, "agy --conversation abc"},
		{"letta", SessionRef{ID: "default:agent-7"}, "letta --conversation default --agent agent-7"},
		{"letta", SessionRef{ID: "other"}, "letta --conversation other"},
	} {
		argv, ok := Resume(PersistedSession{Source: "tend:" + c.agent, Agent: c.agent, Session: c.ref})
		if !ok {
			t.Errorf("%s: no resume command", c.agent)
			continue
		}
		if got := strings.Join(argv, " "); got != c.want {
			t.Errorf("%s: %q, want %q", c.agent, got, c.want)
		}
	}
}

// TestAResumeOnlyComesFromTheIntegrationWeShipped: the reference arrives from
// a hook and ends up on a command line. A script that could name one would be
// choosing what tend runs, with an argument of its own choosing.
func TestAResumeOnlyComesFromTheIntegrationWeShipped(t *testing.T) {
	if _, ok := Resume(PersistedSession{Source: "my-script", Agent: "claude", Session: SessionRef{ID: "x"}}); ok {
		t.Error("an unofficial source got a resume command")
	}
	if _, ok := Resume(PersistedSession{Source: "tend:claude", Agent: "claude", Session: SessionRef{ID: "a\nb"}}); ok {
		t.Error("a reference with a newline in it was accepted")
	}
	if _, ok := Resume(PersistedSession{Source: "tend:claude", Agent: "claude", Session: SessionRef{Path: "relative/path"}}); ok {
		t.Error("a relative path was accepted")
	}
	// claude resumes by id; a path is not one, whoever sent it.
	if _, ok := Resume(PersistedSession{Source: "tend:claude", Agent: "claude", Session: SessionRef{Path: "/tmp/x.jsonl"}}); ok {
		t.Error("claude was given a path where an id belongs")
	}
	if _, ok := Resume(PersistedSession{Source: "tend:nosuch", Agent: "nosuch", Session: SessionRef{ID: "x"}}); ok {
		t.Error("an agent with no resume flag got one")
	}
}

// TestCodexResumeTypedByHandIsRemembered is herdr's
// persisted_session_from_launch_args: exactly `codex resume <id>` names the
// conversation, and nothing else does.
func TestCodexResumeTypedByHandIsRemembered(t *testing.T) {
	p, ok := SessionFromLaunch([]string{"codex", "resume", "0199-abc"})
	if !ok || p.Agent != "codex" || p.Session.ID != "0199-abc" {
		t.Errorf("got %+v, %v", p, ok)
	}
	for _, argv := range [][]string{
		{"codex", "resume", "--last"}, {"codex", "resume"}, {"claude", "resume", "x"},
		{"codex", "resume", "x", "extra"},
	} {
		if _, ok := SessionFromLaunch(argv); ok {
			t.Errorf("%v should not name a conversation", argv)
		}
	}
}
