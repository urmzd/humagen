package httpapi

import (
	"errors"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"net/http"
	"strconv"
)

func (s *Server) getGitHubPull(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	s.gitHubItem(w, r, who, true)
}
func (s *Server) getGitHubIssue(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	s.gitHubItem(w, r, who, false)
}
func (s *Server) gitHubItem(w http.ResponseWriter, r *http.Request, who auth.Principal, pull bool) {
	q := r.URL.Query()
	number, _ := strconv.Atoi(q.Get("number"))
	page := 1
	if q.Has("page") {
		page, _ = strconv.Atoi(q.Get("page"))
	}
	var result any
	var err error
	if pull {
		result, err = s.P.GitHubPullPage(r.Context(), who.Tenant, q.Get("repo"), number, page)
	} else {
		result, err = s.P.GitHubIssue(r.Context(), who.Tenant, q.Get("repo"), number, page)
	}
	switch {
	case errors.Is(err, platform.ErrSourceInvalid):
		fail(w, http.StatusBadRequest, "invalid repository, number or page")
		return
	case errors.Is(err, platform.ErrSourceNotFound):
		fail(w, http.StatusNotFound, "not found")
		return
	case err != nil:
		fail(w, http.StatusBadGateway, "GitHub evidence unavailable")
		return
	}
	ok(w, http.StatusOK, result)
}
