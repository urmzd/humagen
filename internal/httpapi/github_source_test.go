package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/integrations/github"
)

const sourceCommit = "316c56f81540c3eaca02184173df322a48aebdd9"

// The evidence of a review opens as code: the lines read through the
// workspace's GitHub client, only for a repository it may read, and only
// within the bounds.
// seatSourceClient gives tenant a GitHub client serving acme, through fake,
// which holds main.go at sourceCommit in acme/api and in stranger/secret.
func seatSourceClient(t *testing.T, f *fixture, tenant string, fake *github.Fake) {
	t.Helper()
	in := platform.ClientInput{
		Client: clients.Client{
			ID: "gh", Name: "gh", Surface: clients.GitHub, Enabled: true,
			Identity: clients.Identity{Owners: []string{"acme"}}, Agents: []string{clients.AnyAgent},
		},
		Token: clients.SecretInput{Value: "token"}, SigningSecret: clients.SecretInput{Value: "secret"},
	}
	if _, err := f.p.CreateClient(context.Background(), tenant, "test", in); err != nil {
		t.Fatal(err)
	}
	for _, repo := range []string{"acme/api", "stranger/secret"} {
		owner, name, _ := strings.Cut(repo, "/")
		ref := github.Ref{Owner: owner, Repo: name, Number: 1}
		var c github.Context
		c.Subject.Head.SHA = sourceCommit
		if err := fake.SeedContext(ref, c); err != nil {
			t.Fatal(err)
		}
		if err := fake.SeedFiles(ref, map[string]string{"main.go": "package main\n\nfunc main() {\n\tprintln(1)\n}\n"}); err != nil {
			t.Fatal(err)
		}
	}
}

// sourceQuery asks for lines of main.go at sourceCommit.
func sourceQuery(repo, start, end string) string {
	q := url.Values{"repo": {repo}, "sha": {sourceCommit}, "file": {"main.go"}, "start": {start}, "end": {end}, "context": {"1"}}
	return "/api/github/source?" + q.Encode()
}

func TestGitHubSourceAnswersGrantedLinesAndNothingElse(t *testing.T) {
	fake := github.NewFake()
	f := newFixtureWith(t, platform.Config{GitHubAPI: func(string) github.API { return fake }, ClientInterval: -1})
	seatSourceClient(t, f, "acme", fake)
	reader := f.machine(t, "acme", "humagen:runs:read")
	query := sourceQuery

	res, got := f.do(t, nil, "GET", query("acme/api", "3", "4"), reader, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("granted = %d %v", res.StatusCode, got)
	}
	lines, _ := got["lines"].([]any)
	if got["first"] != float64(2) || len(lines) != 4 || lines[1] != "func main() {" || got["total"] != float64(5) {
		t.Fatalf("excerpt = %v", got)
	}
	if !strings.HasSuffix(got["url"].(string), "/blob/"+sourceCommit+"/main.go#L3-L4") {
		t.Fatalf("url = %v", got["url"])
	}

	for name, tc := range map[string]struct {
		tok, path string
		want      int
	}{
		"a repository the client neither serves nor was granted": {reader, query("stranger/secret", "1", "1"), http.StatusNotFound},
		"another workspace":   {f.machine(t, "globex", "humagen:runs:read"), query("acme/api", "1", "1"), http.StatusNotFound},
		"more than 200 lines": {reader, query("acme/api", "1", "202"), http.StatusBadRequest},
		"no lines":            {reader, query("acme/api", "", ""), http.StatusBadRequest},
		"no permission":       {f.machine(t, "acme", "humagen:agents:read"), query("acme/api", "1", "1"), http.StatusForbidden},
	} {
		if res, out := f.do(t, nil, "GET", tc.path, tc.tok, ""); res.StatusCode != tc.want {
			t.Errorf("%s = %d %v, want %d", name, res.StatusCode, out, tc.want)
		}
	}
}

// contractSource answers every status GET /api/github/source documents, in a
// workspace of its own, for TestResponsesMatchTheSpec.
func contractSource(t *testing.T, f *fixture, c *contract, fake *github.Fake) {
	t.Helper()
	seatSourceClient(t, f, "source-check", fake)
	tok := f.machine(t, "source-check", "humagen:runs:read")
	const route = "/api/github/source"
	c.call(tok, "GET", route, sourceQuery("acme/api", "3", "4"), "", http.StatusOK)
	c.call(tok, "GET", route, sourceQuery("acme/api", "0", "4"), "", http.StatusBadRequest)
	c.call(tok, "GET", route, sourceQuery("stranger/secret", "1", "1"), "", http.StatusNotFound)
	fake.Err = errors.New("github is down")
	defer func() { fake.Err = nil }()
	q := url.Values{"repo": {"acme/api"}, "sha": {sourceCommit}, "file": {"other.go"}, "start": {"1"}}
	c.call(tok, "GET", route, route+"?"+q.Encode(), "", http.StatusBadGateway)
}
