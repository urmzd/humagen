package library

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestAgentDefaultSkillsReplayCASAndCloning(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := OpenDefaults(st)
		if err != nil {
			t.Fatal(err)
		}
		refs := []SkillRef{{ID: "skill", Version: 2}}
		first, err := s.Set("t", "agent", 3, 0, refs)
		if err != nil {
			t.Fatal(err)
		}
		refs[0].Version = 9
		first.Skills[0].Version = 8
		if _, err := s.Set("t", "agent", 4, 0, []SkillRef{}); !errors.Is(err, ErrConflict) {
			t.Fatal("stale save", err)
		}
		again, err := OpenDefaults(st)
		if err != nil {
			t.Fatal(err)
		}
		saved := again.Get("t", "agent")
		if saved.Version != 1 || saved.AgentRevision != 3 || saved.Skills[0].Version != 2 {
			t.Fatal("lost immutable references", saved)
		}
		users := again.Using("t", SkillRef{ID: "skill", Version: 2})
		if len(users) != 1 || users[0].Agent != "agent" {
			t.Fatal("exact inverse binding lost after replay", users)
		}
		users[0].Skills[0].Version = 6
		if len(again.Using("t", SkillRef{ID: "skill", Version: 3})) != 0 || len(again.Using("other", SkillRef{ID: "skill", Version: 2})) != 0 {
			t.Fatal("inverse binding widened version or tenant")
		}
		saved.Skills[0].Version = 7
		if again.Get("t", "agent").Skills[0].Version != 2 {
			t.Fatal("getter aliases saved bindings")
		}
		if len(again.Get("other", "agent").Skills) != 0 {
			t.Fatal("cross tenant bindings")
		}
	})
}
func TestAgentDefaultSkillWriteFailureDoesNotMutate(t *testing.T) {
	st := statetest.NewFaulty(state.NewMemory())
	s, err := OpenDefaults(st)
	if err != nil {
		t.Fatal(err)
	}
	st.Fail(errors.New("disk unavailable"))
	if _, err := s.Set("t", "agent", 1, 0, []SkillRef{{ID: "skill", Version: 1}}); err == nil {
		t.Fatal("failed durable write accepted")
	}
	if s.Get("t", "agent").Version != 0 {
		t.Fatal("failed write changed current defaults")
	}
}
