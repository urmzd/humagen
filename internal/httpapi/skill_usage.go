package httpapi

import (
	"net/http"
	"strconv"

	"github.com/urmzd/humagen/pkg/auth"
)

func (s *Server) listSkillAgents(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	row, found := s.skillRevision(w, r, who)
	if !found {
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			fail(w, http.StatusBadRequest, "limit is between 1 and 200")
			return
		}
		limit = parsed
	}
	got, err := s.P.SkillAgents(r.Context(), who, row.ID, row.Version, limit)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, got)
}
