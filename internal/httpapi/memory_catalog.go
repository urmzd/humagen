package httpapi

import (
	"github.com/urmzd/humagen/pkg/auth"
	"net/http"
	"strconv"
)

func (s *Server) listMemoryFolders(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 50 {
			fail(w, 400, "limit is between 1 and 50")
			return
		}
	}
	folders, err := s.P.MemoryFolders(r.Context(), who, r.URL.Query().Get("q"), limit, r.URL.Query().Get("selected"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, 200, map[string]any{"folders": folders})
}
