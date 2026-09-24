package server

import (
	"github.com/sousaakira/tend/internal/agents"
	"github.com/sousaakira/tend/internal/proto"
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
