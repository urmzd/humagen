package httpapi

import (
	"errors"
	"net/http"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
)

// sessionBrowser does what a person asked of the session's browser by hand
// and answers with the page as it is afterwards (platform.SessionBrowser,
// which holds the call to what the agent's own is held to).
func (s *Server) sessionBrowser(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.BrowserAction
	if !decode(w, r, &in) {
		return
	}
	view, err := s.P.SessionBrowser(r.Context(), who, r.PathValue("id"), in)
	switch {
	case err == nil:
		ok(w, http.StatusOK, view)
	case errors.Is(err, platform.ErrNoBrowser):
		fail(w, http.StatusNotFound, "this session has no browser to act on")
	case errors.Is(err, platform.ErrBrowserInput):
		fail(w, http.StatusBadRequest, detail(err, platform.ErrBrowserInput))
	case errors.Is(err, platform.ErrBrowserFailed):
		fail(w, http.StatusBadGateway, detail(err, platform.ErrBrowserFailed))
	default:
		s.problem(w, err)
	}
}
