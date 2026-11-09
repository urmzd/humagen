package platform_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/turn/native/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// echoSlack is a Slack resolver that says what it was asked and for whom;
// asked reads what it was asked so far.
func echoSlack(t *testing.T, p *platform.Platform, sources int) (asked func() []platform.ContextRequest) {
	t.Helper()
	var mu sync.Mutex
	var got []platform.ContextRequest
	p.RegisterContextResolver("slack", func(_ context.Context, req platform.ContextRequest) (platform.ContextResult, error) {
		mu.Lock()
		got = append(got, req)
		mu.Unlock()
		out := platform.ContextResult{Text: "They were discussing the release date. " + strings.Repeat("x", 13000)}
		for range sources {
			out.Sources = append(out.Sources, platform.ContextSource{Title: "message", URL: "https://acme.slack.com/archives/C01/p1"})
		}
		return out, nil
	})
	return func() []platform.ContextRequest {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

func TestContextIsPulledFromThePlaceTheSessionCameFrom(t *testing.T) {
	p, ana := slackWorkspace(t)
	got := echoSlack(t, p, 25)
	out, err := p.SendMessages(t.Context(), ana, sayIn("what were they discussing?", slackRef("D0ANA", "1700000000.000100", "")))
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.GetContext(t.Context(), ana, out.ID, "what were they discussing about the release?")
	if err != nil {
		t.Fatal(err)
	}
	if res.Origin != "slack://T0TEAM/D0ANA/1700000000.000100" || !strings.HasPrefix(res.Text, "They were discussing the release date.") {
		t.Fatalf("context = %q from %q", res.Text[:40], res.Origin)
	}
	if !res.Truncated || len([]rune(res.Text)) > 12001 || len(res.Sources) != 20 {
		t.Fatalf("not bounded: truncated %v, %d runes, %d sources", res.Truncated, len([]rune(res.Text)), len(res.Sources))
	}
	if a := got(); len(a) != 1 || a[0].Request != "what were they discussing about the release?" || !a[0].HasAudience || a[0].Audience.Kind != "direct" || a[0].Subject != "ana" {
		t.Fatalf("the resolver was asked %+v", a)
	}
	vic := auth.Principal{Subject: "vic", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
	if _, err := p.GetContext(t.Context(), vic, out.ID, "anything"); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("someone who cannot read the session pulled its context: %v", err)
	}
	if _, err := p.GetContext(t.Context(), ana, out.ID, "  "); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("an empty request = %v", err)
	}
}

func TestContextSaysWhyNothingCanBePulled(t *testing.T) {
	p, ana := slackWorkspace(t)
	console, err := p.SendMessages(t.Context(), ana, sayIn("why did the deploy fail?", nil))
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.GetContext(t.Context(), ana, console.ID, "what happened before?")
	if err != nil || res.Text != "" || !strings.Contains(res.Note, "did not come from another place") {
		t.Fatalf("a console conversation's context = %+v, %v", res, err)
	}
	pr, err := p.SendMessages(t.Context(), ana, sayIn("review it", &platform.ExternalRef{Type: platform.RefGitHub, Repo: "acme/web", Kind: "pull", Number: 9}))
	if err != nil {
		t.Fatal(err)
	}
	res, err = p.GetContext(t.Context(), ana, pr.ID, "the failing checks")
	if err != nil || !strings.Contains(res.Note, "github places here yet") || res.Origin != "github://acme/web/pull/9" {
		t.Fatalf("a place with no resolver = %+v, %v", res, err)
	}
}

func TestAnAgentPullsContextFromWhereItWasAsked(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{
		pickJSON(platform.PickAnswerMyself, "I can take this."),
		agenttest.ToolCallResponse("pull", platform.ToolGetContext, map[string]any{"request": "what were they discussing?"}),
		say("They were discussing the release date."),
	}, nil)
	p := stageEntry(t, c)
	got := echoSlack(t, p, 1)
	seePeople(t, p, auth.Principal{Subject: "ana"})
	in := slackClient("app", "A0APP", "T0TEAM", "*")
	in.Agents = []string{"*"}
	in.Slack.Machine, in.Slack.People = "svc-slack", map[string]string{"U0ANA": "ana"}
	create(t, p, tenant, in)
	if _, err := p.SendMessages(t.Context(), admin, sayIn("what were they discussing?", slackRef("C01ANY", "1700000000.000100", ""))); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for len(got()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if a := got(); len(a) != 1 || a[0].Origin != "slack://T0TEAM/C01ANY/1700000000.000100" || a[0].Request != "what were they discussing?" || a[0].Run == "" {
		t.Fatalf("the agent's get_context reached the resolver as %+v", a)
	}
}
