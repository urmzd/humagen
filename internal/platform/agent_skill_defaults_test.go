package platform_test

import (
	"errors"
	"github.com/urmzd/humagen/pkg/call"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestAgentDefaultSkillsUseExactPublishedRevisionInEveryTurn(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p := startWith(t, platform.Config{Backends: platform.Backends{State: open(t)}})
		who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
		if err := p.SetAgentHome(t.Context(), tenant, "lead", projects.Root, govern); err != nil {
			t.Fatal(err)
		}
		scope := library.Scope{Kind: "workspace"}
		definition := library.Definition{Skill: projects.Skill{Name: "default-skill", Description: "Default review", Instructions: "FOLLOW THE PUBLISHED CHECKLIST"}, Tools: []string{}}
		first, err := p.SkillLibrary.Draft(tenant, "ada", scope, definition, 0, nil, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		refs := []platform.AgentSkillRef{{ID: first.ID, Version: first.Version}}
		if _, err := p.SetAgentSkills(who, "lead", 1, 0, refs); !errors.Is(err, library.ErrInvalid) {
			t.Fatal("unpublished default", err)
		}
		if err := p.SkillLibrary.Tested(tenant, scope, first.Name, first.Version, "trusted-fixture-test"); err != nil {
			t.Fatal(err)
		}
		if _, err := p.SkillLibrary.Publish(tenant, "ada", scope, first.Name, first.Version, time.Now()); err != nil {
			t.Fatal(err)
		}
		saved, err := p.SetAgentSkills(who, "lead", 1, 0, refs)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.SetAgentSkills(who, "lead", 1, 0, []platform.AgentSkillRef{}); !errors.Is(err, library.ErrConflict) {
			t.Fatal("stale defaults", err)
		}
		if _, err := p.SetAgentSkills(who, "lead", 2, saved.Version, refs); !errors.Is(err, library.ErrConflict) {
			t.Fatal("wrong registry revision", err)
		}
		definition.Instructions = "UNPUBLISHED REPLACEMENT"
		if _, err := p.SkillLibrary.Draft(tenant, "ada", scope, definition, first.Version, nil, time.Now()); err != nil {
			t.Fatal(err)
		}
		turn := platform.Turn{ID: "agent-default-pinned", Agent: "lead", Text: "A normal turn without skill syntax", Actor: platform.ActorOf(who)}
		run, err := p.Converse(t.Context(), tenant, turn)
		if err != nil {
			t.Fatal(err)
		}
		run = await(t, p, run.ID, runs.Completed)
		raw := run.Tag().Meta[runs.MetaSkills]
		if !strings.Contains(raw, first.Instructions) || strings.Contains(raw, definition.Instructions) || run.Tag().Meta[operations.MetaTools] != "[]" {
			t.Fatalf("default was not compiled by normal skill path: %s", raw)
		}
		if _, err := p.SetAgentSkills(who, "lead", 1, saved.Version, []platform.AgentSkillRef{}); err != nil {
			t.Fatal(err)
		}
		same, err := p.Converse(t.Context(), tenant, turn)
		if err != nil || same.ID != run.ID || same.Tag().Meta[runs.MetaSkills] != raw {
			t.Fatal("same-ID replay changed pinned defaults", err)
		}
	})
}

func TestPersonalDefaultSkillsRequirePrivateHomeAndPreventPublicMove(t *testing.T) {
	p := start(t, call.Limits{})
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	home, err := p.PersonalProject(who)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetAgentHome(t.Context(), tenant, "helper", home.Name, projects.Writer{By: "ada", Govern: true}); err != nil {
		t.Fatal(err)
	}
	scope := library.Scope{Kind: "personal", Subject: "ada"}
	r, err := p.SkillLibrary.Draft(tenant, "ada", scope, library.Definition{Skill: projects.Skill{Name: "private-default", Description: "Private instructions", Instructions: "PRIVATE DEFAULT INSTRUCTIONS"}, Tools: []string{}}, 0, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SkillLibrary.Tested(tenant, scope, r.Name, 1, "trusted-fixture-test"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SkillLibrary.Publish(tenant, "ada", scope, r.Name, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	refs := []platform.AgentSkillRef{{ID: r.ID, Version: 1}}
	if _, err := p.SetAgentSkills(who, "helper", 1, 0, refs); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAgentHome(t.Context(), tenant, "helper", projects.Root, projects.Writer{By: "ada", Govern: true}); !errors.Is(err, projects.ErrInUse) {
		t.Fatal("private defaults moved to Company", err)
	}
	other := who
	other.Subject = "eve"
	if _, err := p.ValidateAgentSkills(other, home.Name, refs); !errors.Is(err, library.ErrNotFound) {
		t.Fatal("private default oracle", err)
	}
	run, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "private-default", Agent: "helper", Project: home.Name, Text: "Brief me", Actor: platform.ActorOf(who)})
	if err != nil {
		t.Fatal(err)
	}
	run = await(t, p, run.ID, runs.Completed)
	if run.Tag().Meta[platform.MetaPrivateLibraryContext] != "true" || !p.ContextFlags.Has(tenant, run.ID) || !p.RunRequiresReviewedSummary(t.Context(), run, projects.Root) {
		t.Fatal("private default lacked durable classification")
	}
}
