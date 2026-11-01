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
		if !errors.Is(err, ErrUpstreamRead) || len(hits) != 0 {
			t.Fatalf("external retained copy bypassed upstream access: %+v %v", hits, err)
		}
		documents, err := s.Documents("acme", "docs")
		if err != nil || len(documents) != 1 {
			t.Fatal("collection lost its retained data", documents, err)
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
	if !src.Lifecycle.Equal(second) || len(src.Lifecycle.Attempts) != 0 {
		t.Fatal("denied operation changed source", src.Lifecycle)
	}
}

func TestScopedReadsRejectReconfiguredEmptySource(t *testing.T) {
	s := NewStore()
	_, _ = s.Create("acme", "docs", "", "ana", now)
	if _, err := s.Configure("acme", "docs", SourceConfig{Mode: Indexed, Provider: "manual", Owner: "ana", Project: "Secret"}, now); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Add("acme", "docs", "Private", "", "private canary", "ana", now)
	if _, err := s.DocumentsForScope("acme", "docs", nil); !errors.Is(err, ErrNotFound) {
		t.Fatal("legacy reader read private docs", err)
	}
	if _, err := s.SearchForScopes("acme", []string{"docs"}, "canary", 6, "", map[string]*SourceConfig{"docs": nil}); !errors.Is(err, ErrNotFound) {
		t.Fatal("legacy reader searched private docs", err)
	}
	if hits, err := s.SearchForScopes("acme", nil, "canary", 6, "", nil); err != nil || len(hits) != 0 {
		t.Fatal("empty scope searched all tenant data", hits, err)
	}
}

func TestScopedExternalFileMetadataStaysWithheldAcrossRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		for _, mode := range []Mode{Indexed, Hybrid} {
			t.Run(string(mode), func(t *testing.T) {
				st := open(t)
				s, err := Open(st)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = s.Create("tenant", "docs", "", "owner", now)
				cfg := SourceConfig{Mode: mode, Provider: "drive", Origin: "folder", Owner: "owner", Project: "Secret"}
				configured, err := s.Configure("tenant", "docs", cfg, now)
				if err != nil {
					t.Fatal(err)
				}
				cfg = configured.Lifecycle.SourceConfig
				id, err := s.BeginSync("tenant", "docs", now)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.CompleteSync("tenant", "docs", id, []ImportDocument{{ID: "private-file", Title: "Private title", URI: "https://example.test/private", Text: "retained canary"}}, "owner", now); err != nil {
					t.Fatal(err)
				}
				if _, err := s.SetExcluded("tenant", "docs", "private-file", true, now); err != nil {
					t.Fatal(err)
				}
				if _, err := s.SaveVariant("tenant", "docs", "retained", now); err != nil {
					t.Fatal(err)
				}
				for attempt := range 2 {
					if attempt == 1 {
						s, err = Open(st)
						if err != nil {
							t.Fatal(err)
						}
					}
					before, _ := s.Get("tenant", "docs")
					if docs, err := s.DocumentsForScope("tenant", "docs", &cfg); !errors.Is(err, ErrUpstreamRead) || len(docs) != 0 {
						t.Fatal("project access exposed retained provider file listing", docs, err)
					}
					for _, docID := range []string{"private-file", "unknown-file"} {
						if doc, err := s.SetExcludedForScope("tenant", "docs", docID, false, now.Add(time.Hour), &cfg); !errors.Is(err, ErrUpstreamRead) || doc.ID != "" || doc.Title != "" || doc.URI != "" {
							t.Fatal("curation disclosed retained provider metadata", doc, err)
						}
					}
					if variant, err := s.SaveVariantForScope("tenant", "docs", "retained", now.Add(time.Hour), &cfg); !errors.Is(err, ErrUpstreamRead) || len(variant.Excluded) != 0 {
						t.Fatal("variant disclosed retained provider file IDs", variant, err)
					}
					if hits, err := s.SearchForScopes("tenant", []string{"docs"}, "canary", 6, "retained", map[string]*SourceConfig{"docs": &cfg}); !errors.Is(err, ErrUpstreamRead) || len(hits) != 0 {
						t.Fatal("saved view bypassed provider authorization", hits, err)
					}
					if _, err := s.SetExcludedForScope("other", "docs", "private-file", false, now, &cfg); !errors.Is(err, ErrNotFound) {
						t.Fatal("cross-tenant curation exposed existence", err)
					}
					stale := cfg
					stale.Project = "Other"
					if _, err := s.SaveVariantForScope("tenant", "docs", "retained", now, &stale); !errors.Is(err, ErrNotFound) {
						t.Fatal("stale scope exposed source authority", err)
					}
					after, _ := s.Get("tenant", "docs")
					if !reflect.DeepEqual(before, after) {
						t.Fatal("denied curation changed retained source", before, after)
					}
				}
			})
		}
	})
}

