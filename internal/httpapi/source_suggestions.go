package httpapi

import (
	"github.com/urmzd/humagen/pkg/auth"
	"net/http"
	"strconv"
)

func (s *Server) sourceSuggestionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/source-link-suggestions", s.authed(auth.ClientsRead, s.listSourceSuggestions))
	mux.HandleFunc("POST /api/source-link-suggestions/{id}/decision", s.authed(auth.ClientsWrite, s.seenSourceSuggestion(s.decideSourceSuggestion)))
	mux.HandleFunc("GET /api/knowledge/{name}/attachments", s.authed(auth.KnowledgeRead, s.seenSource(s.sourceAttachments)))
	mux.HandleFunc("PUT /api/knowledge/{name}/attachment", s.authed(auth.KnowledgeWrite, s.seenSource(s.attachSource)))
	mux.HandleFunc("DELETE /api/knowledge/{name}/attachment", s.authed(auth.KnowledgeWrite, s.seenSource(s.detachSource)))
}
func (s *Server) listSourceSuggestions(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	query := r.URL.Query()
	status := query.Get("state")
	if status == "" {
		status = "suggested"
	}
	if status != "suggested" && status != "linked" && status != "ignored" {
		fail(w, 400, "invalid suggestion state")
		return
	}
	limit := 50
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			fail(w, 400, "limit must be between1 and200")
			return
		}
		limit = parsed
	}
	ok(w, http.StatusOK, map[string]any{"suggestions": s.P.SourceSuggestionsAs(who, query.Get("client"), query.Get("knowledge"), status, limit)})
}
func (s *Server) seenSourceSuggestion(next func(http.ResponseWriter, *http.Request, auth.Principal)) func(http.ResponseWriter, *http.Request, auth.Principal) {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		if _, err := s.P.SourceSuggestionAs(who, r.PathValue("id")); err != nil {
			s.problem(w, err)
			return
		}
		next(w, r, who)
	}
}
func (s *Server) decideSourceSuggestion(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Action  string `json:"action"`
		Version int    `json:"version"`
	}
	if !decode(w, r, &body) {
		return
	}
	result, err := s.P.DecideSourceSuggestion(who, r.PathValue("id"), body.Action, body.Version)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 200, result)
}
func (s *Server) sourceAttachments(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	result, err := s.P.SourceAttachmentsAs(who, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 200, map[string]any{"attachments": result})
}
func (s *Server) attachSource(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Project string `json:"project"`
		Channel string `json:"channel"`
	}
	if !decode(w, r, &body) {
		return
	}
	result, err := s.P.AttachSource(who, r.PathValue("name"), body.Project, body.Channel)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 200, result)
}
func (s *Server) detachSource(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.DetachSource(who, r.PathValue("name"), r.URL.Query().Get("project"), r.URL.Query().Get("channel")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
