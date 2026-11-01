package library

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestSkillRevisionsPublishExactTestedContentAndSurviveReplay(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		scope := Scope{Kind: "personal", Subject: "ada"}
		definition := Definition{Skill: projects.Skill{Name: "summarize", Description: "Summarize notes", Instructions: "Use three sentences"}, Tools: []string{}}
		first, err := s.Draft("acme", "ada", scope, definition, 0, nil, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Publish("acme", "ada", scope, first.Name, first.Version, time.Now()); !errors.Is(err, ErrConflict) {
			t.Fatalf("untested published: %v", err)
		}
		if err := s.Tested("acme", scope, first.Name, first.Version, "test-run"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Publish("acme", "ada", scope, first.Name, first.Version, time.Now()); err != nil {
			t.Fatal(err)
		}
		definition.Instructions = "Changed draft"
		if _, err := s.Draft("acme", "ada", scope, definition, 1, nil, time.Now()); err != nil {
			t.Fatal(err)
		}
		s, err = Open(st)
		if err != nil {
			t.Fatal(err)
		}
		published, ok, err := s.Published("acme", scope, first.Name)
		if err != nil || !ok || published.Version != 1 || published.Instructions != "Use three sentences" {
			t.Fatalf("draft changed live revision: %+v", published)
		}
		if other, err := s.List("other", scope); err != nil || len(other) != 0 {
			t.Fatal("cross-tenant skill", err)
		}
		if bob, err := s.List("acme", Scope{Kind: "personal", Subject: "bob"}); err != nil || len(bob) != 0 {
			t.Fatal("cross-scope skill")
		}
		if _, err := s.Draft("acme", "ada", scope, definition, 1, nil, time.Now()); !errors.Is(err, ErrConflict) {
			t.Fatal("stale draft accepted")
		}
	})
}

func TestFailedSkillWritesAndReturnedSlicesDoNotChangePublishedRevision(t *testing.T) {
	st := statetest.NewFaulty(state.NewMemory())
	s, _ := Open(st)
	scope := Scope{Kind: "workspace"}
	d := Definition{Skill: projects.Skill{Name: "review", Description: "Review", Instructions: "Read carefully"}, Tools: []string{"file_read"}}
	r, err := s.Draft("acme", "ada", scope, d, 0, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	d.Tools[0] = "file_write"
	r.Tools[0] = "file_write"
	st.Fail(statetest.ErrInjected)
	if err := s.Tested("acme", scope, "review", 1, "run"); err == nil {
		t.Fatal("failed write accepted")
	}
	held, _ := s.Get("acme", scope, "review", 1)
	if held.Tools[0] != "file_read" || len(held.Tests) != 0 {
		t.Fatalf("mutated immutable revision: %+v", held)
	}
}
