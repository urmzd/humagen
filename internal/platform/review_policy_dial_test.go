package platform

import (
	"encoding/json"
	"testing"
	"time"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/flow/address"
	github "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
)

// The connector the platform dials for a GitHub client (p.dial) approves a
// clean automatic review with green checks: it is given the store an
// approval's claim is kept in.
func TestTheDialedConnectorApprovesACleanAutomaticReview(t *testing.T) {
	api := github.NewFake()
	p, err := New(Config{Workspace: t.TempDir(), ClientInterval: -1, GitHubAPI: func(string) github.API { return api }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if _, err := p.Agents.Create(t.Context(), "t", wire.AgentSpec{Name: "gatekeeper", Provider: "offline", Model: "offline", SystemPrompt: "Review", Review: wire.Review{Approve: wire.ReviewApproveWhenClean}}, "test"); err != nil {
		t.Fatal(err)
	}
	client := clients.Client{Tenant: "t", ID: "gh", Surface: clients.GitHub, Enabled: true, Managed: clients.ManagedStatic, DefaultAgent: "gatekeeper",
		Identity: clients.Identity{Owners: []string{"acme"}, AppID: "9001"},
		GitHub:   &clients.GitHubOptions{Login: "humagen[bot]"}}
	ref := github.Ref{Owner: "acme", Repo: "site", Number: 7}
	proof := github.Context{Files: []github.ContextFile{{Filename: "a.go", Patch: "@@ -1 +1 @@\n-old\n+new"}}}
	proof.Subject.Head.SHA = approvalSHA
	proof.Subject.State = "open"
	proof.Subject.User.Login, proof.Subject.User.ID = "octocat", 583231
	if err := api.SeedContext(ref, proof); err != nil {
		t.Fatal(err)
	}
	for _, c := range []github.CommitCheck{green("test"), green("lint")} {
		if err := api.SeedCommitCheck("acme", "site", approvalSHA, c); err != nil {
			t.Fatal(err)
		}
	}
	req := clients.AutoReviewRequest{Repository: "acme/site", Number: 7, SHA: approvalSHA, Author: "octocat", AuthorID: 583231}
	if _, _, _, err := p.planAutoReview(client, req); err != nil {
		t.Fatal(err)
	}
	tag := autoReviewTag(client, "pull_request", req, "")
	at := time.Now().UTC().Add(-time.Minute)
	tag.Time = at
	wire.SetMeta(&tag, runs.MetaClient, client.ID)
	runID := runtime.RunID("t", tag.ID)
	if err := p.Runs.Record(runID, tag, 1); err != nil {
		t.Fatal(err)
	}
	run, err := p.Runs.Get("t", runID)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _, _ := p.trustedReviewRun(client, run, "acme/site", 7); !ok {
		t.Fatal("the automatic review is not trusted")
	}
	st, err := p.dial(client, resolved{token: "token", signing: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	report := presentation.ReviewReport{Format: presentation.ReviewFormatV2, Title: "Review", Verdict: "approve", Summary: "Inspected.", Findings: []presentation.ReviewFinding{}}
	text, _ := json.Marshal(report)
	done := reviewAppend(t, p, run, 1, at.Add(30*time.Second), event.Completed{Text: string(text)})
	target := address.MustParse("github://acme/site/pull/7?head_sha=" + approvalSHA)
	done.Channel = target.String()
	if err := st.peer.Deliver(t.Context(), target, done); err != nil {
		t.Fatal(err)
	}
	reviews := api.SubmittedReviews()
	if len(reviews) != 1 {
		t.Fatalf("posted %+v", reviews)
	}
	if reviews[0].Review.Event != "APPROVE" {
		t.Errorf("posted %s, want APPROVE", reviews[0].Review.Event)
	}
}
