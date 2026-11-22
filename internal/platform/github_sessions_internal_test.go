package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/connector"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn/native"
)

// githubActionPeer is a GitHub client's connector with one action that
// writes, and counts its writes.
type githubActionPeer struct{ writes int }

func (g *githubActionPeer) Name() string { return string(clients.GitHub) }
func (g *githubActionPeer) Ingest(ctx context.Context, _ connector.Publish) error {
	<-ctx.Done()
	return ctx.Err()
}
func (g *githubActionPeer) Ingesting() bool                                  { return false }
func (g *githubActionPeer) Flush(context.Context) error                      { return nil }
func (g *githubActionPeer) ServeHTTP(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }
func (g *githubActionPeer) Deliver(context.Context, address.Address, event.Envelope) error {
	return nil
}
func (g *githubActionPeer) Actions() []connector.Action {
	return []connector.Action{{Name: "github_add_label", Description: "Add a label", Schema: json.RawMessage(`{"type":"object"}`), Invoke: func(context.Context, address.Address, json.RawMessage) (connector.Result, error) {
		g.writes++
		return connector.Result{}, nil
	}}}
}

const githubPull = "github://acme/repo/pull/7"

// githubSessionRuns is a platform with a GitHub client "gh" mapping
// octocat to ana, and three runs in the session of acme/repo#7: the run a
// mention raised, an agent it called, and ana's reply in the console.
func githubSessionRuns(t *testing.T) (p *Platform, root, child, reply runs.Run, at time.Time) {
	t.Helper()
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if _, err := p.Agents.Create(t.Context(), "t", wire.AgentSpec{Name: "reviewer", Provider: "offline", Model: "offline", SystemPrompt: "Review recorded evidence"}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Clients.Create("t", clients.Client{ID: "gh", Name: "GitHub", Surface: clients.GitHub, Identity: clients.Identity{Owners: []string{"acme"}, Installation: "101"}, Enabled: true, Agents: []string{"reviewer"}, GitHub: &clients.GitHubOptions{People: map[string]string{"octocat": "ana"}}}, clients.DefaultCaps(), time.Now()); err != nil {
		t.Fatal(err)
	}
	at = time.Now().UTC().Add(-time.Minute)
	record := func(tag event.Envelope) runs.Run {
		t.Helper()
		id := runtime.RunID("t", tag.ID)
		if err := p.Runs.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
		r, err := p.Runs.Get("t", id)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	tag := wire.NewEvent(wire.Raise{ID: "gh-mention", Tenant: "t", Agent: "reviewer", Origin: runs.OriginGitHub, Channel: githubPull, Text: "Review", At: at,
		Actor: event.Actor{ID: "octocat", User: "ana"}, Context: map[string]string{runs.MetaClient: "gh"}})
	tag.Source = githubPull
	wire.SetGitHubHeadSHA(&tag, strings.Repeat("a", 40))
	tag = p.fileInGitHubSession(t.Context(), tag, "", "gh")
	root = record(tag)
	if th, ok := p.githubSessionOf("t", root.Thread); !ok || !p.privateThread("t", th.ID) {
		t.Fatalf("the mention is in %q, not a private GitHub session", root.Thread)
	}
	called, err := call.New(tag, call.Request{Key: "c1", Agent: "reviewer", Text: "look closer", Mode: call.Subagent}, call.State{}, call.Limits{MaxDepth: 3, MaxFanOut: 4}, at)
	if err != nil {
		t.Fatal(err)
	}
	child = record(called)
	if child.Thread != root.Thread || child.Origin == runs.OriginGitHub || child.Client != "gh" {
		t.Fatalf("the called run is %s's in %q through %q", child.Origin, child.Thread, child.Client)
	}
	asked := wire.NewEvent(wire.Raise{ID: "console-reply", Tenant: "t", Agent: "reviewer", Origin: runs.OriginAPI, Text: "and this?", At: at,
		Actor: event.Actor{ID: "ana", User: "ana"}, Context: map[string]string{runs.MetaThread: root.Thread}})
	wire.SetGitHubHeadSHA(&asked, strings.Repeat("a", 40))
	reply = record(asked)
	if reply.Thread != root.Thread {
		t.Fatalf("the console reply is in %q", reply.Thread)
	}
	return p, root, child, reply, at
}

// A GitHub run in its own session, and an agent it called, act on GitHub
// as GitHub runs did before they were filed. A person's reply in the
// console, in the same private session, does not.
func TestAGitHubSessionsRunsKeepTheirClientActions(t *testing.T) {
	p, root, child, reply, _ := githubSessionRuns(t)
	c, err := p.Clients.Get("t", "gh")
	if err != nil {
		t.Fatal(err)
	}
	peer := &githubActionPeer{}
	station := &seat{client: c, peer: peer}
	p.surfaces["github"].seats = []*seat{station}
	invoke := func(run runs.Run) error {
		t.Helper()
		actions := capabilityActor{p: p, st: station, req: runtime.Request{Tenant: "t", RunID: run.ID, Definition: native.Revision{Spec: native.Spec{Name: "reviewer"}}}}.Actions()
		if len(actions) != 1 {
			t.Fatalf("%d actions offered", len(actions))
		}
		_, err := actions[0].Invoke(t.Context(), address.MustParse(githubPull), json.RawMessage(`{}`))
		return err
	}
	if err := invoke(root); err != nil {
		t.Fatalf("the mention's run may not act on GitHub: %v", err)
	}
	if err := invoke(child); err != nil {
		t.Fatalf("an agent the mention called may not act on GitHub: %v", err)
	}
	if err := invoke(reply); !errors.Is(err, operations.ErrDenied) {
		t.Fatalf("a console reply in the private session acted on GitHub: %v", err)
	}
	if peer.writes != 2 {
		t.Fatalf("%d writes, want 2", peer.writes)
	}
}

// A review a GitHub run posts keeps the evidence it recorded, as before
// GitHub runs were filed in a private session.
func TestAGitHubSessionsReviewKeepsItsEvidence(t *testing.T) {
	p, root, _, reply, at := githubSessionRuns(t)
	for _, run := range []runs.Run{root, reply} {
		reviewAppend(t, p, run, 1, at.Add(time.Second), event.ToolDone{ID: "real", Name: "test", Result: "src/input.go failed validation"})
		text := reviewReport(t, presentation.ReviewEvidence{Step: "real", Source: "src/input.go", Observation: "Recorded test failed"})
		reviewAppend(t, p, run, 2, at.Add(2*time.Second), event.Completed{Text: text})
		got := p.reviewText(t.Context(), run, text, nil, time.Time{})
		if run.ID == reply.ID {
			if got != reviewEvidenceUnavailable {
				t.Fatalf("a console reply in the private session published its evidence: %q", got)
			}
			continue
		}
		if got == reviewEvidenceUnavailable || got == reviewNeedsCorrection || evidenceOf(t, got)[0].Step != "real" {
			t.Fatalf("the GitHub run's review lost its evidence: %q", got)
		}
	}
}

// A GitHub session with one person in it is never that person's own
// private conversation: their private sources stay closed, and an answer
// reads what a GitHub thread may show.
func TestAOnePersonGitHubSessionKeepsPrivateSourcesClosed(t *testing.T) {
	p, root, _, _, _ := githubSessionRuns(t)
	req := runtime.Request{Tenant: "t", RunID: root.ID}
	if _, person, err := p.ownPrivateConversation(req); err == nil {
		t.Fatalf("the GitHub session is %s's own private conversation", person)
	}
	a, err := p.corpusAudienceOf(req)
	if err != nil || a.kind != AudienceGitHub || a.direct() {
		t.Fatalf("the GitHub session's corpus audience = %+v, %v", a, err)
	}
}

// What is said in a GitHub session is remembered as before: never as a
// personal memory proposal, as a private conversation's turns are. A
// console reply there is a private turn.
func TestAGitHubSessionsTurnsAreNotPrivateToMemory(t *testing.T) {
	p, root, child, reply, _ := githubSessionRuns(t)
	for _, run := range []runs.Run{root, child} {
		if private, err := p.privateTurn(run); err != nil || private {
			t.Fatalf("%s's turn in the GitHub session is private to memory (%v)", run.Origin, err)
		}
	}
	if private, err := p.privateTurn(reply); err != nil || !private {
		t.Fatalf("a console reply in the private session is not private to memory (%v)", err)
	}
}

// Two clients serving one repository do not share a session: the client
// that answered first holds the pull request's, and the other's runs stay
// their own conversations, with that client's actions as before.
func TestTwoClientsOnOneRepositoryDoNotShareASession(t *testing.T) {
	p, root, _, _, at := githubSessionRuns(t)
	if _, err := p.Clients.Create("t", clients.Client{ID: "gh2", Name: "GitHub too", Surface: clients.GitHub, Identity: clients.Identity{Owners: []string{"acme"}, Installation: "202"}, Enabled: true, Agents: []string{"reviewer"}}, clients.DefaultCaps(), time.Now()); err != nil {
		t.Fatal(err)
	}
	tag := wire.NewEvent(wire.Raise{ID: "gh2-mention", Tenant: "t", Agent: "reviewer", Origin: runs.OriginGitHub, Channel: githubPull, Text: "Review", At: at,
		Actor: event.Actor{ID: "hubber"}, Context: map[string]string{runs.MetaClient: "gh2"}})
	tag.Source = githubPull
	if filed := wire.Meta(p.fileInGitHubSession(t.Context(), tag, "", "gh2"))[runs.MetaThread]; filed != "" {
		t.Fatalf("the second client's run is filed in %q (the first client's is %q)", filed, root.Thread)
	}
	// The asker of a mention on the first client is still added when its
	// session is found.
	again := wire.NewEvent(wire.Raise{ID: "gh-again", Tenant: "t", Agent: "reviewer", Origin: runs.OriginGitHub, Channel: githubPull, Text: "Again", At: at,
		Actor: event.Actor{ID: "monalisa", User: "mona"}, Context: map[string]string{runs.MetaClient: "gh"}})
	again.Source = githubPull
	if filed := wire.Meta(p.fileInGitHubSession(t.Context(), again, "", "gh"))[runs.MetaThread]; filed != root.Thread {
		t.Fatalf("a later mention on the first client is in %q, want %q", filed, root.Thread)
	}
	th, err := p.Threads.Get("t", root.Thread)
	if err != nil || !sessionOf(th, "mona") || !sessionOf(th, "ana") {
		t.Fatalf("the session's people are %+v, %v", th.Participants, err)
	}
}
