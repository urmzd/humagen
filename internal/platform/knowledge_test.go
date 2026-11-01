package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
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
	r, err := p.Invoke(t.Context(), wire.NewEvent(wire.Raise{ID: id, Tenant: tenant, Agent: agent, Origin: "api", Text: "Who signs renewals?", Context: map[string]string{runs.MetaProject: project}}))
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
	model := &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{say("1"), say("2"), say("3"), say("4"), say("5"), say("6")}}
	p := startWith(t, platform.Config{Ext: platform.Extensions{Providers: map[string]native.ProviderFunc{
		"scripted": func(native.Spec) (saigetypes.Provider, error) { return model, nil },
	}}})
	ctx := t.Context()
	if _, err := p.Agents.Create(ctx, tenant, wire.AgentSpec{Name: "support", Provider: "scripted", Model: "m", SystemPrompt: "Answer.",
		Sources: []wire.Source{{Name: "own", URI: "knowledge://own"}}}, "test"); err != nil {
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
		if got := wire.Meta(run.Tag())[runs.MetaSources]; got != c.pinned {
			t.Errorf("a run in %q was pinned %q, want %q", c.project, got, c.pinned)
		}
		text := sent(t, model.Calls[i])
		if strings.Contains(text, runs.MetaSourcesV0) {
			t.Errorf("a run in %q was told the names of its sources: %s", c.project, text)
		}
		for _, word := range every {
			if want := slices.Contains(c.words, word); strings.Contains(text, word) != want {
				t.Errorf("a run in %q was sent %q: %v, want %v: %s", c.project, word, !want, want, text)
			}
		}
	}

	// A caller cannot name sources of its own: the platform's pin replaces
	// them.
	r, err := p.Invoke(ctx, wire.NewEvent(wire.Raise{ID: "claims", Tenant: tenant, Agent: "support", Origin: "api", Text: "Who signs renewals?", Context: map[string]string{runs.MetaProject: "Field", runs.MetaSources: "initech-notes"}}))
	if err != nil {
		t.Fatal(err)
	}
	if got := wire.Meta(await(t, p, r.ID, runs.Completed).Tag())[runs.MetaSources]; got != "playbook,guide" {
		t.Fatalf("a run that claimed its sources was pinned %q", got)
	}
	if text := sent(t, model.Calls[4]); strings.Contains(text, "Ivan") {
		t.Fatalf("a run was grounded in sources its tag claimed: %s", text)
	}

	// Nor under the key runs were pinned with before it was private, even
	// where no project names anything for the pin to replace it with.
	name(t, p, projects.Root)
	r, err = p.Invoke(ctx, wire.NewEvent(wire.Raise{ID: "claims-v0", Tenant: tenant, Agent: "support", Origin: "api", Text: "Who signs renewals?", Context: map[string]string{runs.MetaProject: projects.Root, runs.MetaSourcesV0: "initech-notes"}}))
	if err != nil {
		t.Fatal(err)
	}
	if got := runs.SourcesOf(wire.Meta(await(t, p, r.ID, runs.Completed).Tag())); len(got) != 0 {
		t.Fatalf("a run that claimed its sources under the old key was pinned %q", got)
	}
	if text := sent(t, model.Calls[5]); strings.Contains(text, "Ivan") || strings.Contains(text, "Gus") {
		t.Fatalf("a run was grounded in sources its tag claimed under the old key: %s", text)
	}
}

// A run another run called is grounded as its root was: the sources pinned
// when the conversation's first run was accepted, whatever its project names
// by the time of the call.
func TestACalledRunIsGivenItsRootsKnowledge(t *testing.T) {
	model := &heldFirst{held: &holding{called: make(chan struct{})}, rest: &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{say("done")}}}
	held := model.held
	p := startWith(t, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 2}, AgentOptions: []native.Option{
		native.WithProvider(native.ProviderOffline, func(native.Spec) (saigetypes.Provider, error) { return model, nil }),
	}})
	ctx := t.Context()
	if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: "Field"}); err != nil {
		t.Fatal(err)
	}
	shelf(t, p, map[string]string{"playbook": "Paula", "pricing": "Priya"})
	name(t, p, "Field", "playbook")
	root, err := p.Invoke(ctx, wire.NewEvent(wire.Raise{ID: "root", Tenant: tenant, Agent: "lead", Origin: "api", Text: "hi", Context: map[string]string{runs.MetaProject: "Field"}}))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-held.called:
	case <-time.After(10 * time.Second):
		t.Fatal("the root never called its model")
	}
	name(t, p, "Field", "pricing")
	child, err := p.Call(ctx, tenant, root.ID, call.Request{Key: "k1", Agent: "helper", Text: "Who signs renewals?", Mode: call.Delegate})
	if err != nil {
		t.Fatal(err)
	}
	if got := wire.Meta(child.Tag())[runs.MetaSources]; got != "playbook" || child.Project != "Field" {
		t.Fatalf("a called run in %q was pinned %q, want its root's", child.Project, got)
	}
	await(t, p, child.ID, runs.Completed)
	if text := sent(t, model.rest.Calls[0]); !strings.Contains(text, "Paula") || strings.Contains(text, "Priya") {
		t.Fatalf("a called run was not grounded in its root's sources alone: %s", text)
	}
}

