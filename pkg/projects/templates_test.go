package projects_test

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestTemplatesWriteStructureAndSkillsAtomicallyAndSurviveRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		templates := projects.DefaultTemplates()
		if err := projects.ValidateTemplates(templates); err != nil {
			t.Fatal(err)
		}
		delivery := templates[0]
		pr, err := s.CreateTemplate("acme", team("Launch", ""), delivery, ada, t0)
		if err != nil {
			t.Fatal(err)
		}
		if pr.Template != "delivery" || len(pr.Skills) != 1 {
			t.Fatalf("template missing configuration: %+v", pr)
		}
		channels, err := s.Channels("acme", "Launch")
		if err != nil || len(channels) != 3 {
			t.Fatalf("channels: %+v %v", channels, err)
		}
		again, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		got, err := again.Get("acme", "Launch")
		if err != nil || got.Template != "delivery" || got.Instructions == "" {
			t.Fatalf("template restart: %+v %v", got, err)
		}
	})
	raw := statetest.NewFaulty(state.NewMemory())
	s, err := projects.Open(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw.Fail(statetest.ErrInjected)
	if _, err := s.CreateTemplate("acme", team("Launch", ""), projects.DefaultTemplates()[0], ada, t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("failed write: %v", err)
	}
	if _, err := s.Get("acme", "Launch"); !errors.Is(err, projects.ErrNotFound) {
		t.Fatal("partial template project visible")
	}
}
