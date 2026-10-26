package projects_test

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestStagedForkIsHiddenEvenFromOwnersAndGovernUntilAtomicPublication(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		root := team("forked", "")
		root.Members = members("ada=owner")
		root.Fork = &projects.ForkOrigin{Operation: "pf-123", Source: "original", By: "ada", At: t0}
		mustCreate(t, s, "acme", root, ada)
		child := team("forked-child", "forked")
		child.Members = members("ada=owner")
		child.Fork = &projects.ForkOrigin{Operation: "pf-123", Source: "original-child", By: "ada", At: t0}
		mustCreate(t, s, "acme", child, ada)
		for _, reader := range []projects.Reader{{Subject: "ada"}, {Subject: "other"}, {Subject: "root", Govern: true}} {
			for _, name := range []string{"forked", "forked-child"} {
				if s.Tree("acme").Resolve(name, reader).Role != projects.None || s.Tree("acme").Roles(reader)[name].Role != projects.None {
					t.Fatal("staged project exposed")
				}
			}
		}
		if _, err := s.Update("acme", "forked", 1, ada, t0, func(pr *projects.Project) error { pr.Fork.Ready = true; return nil }); !errors.Is(err, projects.ErrInvalid) {
			t.Fatalf("ordinary update published fork: %v", err)
		}
		again, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if !again.Tree("acme").PendingFork("forked-child") {
			t.Fatal("restart removed stage gate")
		}
		if err := again.CompleteFork("acme", "pf-123", []string{"forked", "forked-child"}, ada, t0); err != nil {
			t.Fatal(err)
		}
		if again.Tree("acme").PendingFork("forked-child") || again.Tree("acme").Resolve("forked-child", projects.Reader{Subject: "ada"}).Role != projects.Owner {
			t.Fatal("completed fork did not publish")
		}
	})
}
