package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
)

func (s *Server) discover(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	query := r.URL.Query()
	in := platform.DiscoveryQuery{Q: query.Get("q")}
	if query.Has("limit") {
		n, err := strconv.Atoi(query.Get("limit"))
		if err != nil || n < 1 || n > 50 {
			fail(w, http.StatusBadRequest, "limit is between 1 and 50")
			return
		}
		in.Limit = n
	}
	if kinds := query.Get("kinds"); kinds != "" {
		in.Kinds = strings.Split(kinds, ",")
	}
	if query.Has("project") {
		project := query.Get("project")
		in.Project = &project
	}
	items, err := s.P.Discover(r.Context(), who, in)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"items": items})
}
