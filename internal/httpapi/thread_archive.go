package httpapi

import (
	"net/http"

	"github.com/urmzd/humagen/pkg/auth"
)

// archiveRoutes put conversations away and run the admin deletion queue. An
// archived conversation waits 30 days, or is asked for now, and nothing is
// deleted until an admin approves it.
func (s *Server) archiveRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/threads/{id}/archive", s.authed(auth.RunsWrite, s.seenThread(s.archiveThread)))
	mux.HandleFunc("DELETE /api/threads/{id}/archive", s.authed(auth.RunsWrite, s.seenThread(s.unarchiveThread)))
	mux.HandleFunc("GET /api/deletions", s.authed(auth.ProjectsGovern, s.listDeletions))
	mux.HandleFunc("POST /api/deletions/{id}/approve", s.authed(auth.ProjectsGovern, s.approveDeletion))
	mux.HandleFunc("POST /api/deletions/{id}/reject", s.authed(auth.ProjectsGovern, s.rejectDeletion))
}

func (s *Server) archiveThread(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in struct {
		DeleteNow bool `json:"delete_now"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	got, err := s.P.ArchiveThread(who, r.PathValue("id"), in.DeleteNow)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.P.Seer(who).Thread(s.P.ShowThread(who.Tenant, got)))
}

func (s *Server) unarchiveThread(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	got, err := s.P.UnarchiveThread(who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.P.Seer(who).Thread(s.P.ShowThread(who.Tenant, got)))
}

func (s *Server) listDeletions(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	list, err := s.P.DeletionQueue(who)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"deletions": list})
}

func (s *Server) approveDeletion(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.ApproveDeletion(who, r.PathValue("id")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rejectDeletion(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.RejectDeletion(who, r.PathValue("id")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
