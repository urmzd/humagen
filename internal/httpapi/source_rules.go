package httpapi

import (
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/sourcelinks"
	"net/http"
)

func (s *Server) sourceRuleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/source-link-rules", s.authed(auth.ClientsRead, s.listSourceRules))
	mux.HandleFunc("POST /api/source-link-rules", s.authed(auth.ClientsWrite, s.saveSourceRule))
	mux.HandleFunc("PUT /api/source-link-rules/{id}", s.authed(auth.ClientsWrite, s.seenSourceRule(s.saveSourceRule)))
	mux.HandleFunc("DELETE /api/source-link-rules/{id}", s.authed(auth.ClientsWrite, s.seenSourceRule(s.deleteSourceRule)))
	mux.HandleFunc("POST /api/source-link-rules/preview", s.authed(auth.ClientsRead, s.previewSourceRules))
}
func (s *Server) listSourceRules(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	rules, err := s.P.SourceRulesAs(who)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, rules)
}
func (s *Server) saveSourceRule(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var rule sourcelinks.Rule
	if !decode(w, r, &rule) {
		return
	}
	if rule.ID != "" || rule.CopiedFrom != "" {
		s.problem(w, sourcelinks.ErrInvalid)
		return
	}
	result, err := s.P.SaveSourceRule(who, r.PathValue("id"), rule)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, result)
}
func (s *Server) deleteSourceRule(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.DeleteSourceRule(who, r.PathValue("id")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) previewSourceRules(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		sourcelinks.Resource
		Candidate *sourcelinks.Rule `json:"candidate,omitempty"`
	}
	if !decode(w, r, &body) {
		return
	}
	matches, err := s.P.PreviewSourceRuleCandidate(who, body.Resource, body.Candidate)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"matches": matches})
}

func (s *Server) seenSourceRule(next func(http.ResponseWriter, *http.Request, auth.Principal)) func(http.ResponseWriter, *http.Request, auth.Principal) {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		rule, err := s.P.SourceLinks.Get(who.Tenant, r.PathValue("id"))
		if err != nil || !s.P.Sees(who, platform.Place{Project: projects.NameOf(rule.Project)}) {
			s.problem(w, sourcelinks.ErrNotFound)
			return
		}
		next(w, r, who)
	}
}