func TestScopedLocalFileCurationKeepsNativeAccess(t *testing.T) {
	for _, managed := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "configured"}[managed], func(t *testing.T) {
			s := NewStore()
			_, _ = s.Create("tenant", "docs", "", "owner", now)
			var cfg *SourceConfig
			if managed {
				cfg = &SourceConfig{Mode: Indexed, Provider: "manual", Owner: "owner", Project: "Secret"}
				configured, err := s.Configure("tenant", "docs", *cfg, now)
				if err != nil {
					t.Fatal(err)
				}
				*cfg = configured.Lifecycle.SourceConfig
			}
			doc, err := s.Add("tenant", "docs", "Local title", "", "local canary", "owner", now)
			if err != nil {
				t.Fatal(err)
			}
			changed, err := s.SetExcludedForScope("tenant", "docs", doc.ID, true, now, cfg)
			if err != nil || changed.Title != "Local title" || !changed.Excluded {
				t.Fatal("local document curation lost native access", changed, err)
			}
			variant, err := s.SaveVariantForScope("tenant", "docs", "local-view", now, cfg)
			if err != nil || !reflect.DeepEqual(variant.Excluded, []string{doc.ID}) {
				t.Fatal("local saved view lost its curated selection", variant, err)
			}
			docs, err := s.DocumentsForScope("tenant", "docs", cfg)
			if err != nil || len(docs) != 1 || docs[0].ID != doc.ID {
				t.Fatal("local file listing lost native access", docs, err)
			}
		})
	}
}

