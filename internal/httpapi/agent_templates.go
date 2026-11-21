package httpapi

import (
	"net/http"
	"strconv"

	"github.com/urmzd/humagen/pkg/auth"
)

// listAgentTemplates answers the goals a new agent can start from, and the
// models it starts on.
func (s *Server) listAgentTemplates(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	ok(w, http.StatusOK, s.P.AgentTemplates(r.Context(), who.Tenant))
}

// retireAgent retires an agent: it leaves the workspace, and its sessions
// and revisions stay readable. `rev`, when given, is the revision the
// caller read; one written since is a conflict.
func (s *Server) retireAgent(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	rev := 0
	if q := r.URL.Query().Get("rev"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 0 {
			fail(w, http.StatusBadRequest, "rev is a revision number")
			return
		}
		rev = n
	}
	if err := s.P.RetireAgent(r.Context(), who, r.PathValue("name"), rev); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
