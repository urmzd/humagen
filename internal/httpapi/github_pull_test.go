package httpapi

import (
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"net/http"
	"testing"
)

func TestGitHubItemsAreTenantScopedPermissionCheckedAndBounded(t *testing.T) {
	fake := github.NewFake()
	f := newFixtureWith(t, platform.Config{GitHubAPI: func(string) github.API { return fake }, ClientInterval: -1})
	seatSourceClient(t, f, "acme", fake)
	reader := f.machine(t, "acme", "humagen:runs:read")
	var c github.Context
	c.Subject.Title = "A pull"
	c.Subject.Head.SHA = sourceCommit
	c.Subject.Number = 1
	c.Subject.PullRequest = &struct {
		URL string `json:"url"`
	}{URL: "https://api.github.com/repos/acme/api/pulls/1"}
	if err := fake.SeedContext(github.Ref{Owner: "acme", Repo: "api", Number: 1}, c); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"pull", "issue"} {
		base := "/api/github/" + kind + "?repo=acme%2Fapi&number=1"
		for _, tc := range []struct {
			path, token string
			want        int
		}{
			{base, reader, http.StatusOK},
			{base, f.machine(t, "globex", "humagen:runs:read"), http.StatusNotFound},
			{base, f.machine(t, "acme", "humagen:agents:read"), http.StatusForbidden},
			{base + "&page=11", reader, http.StatusBadRequest},
			{"/api/github/" + kind + "?repo=stranger%2Fsecret&number=1", reader, http.StatusNotFound},
		} {
			res, body := f.do(t, nil, "GET", tc.path, tc.token, "")
			if res.StatusCode != tc.want {
				t.Fatalf("%s: %d %v want %d", tc.path, res.StatusCode, body, tc.want)
			}
		}
	}
}
