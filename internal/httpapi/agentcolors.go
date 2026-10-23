package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/urmzd/fiber/pkg/registry"

	"github.com/urmzd/humagen/pkg/auth"
)

// agentHues resolves only explicitly requested names, never enumerating agents.
func (s *Server) agentHues(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	raw := r.URL.Query().Get("names")
	if raw == "" {
		fail(w, http.StatusBadRequest, "names is required")
		return
	}
	names := strings.Split(raw, ",")
	if len(names) > 100 {
		fail(w, http.StatusBadRequest, "at most 100 agent names")
		return
	}
	seer := s.P.Seer(who)
	visible := make([]string, 0, len(names))
	for _, name := range names {
		if len(name) > 200 {
			fail(w, http.StatusBadRequest, "an agent name is at most 200 characters")
			return
		}
		if name == "" || !seer.Agent(name) {
			continue
		}
		if _, err := s.P.Agents.Get(r.Context(), who.Tenant, name, 0); err != nil {
			if errors.Is(err, registry.ErrNotFound) {
				continue
			}
			s.problem(w, err)
			return
		}
		visible = append(visible, name)
	}
	hues, err := s.P.AgentColors.Assign(who.Tenant, visible)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"hues": hues})
}
