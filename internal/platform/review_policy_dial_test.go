package platform

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
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

// appNamed is the fake GitHub as an App's installation: it also reads the
// login GitHub names for the App's bot account.
type appNamed struct {
	*github.Fake
	login string
	err   error
}

func (a appNamed) AppLogin(context.Context) (string, error) { return a.login, a.err }

// testAppKey is a PEM private key, the credential a GitHub App client
// authenticates with.
func testAppKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// The connector the platform dials for a GitHub client (p.dial) approves a
// clean automatic review with green checks only for a client that
// authenticates as a GitHub App whose bot account, as GitHub names it, is
// the client's login. A token client, with an app ID and a bot login or
// not, posts a comment, and so does an App whose login GitHub names
// otherwise or cannot read.
func TestTheDialedConnectorApprovesOnlyAsTheApp(t *testing.T) {
	key := testAppKey(t)
	for name, c := range map[string]struct {
		token   string
		login   string
		named   string
		readErr error
		want    string
	}{
		"an App, named as its login":        {token: key, login: "humagen[bot]", named: "humagen[bot]", want: "APPROVE"},
		"a token with an app ID":            {token: "ghp_token", login: "humagen[bot]", named: "humagen[bot]", want: "COMMENT"},
		"an App GitHub names otherwise":     {token: key, login: "humagen[bot]", named: "other-app[bot]", want: "COMMENT"},
		"an App whose name cannot be read":  {token: key, login: "humagen[bot]", readErr: errors.New("github: app read returned status 502"), want: "COMMENT"},
		"an App with a login of no account": {token: key, login: "humagen", named: "humagen[bot]", want: "COMMENT"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := dialedReview(t, c.token, c.login, appNamed{login: c.named, err: c.readErr}); got != c.want {
				t.Errorf("posted %s, want %s", got, c.want)
			}
		})
	}
}

// dialedReview delivers a clean automatic review through the connector
// p.dial builds for a client with app ID 123, the given credential and
// login, and returns the event GitHub was sent.
func dialedReview(t *testing.T, token, login string, named appNamed) string {
	t.Helper()
	fake := github.NewFake()
	named.Fake = fake
	p, err := New(Config{Workspace: t.TempDir(), ClientInterval: -1, GitHubAPI: func(string) github.API { return named }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if _, err := p.Agents.Create(t.Context(), "t", wire.AgentSpec{Name: "gatekeeper", Provider: "offline", Model: "offline", SystemPrompt: "Review", Review: wire.Review{Approve: wire.ReviewApproveWhenClean}}, "test"); err != nil {
		t.Fatal(err)
	}
	client := clients.Client{Tenant: "t", ID: "gh", Surface: clients.GitHub, Enabled: true, Managed: clients.ManagedStatic, DefaultAgent: "gatekeeper",
		Identity: clients.Identity{Owners: []string{"acme"}, AppID: "123", Installation: "987654"},
		GitHub:   &clients.GitHubOptions{Login: login}}
	ref := github.Ref{Owner: "acme", Repo: "site", Number: 7}
	proof := github.Context{Files: []github.ContextFile{{Filename: "a.go", Patch: "@@ -1 +1 @@\n-old\n+new"}}}
	proof.Subject.Head.SHA = approvalSHA
	proof.Subject.State = "open"
	proof.Subject.User.Login, proof.Subject.User.ID = "octocat", 583231
	if err := fake.SeedContext(ref, proof); err != nil {
		t.Fatal(err)
	}
	for _, c := range []github.CommitCheck{green("test"), green("lint")} {
		if err := fake.SeedCommitCheck("acme", "site", approvalSHA, c); err != nil {
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
	st, err := p.dial(client, resolved{token: token, signing: "secret"})
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
	reviews := fake.SubmittedReviews()
	if len(reviews) != 1 {
		t.Fatalf("posted %+v", reviews)
	}
	return reviews[0].Review.Event
}
