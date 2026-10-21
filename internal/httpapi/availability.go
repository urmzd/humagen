package httpapi

import (
	"net/http"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/projects"
)

// Agents in projects. A project turns agents on for itself and everything
// below it (enable, "*" for every platform agent), may narrow what runs there
// (allow), and names who answers a message that tags no agent (default, an
// agent or "auto"). An agent may live in one project, its home: then it is
// available there and below, and nowhere else.

// getProjectAgents reads what a project turns on, what is available there as
// a result and where each comes from, and who answers untagged messages.
func (s *Server) getProjectAgents(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	v, err := s.P.ProjectAgentsOf(who.Tenant, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, v)
}

// putProjectAgents replaces what a project turns on, as a whole. It needs
// ownership of the project; the workspace's own, which hold the workspace
// default, need projects:govern as well.
func (s *Server) putProjectAgents(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body platform.AgentsInput
	if !decode(w, r, &body) {
		return
	}
	if _, _, found := s.visible(w, who, r.PathValue("name"), projects.Owner); !found {
		return
	}
	v, err := s.P.SetProjectAgents(r.Context(), who.Tenant, r.PathValue("name"), writer(who), body)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, v)
}

// agentHome is where an agent lives, as the home route answers.
type agentHome struct {
	Agent string `json:"agent"`
	platform.AgentPlace
}

// putAgentHome makes an agent live in a project, or, with no project or "~",
// a platform agent again. It is refused, naming them, while anything outside
// the new home would lose the agent.
func (s *Server) putAgentHome(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Project string `json:"project"`
	}
	if !decode(w, r, &body) {
		return
	}
	name := r.PathValue("name")
	// Moving an agent changes both the project it leaves and the one it
	// comes to, so it needs ownership of each.
	if err := s.P.OwnsHome(who, name); err != nil {
		s.problem(w, err)
		return
	}
	if projects.NameOf(body.Project) != projects.Root {
		if _, _, found := s.visible(w, who, body.Project, projects.Owner); !found {
			return
		}
	}
	if err := s.P.SetAgentHome(r.Context(), who.Tenant, name, body.Project, writer(who)); err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, agentHome{Agent: name, AgentPlace: s.P.AgentPlaces(who.Tenant, []string{name})[name]})
}
