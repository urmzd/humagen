package httpapi

import (
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"net/http"
)

func (s *Server) forkProject(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.ProjectForkInput
	if !decode(w, r, &in) {
		return
	}
	result, err := s.P.ForkProject(r.Context(), who, r.PathValue("name"), in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusAccepted, result)
}
func (s *Server) projectFork(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	result, err := s.P.ProjectFork(who, r.PathValue("name"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, result)
}
