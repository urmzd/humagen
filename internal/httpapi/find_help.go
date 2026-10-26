package httpapi

import (
	"github.com/urmzd/humagen/pkg/auth"
	"net/http"
)

func (s *Server) findHelp(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	found, err := s.P.FindHelp(r.Context(), who, r.URL.Query().Get("q"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"suggestions": found})
}
