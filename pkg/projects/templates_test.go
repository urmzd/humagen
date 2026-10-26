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
func TestInitiativeAgentsCannotExecuteInChildProjects(t *testing.T) {
	s := projects.NewStore()
	var initiative projects.Template
	for _, template := range projects.DefaultTemplates() {
		if template.Initiative {
			initiative = template
		}
	}
	pr, err := s.CreateTemplate("acme", team("Strategy", ""), initiative, ada, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !pr.Initiative {
		t.Fatal("initiative marker not stored")
	}
	mustCreate(t, s, "acme", team("Delivery", "Strategy"), ada)
	if err := s.SetHome("acme", "coordinator", "Strategy", ada, t0, nil); err != nil {
		t.Fatal(err)
	}
	tree := s.Tree("acme")
	if !tree.IsAvailable("Strategy", "coordinator") || tree.IsAvailable("Delivery", "coordinator") {
		t.Fatal("initiative agent escaped coordination project")
	}
	for _, name := range tree.Available("Delivery").Names {
		if name == "coordinator" {
			t.Fatal("initiative agent offered for direct child execution")
		}
	}
}
