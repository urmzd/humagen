package httpapi

import (
	"github.com/urmzd/humagen/pkg/auth"
	"net/http"
)

func (s *Server) sourceRecoveryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/knowledge/{name}/resources", s.authed(auth.KnowledgeRead, s.seenSource(s.sourceResources)))
	mux.HandleFunc("POST /api/knowledge/{name}/snapshot", s.authed(auth.KnowledgeWrite, s.seenSource(s.snapshotSource)))
	mux.HandleFunc("POST /api/knowledge/{name}/replace", s.authed(auth.KnowledgeWrite, s.seenSource(s.replaceSource)))
	mux.HandleFunc("PUT /api/knowledge/{name}/sync-settings", s.authed(auth.KnowledgeWrite, s.seenSource(s.sourceSyncSettings)))
}
func (s *Server) sourceResources(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	resources, err := s.P.SourceResources(r.Context(), who, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"resources": resources, "complete": true})
}
func (s *Server) snapshotSource(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	source, err := s.P.SnapshotSource(who, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, source)
}
func (s *Server) replaceSource(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	source, err := s.P.ReplaceSource(r.Context(), who, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, source)
}
func (s *Server) sourceSyncSettings(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Every int `json:"every_seconds"`
		Stop  int `json:"stop_after_seconds"`
	}
	if !decode(w, r, &body) {
		return
	}
	source, err := s.P.SetSourceSyncSettings(who, r.PathValue("name"), body.Every, body.Stop)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, source)
}
