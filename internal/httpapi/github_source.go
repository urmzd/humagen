package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
)

// sourceExcerpt is lines of a file at a commit, as GET /api/github/source answers.
type sourceExcerpt struct {
	Repo  string   `json:"repo"`
	SHA   string   `json:"sha"`
	Path  string   `json:"path"`
	First int      `json:"first"`
	Start int      `json:"start"`
	End   int      `json:"end"`
	Total int      `json:"total"`
	Lines []string `json:"lines"`
	URL   string   `json:"url"`
}

// getGitHubSource answers the lines an evidence permalink points at, read
// through a GitHub client of the caller's workspace, so the client's token
// stays on the server. A repository no client may read is not found.
func (s *Server) getGitHubSource(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	q := r.URL.Query()
	number := func(name string, fallback int) (int, bool) {
		v := q.Get(name)
		if v == "" {
			return fallback, true
		}
		n, err := strconv.Atoi(v)
		return n, err == nil
	}
	start, okStart := number("start", 0)
	end, okEnd := number("end", start)
	around, okContext := number("context", 3)
	if !okStart || !okEnd || !okContext {
		fail(w, http.StatusBadRequest, "start, end and context are line counts")
		return
	}
	ex, err := s.P.GitHubSource(r.Context(), who.Tenant, platform.SourceRequest{
		Repo: q.Get("repo"), SHA: q.Get("sha"), Path: q.Get("file"), Start: start, End: end, Context: around,
	})
	switch {
	case errors.Is(err, platform.ErrSourceInvalid):
		fail(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, platform.ErrSourceNotFound):
		fail(w, http.StatusNotFound, "not found")
		return
	case err != nil:
		s.Log.Warn("httpapi: source unavailable", "tenant", who.Tenant, "err", err)
		fail(w, http.StatusBadGateway, "GitHub did not give the file as text")
		return
	}
	ok(w, http.StatusOK, sourceExcerpt{
		Repo: ex.Repo, SHA: ex.SHA, Path: ex.Path, First: ex.First, Start: ex.Start, End: ex.End,
		Total: ex.Total, Lines: ex.Lines, URL: ex.URL,
	})
}
