package httpapi

import (
	"net/http"
	"strconv"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/reposettings"
)

// A GitHub client's repositories each have settings of their own, kept in
// the workspace: automatic review, the agent that answers a mention naming
// none, instructions, and what is read before a run starts. Anyone who may
// read clients may read them; only someone who may change clients may change
// them. A repository the client does not cover, and a client of another
// workspace, are not found.
func (s *Server) githubRepoRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/clients/{id}/repositories", s.authed(auth.ClientsRead, s.listGitHubRepositories))
	mux.HandleFunc("GET /api/clients/{id}/repositories/{owner}/{repo}", s.authed(auth.ClientsRead, s.getGitHubRepository))
	mux.HandleFunc("PUT /api/clients/{id}/repositories/{owner}/{repo}", s.authed(auth.ClientsWrite, s.putGitHubRepository))
	mux.HandleFunc("DELETE /api/clients/{id}/repositories/{owner}/{repo}", s.authed(auth.ClientsWrite, s.deleteGitHubRepository))
}

// githubRepoBody is a repository's settings as a caller writes them, with
// the version they read: 0 while it follows its client.
type githubRepoBody struct {
	AutoReview        *reposettings.AutoReview `json:"auto_review"`
	Agent             string                   `json:"agent"`
	Instructions      string                   `json:"instructions"`
	Reads             []string                 `json:"reads"`
	CrossSurfaceReads *bool                    `json:"cross_surface_reads"`
	Approve           string                   `json:"approve"`
	Version           int64                    `json:"version"`
}

func repoPath(r *http.Request) string { return r.PathValue("owner") + "/" + r.PathValue("repo") }

func (s *Server) listGitHubRepositories(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.GitHubRepositories(r.Context(), who.Tenant, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) getGitHubRepository(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.GitHubRepository(r.Context(), who.Tenant, r.PathValue("id"), repoPath(r))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) putGitHubRepository(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body githubRepoBody
	if !decode(w, r, &body) {
		return
	}
	in := reposettings.Settings{AutoReview: body.AutoReview, Agent: body.Agent, Instructions: body.Instructions, Reads: body.Reads, CrossSurfaceReads: body.CrossSurfaceReads, Approve: body.Approve}
	out, err := s.P.SetGitHubRepository(r.Context(), who, r.PathValue("id"), repoPath(r), in, body.Version)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

// deleteGitHubRepository returns a repository to its client's defaults. The
// version read is given as ?version=.
func (s *Server) deleteGitHubRepository(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	// Another workspace's client, or a repository it does not cover, is not
	// found before the version is read.
	if _, err := s.P.GitHubRepository(r.Context(), who.Tenant, r.PathValue("id"), repoPath(r)); err != nil {
		s.problem(w, err)
		return
	}
	version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
	if err != nil || version <= 0 {
		fail(w, http.StatusBadRequest, "name the version of the settings you read as ?version=")
		return
	}
	out, err := s.P.ResetGitHubRepository(r.Context(), who, r.PathValue("id"), repoPath(r), version)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}
