package platform_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

func learningRun(t *testing.T, p *platform.Platform) (auth.Principal, string) {
	t.Helper()
	who := auth.Principal{Tenant: tenant, Subject: "ada", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	if _, err := p.Projects.Create(tenant, projects.Project{Name: "Learning", Members: []projects.Entry{{Who: "ada", Role: projects.Owner}, {Who: "ben", Role: projects.Member}}}, projects.Writer{By: "ada", Govern: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	th, _, err := p.Threads.Start(tenant, threads.Thread{Project: "Learning", Channel: "general", Visibility: threads.Private}, threads.Message{By: "ada", Name: "Ada", Text: "Consider the launch"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	id := runtime.RunID(tenant, "learning-outcome")
	if err := p.Runs.Record(id, wire.NewEvent(wire.Raise{ID: "learning-outcome", Tenant: tenant, Agent: "helper", Origin: runs.OriginAPI, Text: "Review the plan", Context: map[string]string{runs.MetaProject: "Learning", runs.MetaThread: th.ID}, At: time.Now()}), 1); err != nil {
		t.Fatal(err)
	}
	action, _ := json.Marshal(event.Action{Name: platform.ToolOutcome, Data: json.RawMessage(`{"kind":"decision","summary":"Use the Tuesday plan.","decisions":["Keep the Tuesday launch"],"next_action":"Confirm the owner"}`)})
	p.Runs.Observe(event.Envelope{Tenant: tenant, Run: id, Type: event.TypeAction, Data: wire.BodyOf(event.TypeAction, action), Time: time.Now()})
	end, _ := json.Marshal(wire.Lifecycle{Text: "The plan is ready."})
	p.Runs.Observe(event.Envelope{Tenant: tenant, Run: id, Type: event.TypeCompleted, Data: wire.BodyOf(event.TypeCompleted, end), Time: time.Now()})
	if err := p.Runs.Flush(); err != nil {
		t.Fatal(err)
	}
	return who, id
}
func learnedDefinition(name string) library.Definition {
	return library.Definition{Skill: projects.Skill{Name: name, Description: "Review a launch", Instructions: "Review the facts, then ask the owner."}, Tools: []string{}}
}

func TestOutcomeLearningRequiresHumanReviewAndSurvivesRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		p, err := newOn(t, st)
		if err != nil {
			t.Fatal(err)
		}
		who, id := learningRun(t, p)
		if len(p.Memory.List(tenant, memory.ProjectOwner("Learning"))) != 0 {
			t.Fatal("agent outcome approved itself")
		}
		preview, err := p.OutcomeLearning(who, id, library.Scope{})
		if err != nil {
			t.Fatal(err)
		}
		if preview.Scope.Kind != "personal" || !preview.CanDraft || !preview.CanRemember || !preview.MayProject {
			t.Fatalf("wrong review capabilities: %+v", preview)
		}
		input := platform.OutcomeSkillDraft{Snapshot: preview.Snapshot, Definition: learnedDefinition("review-launch")}
		skill, err := p.DraftOutcomeSkill(who, id, input)
		if err != nil {
			t.Fatal(err)
		}
		if skill.Version != 1 || len(skill.Tools) != 0 || !skill.PublishedAt.IsZero() || len(skill.Tests) != 0 || skill.OriginRun != id {
			t.Fatalf("not a tool-free passive draft: %+v", skill)
		}
		if _, active := p.SkillLibrary.Published(tenant, skill.Scope, skill.Name); active {
			t.Fatal("draft became active without test/publication")
		}
		if again, err := p.DraftOutcomeSkill(who, id, input); err != nil || again.ID != skill.ID {
			t.Fatal("network retry created another draft", err)
		}
		note, err := p.RememberOutcomeDecision(who, id, platform.OutcomeDecisionMemory{Snapshot: preview.Snapshot, Decision: 0, Text: "Keep the Tuesday launch"})
		if err != nil {
			t.Fatal(err)
		}
		if note.Status != memory.Approved || note.Kind != memory.Decision || note.ReviewedBy != "ada" || note.By == id {
			t.Fatalf("review was not an explicit human decision: %+v", note)
		}
		again, err := newOn(t, st)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := again.SkillLibrary.Get(tenant, skill.Scope, skill.Name, 1)
		if err != nil || restored.OriginRun != id {
			t.Fatal("lost source provenance", err)
		}
		if notes := again.Memory.List(tenant, memory.PersonalOwner("ada")); len(notes) != 1 || notes[0].ID != note.ID || notes[0].ReviewedBy != "ada" {
			t.Fatalf("lost reviewed decision: %+v", notes)
		}
		shared, err := again.OutcomeLearning(who, id, library.Scope{Kind: "project"})
		if err != nil {
			t.Fatal(err)
		}
		projectSkill, err := again.DraftOutcomeSkill(who, id, platform.OutcomeSkillDraft{Snapshot: shared.Snapshot, Scope: shared.Scope, Definition: learnedDefinition("shared-review")})
		if err != nil {
			t.Fatal(err)
		}
		peer := auth.Principal{Tenant: tenant, Subject: "ben", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
		if view := again.LibraryView(peer, projectSkill); view.OriginRun != "" {
			t.Fatal("project reader learned a private source conversation")
		}
		promotion, err := again.SkillLibrary.Draft(tenant, "ada", library.Scope{Kind: "workspace"}, projectSkill.Definition, 0, &library.Provenance{Scope: projectSkill.Scope, Name: projectSkill.Name, Version: 1}, time.Now())
		if err != nil || promotion.OriginRun != "" {
			t.Fatal("promotion copied private run capability", err)
		}
	})
}

func TestOutcomeLearningRefusesHiddenStalePendingAndToolGrants(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p, err := newOn(t, open(t))
		if err != nil {
			t.Fatal(err)
		}
		who, id := learningRun(t, p)
		peer := auth.Principal{Tenant: tenant, Subject: "ben", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
		if _, err := p.OutcomeLearning(peer, id, library.Scope{}); !errors.Is(err, runs.ErrNotFound) {
			t.Fatal("private source became a learning oracle", err)
		}
		governor := who
		governor.Subject = "outsider"
		if _, err := p.OutcomeLearning(governor, id, library.Scope{}); !errors.Is(err, runs.ErrNotFound) {
			t.Fatal("govern read permitted copying private work", err)
		}
		machine := who
		machine.Kind = auth.Machine
		if _, err := p.OutcomeLearning(machine, id, library.Scope{}); !errors.Is(err, runs.ErrNotFound) {
			t.Fatal("machine approved its own outcome", err)
		}
		preview, err := p.OutcomeLearning(who, id, library.Scope{})
		if err != nil {
			t.Fatal(err)
		}
		definition := learnedDefinition("unsafe")
		definition.Tools = []string{"file_write"}
		if _, err := p.DraftOutcomeSkill(who, id, platform.OutcomeSkillDraft{Snapshot: preview.Snapshot, Definition: definition}); !errors.Is(err, library.ErrInvalid) {
			t.Fatal("outcome silently granted tools", err)
		}
		if _, err := p.Memory.AddDetailed(tenant, memory.PersonalOwner("ada"), "Keep the Tuesday launch", memory.Consolidated, memory.Pending, id, time.Now(), memory.Details{Kind: memory.Decision}); err != nil {
			t.Fatal(err)
		}
		if _, err := p.RememberOutcomeDecision(who, id, platform.OutcomeDecisionMemory{Snapshot: preview.Snapshot, Text: "Keep the Tuesday launch"}); !errors.Is(err, memory.ErrReviewConflict) {
			t.Fatal("pending duplicate was reported approved", err)
		}
		pr, _ := p.Projects.Get(tenant, "Learning")
		if _, err := p.Projects.Update(tenant, "Learning", pr.Version, projects.Writer{By: "ada"}, time.Now(), func(pr *projects.Project) error { pr.Description = "Audience changed"; return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := p.DraftOutcomeSkill(who, id, platform.OutcomeSkillDraft{Snapshot: preview.Snapshot, Definition: learnedDefinition("stale")}); !errors.Is(err, runs.ErrLearningConflict) {
			t.Fatal("stale audience review accepted", err)
		}
		raw, _ := p.Runs.Get(tenant, id)
		th, _ := p.Threads.Get(tenant, raw.Thread)
		if _, err := p.Threads.Share(tenant, th.ID, th.Version, th.Project, th.Channel, "", nil, "", nil, nil, "ada", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.OutcomeLearning(peer, id, library.Scope{Kind: "project"}); !errors.Is(err, projects.ErrForbidden) {
			t.Fatal("ordinary member could approve shared project memory", err)
		}
	})
}
