package httpapi

import (
	"net/http"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/personctx"
)

// A person's own context (pkg/personctx): their run settings, the tool
// servers they add for themselves and their memory volume. The routes act on
// the caller and take no subject, so nobody can name someone else.

func (s *Server) getPersonContext(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	out, err := s.P.PersonContext(who)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) putPersonContext(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body personctx.Input
	if !decode(w, r, &body) {
		return
	}
	out, err := s.P.SetPersonContext(r.Context(), who, body)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}
