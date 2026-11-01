package knowledge

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Create("acme", "handbook", "How we work", "ada", now); err != nil {
			t.Fatal(err)
		}
		long := strings.Repeat("Deploys need a green build and a second reviewer. ", 40)
		rel, err := s.Add("acme", "handbook", "Releases", "", "Merging to main runs the release workflow.\n\n"+long, "ada", now)
		if err != nil {
			t.Fatal(err)
		}
		call, _ := s.Add("acme", "handbook", "On call", "https://wiki/oncall", "The on-call engineer acknowledges a page within five minutes.", "ada", now)
		gone, _ := s.Add("acme", "handbook", "Old", "", "Nobody reads this release note.", "ada", now)
		if _, err := s.SetExcluded("acme", "handbook", call.ID, true, now); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SaveVariant("acme", "handbook", "no-oncall", now); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SaveVariant("acme", "handbook", "dropped", now); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteVariant("acme", "handbook", "dropped"); err != nil {
			t.Fatal(err)
		}
		if err := s.Remove("acme", "handbook", gone.ID, now); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Create("acme", "scratch", "", "ada", now); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Add("acme", "scratch", "x", "", "x marks the spot", "ada", now); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete("acme", "scratch"); err != nil {
			t.Fatal(err)
		}

		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(sources(t, again, "acme"), sources(t, s, "acme")) || len(sources(t, again, "acme")) != 1 {
			t.Fatalf("sources after restart = %+v, want %+v", sources(t, again, "acme"), sources(t, s, "acme"))
		}
		gotDocs, _ := again.Documents("acme", "handbook")
		wantDocs, _ := s.Documents("acme", "handbook")
		if !reflect.DeepEqual(gotDocs, wantDocs) || len(gotDocs) != 2 || gotDocs[1].ID != rel.ID {
			t.Fatalf("documents after restart = %+v, want %+v", gotDocs, wantDocs)
		}
		for _, q := range []struct{ query, variant string }{{"release workflow", ""}, {"second reviewer", ""}, {"page minutes", ""}, {"page minutes", "no-oncall"}} {
			var names []string
			if q.variant != "" {
				names = []string{"handbook"}
			}
			got, err := again.Search("acme", names, q.query, 10, q.variant)
			if err != nil {
				t.Fatal(err)
			}
			want, _ := s.Search("acme", names, q.query, 10, q.variant)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("search %q after restart = %+v, want %+v", q.query, got, want)
			}
		}
		// The next document after a restart goes after the others.
		late, _ := again.Add("acme", "handbook", "Late", "", "Added after the restart.", "ada", now)
		third, _ := Open(st)
		if docs, _ := third.Documents("acme", "handbook"); len(docs) != 3 || docs[0].ID != late.ID {
			t.Fatalf("documents after a second restart = %+v", docs)
		}
		if len(sources(t, again, "globex")) != 0 {
			t.Fatal("another tenant sees the sources")
		}
	})
}

func TestFailedWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, _ := Open(f)
	if _, err := s.Create("acme", "handbook", "", "ada", now); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Add("acme", "handbook", "Releases", "", "Merging to main runs the release workflow.", "ada", now)
	f.Fail(statetest.ErrInjected)
	if _, err := s.Add("acme", "handbook", "More", "", "release more", "ada", now); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Add = %v", err)
	}
	if _, err := s.SetExcluded("acme", "handbook", d.ID, true, now); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("SetExcluded = %v", err)
	}
	if err := s.Remove("acme", "handbook", d.ID, now); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Remove = %v", err)
	}
	if err := s.Delete("acme", "handbook"); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Delete = %v", err)
	}
	src := sources(t, s, "acme")
	if len(src) != 1 || src[0].Documents != 1 || src[0].Excluded != 0 {
		t.Fatalf("after failed writes = %+v", src)
	}
	if hits, _ := s.Search("acme", nil, "release", 10, ""); len(hits) != 1 {
		t.Fatalf("search after failed writes = %+v", hits)
	}
}
