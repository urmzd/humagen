package sourcelinks

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database see each other's rules at once, and rules
// added through both at the same time stop at MaxRules.
func TestTwoProcessesAgreeOnRules(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := Open(a)
		two, _ := Open(b)
		rule := Rule{ID: "launch", Provider: "github", Attribute: "label", Pattern: "launch", Project: "Launch", Mode: Auto}
		if _, err := one.Put("tenant", rule); err != nil {
			t.Fatal(err)
		}
		if got, err := two.Get("tenant", "launch"); err != nil || got != rule {
			t.Fatalf("the other process reads %+v, %v", got, err)
		}
		if err := two.Delete("tenant", "launch"); err != nil {
			t.Fatal(err)
		}
		if _, err := one.Get("tenant", "launch"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a deleted rule reads %v", err)
		}

		// Fill to ten under the bound, then add thirty at once.
		for i := range MaxRules - 10 {
			if _, err := one.Put("tenant", Rule{ID: fmt.Sprintf("r-%03d", i), Provider: "github", Attribute: "label", Pattern: "x", Project: "P"}); err != nil {
				t.Fatal(err)
			}
		}
		var kept, refused atomic.Int64
		var wg sync.WaitGroup
		for i := MaxRules - 10; i < MaxRules+20; i++ {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				_, err := s.Put("tenant", Rule{ID: fmt.Sprintf("r-%03d", i), Provider: "github", Attribute: "label", Pattern: "x", Project: "P"})
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
		for _, s := range []*Store{one, two} {
			if rules := list(t, s, "tenant"); len(rules) != MaxRules {
				t.Fatalf("%d rules, want %d", len(rules), MaxRules)
			}
		}
	})
}

// A suggestion observed through one process is read through the other, and
// changes to one suggestion through both at once are each kept.
func TestTwoProcessesAgreeOnSuggestions(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := OpenSuggestions(a)
		two, _ := OpenSuggestions(b)
		at := time.Unix(0, 0).UTC()
		resource := func(label string) Resource {
			return Resource{Provider: "github", ID: "owner/repo", Attributes: map[string][]string{"label": {label}}}
		}
		match := []Match{{Project: "Field", Mode: Suggest, Rules: []string{"rule"}}}
		rows, err := one.Observe("tenant", "app", "", resource("first"), match, at)
		if err != nil {
			t.Fatal(err)
		}
		id := rows[0].ID
		if got, err := two.Get("tenant", id); err != nil || got.Version != 1 {
			t.Fatalf("the other process reads %+v, %v", got, err)
		}

		const writers = 16
		var wg sync.WaitGroup
		for i := range writers {
			s := []*Suggestions{one, two}[i%2]
			wg.Go(func() {
				if _, err := s.Observe("tenant", "app", "", resource(fmt.Sprintf("l-%02d", i)), match, at); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		got, err := one.Get("tenant", id)
		if err != nil || got.Version != writers+1 {
			t.Fatalf("version %d after %d changes, want %d (%v)", got.Version, writers, writers+1, err)
		}

		// One decision wins at a version; the other is told it moved.
		var won, lost atomic.Int64
		for i := range 2 {
			s := []*Suggestions{one, two}[i]
			wg.Go(func() {
				_, err := s.Decide("tenant", id, "link", got.Version, at)
				switch {
				case err == nil:
					won.Add(1)
				case errors.Is(err, ErrConflict):
					lost.Add(1)
				default:
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if won.Load() != 1 || lost.Load() != 1 {
			t.Fatalf("%d decisions won and %d lost", won.Load(), lost.Load())
		}

		// Attachments through both are counted once each.
		for i := range 20 {
			s := []*Suggestions{one, two}[i%2]
			wg.Go(func() {
				r := Resource{Provider: "drive", ID: fmt.Sprintf("f-%02d", i), Attributes: map[string][]string{"folder": {"x"}}}
				if _, err := s.Attach("tenant", "docs", r, "Field", "", at); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		var n int
		if _, err := lookup(a, suggestionsNamespace, "tenant", suggestionCount, &n); err != nil || n != 21 {
			t.Fatalf("count %d, want 21 (%v)", n, err)
		}
		if err := two.Detach("tenant", "docs", "Field", "", at); err != nil {
			t.Fatal(err)
		}
		linked, err := one.List("tenant", "", "linked", 200, nil)
		if err != nil || len(linked) != 1 {
			t.Fatalf("linked after detach = %d, %v", len(linked), err)
		}
	})
}

// Suggestions kept before the count and index keys are indexed once, though
// two processes write to the tenant at the same time.
func TestTwoProcessesIndexOldSuggestionsOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		old := state.NewTable[Suggestion](a, suggestionsNamespace)
		at := time.Unix(0, 0).UTC()
		for i := range 30 {
			r := Resource{Provider: "drive", ID: fmt.Sprintf("old-%02d", i), Attributes: map[string][]string{"folder": {"x"}}}
			row := Suggestion{Resource: r, Project: "Field", Knowledge: "docs", State: "linked", Rules: []string{}, Version: 1, CreatedAt: at, UpdatedAt: at}
			row.ID = suggestionID("", r, Match{Project: "Field"}, "docs")
			if err := old.Put("tenant", row.ID, row); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		for i := range 8 {
			wg.Go(func() {
				s, _ := OpenSuggestions([]state.Store{a, b}[i%2])
				r := Resource{Provider: "drive", ID: fmt.Sprintf("new-%02d", i), Attributes: map[string][]string{"folder": {"x"}}}
				if _, err := s.Attach("tenant", "other", r, "Field", "", at); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		s, _ := OpenSuggestions(b)
		var n int
		if _, err := lookup(a, suggestionsNamespace, "tenant", suggestionCount, &n); err != nil || n != 38 {
			t.Fatalf("count %d, want 38 (%v)", n, err)
		}
		// The old attachments are found through their index and detached.
		if err := s.Detach("tenant", "docs", "Field", "", at); err != nil {
			t.Fatal(err)
		}
		linked, err := s.List("tenant", "", "linked", 200, nil)
		if err != nil || len(linked) != 8 {
			t.Fatalf("%d linked after detaching the old ones, want 8 (%v)", len(linked), err)
		}
	})
}
