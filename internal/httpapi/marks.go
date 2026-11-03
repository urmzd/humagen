package httpapi

import (
	"net/http"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/marks"
)

// markRoutes serve what a person marked on the conversations they read. Every
// route acts on the caller and takes no subject, so there is no way to name
// someone else, and a conversation the caller cannot read is not found.
func (s *Server) markRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/marks", s.authed(auth.RunsRead, s.getMarks))
	mux.HandleFunc("PATCH /api/threads/{id}/marks", s.authed(auth.RunsRead, s.patchThreadMarks))
	mux.HandleFunc("POST /api/marks/tips", s.authed(auth.RunsRead, s.dismissTip))
}

func (s *Server) getMarks(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	got, err := s.P.Marks.Get(who.Tenant, who.Subject)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, got)
}

// patchThreadMarks archives a conversation for the caller or brings it back,
// and marks it read now. Each field left out is left as it is.
func (s *Server) patchThreadMarks(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Archived *bool `json:"archived"`
		Read     bool  `json:"read"`
	}
	if !decode(w, r, &body) {
		return
	}
	id := r.PathValue("id")
	if !marks.ValidID(id) || !s.P.SeesThread(who, id) {
		notFound(w)
		return
	}
	got, err := s.P.Marks.Get(who.Tenant, who.Subject)
	if body.Archived != nil && err == nil {
		got, err = s.P.Marks.SetArchived(who.Tenant, who.Subject, id, *body.Archived)
	}
	if body.Read && err == nil {
		got, err = s.P.Marks.MarkRead(who.Tenant, who.Subject, id, time.Now())
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, got)
}

// dismissTip records that the caller dismissed a one-time tip.
func (s *Server) dismissTip(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &body) {
		return
	}
	got, err := s.P.Marks.DismissTip(who.Tenant, who.Subject, body.Name)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, got)
}
