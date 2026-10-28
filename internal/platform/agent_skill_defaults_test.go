package platform_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/contextflags"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	saigetypes "github.com/urmzd/saige/agent/types"
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
		preview, err := p.ProjectContextAs(t.Context(), who, projects.Root, "", "lead")
		if err != nil {
			t.Fatal(err)
		}
		foundDefault := false
		for _, layer := range preview.Layers {
			if layer.Kind == "agent_skills" {
				foundDefault = layer.Sent && layer.Used == 1 && strings.Contains(layer.Text, "default-skill@1")
			}
		}
		if !foundDefault {
			t.Fatal("actual default missing from composition preview", preview)
		}
		limited := auth.Principal{Kind: auth.Machine, Tenant: tenant, Subject: "limited-agent-editor", Scopes: []auth.Permission{auth.AgentsRead, auth.AgentsWrite}}
		if _, err := p.SetAgentSkills(limited, "lead", 1, saved.Version, []platform.AgentSkillRef{}); !errors.Is(err, projects.ErrForbidden) {
			t.Fatal("writer without library read removed hidden defaults", err)
		}
		retained, err := p.AgentSkills(tenant, "lead")
		if err != nil || len(retained.Skills) != 1 || retained.Version != saved.Version {
			t.Fatal("unread default mutated", retained, err)
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
		raw := wire.Meta(run.Tag())[runs.MetaSkills]
		if !strings.Contains(raw, first.Instructions) || strings.Contains(raw, definition.Instructions) || wire.Meta(run.Tag())[operations.MetaTools] != "[]" {
			t.Fatalf("default was not compiled by normal skill path: %s", raw)
		}

		rev, err := p.Agents.Get(t.Context(), tenant, "lead", 0)
		if err != nil {
			t.Fatal(err)
		}
		rev.Spec.SystemPrompt = "Updated agent instructions"
		nextRevision, err := p.ReviseBy(t.Context(), who, rev.Spec, rev.Rev)
		if err != nil {
			t.Fatal(err)
		}
		preview, err = p.ProjectContextAs(t.Context(), who, projects.Root, "", "lead")
		if err != nil {
			t.Fatal(err)
		}
		for _, layer := range preview.Layers {
			if layer.Kind == "agent_skills" && (layer.Sent || layer.Used != 0 || layer.Omitted != 1) {
				t.Fatal("preview claimed stale binding would be sent", layer)
			}
		}
		if state := p.AgentState(who, "lead"); state.State != "needs_review" {
			t.Fatal("stale defaults advertised ready", state.State)
		}
		changed := turn
		changed.ID = "new-agent-revision"
		if _, err := p.Converse(t.Context(), tenant, changed); !errors.Is(err, library.ErrConflict) {
			t.Fatal("new spec silently reused old skill binding", err)
		}
		rebound, err := p.SetAgentSkills(who, "lead", nextRevision.Rev, saved.Version, refs)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.SetAgentSkills(who, "lead", nextRevision.Rev, rebound.Version, []platform.AgentSkillRef{}); err != nil {
			t.Fatal(err)
		}
		same, err := p.Converse(t.Context(), tenant, turn)
		if err != nil || same.ID != run.ID || wire.Meta(same.Tag())[runs.MetaSkills] != raw {
			t.Fatal("same-ID replay changed pinned defaults", err)
		}
	})
}

func TestPersonalDefaultSkillsRequirePrivateHomeAndPreventPublicMove(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := &faultyIn{Store: open(t)}
		provider := &privateContextWitness{Provider: script(say("Done.")), calls: make(chan struct{}, 10)}
		p := startWith(t, platform.Config{Backends: platform.Backends{State: st}, AgentOptions: []native.Option{native.WithProvider(native.ProviderOffline, func(native.Spec) (saigetypes.Provider, error) { return provider, nil })}})
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
		auditRows, err := p.Audit.List(tenant, audit.Query{Resource: "agent", From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour)}, func(audit.Record) bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		foundAudit := false
		for _, row := range auditRows {
			if row.Action == "agent.skills" {
				foundAudit = true
				if row.Scope != "personal" || row.Subject != "ada" {
					t.Fatal("personal skill binding audit missing owner scope", row)
				}
			}
		}
		if !foundAudit {
			t.Fatal("personal skill binding audit missing")
		}
		if err := p.SetAgentHome(t.Context(), tenant, "helper", projects.Root, projects.Writer{By: "ada", Govern: true}); !errors.Is(err, projects.ErrInUse) {
			t.Fatal("private defaults moved to Company", err)
		}
		other := who
		other.Subject = "eve"
		if _, err := p.ValidateAgentSkills(other, home.Name, refs); !errors.Is(err, library.ErrNotFound) {
			t.Fatal("private default oracle", err)
		}
		turn := platform.Turn{ID: "private-default", Agent: "helper", Project: home.Name, Text: "Brief me", Actor: platform.ActorOf(who)}
		st.fail("context.flags")
		if _, err := p.Converse(t.Context(), tenant, turn); err == nil {
			t.Fatal("private default dispatched after failed classification")
		}
		select {
		case <-provider.calls:
			t.Fatal("model consumed private default after failed write")
		default:
		}
		st.fail("")
		run, err := p.Converse(t.Context(), tenant, turn)
		if err != nil {
			t.Fatal(err)
		}
		run = await(t, p, run.ID, runs.Completed)
		if wire.Meta(run.Tag())[platform.MetaPrivateLibraryContext] != "true" || !p.ContextFlags.Has(tenant, run.ID) || !p.RunRequiresReviewedSummary(t.Context(), run, projects.Root) {
			t.Fatal("private default lacked durable classification")
		}
		reopened, err := contextflags.Open(st)
		if err != nil || !reopened.Has(tenant, run.ID) {
			t.Fatal("private default classification lost after replay", err)
		}
	})
}
