package httpapi

import (
	"net/http"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
)

func (s *Server) workspaceFileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/threads/{id}/sandboxes", s.authed(auth.RunsRead, s.seenThread(s.sandboxes)))
	mux.HandleFunc("GET /api/threads/{id}/files", s.authed(auth.RunsRead, s.seenThread(s.workspaceFiles)))
	mux.HandleFunc("GET /api/threads/{id}/files/content", s.authed(auth.RunsRead, s.seenThread(s.workspaceFile)))
	mux.HandleFunc("PUT /api/threads/{id}/files/content", s.authed(auth.RunsWrite, s.seenThread(s.writeWorkspaceFile)))
	mux.HandleFunc("DELETE /api/threads/{id}/files/content", s.authed(auth.RunsWrite, s.seenThread(s.deleteWorkspaceFile)))
	mux.HandleFunc("POST /api/threads/{id}/files/publish", s.authed(auth.ArtifactsWrite, s.seenThread(s.publishWorkspaceFile)))
}

func (s *Server) sandboxes(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.Sandboxes(r.Context(), who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) workspaceFiles(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.WorkspaceFiles(r.Context(), who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) workspaceFile(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.WorkspaceFile(r.Context(), who, r.PathValue("id"), r.URL.Query().Get("path"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) writeWorkspaceFile(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if !s.workspaceFileThread(w, r, who) {
		return
	}
	var in platform.WorkspaceFileWrite
	if !decode(w, r, &in) {
		return
	}
	out, err := s.P.WriteWorkspaceFile(r.Context(), who, r.PathValue("id"), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) deleteWorkspaceFile(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if !s.workspaceFileThread(w, r, who) {
		return
	}
	if err := s.P.DeleteWorkspaceFile(r.Context(), who, r.PathValue("id"), r.URL.Query().Get("path"), r.URL.Query().Get("revision")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) publishWorkspaceFile(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if !s.workspaceFileThread(w, r, who) {
		return
	}
	var in platform.WorkspaceFilePublish
	if !decode(w, r, &in) {
		return
	}
	out, err := s.P.PublishWorkspaceFile(r.Context(), who, r.PathValue("id"), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, s.P.ShownArtifact(who.Tenant, out))
}

func (s *Server) workspaceFileThread(w http.ResponseWriter, r *http.Request, who auth.Principal) bool {
	th, err := s.P.ThreadRecord(who.Tenant, r.PathValue("id"))
	if err != nil || !s.P.Seer(who).Conversation(th.ID, th.Project) {
		notFound(w)
		return false
	}
	return true
}
