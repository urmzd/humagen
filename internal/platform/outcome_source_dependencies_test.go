package platform_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestSourceDependenciesUseOnlyVisibleTypedLearningOrigins(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		p, err := newOn(t, st)
		if err != nil {
			t.Fatal(err)
		}
		who, id := learningRun(t, p)
		if _, err := p.DraftSkill(who, library.Scope{Kind: "personal", Subject: who.Subject}, learnedDefinition("unrelated-skill"), 0, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Memory.AddDetailed(tenant, memory.PersonalOwner(who.Subject), "Merely mentions evidence-source", memory.Consolidated, memory.Approved, id, time.Now(), memory.Details{Kind: memory.Decision}); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Knowledge.Create(tenant, "evidence-source", "", who.Subject, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Knowledge.Create(tenant, "unrelated-source", "", who.Subject, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.SourceEvidence.RecordQuery(tenant, id, "trusted-query", []knowledge.CitationEvidence{{Provider: "manual", Source: "evidence-source", DocumentID: "doc", Title: "Private Evidence", Text: "private evidence marker", Mode: knowledge.Indexed, Project: "Learning", Person: who.Subject}}); err != nil {
			t.Fatal(err)
		}
		preview, err := p.OutcomeLearning(who, id, library.Scope{})
		if err != nil {
			t.Fatal(err)
		}
		skill, err := p.DraftOutcomeSkill(who, id, platform.OutcomeSkillDraft{Snapshot: preview.Snapshot, Definition: learnedDefinition("typed-origin")})
		if err != nil {
			t.Fatal(err)
		}
		note, err := p.RememberOutcomeDecision(who, id, platform.OutcomeDecisionMemory{Snapshot: preview.Snapshot, Text: "Keep the Tuesday launch", Decision: 0})
		if err != nil {
			t.Fatal(err)
		}
		deps, err := p.SourceDependenciesAs(context.Background(), who, "evidence-source")
		if err != nil {
			t.Fatal(err)
		}
		if len(deps.Skills) != 1 || deps.Skills[0].ID != skill.ID || len(deps.Memory) != 1 || deps.Memory[0].ID != note.ID {
			t.Fatalf("typed learning dependencies missing: %+v", deps)
		}
		body, _ := json.Marshal(deps)
		if strings.Contains(string(body), id) || strings.Contains(string(body), "private evidence marker") || strings.Contains(string(body), "Private Evidence") {
			t.Fatalf("private origin/evidence in DTO: %s", body)
		}
		unrelated, err := p.SourceDependenciesAs(context.Background(), who, "unrelated-source")
		if err != nil || len(unrelated.Skills) != 0 || len(unrelated.Memory) != 0 {
			t.Fatal("unrelated source acquired dependencies", unrelated, err)
		}
		replay, err := newOn(t, st)
		if err != nil {
			t.Fatal(err)
		}
		got, err := replay.SourceDependenciesAs(context.Background(), who, "evidence-source")
		if err != nil || len(got.Skills) != 1 || len(got.Memory) != 1 {
			t.Fatal("learning provenance did not replay", got, err)
		}
		outsider := who
		outsider.Subject = "govern-only"
		got, err = replay.SourceDependenciesAs(context.Background(), outsider, "evidence-source")
		if err != nil || len(got.Skills) != 0 || len(got.Memory) != 0 {
			t.Fatal("govern-only reader acquired private learning lineage", got, err)
		}
		noKnowledge := who
		noKnowledge.Roles = nil
		noKnowledge.Scopes = []auth.Permission{auth.RunsRead, auth.ProjectsRead}
		if _, err := replay.SourceDependenciesAs(context.Background(), noKnowledge, "evidence-source"); err == nil {
			t.Fatal("knowledge permission revocation retained graph")
		}
		pr, _ := replay.Projects.Get(tenant, "Learning")
		if _, err := replay.Projects.Update(tenant, pr.Name, pr.Version, projects.Writer{By: who.Subject, Govern: true}, time.Now(), func(pr *projects.Project) error {
			pr.Members = []projects.Entry{{Who: "ben", Role: projects.Owner}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		got, err = replay.SourceDependenciesAs(context.Background(), who, "evidence-source")
		if err != nil || len(got.Skills) != 0 || len(got.Memory) != 0 {
			t.Fatal("origin project membership revocation retained learning lineage", got, err)
		}
	})
}
