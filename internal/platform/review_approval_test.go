package platform

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	gh "github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/sourcelinks"
	"github.com/urmzd/orchagent/flow/address"
	"github.com/urmzd/orchagent/flow/connector"
	transport "github.com/urmzd/orchagent/flow/connectors/github"
)

func TestNativeReviewApprovalUsesDestinationProject(t *testing.T) {
	api := transport.NewFake()
	p, err := New(Config{Workspace: t.TempDir(), GitHubAPI: func(string) gh.API { return api }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	for _, name := range []string{"Origin", "Destination"} {
		pr, err := p.CreateProject(t.Context(), "t", projects.Writer{By: "admin", Govern: true}, ProjectInput{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		if name == "Origin" {
			if _, err = p.Projects.Update("t", pr.Name, pr.Version, projects.Writer{By: "admin", Govern: true}, time.Now(), func(next *projects.Project) error { next.ApproveReviews = true; return nil }); err != nil {
				t.Fatal(err)
			}
		}
	}
	c, err := p.Clients.Create("t", clients.Client{ID: "app", Name: "App", Surface: clients.GitHub, Identity: clients.Identity{Owners: []string{"acme"}}, Enabled: true, Agents: []string{"lead"}, Home: &clients.Home{Project: "Origin"}, GitHub: &clients.GitHubOptions{Login: "humagen[bot]", ActionRepositories: []string{"acme/target"}}}, clients.DefaultCaps(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ref := transport.Ref{Owner: "acme", Repo: "target", Number: 1}
	sha := strings.Repeat("a", 40)
	proof := transport.Context{}
	proof.Subject.Head.SHA = sha
	if err := api.SeedContext(ref, proof); err != nil {
		t.Fatal(err)
	}
	resource := sourcelinks.Resource{Provider: "github", ID: "acme/target", Attributes: map[string][]string{"repository": {"acme/target"}}}
	links, err := p.SourceSuggestions.Observe("t", c.ID, "", resource, []sourcelinks.Match{{Project: "Destination", Mode: sourcelinks.Suggest}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.SourceSuggestions.Decide("t", links[0].ID, "link", links[0].Version, time.Now()); err != nil {
		t.Fatal(err)
	}
	source := address.MustParse("github://acme/source/pull/2")
	tag := wire.NewEvent(wire.Raise{ID: "request", Tenant: "t", Agent: "lead", Origin: runs.OriginGitHub, Channel: source.String(), Text: "Review", At: time.Now(), Context: map[string]string{runs.MetaClient: c.ID, runs.MetaProject: "Origin"}})
	if err := p.Runs.Record("review-run", tag, 1); err != nil {
		t.Fatal(err)
	}
	st, err := p.dial(c, resolved{token: "test", signing: "test"})
	if err != nil {
		t.Fatal(err)
	}
	var action connector.Action
	for _, a := range st.peer.Actions() {
		if a.Name == "github_create_review" {
			action = a
		}
	}
	if action.Invoke == nil {
		t.Fatal("review action unavailable")
	}
	args, _ := json.Marshal(map[string]any{"address": "github://acme/target/pull/1", "commit_id": sha, "event": "APPROVE", "body": "Looks good"})
	if _, err := action.Invoke(connector.WithRunID(t.Context(), "review-run"), source, args); err == nil {
		t.Fatal("origin project approval bypassed destination denial")
	}
	pr, _ := p.Projects.Tree("t").Get("Destination")
	if _, err := p.Projects.Update("t", pr.Name, pr.Version, projects.Writer{By: "admin", Govern: true}, time.Now(), func(next *projects.Project) error { next.ApproveReviews = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := action.Invoke(connector.WithRunID(t.Context(), "review-run"), source, args); err != nil {
		t.Fatalf("explicit destination grant refused: %v", err)
	}
	if p.reviewApproval(c, transport.View{Tenant: "other", RunID: "review-run", Channel: "github://acme/target/pull/1"}) {
		t.Fatal("foreign tenant approved")
	}
	conflict, err := p.SourceSuggestions.Observe("t", c.ID, "", resource, []sourcelinks.Match{{Project: "Origin", Mode: sourcelinks.Suggest}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.SourceSuggestions.Decide("t", conflict[0].ID, "link", conflict[0].Version, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.reviewApproval(c, transport.View{Tenant: "t", RunID: "review-run", Channel: "github://acme/target/pull/1"}) {
		t.Fatal("ambiguous destination approved")
	}
}
