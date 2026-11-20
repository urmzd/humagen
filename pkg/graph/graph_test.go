package graph

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

var now = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func doc(key, member string) Node {
	n := NewNode(Document, key, key)
	n.Member = member
	return n
}

func TestSlicesAndDecisionsAreKept(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s, err := Open(open(t))
		if err != nil {
			t.Fatal(err)
		}
		ana, policy := NewNode(Person, "person:ana", "Ana"), doc("document:source/hr/1", "source/hr")
		sl := Slice{Corpus: "hr", Nodes: []Node{ana, policy}, Edges: []Edge{{From: ana.ID, To: policy.ID, Type: Authored}}, BuiltAt: now}
		if err := s.PutSlice("acme", sl); err != nil {
			t.Fatal(err)
		}
		got, err := s.Slice("acme", "hr")
		if err != nil || len(got.Nodes) != 2 || len(got.Edges) != 1 {
			t.Fatalf("slice %+v, %v", got, err)
		}
		if _, err := s.Slice("globex", "hr"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("another tenant reads it: %v", err)
		}
		if err := s.Decide("acme", Decision{Node: policy.ID, Owner: "bo", State: Confirmed, By: "ana", At: now}); err != nil {
			t.Fatal(err)
		}
		ds, err := s.Decisions("acme")
		if err != nil || ds[policy.ID].Owner != "bo" {
			t.Fatalf("decisions %+v, %v", ds, err)
		}
		if err := s.DeleteSlice("acme", "hr"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Slice("acme", "hr"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a deleted slice reads %v", err)
		}
	})
}

func TestSlicesAreChecked(t *testing.T) {
	s := New()
	a, b := doc("document:a", "source/a"), doc("document:b", "source/a")
	forged := a
	forged.ID = "not-its-key"
	for name, sl := range map[string]Slice{
		"no corpus":         {Nodes: []Node{a}},
		"a forged id":       {Corpus: "c", Nodes: []Node{forged}},
		"a node twice":      {Corpus: "c", Nodes: []Node{a, a}},
		"an edge elsewhere": {Corpus: "c", Nodes: []Node{a}, Edges: []Edge{{From: a.ID, To: b.ID, Type: PartOf}}},
	} {
		if err := s.PutSlice("acme", sl); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := s.Decide("acme", Decision{Node: a.ID, State: Confirmed, By: "ana"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a confirmation with no owner: %v", err)
	}
}

// Slices merge on shared nodes; keeping only what a reader may see drops a
// hidden document and the person known only through it.
func TestMergeAndKeep(t *testing.T) {
	ana, bo := NewNode(Person, "person:ana", "Ana"), NewNode(Person, "person:bo", "Bo")
	open, hidden := doc("document:open", "source/open"), doc("document:hidden", "source/hidden")
	g := Merge([]Slice{
		{Corpus: "one", Nodes: []Node{ana, open}, Edges: []Edge{{From: ana.ID, To: open.ID, Type: Authored}}},
		{Corpus: "two", Nodes: []Node{ana, bo, open, hidden}, Edges: []Edge{{From: ana.ID, To: open.ID, Type: Authored}, {From: bo.ID, To: hidden.ID, Type: Authored}}},
	})
	if len(g.Nodes) != 4 || len(g.Edges) != 2 {
		t.Fatalf("merged %+v", g)
	}
	for _, n := range g.Nodes {
		if n.ID == ana.ID && !slices.Equal(n.Corpora, []string{"one", "two"}) {
			t.Fatalf("ana came from %v", n.Corpora)
		}
	}
	kept := g.Keep(func(n Node) bool { return n.Member != "source/hidden" })
	names := []string{}
	for _, n := range kept.Nodes {
		names = append(names, n.Name)
	}
	if !slices.Equal(names, []string{"Ana", "document:open"}) || len(kept.Edges) != 1 {
		t.Fatalf("kept %v and %d edges", names, len(kept.Edges))
	}
}

func TestStaleness(t *testing.T) {
	year := 365 * 24 * time.Hour
	fresh, old := now.Add(-24*time.Hour), now.Add(-2*year)
	n := doc("document:a", "source/a")
	n.ChangedAt = &fresh
	if got := StalenessOf(n, true, false, year, now); got.Stale || len(got.Reasons) != 0 || got.Score > 0.01 {
		t.Fatalf("a fresh owned document = %+v", got)
	}
	n.ChangedAt = &old
	if got := StalenessOf(n, true, false, year, now); !got.Stale || got.Score != 0.75 || !slices.Equal(got.Reasons, []string{ReasonOld}) {
		t.Fatalf("a document two half-lives old = %+v", got)
	}
	n.ChangedAt, n.OutOfDate = &fresh, true
	if got := StalenessOf(n, false, true, year, now); !got.Stale || !slices.Equal(got.Reasons, []string{ReasonSuperseded, ReasonUnowned, ReasonOutOfDate}) {
		t.Fatalf("an unowned, superseded, out of date document = %+v", got)
	}
}
