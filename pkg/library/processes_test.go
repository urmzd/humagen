package library_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func definition(name, text string) library.Definition {
	return library.Definition{Skill: projects.Skill{Name: name, Description: "Skill " + name, Instructions: text}, Tools: []string{}}
}

func open(t *testing.T, st state.Store) *library.Store {
	t.Helper()
	s, err := library.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Two processes on one database see each other's skills at once, and
// drafts of one skill through either, each re-reading on a conflict, are
// each kept once, numbered without a gap or a repeat.
func TestSkillsTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, two := open(t, a), open(t, b)
		scope := library.Scope{Kind: "workspace"}
		first, err := one.Draft("acme", "ada", scope, definition("review", "v1"), 0, nil, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		got, err := two.Find("acme", first.ID)
		if err != nil || got.Instructions != "v1" {
			t.Fatalf("the other process finds %+v, %v", got, err)
		}
		if err := two.Tested("acme", scope, "review", 1, "run"); err != nil {
			t.Fatal(err)
		}
		if _, err := one.Publish("acme", "ada", scope, "review", 1, time.Now()); err != nil {
			t.Fatalf("a test recorded through the other process is not read: %v", err)
		}

		const drafts = 16
		var wg sync.WaitGroup
		for i := range drafts {
			s := []*library.Store{one, two}[i%2]
			wg.Go(func() {
				for {
					rows, err := s.History("acme", scope, "review")
					if err != nil {
						t.Error(err)
						return
					}
					_, err = s.Draft("acme", "ada", scope, definition("review", fmt.Sprintf("draft %d", i)), len(rows), nil, time.Now())
					if errors.Is(err, library.ErrConflict) {
						continue
					}
					if err != nil {
						t.Error(err)
					}
					return
				}
			})
		}
		wg.Wait()
		rows, err := one.History("acme", scope, "review")
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != drafts+1 {
			t.Fatalf("%d revisions, want %d", len(rows), drafts+1)
		}
		seen := map[string]bool{}
		for i, r := range rows {
			if r.Version != i+1 || seen[r.Instructions] {
				t.Fatalf("revision %d = %d %q", i, r.Version, r.Instructions)
			}
			seen[r.Instructions] = true
		}
		if pub, ok, err := two.Published("acme", scope, "review"); err != nil || !ok || pub.Version != 1 {
			t.Fatalf("lost the publication: %+v %v %v", pub, ok, err)
		}

		// New skills in one scope from both processes stop at the cap.
		personal := library.Scope{Kind: "personal", Subject: "bob"}
		for i := range library.MaxSkills + 8 {
			s := []*library.Store{one, two}[i%2]
			wg.Go(func() {
				_, err := s.Draft("acme", "bob", personal, definition(fmt.Sprintf("skill-%d", i), "x"), 0, nil, time.Now())
				if err != nil && !errors.Is(err, library.ErrInvalid) {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		list, err := two.List("acme", personal)
		if err != nil || len(list) != library.MaxSkills {
			t.Fatalf("%d skills in a scope, want %d: %v", len(list), library.MaxSkills, err)
		}
		for _, r := range list {
			if found, err := one.Find("acme", r.ID); err != nil || found.Name != r.Name {
				t.Fatalf("Find(%s) = %+v, %v", r.ID, found, err)
			}
		}
	})
}

// Defaults set through both processes at once keep their version in step:
// each write that wins is the next version, and none is lost.
func TestDefaultsTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := library.OpenDefaults(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := library.OpenDefaults(b)
		if err != nil {
			t.Fatal(err)
		}
		ref := func(v int) []library.SkillRef { return []library.SkillRef{{ID: "skill", Version: v}} }
		if _, err := one.Set("acme", "lead", 1, 0, ref(1)); err != nil {
			t.Fatal(err)
		}
		if got, err := two.Get("acme", "lead"); err != nil || got.Version != 1 {
			t.Fatalf("the other process reads %+v, %v", got, err)
		}
		const writers = 12
		var wg sync.WaitGroup
		for i := range writers {
			s := []*library.DefaultsStore{one, two}[i%2]
			wg.Go(func() {
				for {
					cur, err := s.Get("acme", "lead")
					if err != nil {
						t.Error(err)
						return
					}
					_, err = s.Set("acme", "lead", 1, cur.Version, ref(10+i))
					if errors.Is(err, library.ErrConflict) {
						continue
					}
					if err != nil {
						t.Error(err)
					}
					return
				}
			})
		}
		wg.Wait()
		got, err := one.Get("acme", "lead")
		if err != nil || got.Version != writers+1 {
			t.Fatalf("version %d, want %d: %v", got.Version, writers+1, err)
		}
		// Only the last binding is indexed.
		users := 0
		for v := 1; v < 10+writers; v++ {
			list, err := two.Using("acme", library.SkillRef{ID: "skill", Version: v})
			if err != nil {
				t.Fatal(err)
			}
			users += len(list)
		}
		if users != 1 {
			t.Fatalf("%d bindings indexed, want 1", users)
		}
	})
}

// Skills and defaults kept by an older release, without index and counter
// records, are indexed once though two processes open the stores at once.
func TestTwoProcessesConvertOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		old := state.NewTable[[]library.Revision](a, "library.skills")
		scope := library.Scope{Kind: "project", Project: "docs"}
		at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := range 5 {
			name := fmt.Sprintf("old-%d", i)
			r := library.Revision{Definition: definition(name, "kept"), Scope: scope, Version: 1, By: "ada", At: at, Tests: []string{}}
			if err := old.Put("acme", scope.Key()+"/"+name, []library.Revision{r}); err != nil {
				t.Fatal(err)
			}
		}
		oldDefaults := state.NewTable[library.AgentDefaults](a, "library.agent_defaults")
		for i := range 3 {
			agent := fmt.Sprintf("agent-%d", i)
			if err := oldDefaults.Put("acme", agent, library.AgentDefaults{Agent: agent, AgentRevision: 1, Version: 1, Skills: []library.SkillRef{{ID: "skill", Version: 1}}}); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		skills := make([]*library.Store, 4)
		defaults := make([]*library.DefaultsStore, 4)
		for i := range skills {
			wg.Go(func() {
				st := []state.Store{a, b}[i%2]
				var err error
				if skills[i], err = library.Open(st); err != nil {
					t.Error(err)
				}
				if defaults[i], err = library.OpenDefaults(st); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if t.Failed() {
			return
		}
		for i := range 5 {
			name := fmt.Sprintf("old-%d", i)
			got, err := skills[3].Find("acme", library.ID(scope, name))
			if err != nil || got.Name != name {
				t.Fatalf("Find(%s) = %+v, %v", name, got, err)
			}
		}
		// The scope counts what was kept: the cap leaves room for the rest.
		for i := range library.MaxSkills {
			_, err := skills[i%4].Draft("acme", "ada", scope, definition(fmt.Sprintf("new-%d", i), "x"), 0, nil, time.Now())
			if i < library.MaxSkills-5 && err != nil {
				t.Fatalf("draft %d: %v", i, err)
			}
			if i >= library.MaxSkills-5 && !errors.Is(err, library.ErrInvalid) {
				t.Fatalf("draft %d past the cap: %v", i, err)
			}
		}
		users, err := defaults[1].Using("acme", library.SkillRef{ID: "skill", Version: 1})
		if err != nil || len(users) != 3 {
			t.Fatalf("Using = %+v, %v", users, err)
		}
		if _, err := defaults[2].Set("acme", "agent-0", 1, 1, []library.SkillRef{{ID: "skill", Version: 2}}); err != nil {
			t.Fatal(err)
		}
		if users, err := defaults[0].Using("acme", library.SkillRef{ID: "skill", Version: 1}); err != nil || len(users) != 2 {
			t.Fatalf("Using after a change = %+v, %v", users, err)
		}
	})
}
