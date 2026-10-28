package platform_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/contextflags"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func TestLibraryUsesPublishedRevisionThroughRealPinnedSkillComposition(t *testing.T) {
	p := start(t, call.Limits{})
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	if _, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: "Shared"}); err != nil {
		t.Fatal(err)
	}
	scope := library.Scope{Kind: "project", Project: "Shared"}
	definition := library.Definition{Skill: projects.Skill{Name: "summarize", Description: "Summarize notes", Instructions: "Use exactly three sentences."}, Tools: []string{}}
	first, err := p.DraftSkill(who, scope, definition, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.AvailableSkills(tenant, "Shared", who.Subject)) != 0 {
		t.Fatal("draft became invocable")
	}
	if _, err := p.PublishSkill(who, first.ID, 1); !errors.Is(err, library.ErrConflict) {
		t.Fatal("untested revision published")
	}
	test, err := p.TestSkill(t.Context(), who, first.ID, 1, "lead", "Shared", "Summarize a release")
	if err != nil {
		t.Fatal(err)
	}
	completed := await(t, p, test.ID, runs.Completed)
	if wire.Meta(completed.Tag())[operations.MetaForceAsk] != "true" || wire.Meta(completed.Tag())[operations.MetaTools] != "[]" {
		t.Fatalf("test escaped policy: %+v", wire.Meta(completed.Tag()))
	}
	if !strings.Contains(wire.Meta(completed.Tag())[runs.MetaSkills], definition.Instructions) {
		t.Fatal("test did not pin exact draft")
	}
	if _, err := p.PublishSkill(who, first.ID, 1); err != nil {
		t.Fatal(err)
	}
	definition.Instructions = "Unpublished changed instructions."
	if _, err := p.DraftSkill(who, scope, definition, 1, nil); err != nil {
		t.Fatal(err)
	}
	posted, err := p.StartThread(t.Context(), who, platform.ThreadInput{Project: "Shared", Agents: []string{"lead"}, Text: "$summarize Release notes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(posted.Runs) != 1 {
		t.Fatalf("skill did not start selected agent: %+v", posted)
	}
	normal := await(t, p, posted.Runs[0].ID, runs.Completed)
	raw := wire.Meta(normal.Tag())[runs.MetaSkills]
	if !strings.Contains(raw, first.Instructions) || strings.Contains(raw, definition.Instructions) {
		t.Fatalf("live run used draft: %s", raw)
	}
	if wire.Meta(normal.Tag())[operations.MetaTools] != "[]" {
		t.Fatal("published skill lost tool restriction")
	}
}

func TestPersonalSkillPromotionRedactsPrivateProvenance(t *testing.T) {
	p := start(t, call.Limits{})
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	personal, err := p.PersonalProject(who)
	if err != nil {
		t.Fatal(err)
	}
	scope := library.Scope{Kind: "personal", Subject: who.Subject}
	definition := library.Definition{Skill: projects.Skill{Name: "brief", Description: "Brief me", Instructions: "Start with the conclusion."}, Tools: []string{}}
	r, err := p.DraftSkill(who, scope, definition, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	other := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "other", Roles: []auth.Role{auth.Admin}}
	if _, err := p.LibrarySkill(other, r.ID); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("admin private skill oracle: %v", err)
	}
	if _, err := p.TestSkill(t.Context(), who, r.ID, 1, "lead", "~", "Test"); err == nil {
		t.Fatal("personal draft tested in shared scope")
	}
	test, err := p.TestSkill(t.Context(), who, r.ID, 1, "lead", personal.Name, "Test")
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, test.ID, runs.Completed)
	if _, err := p.PublishSkill(who, r.ID, 1); err != nil {
		t.Fatal(err)
	}
	promoted, err := p.DraftSkill(who, library.Scope{Kind: "workspace"}, definition, 0, &library.Provenance{Scope: scope, Name: r.Name, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if promoted.From == nil {
		t.Fatal("provenance lost for owner")
	}
	if p.LibraryView(other, promoted).From != nil {
		t.Fatal("public promotion exposed private source")
	}
	if _, err := p.LibrarySkill(auth.Principal{Kind: auth.Machine, Tenant: tenant, Subject: "ada", Scopes: []auth.Permission{auth.ProjectsGovern}}, r.ID); !errors.Is(err, library.ErrNotFound) {
		t.Fatal("machine impersonated personal owner")
	}
}

func TestPersonalSkillClassifiedBeforeDispatchAndSurvivesReplay(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := &faultyIn{Store: open(t)}
		provider := &privateContextWitness{Provider: script(say("Done.")), calls: make(chan struct{}, 10)}
		p := startWith(t, platform.Config{Backends: platform.Backends{State: st}, AgentOptions: []native.Option{native.WithProvider(native.ProviderOffline, func(native.Spec) (saigetypes.Provider, error) { return provider, nil })}})
		who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
		home, err := p.PersonalProject(who)
		if err != nil {
			t.Fatal(err)
		}
		definition := library.Definition{Skill: projects.Skill{Name: "private-brief", Description: "Brief me", Instructions: "Use the private briefing outline."}, Tools: []string{}}
		revision, err := p.DraftSkill(who, library.Scope{Kind: "personal", Subject: who.Subject}, definition, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		turn := platform.Turn{ID: "private-skill-admission", Agent: "lead", Project: home.Name, Text: "Prepare a brief", Actor: platform.ActorOf(who), ForceAsk: true, TestSkill: &revision}
		st.fail("context.flags")
		if _, err := p.Converse(t.Context(), tenant, turn); err == nil {
			t.Fatal("private instructions dispatched without durable classification")
		}
		select {
		case <-provider.calls:
			t.Fatal("model consumed private skill after failed write")
		default:
		}
		st.fail("")
		accepted, err := p.Converse(t.Context(), tenant, turn)
		if err != nil {
			t.Fatal(err)
		}
		completed := await(t, p, accepted.ID, runs.Completed)
		if wire.Meta(completed.Tag())[platform.MetaPrivateLibraryContext] != "true" || !p.ContextFlags.Has(tenant, completed.ID) || !p.RunRequiresReviewedSummary(t.Context(), completed, "") {
			t.Fatal("private library context was publishable")
		}
		turn.TestSkill = nil
		turn.Text = "Changed retry"
		same, err := p.Converse(t.Context(), tenant, turn)
		if err != nil || same.ID != completed.ID || wire.Meta(same.Tag())[platform.MetaPrivateLibraryContext] != "true" {
			t.Fatal("retry changed private classification")
		}
		again, err := contextflags.Open(st)
		if err != nil || !again.Has(tenant, completed.ID) {
			t.Fatal("private classification lost on restart")
		}
	})
}

