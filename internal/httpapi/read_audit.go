package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
)

// listReadAudit exposes exceptional reads only to workspace administrators.
func (s *Server) listReadAudit(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	admin := false
	for _, role := range who.Roles {
		if role == auth.Admin {
			admin = true
		}
	}
	if who.Kind == auth.Machine || !admin {
		fail(w, http.StatusForbidden, "workspace administrator required")
		return
	}

	to := time.Now().UTC()
	if raw := r.URL.Query().Get("to"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid audit time window")
			return
		}
		to = parsed
	}
	from := to.Add(-24 * time.Hour)
	if raw := r.URL.Query().Get("from"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid audit time window")
			return
		}
		from = parsed
	}
	limit := 100

	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 500 {
			fail(w, http.StatusBadRequest, "limit must be between 1 and 500")
			return
		}
		limit = v
	}
	if !from.Before(to) || to.Sub(from) > 31*24*time.Hour {
		fail(w, http.StatusBadRequest, "audit window must be at most 31 days")
		return
	}
	ok(w, http.StatusOK, map[string]any{"records": s.P.ReadAudit.List(who.Tenant, from, to, limit)})
}
