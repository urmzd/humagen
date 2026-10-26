package knowledge

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestLifecycleRetainsLastGoodCopyAndBoundsAttempts(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = s.Create("acme", "docs", "", "ada", now)
		_, err = s.Configure("acme", "docs", SourceConfig{Mode: Indexed, Provider: "drive", Origin: "folder-1", Owner: "ada", StopAfterSeconds: 86400}, now)
		if err != nil {
			t.Fatal(err)
		}
		id, err := s.BeginSync("acme", "docs", now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.CompleteSync("acme", "docs", id, []ImportDocument{{ID: "external-id", Title: "Runbook", Text: "deploy release safely"}}, "ada", now); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= MaxAttempts+3; i++ {
			at := now.Add(time.Duration(i) * time.Minute)
			id, err = s.BeginSync("acme", "docs", at)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.FailSync("acme", "docs", id, AuthExpired, at); err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.Get("acme", "docs")
		if err != nil {
			t.Fatal(err)
		}
		if got.Lifecycle.Status != NeedsAttention || got.Lifecycle.LastGoodAt == nil || !got.Lifecycle.LastGoodAt.Equal(now) || len(got.Lifecycle.Attempts) != MaxAttempts {
			t.Fatalf("lifecycle: %+v", got.Lifecycle)
		}
		hits, err := s.Search("acme", []string{"docs"}, "deploy", 6, "")
		if err != nil || len(hits) != 1 {
			t.Fatalf("last good copy: %+v %v", hits, err)
		}
		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		replay, _ := again.Get("acme", "docs")
		if !reflect.DeepEqual(got, replay) {
			t.Fatalf("replay: %+v != %+v", got, replay)
		}
		got.Lifecycle.Attempts[0].Cause = "mutated"
		isolated, _ := s.Get("acme", "docs")
		if isolated.Lifecycle.Attempts[0].Cause == "mutated" {
			t.Fatal("aliased attempts")
		}
		if _, err := s.Get("other", "docs"); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	})
}

func TestSyncCompletionCannotReplaceNewerOrInvalidSnapshot(t *testing.T) {
	s := NewStore()
	_, _ = s.Create("acme", "docs", "", "ada", now)
	_, _ = s.Add("acme", "docs", "Old", "", "trusted old text", "ada", now)
	first, _ := s.BeginSync("acme", "docs", now)
	if _, err := s.BeginSync("acme", "docs", now); !errors.Is(err, ErrExists) {
		t.Fatalf("concurrent: %v", err)
	}
	if _, err := s.CompleteSync("acme", "docs", first, []ImportDocument{{Title: "", Text: "invalid"}}, "ada", now); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.FailSync("acme", "docs", first, Unavailable, now); err != nil {
		t.Fatal(err)
	}
	second, _ := s.BeginSync("acme", "docs", now)
	if _, err := s.CompleteSync("acme", "docs", first, nil, "ada", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("stale completion: %v", err)
	}
	docs, _ := s.Documents("acme", "docs")
	if len(docs) != 1 || docs[0].Title != "Old" {
		t.Fatal(docs)
	}
	_, err := s.CompleteSync("acme", "docs", second, []ImportDocument{{ID: "one", Title: "New", Text: "new trusted text"}}, "ada", now)
	if err != nil {
		t.Fatal(err)
	}
	docs, _ = s.Documents("acme", "docs")
	if len(docs) != 1 || docs[0].Title != "New" {
		t.Fatal(docs)
	}
}

func TestRefreshCannotReattributeOrAcceptUnstableDocumentIDs(t *testing.T) {
	s := NewStore()
	_, _ = s.Create("acme", "docs", "", "ana", now)
	cfg := SourceConfig{Mode: Indexed, Provider: "drive", Origin: "folder1", Owner: "ana"}
	if _, err := s.Configure("acme", "docs", cfg, now); err != nil {
		t.Fatal(err)
	}
	id, _ := s.BeginSync("acme", "docs", now)
	if _, err := s.CompleteSync("acme", "docs", id, []ImportDocument{{ID: "stable", Title: "Good", Text: "trusted text"}}, "ana", now); err != nil {
		t.Fatal(err)
	}
	cfg.Owner = "bob"
	if _, err := s.Configure("acme", "docs", cfg, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("retained owner changed: %v", err)
	}
	src, _ := s.Get("acme", "docs")
	if src.Lifecycle.Owner != "ana" {
		t.Fatal("retained copy reattributed")
	}
	id, _ = s.BeginSync("acme", "docs", now)
	if _, err := s.CompleteSync("acme", "docs", id, []ImportDocument{{Title: "No ID", Text: "new text"}}, "ana", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unstable ID accepted: %v", err)
	}
	docs, _ := s.Documents("acme", "docs")
	if len(docs) != 1 || docs[0].ID != "stable" {
		t.Fatal("invalid snapshot changed good copy")
	}
}

func TestInterruptedIndexingRetainsCopyAndCanRetry(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = s.Create("acme", "docs", "", "ana", now)
		_, _ = s.Add("acme", "docs", "Good", "", "trusted copy", "ana", now)
		id, err := s.BeginSync("acme", "docs", now)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.IndexSync("acme", "docs", id, now); err != nil {
			t.Fatal(err)
		}
		src, _ := s.Get("acme", "docs")
		if src.Lifecycle.Status != Indexing {
			t.Fatal(src.Lifecycle.Status)
		}
		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		src, _ = again.Get("acme", "docs")
		if src.Lifecycle.Status != NeedsAttention || src.Lifecycle.Cause != Interrupted {
			t.Fatal(src.Lifecycle)
		}
		hits, err := again.Search("acme", []string{"docs"}, "trusted", 6, "")
		if err != nil || len(hits) != 1 {
			t.Fatalf("lost retained copy: %+v %v", hits, err)
		}
		if _, err := again.BeginSync("acme", "docs", now); err != nil {
			t.Fatal("interrupted attempt prevented retry", err)
		}
	})
}

func TestAuthorizedSourceSnapshotCannotRaceConfiguration(t *testing.T) {
	s := NewStore()
	_, _ = s.Create("acme", "docs", "", "ana", now)
	first := SourceConfig{Mode: Indexed, Provider: "drive", Origin: "folder1", Owner: "ana", Project: "First", StopAfterSeconds: 86400}
	if _, err := s.ConfigureIfCurrent("acme", "docs", first, now, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginSyncIfCurrent("acme", "docs", now, nil); !errors.Is(err, ErrExists) {
		t.Fatalf("legacy snapshot admitted after configure: %v", err)
	}
	second := first
	second.Origin = "folder2"
	second.Project = "Second"
	if _, err := s.ConfigureIfCurrent("acme", "docs", second, now, &first); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginSyncIfCurrent("acme", "docs", now, &first); !errors.Is(err, ErrExists) {
		t.Fatalf("old scope import admitted: %v", err)
	}
	if _, err := s.ConfigureIfCurrent("acme", "docs", first, now, &first); !errors.Is(err, ErrExists) {
		t.Fatalf("old scope overwritten: %v", err)
	}
	src, _ := s.Get("acme", "docs")
	if src.Lifecycle.SourceConfig != second || len(src.Lifecycle.Attempts) != 0 {
		t.Fatal("denied operation changed source", src.Lifecycle)
	}
}