func TestContentVersionChangesOnlyForRetainedContentAcrossRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = s.Create("acme", "docs", "", "ana", now)
		_, _ = s.Configure("acme", "docs", SourceConfig{Mode: Indexed, Provider: "drive", Origin: "folder1", Owner: "ana"}, now)
		docs := []ImportDocument{{ID: "a", Title: "A", Text: "first content"}, {ID: "b", Title: "B", Text: "second content"}}
		refresh := func(s *Store, docs []ImportDocument, at time.Time) uint64 {
			t.Helper()
			id, err := s.BeginSync("acme", "docs", at)
			if err != nil {
				t.Fatal(err)
			}
			src, err := s.CompleteSync("acme", "docs", id, docs, "ana", at)
			if err != nil {
				t.Fatal(err)
			}
			return src.ContentVersion
		}
		if v := refresh(s, docs, now); v != 1 {
			t.Fatal(v)
		}
		docs[0], docs[1] = docs[1], docs[0]
		if v := refresh(s, docs, now.Add(time.Hour)); v != 1 {
			t.Fatal("unchanged reordered refresh advanced", v)
		}
		docs[0].Text = "changed content"
		if v := refresh(s, docs, now.Add(2*time.Hour)); v != 2 {
			t.Fatal(v)
		}
		replay, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if v := refresh(replay, docs, now.Add(3*time.Hour)); v != 2 {
			t.Fatal("unchanged replay refresh advanced", v)
		}
		pending, err := replay.BeginSync("acme", "docs", now.Add(4*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if err := replay.IndexSync("acme", "docs", pending, now.Add(4*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := replay.FailSync("acme", "docs", pending, Unavailable, now.Add(4*time.Hour)); err != nil {
			t.Fatal(err)
		}
		unchanged, _ := replay.Get("acme", "docs")
		if unchanged.ContentVersion != 2 {
			t.Fatal("failed refresh advanced content version", unchanged.ContentVersion)
		}

		if _, err := replay.SetExcluded("acme", "docs", "a", true, now); err != nil {
			t.Fatal(err)
		}
		src, _ := replay.Get("acme", "docs")
		if src.ContentVersion != 3 {
			t.Fatal(src.ContentVersion)
		}
		_, _ = replay.SetExcluded("acme", "docs", "a", true, now)
		src, _ = replay.Get("acme", "docs")
		if src.ContentVersion != 3 {
			t.Fatal("unchanged curation advanced cursor")
		}
	})
}

func TestContentCursorSurvivesDeleteRecreateAndRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = s.Create("acme", "docs", "", "ana", now)
		_, _ = s.Add("acme", "docs", "Old", "", "old text", "ana", now)
		before, _ := s.Get("acme", "docs")
		if err := s.Delete("acme", "docs"); err != nil {
			t.Fatal(err)
		}
		tomb, _ := s.ContentCursor("acme", "docs")
		if tomb <= before.ContentVersion {
			t.Fatal("deletion reset cursor")
		}
		replay, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if cursor, _ := replay.ContentCursor("acme", "docs"); cursor != tomb {
			t.Fatal("deleted cursor lost across restart")
		}
		recreated, err := replay.Create("acme", "docs", "", "ana", now)
		if err != nil {
			t.Fatal(err)
		}
		if recreated.ContentVersion < tomb {
			t.Fatal("recreate reused an older cursor")
		}
		_, _ = replay.Add("acme", "docs", "New", "", "new text", "ana", now)
		after, _ := replay.Get("acme", "docs")
		if after.ContentVersion <= tomb {
			t.Fatal("new content reused historical revision")
		}
	})
}

func TestSnapshotDecisionDoesNotInventASuccessfulRefresh(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s, err := Open(open(t))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = s.Create("tenant", "docs", "", "owner", now)
		cfg := SourceConfig{Mode: Indexed, Provider: "drive", Origin: "folder", Owner: "owner"}
		_, err = s.Configure("tenant", "docs", cfg, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := s.BeginSync("tenant", "docs", now)
		_, err = s.CompleteSync("tenant", "docs", id, []ImportDocument{{ID: "doc", Title: "Copy", Text: "retained contents"}}, "owner", now)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Snapshot = true
		_, err = s.Configure("tenant", "docs", cfg, now)
		if err != nil {
			t.Fatal(err)
		}
		id, err = s.BeginSync("tenant", "docs", now)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.FailSync("tenant", "docs", id, AuthExpired, now)
		if err != nil {
			t.Fatal(err)
		}
		shown, err := s.Configure("tenant", "docs", cfg, now)
		if err != nil {
			t.Fatal(err)
		}
		if shown.Lifecycle.Status != NeedsAttention {
			t.Fatal("configuration save erased failed replacement")
		}
		shown, err = s.KeepSnapshot("tenant", "docs", cfg, now, &shown.Lifecycle.SourceConfig)
		if err != nil {
			t.Fatal(err)
		}
		if shown.Lifecycle.Status != Available || shown.Lifecycle.Attempts[len(shown.Lifecycle.Attempts)-1].Status != "failed" {
			t.Fatal("snapshot decision fabricated successful refresh", shown.Lifecycle)
		}
		hits, err := s.Search("tenant", []string{"docs"}, "retained", 1, "")
		if !errors.Is(err, ErrUpstreamRead) || len(hits) != 0 {
			t.Fatal("snapshot bypassed current upstream access", hits, err)
		}
		documents, err := s.Documents("tenant", "docs")
		if err != nil || len(documents) != 1 || shown.Lifecycle.LastGoodAt == nil {
			t.Fatal("snapshot lost retained data or collection provenance", documents, err)
		}
	})
}
