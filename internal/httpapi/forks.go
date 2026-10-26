package httpapi

import (
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"net/http"
)

func (s *Server) forkThread(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.ForkInput
	if !decode(w, r, &in) {
		return
	}
	th, err := s.P.ForkThread(r.Context(), who, r.PathValue("id"), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, s.P.Seer(who).Thread(s.P.ShowThread(who.Tenant, th)))
}

func (s *Server) bringBackResult(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.BringBackInput
	if !decode(w, r, &in) {
		return
	}
	m, err := s.P.BringBackResult(r.Context(), who, r.PathValue("id"), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	// Reuse the public message projection: subjects never leave as raw ids.
	shown := s.P.ShowPosted(who.Tenant, platform.Posted{Message: m})
	ok(w, http.StatusCreated, shown.Message)
}