// heldFirst holds the first model call until its run is stopped and answers
// the rest from a script.
type heldFirst struct {
	held  *holding
	rest  *agenttest.ScriptedProvider
	calls atomic.Int32
}

func (h *heldFirst) Stream(ctx context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
	if h.calls.Add(1) == 1 {
		return h.held.Stream(ctx, req)
	}
	return h.rest.Stream(ctx, req)
}

// A run keeps the sources it was accepted with: a change to what its
// projects name, made while it works, and a restart of the platform in the
// middle of it, change nothing for it, and the next run is grounded in what
// they name now. Kept with memory and with Postgres.
func TestARunKeepsItsKnowledgeAcrossAnEditAndARestart(t *testing.T) {
	eachBackend(t, func(t *testing.T, first platform.Backends, again func() platform.Backends) {
		ctx := t.Context()
		model := func(m saigetypes.Provider) []native.Option {
			return []native.Option{native.WithProvider(native.ProviderOffline, func(native.Spec) (saigetypes.Provider, error) { return m, nil })}
		}
		held := &holding{called: make(chan struct{})}
		p, stop := runningWith(t, platform.Config{Backends: first, AgentOptions: model(held)})
		if _, err := p.Agents.Create(ctx, tenant, wire.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead."}, "test"); err != nil {
			t.Fatal(err)
		}
		for _, in := range []platform.ProjectInput{{Name: "Field"}, {Name: "Globex", Parent: "Field"}} {
			if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "ada"}, in); err != nil {
				t.Fatal(err)
			}
		}
		shelf(t, p, map[string]string{"old-playbook": "Dana", "new-playbook": "Eve"})
		name(t, p, "Field", "old-playbook")
		r, err := p.Invoke(ctx, wire.NewEvent(wire.Raise{ID: "in-flight", Tenant: tenant, Agent: "lead", Origin: "api", Text: "Who signs renewals?", Context: map[string]string{runs.MetaProject: "Globex"}}))
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
		if got := wire.Meta(resumed.Tag())[runs.MetaSources]; got != "old-playbook" {
			t.Fatalf("the run after a restart is pinned %q", got)
		}
		if len(answer.Calls) != 1 {
			t.Fatalf("model calls after the restart = %d", len(answer.Calls))
		}
		if text := sent(t, answer.Calls[0]); !strings.Contains(text, "Dana") || strings.Contains(text, "Eve") {
			t.Fatalf("the run resumed after an edit and a restart was sent: %s", text)
		}
		next := askIn(t, q, "next", "lead", "Globex")
		if got := wire.Meta(next.Tag())[runs.MetaSources]; got != "new-playbook" {
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

	// A source the project names already may stay after it is deleted, so
	// an edit is not refused for it; one newly named must still exist.
	shelf(t, p, map[string]string{"pricing": "Priya"})
	if err := p.Knowledge.Delete(tenant, "playbook"); err != nil {
		t.Fatal(err)
	}
	kept, err := p.SetProjectKnowledge(reader, "Field", w, platform.KnowledgeInput{Version: got.Version, Knowledge: []string{"playbook", "pricing"}})
	if err != nil || !slices.Equal(kept.Knowledge, []string{"pricing"}) {
		t.Fatalf("an edit keeping a deleted source = %+v, %v", kept, err)
	}
	storedProject, _ := p.Projects.Get(tenant, "Field")
	if !slices.Equal(storedProject.Knowledge, []string{"playbook", "pricing"}) {
		t.Fatal("projection removed a retained dangling association")
	}
	if _, err := p.CreateProject(ctx, tenant, w, platform.ProjectInput{Name: "East"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetProjectKnowledge(reader, "East", w, platform.KnowledgeInput{Version: 1, Knowledge: []string{"playbook"}}); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("newly naming a deleted source = %v", err)
	}
}

// A housekeeping run is given no project's knowledge: it is filed at the
// workspace whatever turn it serves, and its job is the turn.
func TestAHousekeepingRunIsGivenNoKnowledge(t *testing.T) {
	p := startWith(t, platform.Config{OfflineHooks: true})
	ctx := t.Context()
	if _, err := p.Agents.Create(ctx, tenant, wire.AgentSpec{Name: "scribe", Model: "offline", Provider: "offline", SystemPrompt: "Title it."}, "test"); err != nil {
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
	if got := wire.Meta(await(t, p, first.ID, runs.Completed).Tag())[runs.MetaSources]; got != "playbook,guide" {
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
	if title.Origin != runs.OriginSystem || wire.Meta(title.Tag())[runs.MetaSources] != "" {
		t.Fatalf("the housekeeping run %+v was pinned %q", title, wire.Meta(title.Tag())[runs.MetaSources])
	}
}

// A write answers with the version it wrote, or the one it found when it
// changed nothing: a write that lands right after it does not change what it
// answers.
func TestAKnowledgeWriteAnswersWithWhatItWrote(t *testing.T) {
	p := startWith(t, platform.Config{})
	ctx := t.Context()
	if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: "Field"}); err != nil {
		t.Fatal(err)
	}
	shelf(t, p, map[string]string{"playbook": "Paula", "pricing": "Priya"})
	name(t, p, projects.Root, "pricing")
	reader := auth.Principal{Subject: "ada", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.ProjectsRead, auth.ProjectsWrite, auth.KnowledgeRead}}
	w := projects.Writer{By: "ada"}
	lists := [][]string{{"playbook"}, {"pricing", "playbook"}}
	errs := make(chan error, len(lists))
	for _, mine := range lists {
		go func() {
			for range 1000 {
				pr, err := p.Projects.Get(tenant, "Field")
				if err != nil {
					errs <- err
					return
				}
				got, err := p.SetProjectKnowledge(reader, "Field", w, platform.KnowledgeInput{Version: pr.Version, Knowledge: mine})
				if errors.Is(err, projects.ErrVersion) {
					continue
				}
				if err != nil {
					errs <- err
					return
				}
				want := []projects.KnowledgeSource{{Name: mine[0], From: "Field"}}
				if len(mine) == 2 {
					want = append(want, projects.KnowledgeSource{Name: mine[1], From: "Field"})
				} else {
					want = append(want, projects.KnowledgeSource{Name: "pricing", From: projects.Root})
				}
				if !slices.Equal(got.Knowledge, mine) || (got.Version != pr.Version && got.Version != pr.Version+1) || !slices.Equal(got.Given, want) {
					errs <- fmt.Errorf("writing %v from version %d answered %+v", mine, pr.Version, got)
					return
				}
			}
			errs <- nil
		}()
	}
	for range lists {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

// A source a project names is not deleted from under it: the delete is
// refused, naming the projects the caller can see. A project hidden from the
// caller neither holds the delete up nor is named, and keeps a name that
// grounds nothing.
func TestASourceAProjectNamesIsNotDeletedFromUnderIt(t *testing.T) {
	p := startWith(t, platform.Config{})
	ctx := t.Context()
	ada := projects.Writer{By: "ada"}
	for _, in := range []platform.ProjectInput{
		{Name: "Field"},
		{Name: "Secret", Members: []projects.Entry{{Who: "ada", Role: projects.Owner}}},
	} {
		if _, err := p.CreateProject(ctx, tenant, ada, in); err != nil {
			t.Fatal(err)
		}
	}
	shelf(t, p, map[string]string{"playbook": "Paula", "pricing": "Priya", "spare": "Sam"})
	name(t, p, "Field", "playbook")
	name(t, p, "Secret", "pricing")
	bo := auth.Principal{Subject: "bo", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.ProjectsRead, auth.KnowledgeRead, auth.KnowledgeWrite}}

	err := p.DeleteSource(bo, "playbook")
	if !errors.Is(err, platform.ErrSourceInUse) || !strings.Contains(err.Error(), "Field") {
		t.Fatalf("deleting a source Field names = %v", err)
	}
	if !slices.ContainsFunc(must(p.Knowledge.List(tenant)), func(s knowledge.Source) bool { return s.Name == "playbook" }) {
		t.Fatal("a refused delete deleted the source")
	}
	// A caller who may not read projects is told a project names it, not which.
	machine := auth.Principal{Subject: "bo", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.KnowledgeWrite}}
	if err := p.DeleteSource(machine, "playbook"); !errors.Is(err, platform.ErrSourceInUse) || strings.Contains(err.Error(), "Field") {
		t.Fatalf("deleting a source Field names, without projects:read = %v", err)
	}
	// A source that does not exist is not found, though a project names it.
	name(t, p, "Field", "playbook", "ghost")
	if err := p.DeleteSource(bo, "ghost"); !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("deleting a source that does not exist = %v", err)
	}
	if err := p.DeleteSource(bo, "pricing"); err != nil {
		t.Fatalf("deleting a source only a project hidden from the caller names = %v", err)
	}
	if err := p.DeleteSource(bo, "spare"); err != nil {
		t.Fatalf("deleting a source no project names = %v", err)
	}
	if pr, _ := p.Projects.Get(tenant, "Secret"); !slices.Equal(pr.Knowledge, []string{"pricing"}) {
		t.Fatalf("Secret's knowledge after the delete = %v", pr.Knowledge)
	}

	name(t, p, "Field")
	if err := p.DeleteSource(bo, "playbook"); err != nil {
		t.Fatalf("deleting a source no longer named = %v", err)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
