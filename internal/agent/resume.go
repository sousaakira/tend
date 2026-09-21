package agent

import (
	"path/filepath"
	"strings"
	"unicode"
)

// Restoring a pane brings back the place, not the conversation. The agent
// starts again with nothing: the work it had done, the files it had read, the
// question it was halfway through answering are all in its own session, and it
// will not look for one unless it is told to.
//
// So when a hook told tend which conversation the agent was in, a restored
// pane is started with the flag that continues it. The flags are herdr's
// (`agent_resume.rs`), one per agent, because every one of them spells this
// differently.

// Limits on a reference, herdr's. It ends up on a command line, so a wild
// value is refused rather than passed on.
const (
	maxSessionID   = 256
	maxSessionPath = 4096
)

// Valid reports whether a reference is one worth putting on a command line.
func (r SessionRef) Valid() bool {
	value := r.Value()
	if value == "" || strings.ContainsFunc(value, unicode.IsControl) {
		return false
	}
	if r.Path != "" {
		return len(value) <= maxSessionPath && filepath.IsAbs(value)
	}
	return len(value) <= maxSessionID
}

// resumeFlags is how each agent is told to continue a conversation, by the
// agent's own name. The value is the arguments after the program, with %s
// where the reference goes; "=%s" joins it to the flag, as those agents want.
var resumeFlags = map[string][]string{
	"claude":     {"--resume", "%s"},
	"codex":      {"resume", "%s"},
	"copilot":    {"--resume=%s"},
	"devin":      {"--resume", "%s"},
	"droid":      {"--resume", "%s"},
	"kimi":       {"--session", "%s"},
	"mastracode": {"--thread", "%s"},
	"pi":         {"--session", "%s"},
	"omp":        {"--resume=%s"},
	"hermes":     {"--resume", "%s"},
	"opencode":   {"--session", "%s"},
	"qodercli":   {"--resume", "%s"},
	"qwen":       {"--resume", "%s"},
	"kilo":       {"--session", "%s"},
	"cursor":     {"--resume", "%s"},
	"agy":        {"--conversation", "%s"},
	"grok":       {"--resume", "%s"},
}

// programFor is the command an agent is started with, where it differs from
// the agent's own name.
var programFor = map[string]string{
	"cursor": "cursor-agent",
}

// Resume is the command that continues a conversation, and whether there is
// one.
//
// Only for the integration tend ships for that agent: the reference came from
// a hook, it goes onto a command line, and a source that is not tend's own has
// no business deciding what tend runs. That is herdr's rule too.
func Resume(p PersistedSession) ([]string, bool) {
	if !Official(p.Source, p.Agent) || !p.Session.Valid() {
		return nil, false
	}
	// pi and omp resume from a transcript path; every other agent resumes by
	// id, and a path where an id belongs would be a flag pointing at a file
	// the agent cannot read.
	if p.Session.Path != "" && p.Agent != "pi" && p.Agent != "omp" {
		return nil, false
	}

	if p.Agent == "letta" {
		// letta names a conversation and an agent inside it, joined by a
		// colon, and tells them apart on the command line.
		if id, ok := strings.CutPrefix(p.Session.Value(), "default:"); ok {
			if id == "" {
				return nil, false
			}
			return []string{"letta", "--conversation", "default", "--agent", id}, true
		}
		return []string{"letta", "--conversation", p.Session.Value()}, true
	}

	flags, ok := resumeFlags[p.Agent]
	if !ok {
		return nil, false
	}
	program := p.Agent
	if named, ok := programFor[p.Agent]; ok {
		program = named
	}
	argv := []string{program}
	for _, flag := range flags {
		argv = append(argv, strings.Replace(flag, "%s", p.Session.Value(), 1))
	}
	return argv, true
}
