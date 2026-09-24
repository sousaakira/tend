package server

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/sousaakira/tend/internal/agents"
	"github.com/sousaakira/tend/internal/agentsessions"
	"github.com/sousaakira/tend/internal/integration"
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/session"
)

// AgentsCatalog is the agent CLIs tend knows of, as found on this machine,
// for agents.catalog. The server answers it because the agents a pane can
// run are the server machine's, which over --remote is not the client's.
func (s *Server) AgentsCatalog() proto.AgentsCatalogResult {
	found := agents.Find(agents.System, agents.Catalog())
	out := proto.AgentsCatalogResult{Agents: make([]proto.AgentStatus, 0, len(found))}
	for _, a := range found {
		out.Agents = append(out.Agents, proto.AgentStatus{
			ID: a.ID, Name: a.Name, Description: a.Description,
			Installed: a.Installed, Path: a.Path, Version: a.Version,
			InstallKind: a.Install.Kind, InstallCommand: a.Install.Command,
			InstallSource: a.Install.Source, Missing: a.Missing,
		})
	}
	return out
}

// claude is the reader of Claude Code's conversations on this machine, the
// same one each time so what it has read stays read.
func (s *Server) claude() (*agentsessions.Claude, error) {
	s.sessionsOnce.Do(func() {
		if dir, err := integration.ClaudeDir(); err == nil {
			s.claudeSessions = agentsessions.NewClaude(dir)
		}
	})
	if s.claudeSessions == nil {
		return nil, errors.New("no home directory to find Claude Code's sessions in")
	}
	return s.claudeSessions, nil
}

// openConversations is which pane each conversation is open in, by the
// session a hook reported for it. Found under the lock, asked of each
// pane after it, as the pane's own lock comes after the server's.
func (s *Server) openConversations() map[string]session.PaneID {
	s.mu.Lock()
	ids := make([]session.PaneID, 0, len(s.runtimes))
	for id := range s.runtimes {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	open := map[string]session.PaneID{}
	for _, id := range ids {
		if p, ok, err := s.AgentSession(id); err == nil && ok && p.Session.ID != "" {
			open[p.Agent+"\x00"+p.Session.ID] = id
		}
	}
	return open
}

// AgentSessions is agent_sessions.list: the conversations on this machine,
// where the agents run, with the pane each is open in. It reads the disk,
// and holds no lock while it does.
func (s *Server) AgentSessions() (proto.AgentSessionsResult, error) {
	c, err := s.claude()
	if err != nil {
		return proto.AgentSessionsResult{}, err
	}
	list, err := c.List()
	if err != nil {
		return proto.AgentSessionsResult{}, err
	}
	open := s.openConversations()
	out := proto.AgentSessionsResult{Sessions: make([]proto.AgentSessionInfo, 0, len(list))}
	reach := map[string]string{}
	for _, x := range list {
		why, seen := reach[x.Dir]
		if !seen {
			why = unreachable(x.Dir)
			reach[x.Dir] = why
		}
		out.Sessions = append(out.Sessions, proto.AgentSessionInfo{
			Agent: x.Agent, ID: x.ID, Title: x.Title, Dir: x.Dir,
			Modified: x.Modified.Unix(), Size: x.Size, Prompts: x.Prompts,
			Pane:        uint64(open[x.Agent+"\x00"+x.ID]),
			Unreachable: why,
		})
	}
	return out, nil
}

// DeleteAgentSessions is agent_sessions.delete. A conversation open in a
// pane is kept, whatever was asked: the agent is writing to it, and would
// go on writing to a file that is gone.
func (s *Server) DeleteAgentSessions(ids []string) proto.AgentSessionsDeleteResult {
	out := proto.AgentSessionsDeleteResult{Deleted: []string{}, Kept: map[string]string{}}
	c, err := s.claude()
	if err != nil {
		for _, id := range ids {
			out.Kept[id] = err.Error()
		}
		return out
	}
	open := s.openConversations()
	for _, id := range ids {
		if pane, ok := open["claude\x00"+id]; ok {
			out.Kept[id] = fmt.Sprintf("open in pane %d", pane)
			continue
		}
		if err := c.Delete(id); err != nil {
			out.Kept[id] = err.Error()
			continue
		}
		out.Deleted = append(out.Deleted, id)
	}
	return out
}

// unreachable is why a pane could not be started in dir, or "". A
// conversation Claude Code held as root under this user's home — most of
// the owner's were — is in /root, which this user cannot enter: starting
// there failed as "fork/exec /bin/sh: permission denied", which named
// neither the directory nor the reason.
func unreachable(dir string) string {
	if dir == "" {
		return ""
	}
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return dir + " is gone"
	case errors.Is(err, fs.ErrPermission):
		return dir + " is not open to this user"
	case err != nil:
		return err.Error()
	case !info.IsDir():
		return dir + " is not a directory"
	}
	f, err := os.Open(dir)
	if errors.Is(err, fs.ErrPermission) {
		return dir + " is not open to this user"
	}
	if err != nil {
		return err.Error()
	}
	_ = f.Close()
	return ""
}