func TestPersonalSkillRunsThroughOwnedPersonalAgentWithoutBroadeningScope(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := &faultyIn{Store: open(t)}
		provider := &privateContextWitness{Provider: script(say("Private skill test complete.")), calls: make(chan struct{}, 10)}
		p := startWith(t, platform.Config{Backends: platform.Backends{State: st}, AgentOptions: []native.Option{native.WithProvider(native.ProviderOffline, func(native.Spec) (saigetypes.Provider, error) { return provider, nil })}})
		who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
		home, err := p.PersonalProject(who)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.SetAgentHome(t.Context(), tenant, "helper", home.Name, projects.Writer{By: who.Subject, Govern: true}); err != nil {
			t.Fatal(err)
		}
		definition := library.Definition{Skill: projects.Skill{Name: "personal-owned-test", Description: "Test personal instructions", Instructions: "Follow the private review checklist."}, Tools: []string{}}
		draft, err := p.DraftSkill(who, library.Scope{Kind: "personal", Subject: who.Subject}, definition, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.TestSkill(t.Context(), who, draft.ID, 1, "helper", "~", "Test"); !errors.Is(err, projects.ErrForbidden) {
			t.Fatal("explicit shared destination was coerced", err)
		}
		other := who
		other.Subject = "eve"
		if _, err := p.TestSkill(t.Context(), other, draft.ID, 1, "helper", home.Name, "Test"); !errors.Is(err, library.ErrNotFound) {
			t.Fatal("other owner accessed personal draft", err)
		}
		st.fail("context.flags")
		if _, err := p.TestSkill(t.Context(), who, draft.ID, 1, "helper", home.Name, "Test private instructions"); err == nil {
			t.Fatal("test dispatched after failed private classification")
		}
		select {
		case <-provider.calls:
			t.Fatal("model consumed instructions after refused scope or failed classification")
		default:
		}
		st.fail("")
		accepted, err := p.TestSkill(t.Context(), who, draft.ID, 1, "helper", home.Name, "Test private instructions")
		if err != nil {
			t.Fatal(err)
		}
		completed := await(t, p, accepted.ID, runs.Completed)
		select {
		case <-provider.calls:
		default:
			t.Fatal("actual configured adapter was never called")
		}
		if p.RunPlace(tenant, completed).Project != home.Name || wire.Meta(completed.Tag())[operations.MetaForceAsk] != "true" || wire.Meta(completed.Tag())[operations.MetaTools] != "[]" || wire.Meta(completed.Tag())[platform.MetaPrivateLibraryContext] != "true" {
			t.Fatal("personal test lost placement or private action bounds", completed.Tag())
		}
		if !strings.Contains(wire.Meta(completed.Tag())[runs.MetaSkills], definition.Instructions) || !p.ContextFlags.Has(tenant, completed.ID) {
			t.Fatal("exact private skill not durably pinned")
		}
		again, err := library.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		recorded, err := again.Get(tenant, draft.Scope, draft.Name, 1)
		if err != nil || len(recorded.Tests) != 1 || recorded.Tests[0] != completed.ID {
			t.Fatal("personal skill test did not replay", recorded, err)
		}
		flags, err := contextflags.Open(st)
		if err != nil || !flags.Has(tenant, completed.ID) {
			t.Fatal("private classification did not replay", err)
		}
	})
}
