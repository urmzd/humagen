package sourcelinks

import (
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"reflect"
	"testing"
)

func TestRulesReplayAndAmbiguousMatchesAlwaysSuggest(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, rule := range []Rule{{ID: "launch", Provider: "github", Attribute: "label", Pattern: "launch", Project: "Launch", Mode: Auto}, {ID: "ops", Provider: "github", Attribute: "label", Pattern: "ops", Project: "Ops", Mode: Auto}} {
			if _, err := s.Put("tenant", rule); err != nil {
				t.Fatal(err)
			}
		}
		resource := Resource{Provider: "github", ID: "acme/repo", Attributes: map[string][]string{"label": {"launch", "ops"}}}
		matches, err := s.Match("tenant", resource)
		if err != nil || len(matches) != 2 {
			t.Fatalf("%+v %v", matches, err)
		}
		for _, match := range matches {
			if match.Mode != Suggest {
				t.Fatal("ambiguous resource auto-linked")
			}
		}
		replay, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.List("tenant"), replay.List("tenant")) {
			t.Fatal("rules changed across restart")
		}
		copied := s.List("tenant")
		if err := s.RestoreDisabled("tenant", "Fork", "operation1", copied); err != nil {
			t.Fatal(err)
		}
		if err := s.RestoreDisabled("tenant", "Fork", "operation1", copied); err != nil {
			t.Fatal(err)
		}
		rows := s.List("tenant")
		if len(rows) != 4 {
			t.Fatal("copy not idempotent", rows)
		}
		for _, rule := range rows {
			if rule.CopiedFrom != "" {
				if rule.Mode != Off {
					t.Fatal("fork rule enabled")
				}
				rule.Mode = Auto
				rule.CopiedFrom = ""
				if _, err := s.Put("tenant", rule); err == nil {
					t.Fatal("copied rule reenabled")
				}
			}
		}
		if got, _ := s.Match("other", resource); len(got) != 0 {
			t.Fatal("cross-tenant match")
		}
	})
}

func TestRuleSnapshotsRejectConcurrentAudienceChanges(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		old, err := s.PutIfCurrent("tenant", Rule{ID: "one", Provider: "github", Attribute: "repository", Pattern: "repo", Project: "Visible", Mode: Suggest}, nil)
		if err != nil {
			t.Fatal(err)
		}
		changed := old
		changed.Project = "Hidden"
		if _, err := s.Put("tenant", changed); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PutIfCurrent("tenant", old, &old); err != ErrConflict {
			t.Fatalf("stale update: %v", err)
		}
		if err := s.DeleteIfCurrent("tenant", old.ID, old); err != ErrConflict {
			t.Fatalf("stale delete: %v", err)
		}
		replay, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := replay.Get("tenant", old.ID)
		if got.Project != "Hidden" {
			t.Fatal("stale mutation changed retained audience")
		}
	})
}
