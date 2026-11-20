package corpora

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

var at = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func hr() Corpus {
	return Corpus{
		Name:        "hr-policies",
		Title:       " HR policies ",
		Description: "  Leave, pay and conduct policies  ",
		Owner:       "00u-ana",
		Readers:     []Reader{{Kind: Project, Name: "People"}, {Kind: Person, Name: "00u-bo"}, {Kind: Person, Name: "00u-bo"}},
		Members:     []Member{{Kind: Source, Name: "handbook"}, {Kind: Catalog, Name: "warehouse"}, {Kind: Source, Name: "handbook"}},
		Agents:      []string{"hr-helper", "hr-helper"},
	}
}

func TestCorpusLifecycle(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s, err := Open(open(t))
		if err != nil {
			t.Fatal(err)
		}
		made, err := s.Create("acme", hr(), at)
		if err != nil {
			t.Fatal(err)
		}
		if made.Description != "Leave, pay and conduct policies" || made.Title != "HR policies" || made.Links != Isolated || made.Version == 0 || !made.CreatedAt.Equal(at) {
			t.Fatalf("created %+v", made)
		}
		if len(made.Members) != 2 || len(made.Readers) != 2 || !slices.Equal(made.Agents, []string{"hr-helper"}) || !made.Grants("hr-helper") {
			t.Fatalf("duplicates kept: %+v", made)
		}
		if _, err := s.Create("acme", hr(), at); !errors.Is(err, ErrExists) {
			t.Fatalf("a second corpus of one name: %v", err)
		}
		if _, err := s.Get("globex", "hr-policies"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("another tenant reads it: %v", err)
		}

		later := at.Add(time.Hour)
		changed, err := s.Update("acme", "hr-policies", made.Version, later, func(c *Corpus) error {
			c.Name = "renamed"
			c.Links = Shared
			c.Members = append(c.Members, Member{Kind: Source, Name: "benefits"})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if changed.Name != "hr-policies" || changed.Links != Shared || len(changed.Members) != 3 || !changed.UpdatedAt.Equal(later) || !changed.CreatedAt.Equal(at) {
			t.Fatalf("updated %+v", changed)
		}
		if _, err := s.Update("acme", "hr-policies", made.Version, later, func(*Corpus) error { return nil }); !errors.Is(err, ErrConflict) {
			t.Fatalf("an update at an old version: %v", err)
		}
		if err := s.Delete("acme", "hr-policies", made.Version); !errors.Is(err, ErrConflict) {
			t.Fatalf("a delete at an old version: %v", err)
		}
		holding, err := s.Containing("acme", Member{Kind: Source, Name: "benefits"})
		if err != nil || !slices.Equal(holding, []string{"hr-policies"}) {
			t.Fatalf("containing %v, %v", holding, err)
		}
		if err := s.Delete("acme", "hr-policies", changed.Version); err != nil {
			t.Fatal(err)
		}
		if got, err := s.List("acme"); err != nil || len(got) != 0 {
			t.Fatalf("after delete %+v, %v", got, err)
		}
		var tenants []string
		if err := s.EachTenant(func(t string) error { tenants = append(tenants, t); return nil }); err != nil || !slices.Equal(tenants, []string{"acme"}) {
			t.Fatalf("tenants %v, %v", tenants, err)
		}
	})
}

func TestCorpusIsChecked(t *testing.T) {
	s := New()
	for name, c := range map[string]Corpus{
		"bad name":         {Name: "HR", Owner: "o"},
		"no owner":         {Name: "hr"},
		"long description": {Name: "hr", Owner: "o", Description: string(make([]byte, MaxDescription+1))},
		"bad links":        {Name: "hr", Owner: "o", Links: "open"},
		"two-line title":   {Name: "hr", Owner: "o", Title: "HR\npolicies"},
		"bad member kind":  {Name: "hr", Owner: "o", Members: []Member{{Kind: "drive", Name: "x"}}},
		"bad member name":  {Name: "hr", Owner: "o", Members: []Member{{Kind: Source, Name: "X Y"}}},
		"unnamed person":   {Name: "hr", Owner: "o", Readers: []Reader{{Kind: Person}}},
		"named workspace":  {Name: "hr", Owner: "o", Readers: []Reader{{Kind: Workspace, Name: "acme"}}},
		"unknown reader":   {Name: "hr", Owner: "o", Readers: []Reader{{Kind: "group", Name: "x"}}},
		"too many members": {Name: "hr", Owner: "o", Members: members(MaxMembers + 1)},
		"too many readers": {Name: "hr", Owner: "o", Readers: readers(MaxReaders + 1)},
		"unnamed agent":    {Name: "hr", Owner: "o", Agents: []string{""}},
		"spaced agent":     {Name: "hr", Owner: "o", Agents: []string{"hr helper"}},
	} {
		if _, err := s.Create("acme", c, at); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := s.Update("acme", "missing", 0, at, func(*Corpus) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Errorf("updating a missing corpus: %v", err)
	}
	if err := s.Delete("acme", "missing", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting a missing corpus: %v", err)
	}
	if _, err := s.Get("acme", "h"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the head reads as a corpus: %v", err)
	}
}

func members(n int) []Member {
	out := make([]Member, n)
	for i := range out {
		out[i] = Member{Kind: Source, Name: fmt.Sprintf("s-%d", i)}
	}
	return out
}

func readers(n int) []Reader {
	out := make([]Reader, n)
	for i := range out {
		out[i] = Reader{Kind: Person, Name: fmt.Sprintf("p-%d", i)}
	}
	return out
}

// Two processes on one database see each other's corpora at once, and
// corpora created through both at the same time stop at MaxCorpora.
func TestTwoProcessesAgreeOnCorpora(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := Open(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := Open(b)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := one.Create("acme", hr(), at); err != nil {
			t.Fatal(err)
		}
		if got, err := two.Get("acme", "hr-policies"); err != nil || got.Owner != "00u-ana" {
			t.Fatalf("the other process reads %+v, %v", got, err)
		}
		for i := 1; i < MaxCorpora-10; i++ {
			if _, err := one.Create("acme", Corpus{Name: fmt.Sprintf("c-%03d", i), Owner: "o"}, at); err != nil {
				t.Fatal(err)
			}
		}
		var kept, refused atomic.Int64
		var wg sync.WaitGroup
		for i := MaxCorpora - 10; i < MaxCorpora+20; i++ {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				_, err := s.Create("acme", Corpus{Name: fmt.Sprintf("c-%03d", i), Owner: "o"}, at)
				switch {
				case err == nil:
					kept.Add(1)
				case errors.Is(err, ErrLimit):
					refused.Add(1)
				default:
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if kept.Load() != 10 || refused.Load() != 20 {
			t.Fatalf("kept %d and refused %d, want 10 and 20", kept.Load(), refused.Load())
		}
	})
}

// A corpus kept before labels existed reads as internal.
func TestACorpusWithNoLabelReadsAsInternal(t *testing.T) {
	st := state.NewMemory()
	s, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("acme", Corpus{Name: "old", Owner: "o"}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.kept.Update("acme", key("old"), func(r record, _ bool) (record, error) {
		r.Corpus.Label = ""
		return r, nil
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get("acme", "old"); err != nil || got.Label != Internal {
		t.Fatalf("an unlabelled corpus reads %q, %v", got.Label, err)
	}
}
