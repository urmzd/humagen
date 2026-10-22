package platform_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/envelope"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
)

// shelf fills a workspace's knowledge: one source per name, each holding one
// document about renewals that says the word given for it, so a turn shows
// which sources it was grounded in by which words it was sent.
func shelf(t *testing.T, p *platform.Platform, words map[string]string) {
	t.Helper()
	now := time.Now()
	for name, word := range words {
		if _, err := p.Knowledge.Create(tenant, name, "", "test", now); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Knowledge.Add(tenant, name, "Renewals", "", "Renewals are signed by "+word+".", "test", now); err != nil {
			t.Fatal(err)
		}
	}
}

// name sets the knowledge sources a project names, through the store, as
// whoever may.
func name(t *testing.T, p *platform.Platform, project string, sources ...string) {
	t.Helper()
	pr, err := p.Projects.Get(tenant, project)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Projects.SetKnowledge(tenant, project, pr.Version, sources, projects.Writer{By: "ada", Govern: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// askIn runs the agent once in a project and waits for its answer.
func askIn(t *testing.T, p *platform.Platform, id, agent, project string) runs.Run {
	t.Helper()
	r, err := p.Invoke(t.Context(), envelope.Tag{ID: id, Tenant: tenant, Agent: agent, Origin: "api", Text: "Who signs renewals?", Meta: map[string]string{runs.MetaProject: project}})
	if err != nil {
		t.Fatal(err)
	}
	return await(t, p, r.ID, runs.Completed)
}

// sent is what a model call was sent, as text.
func sent(t *testing.T, call agenttest.ScriptedCall) string {
	t.Helper()
	b, err := json.Marshal(call.Messages)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A run filed in a project is grounded in the knowledge sources its project
// and every project above it name, besides its agent's own, and never in a
// sibling's or a child's. The sources are pinned into the run's tag when it
// is accepted, nearest first, and whatever a caller put there is replaced.
func TestARunIsGroundedInTheKnowledgeOfItsProjectAndThoseAboveIt(t *testing.T) {
	model := &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{say("1"), say("2"), say("3"), say("4")}}
	p := startWith(t, platform.Config{Ext: platform.Extensions{Providers: map[string]agentrt.ProviderFunc{
		"scripted": func(agentrt.Spec) (saigetypes.Provider, error) { return model, nil },
	}}})
	ctx := t.Context()
	if _, err := p.Agents.Create(ctx, tenant, agentspec.AgentSpec{Name: "support", Provider: "scripted", Model: "m", SystemPrompt: "Answer.",
		Sources: []agentspec.Source{{Name: "own", URI: "knowledge://own"}}}, "test"); err != nil {
		t.Fatal(err)
	}
	shelf(t, p, map[string]string{"own": "Olga", "guide": "Gus", "playbook": "Paula", "globex-notes": "Gina", "initech-notes": "Ivan"})
	ada := projects.Writer{By: "ada"}
	for _, in := range []platform.ProjectInput{{Name: "Field"}, {Name: "Globex", Parent: "Field"}, {Name: "Initech", Parent: "Field"}} {
		if _, err := p.CreateProject(ctx, tenant, ada, in); err != nil {
			t.Fatal(err)
		}
	}
	name(t, p, projects.Root, "guide")
	name(t, p, "Field", "playbook")
	name(t, p, "Globex", "globex-notes")
	name(t, p, "Initech", "initech-notes")

	every := []string{"Olga", "Gus", "Paula", "Gina", "Ivan"}
	for i, c := range []struct {
		project string
		pinned  string
		words   []string
	}{
		{"Globex", "globex-notes,playbook,guide", []string{"Olga", "Gus", "Paula", "Gina"}},
		{"Initech", "initech-notes,playbook,guide", []string{"Olga", "Gus", "Paula", "Ivan"}},
		{"Field", "playbook,guide", []string{"Olga", "Gus", "Paula"}},
		{projects.Root, "guide", []string{"Olga", "Gus"}},
	} {
		run := askIn(t, p, "in-"+c.project, "support", c.project)
		if got := run.Tag().Meta[runs.MetaSources]; got != c.pinned {
			t.Errorf("a run in %q was pinned %q, want %q", c.project, got, c.pinned)
		}
		text := sent(t, model.Calls[i])
		for _, word := range every {
			if want := slices.Contains(c.words, word); strings.Contains(text, word) != want {
				t.Errorf("a run in %q was sent %q: %v, want %v: %s", c.project, word, !want, want, text)
			}
		}
	}

	// A caller cannot name sources of its own: the platform's pin replaces
	// them.
	r, err := p.Invoke(ctx, envelope.Tag{ID: "claims", Tenant: tenant, Agent: "support", Origin: "api", Text: "Who signs renewals?",
		Meta: map[string]string{runs.MetaProject: "Field", runs.MetaSources: "initech-notes"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := await(t, p, r.ID, runs.Completed).Tag().Meta[runs.MetaSources]; got != "playbook,guide" {
		t.Fatalf("a run that claimed its sources was pinned %q", got)
	}
	if text := sent(t, model.Calls[4]); strings.Contains(text, "Ivan") {
		t.Fatalf("a run was grounded in sources its tag claimed: %s", text)
	}
}

// A run another run called is grounded as its root was: the sources pinned
// when the conversation's first run was accepted, whatever its project names
// by the time of the call.
func TestACalledRunIsGivenItsRootsKnowledge(t *testing.T) {
	held := &holding{called: make(chan struct{})}
	p := startWith(t, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 2}, AgentOptions: []agentrt.Option{
		agentrt.WithProvider(agentrt.ProviderOffline, func(agentrt.Spec) (saigetypes.Provider, error) { return held, nil }),
	}})
	ctx := t.Context()
	if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: "Field"}); err != nil {
		t.Fatal(err)
	}
	shelf(t, p, map[string]string{"playbook": "Paula", "pricing": "Priya"})
	name(t, p, "Field", "playbook")
	root, err := p.Invoke(ctx, envelope.Tag{ID: "root", Tenant: tenant, Agent: "lead", Origin: "api", Text: "hi", Meta: map[string]string{runs.MetaProject: "Field"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-held.called:
	case <-time.After(10 * time.Second):
		t.Fatal("the root never called its model")
	}
	name(t, p, "Field", "pricing")
	child, err := p.Call(ctx, tenant, root.ID, call.Request{Key: "k1", Agent: "helper", Text: "help", Mode: call.Delegate})
	if err != nil {
		t.Fatal(err)
	}
	if got := child.Tag().Meta[runs.MetaSources]; got != "playbook" || child.Project != "Field" {
		t.Fatalf("a called run in %q was pinned %q, want its root's", child.Project, got)
	}
}

// A run keeps the sources it was accepted with: a change to what its
// projects name, made while it works, and a restart of the platform in the
// middle of it, change nothing for it, and the next run is grounded in what
// they name now. Kept with memory and with Postgres.
func TestARunKeepsItsKnowledgeAcrossAnEditAndARestart(t *testing.T) {
	eachBackend(t, func(t *testing.T, first platform.Backends, again func() platform.Backends) {
		ctx := t.Context()
		model := func(m saigetypes.Provider) []agentrt.Option {
			return []agentrt.Option{agentrt.WithProvider(agentrt.ProviderOffline, func(agentrt.Spec) (saigetypes.Provider, error) { return m, nil })}
		}
		held := &holding{called: make(chan struct{})}
		p, stop := runningWith(t, platform.Config{Backends: first, AgentOptions: model(held)})
		if _, err := p.Agents.Create(ctx, tenant, agentspec.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead."}, "test"); err != nil {
			t.Fatal(err)
		}
		for _, in := range []platform.ProjectInput{{Name: "Field"}, {Name: "Globex", Parent: "Field"}} {
			if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "ada"}, in); err != nil {
				t.Fatal(err)
			}
		}
		shelf(t, p, map[string]string{"old-playbook": "Dana", "new-playbook": "Eve"})
		name(t, p, "Field", "old-playbook")
		r, err := p.Invoke(ctx, envelope.Tag{ID: "in-flight", Tenant: tenant, Agent: "lead", Origin: "api", Text: "Who signs renewals?", Meta: map[string]string{runs.MetaProject: "Globex"}})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-held.called:
		case <-time.After(10 * time.Second):
			t.Fatal("the run never called its model")
		}
		// Field names another source while the run works.
		name(t, p, "Field", "new-playbook")
		stop()

		answer := &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{say("Dana."), say("Eve.")}}
		q, _ := runningWith(t, platform.Config{Backends: again(), AgentOptions: model(answer)})
		resumed := await(t, q, r.ID, runs.Completed)
		if got := resumed.Tag().Meta[runs.MetaSources]; got != "old-playbook" {
			t.Fatalf("the run after a restart is pinned %q", got)
		}
		if len(answer.Calls) != 1 {
			t.Fatalf("model calls after the restart = %d", len(answer.Calls))
		}
		if text := sent(t, answer.Calls[0]); !strings.Contains(text, "Dana") || strings.Contains(text, "Eve") {
			t.Fatalf("the run resumed after an edit and a restart was sent: %s", text)
		}
		next := askIn(t, q, "next", "lead", "Globex")
		if got := next.Tag().Meta[runs.MetaSources]; got != "new-playbook" {
			t.Fatalf("the next run is pinned %q", got)
		}
		if text := sent(t, answer.Calls[1]); !strings.Contains(text, "Eve") || strings.Contains(text, "Dana") {
			t.Fatalf("the next run was sent: %s", text)
		}
	})
}

