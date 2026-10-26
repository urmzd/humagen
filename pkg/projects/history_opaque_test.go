package projects_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestProjectHistoryOpaqueIDsHideTenantSequenceGapsAndReplay(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		pr := mustCreate(t, s, "acme", team("Visible", ""), ada)
		if _, err := s.Update("acme", pr.Name, pr.Version, ada, t0, func(p *projects.Project) error { p.Title = "Visible title"; return nil }); err != nil {
			t.Fatal(err)
		}
		before, next, err := s.OpaqueHistory("acme", "Visible", 1, "")
		if err != nil || len(before) != 1 || next == "" {
			t.Fatalf("opaque page: %v %q %v", before, next, err)
		}
		personal, err := s.EnsurePersonal("acme", "hidden-person", t0)
		if err != nil {
			t.Fatal(err)
		}
		for range 12 {
			_, err := s.Update("acme", personal.Name, personal.Version, projects.Writer{By: "hidden-person"}, t0, func(p *projects.Project) error { p.Description += "hidden "; return nil })
			if err != nil {
				t.Fatal(err)
			}
			personal, _ = s.Tree("acme").Get(personal.Name)
		}
		after, afterNext, err := s.OpaqueHistory("acme", "Visible", 1, "")
		if err != nil || !reflect.DeepEqual(before, after) || next != afterNext {
			t.Fatal("hidden project activity changed visible history identifiers")
		}
		page, _, err := s.OpaqueHistory("acme", "Visible", 1, next)
		if err != nil || len(page) != 1 {
			t.Fatalf("opaque cursor did not page: %v %v", page, err)
		}
		if _, _, err := s.OpaqueHistory("acme", personal.Name, 1, next); err == nil {
			t.Fatal("history cursor crossed project scope")
		}
		raw, _ := json.Marshal(before[0])
		var public map[string]any
		if err := json.Unmarshal(raw, &public); err != nil {
			t.Fatal(err)
		}
		if id, ok := public["seq"].(string); !ok || len(id) != 34 {
			t.Fatalf("global numeric sequence escaped: %s", raw)
		}
		again, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		replay, replayNext, err := again.OpaqueHistory("acme", "Visible", 1, "")
		if err != nil || !reflect.DeepEqual(before, replay) || replayNext != next {
			t.Fatal("history identifiers changed on replay")
		}
	})
}
