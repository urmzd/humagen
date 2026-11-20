package httpapi

import (
	"github.com/urmzd/humagen/pkg/auth"
	"net/http"
)

func (s *Server) agentHealth(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if _, err := s.P.Catalog().Get(r.Context(), who.Tenant, r.PathValue("name"), 0); err != nil {
		notFound(w)
		return
	}
	out, err := s.P.AgentEvaluationHealth(who, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 200, out)
}
func (s *Server) agentChanges(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if _, err := s.P.Catalog().Get(r.Context(), who.Tenant, r.PathValue("name"), 0); err != nil {
		notFound(w)
		return
	}
	out, err := s.P.AgentChangeScores(who, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 200, out)
}