// Naming a project's knowledge sources takes knowledge:read, which reads them
// anyway, and only sources the workspace has; a reader without it is shown
// none of them.
func TestNamingAProjectsKnowledgeTakesKnowledgeRead(t *testing.T) {
	p := startWith(t, platform.Config{})
	ctx := t.Context()
	if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: "Field"}); err != nil {
		t.Fatal(err)
	}
	shelf(t, p, map[string]string{"playbook": "Paula"})
	reader := auth.Principal{Subject: "ada", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.ProjectsRead, auth.ProjectsWrite, auth.KnowledgeRead}}
	blind := auth.Principal{Subject: "ada", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.ProjectsRead, auth.ProjectsWrite}}
	w := projects.Writer{By: "ada"}
	if _, err := p.SetProjectKnowledge(blind, "Field", w, platform.KnowledgeInput{Version: 1, Knowledge: []string{"playbook"}}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("naming a source without knowledge:read = %v", err)
	}
	if _, err := p.SetProjectKnowledge(reader, "Field", w, platform.KnowledgeInput{Version: 1, Knowledge: []string{"ghost"}}); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("naming a source the workspace does not have = %v", err)
	}
	got, err := p.SetProjectKnowledge(reader, "Field", w, platform.KnowledgeInput{Version: 1, Knowledge: []string{"playbook"}})
	if err != nil || !slices.Equal(got.Knowledge, []string{"playbook"}) || len(got.Given) != 1 || got.Given[0] != (projects.KnowledgeSource{Name: "playbook", From: "Field"}) {
		t.Fatalf("Field's knowledge = %+v, %v", got, err)
	}
	shown, err := p.ProjectKnowledgeAs(blind, "Field")
	if err != nil || len(shown.Knowledge) != 0 || len(shown.Given) != 0 || shown.Knowledge == nil || shown.Given == nil || shown.Version != got.Version {
		t.Fatalf("Field's knowledge to a reader without knowledge:read = %+v, %v", shown, err)
	}
	if _, err := p.ProjectKnowledgeAs(reader, "Nowhere"); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("a project that does not exist = %v", err)
	}
}

