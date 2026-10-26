package projects_test

import (
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"sync"
	"testing"
	"time"
)

func TestPersonalProjectOwnerBoundarySurvivesRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		p, err := s.EnsurePersonal("acme", "ada", t0)
		if err != nil {
			t.Fatal(err)
		}
		again, err := s.EnsurePersonal("acme", "ada", t0)
		if err != nil || again.Name != p.Name {
			t.Fatalf("idempotence: %v %v", again, err)
		}
		s, err = projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		tree := s.Tree("acme")
		if tree.Resolve(p.Name, projects.Reader{Subject: "ada"}).Role != projects.Owner {
			t.Fatal("owner lost personal access")
		}
		if tree.Resolve(p.Name, projects.Reader{Subject: "other"}).Role != projects.None {
			t.Fatal("Company ownership leaked personal project")
		}
		if tree.Resolve(p.Name, projects.Reader{Subject: "ada", Machine: true}).Role != projects.None {
			t.Fatal("machine received personal project")
		}
		if a := tree.Resolve(p.Name, projects.Reader{Subject: "admin", Govern: true}); a.Via != projects.ViaGovern {
			t.Fatalf("break glass: %+v", a)
		}
	})
}

func TestPersonalProvisioningRacesKeepOneIdentity(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		ids := make(chan string, 32)
		for range 32 {
			wg.Go(func() {
				p, err := s.EnsurePersonal("acme", "ada", time.Now())
				if err != nil {
					t.Error(err)
					return
				}
				ids <- p.Name
			})
		}
		wg.Wait()
		close(ids)
		first := ""
		for id := range ids {
			if first == "" {
				first = id
			}
			if first != id {
				t.Fatal("racing sign-ins created different identities")
			}
		}
		if len(s.List("acme")) != 1 {
			t.Fatal("more than one personal project")
		}
	})
}

func TestPersonalProjectCannotBeOpenedOrReparented(t *testing.T) {
	s := projects.NewStore()
	p, err := s.EnsurePersonal("acme", "ada", t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update("acme", p.Name, p.Version, projects.Writer{By: "root", Govern: true}, t0, func(p *projects.Project) error {
		p.Members = []projects.Entry{{Who: "*", Role: projects.Owner}}
		return nil
	}); err == nil {
		t.Fatal("personal project audience widened")
	}
	if _, err := s.Upsert("acme", projects.Project{Name: p.Name}, []string{projects.FieldMembers}, "root", t0); err == nil {
		t.Fatal("file adopted personal project")
	}
	if err := s.Delete("acme", p.Name, projects.Writer{By: "root", Govern: true}, t0, nil); err == nil {
		t.Fatal("durable personal project deleted")
	}
	if _, err := s.Create("acme", projects.Project{Name: "child", Parent: p.Name}, ada, t0); err == nil {
		t.Fatal("subproject bypassed personal boundary")
	}
}
