package projectforks_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/projectforks"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

var at = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func plan(name string) projectforks.Plan {
	return projectforks.Plan{Source: "original", Destination: name, Projects: []projectforks.ProjectCopy{{Source: "original", Name: name, Config: projects.Project{Instructions: "saved instructions"}}}}
}

func TestPrivateForkPlanIsImmutableAndSurvivesRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		raw := open(t)
		s, err := projectforks.Open(raw)
		if err != nil {
			t.Fatal(err)
		}
		input := plan("copied")
		first, err := s.Start("acme", "ana", input, at)
		if err != nil {
			t.Fatal(err)
		}
		input.Projects[0].Config.Instructions = "mutated caller"
		first.Plan.Projects[0].Config.Instructions = "mutated response"
		second, err := s.Start("acme", "ana", plan("copied"), at)
		if err != nil || second.ID != first.ID || second.Plan.Projects[0].Config.Instructions != "saved instructions" {
			t.Fatalf("plan changed: %+v %v", second, err)
		}
		if _, err := s.Get("acme", "copied", "other"); !errors.Is(err, projectforks.ErrNotFound) {
			t.Fatalf("other owner: %v", err)
		}
		if _, err := s.Get("other", "copied", "ana"); !errors.Is(err, projectforks.ErrNotFound) {
			t.Fatalf("other tenant: %v", err)
		}
		if err := s.Failure("acme", "copied", "ana", errors.New("secret credential must not be recorded"), at); err != nil {
			t.Fatal(err)
		}
		again, err := projectforks.Open(raw)
		if err != nil {
			t.Fatal(err)
		}
		kept, err := again.Get("acme", "copied", "ana")
		if err != nil || kept.ID != first.ID || kept.Error == "" || strings.Contains(kept.Error, "secret") {
			t.Fatalf("restart: %+v %v", kept, err)
		}
	})
}
func TestPlanCapacityAndWritesFailClosed(t *testing.T) {
	s, err := projectforks.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range projectforks.MaxOperations {
		if _, err := s.Start("acme", "ana", plan(fmt.Sprintf("copy-%d", i)), at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Start("acme", "ana", plan("too-many"), at); !errors.Is(err, projectforks.ErrLimit) {
		t.Fatalf("unbounded operations: %v", err)
	}
	raw := statetest.NewFaulty(state.NewMemory())
	s, err = projectforks.Open(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw.Fail(statetest.ErrInjected)
	if _, err := s.Start("acme", "ana", plan("copied"), at); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("failed durable write succeeded: %v", err)
	}
	if _, err := s.Get("acme", "copied", "ana"); !errors.Is(err, projectforks.ErrNotFound) {
		t.Fatal("failed plan appeared in memory")
	}
}