// A housekeeping run is given no project's knowledge: it is filed at the
// workspace whatever turn it serves, and its job is the turn.
func TestAHousekeepingRunIsGivenNoKnowledge(t *testing.T) {
	p := startWith(t, platform.Config{OfflineHooks: true})
	ctx := t.Context()
	if _, err := p.Agents.Create(ctx, tenant, agentspec.AgentSpec{Name: "scribe", Model: "offline", Provider: "offline", SystemPrompt: "Title it."}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := p.SetSettings(ctx, tenant, platform.Settings{TitleAgent: "scribe"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: "Field"}); err != nil {
		t.Fatal(err)
	}
	shelf(t, p, map[string]string{"guide": "Gus", "playbook": "Paula"})
	name(t, p, projects.Root, "guide")
	name(t, p, "Field", "playbook")
	first, err := p.Converse(ctx, tenant, platform.Turn{ID: "api-1", Agent: "lead", Text: "Plan the launch", Project: "Field"})
	if err != nil {
		t.Fatal(err)
	}
	if got := await(t, p, first.ID, runs.Completed).Tag().Meta[runs.MetaSources]; got != "playbook,guide" {
		t.Fatalf("the turn was pinned %q", got)
	}
	var title runs.Run
	eventually(t, "the title agent is asked", func() bool {
		for _, r := range p.Runs.List(tenant, 50) {
			if r.TagID == "sys-title-"+first.ID {
				title = r
				return true
			}
		}
		return false
	})
	if title.Origin != runs.OriginSystem || title.Tag().Meta[runs.MetaSources] != "" {
		t.Fatalf("the housekeeping run %+v was pinned %q", title, title.Tag().Meta[runs.MetaSources])
	}
}
