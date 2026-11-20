package platform_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/runs"
)

// specialists adds agents whose prompts say what they are for, and makes
// routing the root's way of answering a message that names nobody.
func specialists(t *testing.T, p *platform.Platform) {
	t.Helper()
	for _, spec := range []wire.AgentSpec{
		{Name: "ops", Model: "offline", Provider: "offline", SystemPrompt: "You run deployments. Explain why a deploy failed and roll back."},
		{Name: "analyst", Model: "offline", Provider: "offline", SystemPrompt: "You answer questions about cost and spend."},
		{Name: "writer", Model: "offline", Provider: "offline", SystemPrompt: "You draft documents."},
	} {
		if _, err := p.Agents.Create(t.Context(), tenant, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "*"})
}

// sessionRun is the run a session's first message started.
func sessionRun(t *testing.T, v platform.SessionView) runs.Run {
	t.Helper()
	for _, m := range v.Messages {
		if m.Run != nil {
			return *m.Run
		}
	}
	t.Fatalf("session %s started no run: %+v", v.ID, v.Messages)
	return runs.Run{}
}

func TestAConsoleSessionThatNamesNobodyIsAnsweredByTheAgentRoutingFinds(t *testing.T) {
	p := start(t, call.Limits{})
	specialists(t, p)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "Why did the deploy fail last night?"})
	if err != nil {
		t.Fatalf("an unaddressed console message was refused: %v", err)
	}
	if v.Agent != "ops" {
		t.Fatalf("answered by %q, want ops", v.Agent)
	}
	if r := sessionRun(t, v); !strings.Contains(r.Picked, "Joined on its own: matched deploy") {
		t.Fatalf("the answer does not say why ops was picked: %q", r.Picked)
	}
}

func TestWithNobodyTakingItTheBestSearchMatchStillAnswers(t *testing.T) {
	no := join.AskerFunc(func(context.Context, join.Candidate, string) (join.Decision, error) {
		return join.Decision{Reason: "Not mine."}, nil
	})
	p := startWith(t, platform.Config{Router: func(string) join.Asker { return no }})
	specialists(t, p)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "What did the migration cost?"})
	if err != nil {
		t.Fatalf("a console message nobody took did not land: %v", err)
	}
	if v.Agent != "analyst" {
		t.Fatalf("answered by %q, want analyst, the best match", v.Agent)
	}
	if r := sessionRun(t, v); r.Picked != "Picked by search: matched cost." {
		t.Fatalf("picked = %q", r.Picked)
	}
}

func TestAConsoleMessageSearchMatchesNobodyForStillLands(t *testing.T) {
	p := start(t, call.Limits{})
	specialists(t, p)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "zzz qqq"})
	if err != nil {
		t.Fatalf("an unmatched console message did not land: %v", err)
	}
	if r := sessionRun(t, v); r.Picked != "No agent matched it, so the first agent available here answered." || v.Agent != "analyst" {
		t.Fatalf("answered by %q, picked %q; want the first agent available, saying so", v.Agent, r.Picked)
	}
	field(t, p)
	// A workspace that names its default answers with it in every project:
	// a project chooses no default of its own, so Field's "*" is not read.
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "writer"})
	setAgents(t, p, "Field", platform.AgentsInput{Enable: []string{}, Default: "*"})
	v, err = p.StartSession(t.Context(), admin, platform.SessionInput{Project: "Field", Text: "zzz qqq"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Agent != "writer" {
		t.Fatalf("answered by %q; want the workspace default", v.Agent)
	}
}

func TestOnlyTheBestFewAgentsSearchFindsAreAsked(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	count := join.AskerFunc(func(_ context.Context, c join.Candidate, _ string) (join.Decision, error) {
		mu.Lock()
		asked = append(asked, c.Name)
		mu.Unlock()
		return join.Decision{Join: c.Name == "billing-4", Reason: "Mine."}, nil
	})
	p := startWith(t, platform.Config{Router: func(string) join.Asker { return count }})
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "*"})
	for _, name := range []string{"billing-1", "billing-2", "billing-3", "billing-4", "billing-5"} {
		prompt := "You answer billing questions."
		if name == "billing-4" {
			prompt = "You answer billing and invoice questions about invoice disputes."
		}
		if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: name, Model: "offline", Provider: "offline", SystemPrompt: prompt}, "test"); err != nil {
			t.Fatal(err)
		}
	}
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "an invoice dispute on billing"})
	if err != nil || v.Agent != "billing-4" {
		t.Fatalf("answered by %q (%v), want billing-4", v.Agent, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) > join.Shortlist {
		t.Fatalf("asked %d agents %v, want at most %d", len(asked), asked, join.Shortlist)
	}
	if slices.Contains(asked, "lead") || slices.Contains(asked, "helper") {
		t.Fatalf("asked agents search did not find: %v", asked)
	}
}

func TestAnAnswerRatedUpTeachesRoutingAndTakingItBackUnteaches(t *testing.T) {
	p := start(t, call.Limits{})
	specialists(t, p)
	ask := "Summarise the quarterly board pack"
	unmatched := func(when string) {
		t.Helper()
		v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: ask})
		if err != nil {
			t.Fatal(err)
		}
		if r := sessionRun(t, v); v.Agent == "writer" || !strings.HasPrefix(r.Picked, "No agent matched it") {
			t.Fatalf("%s, answered by %q (picked %q); want no match", when, v.Agent, r.Picked)
		}
	}
	unmatched("before any rating")
	r, err := p.Invoke(t.Context(), wire.NewEvent(wire.Raise{ID: "praised", Tenant: tenant, Agent: "writer", Origin: "api", Text: ask}))
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, r.ID, runs.Completed)
	// A rating of one finding is about the finding, not the answer.
	finding, err := p.Feedback.Save(tenant, feedback.Record{Run: r.ID, Subject: admin.Subject, Rating: "up", FindingID: "f1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.RecordFeedbackCandidate(tenant, finding); err != nil {
		t.Fatal(err)
	}
	unmatched("after a finding's rating")
	up, err := p.Feedback.Save(tenant, feedback.Record{Run: r.ID, Subject: admin.Subject, Rating: "up"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.RecordFeedbackCandidate(tenant, up); err != nil {
		t.Fatal(err)
	}
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: ask})
	if err != nil || v.Agent != "writer" {
		t.Fatalf("after a rating up, answered by %q (%v), want writer", v.Agent, err)
	}
	if err := p.Feedback.Remove(tenant, r.ID, admin.Subject); err != nil {
		t.Fatal(err)
	}
	p.RefreshGoodAsk(tenant, r.ID)
	unmatched("after the rating was taken back")
}

func TestAPreviewOfRoutingSaysWhoSearchPassedOver(t *testing.T) {
	p := start(t, call.Limits{})
	specialists(t, p)
	ds, err := p.Route(t.Context(), tenant, "~", "why did the deploy fail")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]join.Decision{}
	for _, d := range ds {
		by[d.Agent] = d
	}
	if !by["ops"].Join {
		t.Fatalf("ops did not join: %+v", by["ops"])
	}
	if d := by["writer"]; d.Join || !strings.Contains(d.Reason, "Search did not find it") {
		t.Fatalf("writer = %+v, want passed over by search", d)
	}
}
