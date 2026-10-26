package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
)

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	admin := false
	for _, role := range who.Roles {
		admin = admin || role == auth.Admin
	}
	if who.Kind == auth.Machine || !admin {
		fail(w, http.StatusForbidden, "workspace administrator required")
		return
	}
	to := time.Now().UTC()
	if raw := r.URL.Query().Get("to"); raw != "" {
		v, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			fail(w, 400, "invalid audit time window")
			return
		}
		to = v
	}
	from := to.Add(-24 * time.Hour)
	if raw := r.URL.Query().Get("from"); raw != "" {
		v, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			fail(w, 400, "invalid audit time window")
			return
		}
		from = v
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > audit.MaxRows {
			fail(w, 400, "limit must be between 1 and 500")
			return
		}
		limit = v
	}
	rows, err := s.P.AuditRecords(who, audit.Query{From: from, To: to, Limit: limit, Actor: r.URL.Query().Get("actor"), Resource: r.URL.Query().Get("resource")})
	if err != nil {
		if errors.Is(err, audit.ErrInvalid) {
			fail(w, 400, "audit window must be at most 31 days")
		} else {
			s.problem(w, err)
		}
		return
	}
	ok(w, http.StatusOK, map[string]any{"records": rows})
}
