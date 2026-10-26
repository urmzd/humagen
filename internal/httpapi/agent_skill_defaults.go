package httpapi

import (
	"net/http"
	"slices"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
)

type agentSkillDefaultsUpdate struct {
	AgentRevision int                      `json:"agent_revision"`
	Version       int                      `json:"version"`
	Skills        []platform.AgentSkillRef `json:"skills"`
}

func (s *Server) getAgentSkillDefaults(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	got, err := s.P.AgentSkills(who.Tenant, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	got.Skills = slices.DeleteFunc(got.Skills, func(ref platform.AgentSkillRef) bool { _, err := s.P.LibrarySkill(who, ref.ID); return err != nil })
	ok(w, http.StatusOK, got)
}
func (s *Server) putAgentSkillDefaults(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	// Resolve hidden/missing agents before decoding an attacker-controlled body.
	if _, err := s.P.AgentSkills(who.Tenant, r.PathValue("name")); err != nil {
		s.problem(w, err)
		return
	}
	var body agentSkillDefaultsUpdate
	if !decode(w, r, &body) {
		return
	}
	got, err := s.P.SetAgentSkills(who, r.PathValue("name"), body.AgentRevision, body.Version, body.Skills)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, got)
}
