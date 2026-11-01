package knowledge

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database see each other's sources at once, a search
// one process read before another's change reflects it, and documents added
// to one source through both at the same time are each kept.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := Open(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := Open(b)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := one.Create("acme", "handbook", "", "ada", now); err != nil {
			t.Fatal(err)
		}
		if _, err := two.Create("acme", "handbook", "", "ada", now); err == nil {
			t.Fatal("the other process created the same source again")
		}
		if _, err := one.Add("acme", "handbook", "Releases", "", "Merging to main runs the release workflow.", "ada", now); err != nil {
			t.Fatal(err)
		}
		if hits, err := two.Search("acme", nil, "release", 5, ""); err != nil || len(hits) != 1 {
			t.Fatalf("the other process finds %+v, %v", hits, err)
		}
		docs, _ := two.Documents("acme", "handbook")
		if _, err := one.SetExcluded("acme", "handbook", docs[0].ID, true, now); err != nil {
			t.Fatal(err)
		}
		if hits, _ := two.Search("acme", nil, "release", 5, ""); len(hits) != 0 {
			t.Fatalf("the other process searches an excluded document: %+v", hits)
		}

		const writers = 16
		var wg sync.WaitGroup
		for i := range writers {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				if _, err := s.Add("acme", "handbook", fmt.Sprintf("Doc %02d", i), "", fmt.Sprintf("word%02d appears here", i), "ada", now); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		for _, s := range []*Store{one, two} {
			src, err := s.Get("acme", "handbook")
			if err != nil || src.Documents != writers+1 || src.Passages != writers+1 || src.Excluded != 1 {
				t.Fatalf("source = %+v, %v", src, err)
			}
			// One revision for the first add, one for the exclusion, one per
			// concurrent add.
			if src.ContentVersion != writers+2 {
				t.Fatalf("content version %d, want %d", src.ContentVersion, writers+2)
			}
			docs, _ := s.Documents("acme", "handbook")
			titles := map[string]bool{}
			for _, d := range docs {
				titles[d.Title] = true
			}
			if len(titles) != writers+1 {
				t.Fatalf("%d distinct documents", len(titles))
			}
			for i := range writers {
				if hits, err := s.Search("acme", []string{"handbook"}, fmt.Sprintf("word%02d", i), 5, ""); err != nil || len(hits) != 1 {
					t.Fatalf("word%02d: %+v, %v", i, hits, err)
				}
			}
		}

		// Variants saved through both at once are each kept.
		for i := range 6 {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				if _, err := s.SaveVariant("acme", "handbook", fmt.Sprintf("v%d", i), now); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		src, _ := one.Get("acme", "handbook")
		if len(src.Variants) != 6 {
			t.Fatalf("%d variants, want 6", len(src.Variants))
		}
		if got, err := two.List("globex"); err != nil || len(got) != 0 {
			t.Fatalf("another tenant lists %+v, %v", got, err)
		}
	})
}

// Sources created through both processes at once stop at MaxSources.
func TestTwoProcessesKeepTheSourceBound(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := Open(a)
		two, _ := Open(b)
		for i := range MaxSources - 4 {
			if _, err := one.Create("acme", fmt.Sprintf("s%03d", i), "", "ada", now); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		for i := MaxSources - 4; i < MaxSources+8; i++ {
			s := []*Store{one, two}[i%2]
			wg.Go(func() { _, _ = s.Create("acme", fmt.Sprintf("s%03d", i), "", "ada", now) })
		}
		wg.Wait()
		got, err := two.List("acme")
		if err != nil || len(got) != MaxSources {
			t.Fatalf("%d sources, want %d (%v)", len(got), MaxSources, err)
		}
	})
}

// A refresh attempt one process began is left alone by another process that
// opens the store, until it has outlived AttemptLease.
func TestTwoProcessesLeaveALiveAttempt(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := Open(a)
		if _, err := one.Create("acme", "docs", "", "ada", now); err != nil {
			t.Fatal(err)
		}
		id, err := one.BeginSync("acme", "docs", time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		two, err := Open(b)
		if err != nil {
			t.Fatal(err)
		}
		src, _ := two.Get("acme", "docs")
		if active(src.Lifecycle) != id {
			t.Fatalf("a live attempt was interrupted: %+v", src.Lifecycle)
		}
		if _, err := two.BeginSync("acme", "docs", time.Now().UTC()); err == nil {
			t.Fatal("a second attempt began beside a live one")
		}
		// Past the lease, the next attempt records the first as interrupted.
		if _, err := two.BeginSync("acme", "docs", time.Now().UTC().Add(AttemptLease)); err != nil {
			t.Fatal(err)
		}
		src, _ = one.Get("acme", "docs")
		if n := len(src.Lifecycle.Attempts); n != 2 || src.Lifecycle.Attempts[0].Cause != Interrupted {
			t.Fatalf("attempts = %+v", src.Lifecycle.Attempts)
		}
	})
}

// Citations recorded for one run through both processes at once are each
// kept, numbered without a repeat.
func TestTwoProcessesRecordEvidence(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := OpenEvidenceStore(a)
		two, _ := OpenEvidenceStore(b)
		entry := func(doc string) CitationEvidence {
			return CitationEvidence{Source: "docs", Provider: "manual", DocumentID: doc, Title: doc, Text: "text " + doc, Mode: Indexed}
		}
		first, err := one.RecordQuery("acme", "run", "q0", []CitationEvidence{entry("d0")})
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := two.Get("acme", "run", first[0].Label, ""); !ok || got.DocumentID != "d0" {
			t.Fatalf("the other process reads %+v, %v", got, ok)
		}
		const writers = 12
		var wg sync.WaitGroup
		for i := 1; i <= writers; i++ {
			s := []*EvidenceStore{one, two}[i%2]
			wg.Go(func() {
				if _, err := s.RecordQuery("acme", "run", fmt.Sprintf("q%d", i), []CitationEvidence{entry(fmt.Sprintf("d%d", i))}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		proofs, err := two.ForRun("acme", "run")
		if err != nil || len(proofs) != writers+1 {
			t.Fatalf("%d proofs, want %d (%v)", len(proofs), writers+1, err)
		}
		var labels []string
		for _, p := range proofs {
			labels = append(labels, p.Label)
		}
		slices.Sort(labels)
		if len(slices.Compact(labels)) != writers+1 {
			t.Fatalf("labels repeat: %v", labels)
		}
		var u evidenceUsage
		if _, err := lookupInto(a, evidenceNamespace, "acme", evidenceUsageKey, &u); err != nil || u.Runs != 1 {
			t.Fatalf("usage = %+v, %v", u, err)
		}
		refs, err := one.RunReferences("acme", "docs", 10, func(string, CitationEvidence) bool { return true })
		if err != nil || !slices.Equal(refs, []string{"run"}) {
			t.Fatalf("references = %v, %v", refs, err)
		}
	})
}
