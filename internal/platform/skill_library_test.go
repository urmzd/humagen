package platform_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/humagen/internal/platform"
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
	if completed.Tag().Meta[operations.MetaForceAsk] != "true" || completed.Tag().Meta[operations.MetaTools] != "[]" {
		t.Fatalf("test escaped policy: %+v", completed.Tag().Meta)
	}
	if !strings.Contains(completed.Tag().Meta[runs.MetaSkills], definition.Instructions) {
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
	raw := normal.Tag().Meta[runs.MetaSkills]
	if !strings.Contains(raw, first.Instructions) || strings.Contains(raw, definition.Instructions) {
		t.Fatalf("live run used draft: %s", raw)
	}
	if normal.Tag().Meta[operations.MetaTools] != "[]" {
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
		p := startWith(t, platform.Config{Backends: platform.Backends{State: st}, AgentOptions: []agentrt.Option{agentrt.WithProvider(agentrt.ProviderOffline, func(agentrt.Spec) (saigetypes.Provider, error) { return provider, nil })}})
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
		if completed.Tag().Meta[platform.MetaPrivateLibraryContext] != "true" || !p.ContextFlags.Has(tenant, completed.ID) || !p.RunRequiresReviewedSummary(t.Context(), completed, "") {
			t.Fatal("private library context was publishable")
		}
		turn.TestSkill = nil
		turn.Text = "Changed retry"
		same, err := p.Converse(t.Context(), tenant, turn)
		if err != nil || same.ID != completed.ID || same.Tag().Meta[platform.MetaPrivateLibraryContext] != "true" {
			t.Fatal("retry changed private classification")
		}
		again, err := contextflags.Open(st)
		if err != nil || !again.Has(tenant, completed.ID) {
			t.Fatal("private classification lost on restart")
		}
	})
}
