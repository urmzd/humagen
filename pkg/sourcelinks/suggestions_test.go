package sourcelinks

import (
	"errors"
	"fmt"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"testing"
	"time"
)

func TestVerifiedSuggestionsDeduplicateDecisionsAndReplay(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := statetest.NewFaulty(open(t))
		s, err := OpenSuggestions(st)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Now()
		resource := Resource{Provider: "github", ID: "owner/repo", Attributes: map[string][]string{"label": {"two", "one"}}}
		matches := []Match{{Project: "Field", Mode: Suggest, Rules: []string{"rule-one"}}}
		rows, err := s.Observe("tenant", "app", "", resource, matches, at)
		if err != nil || len(rows) != 1 {
			t.Fatalf("observe %+v %v", rows, err)
		}
		first := rows[0]
		resource.Attributes["label"] = []string{"one", "two"}
		again, err := s.Observe("tenant", "app", "", resource, matches, at.Add(time.Minute))
		if err != nil || again[0].Version != first.Version {
			t.Fatal("metadata ordering or replay created another change", err)
		}
		rows[0].Resource.Attributes["label"][0] = "caller mutation"
		kept, _ := s.Get("tenant", first.ID)
		if kept.Resource.Attributes["label"][0] != "one" {
			t.Fatal("caller changed durable metadata")
		}
		ignored, err := s.Decide("tenant", first.ID, "ignore", first.Version, at)
		if err != nil {
			t.Fatal(err)
		}
		matches[0].Mode = Auto
		again, err = s.Observe("tenant", "app", "", resource, matches, at)
		if err != nil || again[0].State != "ignored" {
			t.Fatal("observation reversed operator ignore", err)
		}
		if _, err := s.Decide("tenant", first.ID, "link", first.Version, at); !errors.Is(err, ErrConflict) {
			t.Fatal("stale decision accepted", err)
		}
		st.Fail(statetest.ErrInjected)
		if _, err := s.Decide("tenant", first.ID, "link", ignored.Version, at); !errors.Is(err, statetest.ErrInjected) {
			t.Fatal(err)
		}
		kept, _ = s.Get("tenant", first.ID)
		if kept.State != "ignored" {
			t.Fatal("failed write changed decision")
		}
		st.Fail(nil)
		linked, err := s.Decide("tenant", first.ID, "link", kept.Version, at)
		if err != nil || linked.Automatic {
			t.Fatal("manual link not recorded", err)
		}
		replay, err := OpenSuggestions(st)
		if err != nil {
			t.Fatal(err)
		}
		kept, _ = replay.Get("tenant", first.ID)
		if kept.State != "linked" || kept.Automatic {
			t.Fatal("decision lost on restart")
		}
		if _, err := replay.Get("other", first.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross-tenant suggestion visible")
		}
		if rows, err := replay.List("tenant", "", "", 1, func(Suggestion) bool { return false }); err != nil || len(rows) != 0 {
			t.Fatal("hidden suggestion counted")
		}
	})
}
func TestAutomaticRelationTracksLatestVerifiedAttributes(t *testing.T) {
	s := NewSuggestions()
	at := time.Now()
	resource := Resource{Provider: "github", ID: "owner/repo", Attributes: map[string][]string{"label": {"before"}}}
	rows, err := s.Observe("tenant", "app", "", resource, []Match{{Project: "Field", Mode: Auto, Rules: []string{"rule"}}}, at)
	if err != nil {
		t.Fatal(err)
	}
	resource.Attributes["label"] = []string{"after"}
	if _, err := s.Observe("tenant", "app", "", resource, nil, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	kept, _ := s.Get("tenant", rows[0].ID)
	if kept.Resource.Attributes["label"][0] != "after" || kept.Version != 2 {
		t.Fatal("old automatic relation retained outdated matching metadata")
	}
}

func TestSuggestionCapacityFailsClosedAndVisibilityPrecedesLimit(t *testing.T) {
	st := state.NewMemory()
	s, _ := OpenSuggestions(st)
	kept := state.NewTable[Suggestion](st, suggestionsNamespace)
	for i := 0; i < 1500; i++ {
		id := fmt.Sprintf("link-old-%04d", i)
		if err := kept.Put("tenant", id, Suggestion{ID: id, Project: "Hidden", State: "suggested"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := kept.Put("tenant", "link-zz-visible", Suggestion{ID: "link-zz-visible", Project: "Visible", State: "suggested"}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.List("tenant", "", "suggested", 1, func(row Suggestion) bool { return row.Project == "Visible" })
	if err != nil || len(rows) != 1 || rows[0].ID != "link-zz-visible" {
		t.Fatal("hidden records consumed visible limit", rows, err)
	}
	// The count is what caps the tenant: it is at the bound.
	if err := state.NewTable[int](st, suggestionsNamespace).Put("tenant", suggestionCount, MaxSuggestions); err != nil {
		t.Fatal(err)
	}
	resource := Resource{Provider: "github", ID: "new/repo", Attributes: map[string][]string{"repository": {"new/repo"}}}
	if _, err := s.Observe("tenant", "app", "", resource, []Match{{Project: "Visible", Mode: Suggest, Rules: []string{}}}, time.Now()); !errors.Is(err, ErrLimit) {
		t.Fatal("capacity accepted another relation", err)
	}
	if _, err := s.Attach("tenant", "docs", resource, "Visible", "", time.Now()); !errors.Is(err, ErrLimit) {
		t.Fatal("capacity accepted another attachment", err)
	}
}
