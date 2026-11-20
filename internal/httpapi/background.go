package httpapi

import (
	"errors"
	"net/http"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/background"
)

// backgroundList is the caller's own background work.
type backgroundList struct {
	Items []platform.BackgroundView `json:"items"`
}

// listBackground answers the caller's watches, goals and tasks: what is still
// running, then what ended. Nobody reads another person's.
func (s *Server) listBackground(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if who.Kind != auth.User || who.Subject == "" {
		ok(w, http.StatusOK, backgroundList{Items: []platform.BackgroundView{}})
		return
	}
	items, err := s.P.ListBackground(who)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, backgroundList{Items: items})
}

// stopBackground stops one of the caller's own. Another person's, or one
// that does not exist, is not found.
func (s *Server) stopBackground(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	v, err := s.P.StopBackground(who, r.PathValue("id"))
	if errors.Is(err, background.ErrNotFound) {
		fail(w, http.StatusNotFound, "no such background work")
		return
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, v)
}
