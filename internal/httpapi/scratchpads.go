package httpapi

import (
	"net/http"
	"strconv"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
)

func (s *Server) scratchpadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/threads/{id}/scratchpads", s.authed(auth.RunsRead, s.seenThread(s.listScratchpads)))
	mux.HandleFunc("POST /api/threads/{id}/scratchpads", s.authed(auth.RunsWrite, s.seenThread(s.createScratchpad)))
	mux.HandleFunc("GET /api/threads/{id}/scratchpads/{pad}", s.authed(auth.RunsRead, s.seenThread(s.getScratchpad)))
	mux.HandleFunc("PUT /api/threads/{id}/scratchpads/{pad}", s.authed(auth.RunsWrite, s.seenThread(s.updateScratchpad)))
	mux.HandleFunc("DELETE /api/threads/{id}/scratchpads/{pad}", s.authed(auth.RunsWrite, s.seenThread(s.deleteScratchpad)))
}

func (s *Server) listScratchpads(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.ConversationScratchpads(r.Context(), who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) getScratchpad(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.ConversationScratchpad(r.Context(), who, r.PathValue("id"), r.PathValue("pad"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) createScratchpad(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if !s.workspaceFileThread(w, r, who) {
		return
	}
	var in platform.ScratchpadInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.P.CreateScratchpad(r.Context(), who, r.PathValue("id"), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, out)
}

func (s *Server) updateScratchpad(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if !s.workspaceFileThread(w, r, who) {
		return
	}
	var in platform.ScratchpadInput
	if !decode(w, r, &in) {
		return
	}
	out, err := s.P.UpdateScratchpad(r.Context(), who, r.PathValue("id"), r.PathValue("pad"), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) deleteScratchpad(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if !s.workspaceFileThread(w, r, who) {
		return
	}
	version, _ := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
	if err := s.P.DeleteScratchpad(r.Context(), who, r.PathValue("id"), r.PathValue("pad"), version); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
