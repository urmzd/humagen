package knowledge

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestEvidenceWritesBeforeUseAndSurvivesRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := statetest.NewFaulty(open(t))
		s, err := OpenEvidenceStore(st)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Now()
		entry := CitationEvidence{Source: "docs", DocumentID: "doc", Title: "Runbook", Text: "actual retained text", Mode: Indexed, CopyAt: &at}
		st.Fail(statetest.ErrInjected)
		if _, err := s.RecordQuery("tenant", "run", "query", []CitationEvidence{entry}); !errors.Is(err, statetest.ErrInjected) {
			t.Fatal(err)
		}
		if _, ok := s.Get("tenant", "run", "[1]", ""); ok {
			t.Fatal("failed persistence exposed evidence")
		}
		st.Fail(nil)
		first, err := s.RecordQuery("tenant", "run", "query", []CitationEvidence{entry})
		if err != nil || first[0].Label != "[1]" {
			t.Fatal(first, err)
		}
		*first[0].CopyAt = at.Add(time.Hour)
		kept, _ := s.Get("tenant", "run", "[1]", "doc")
		if !kept.CopyAt.Equal(at) {
			t.Fatal("caller mutated retained timestamp")
		}
		replay, err := OpenEvidenceStore(st)
		if err != nil {
			t.Fatal(err)
		}
		again, err := replay.RecordQuery("tenant", "run", "query", []CitationEvidence{entry})
		if err != nil || again[0].Label != "[1]" {
			t.Fatal("idempotent replay lost label", again, err)
		}
		changed := entry
		changed.Text = "different text"
		if _, err := replay.RecordQuery("tenant", "run", "query", []CitationEvidence{changed}); !errors.Is(err, ErrExists) {
			t.Fatal("conflicting query evidence replaced", err)
		}
		next, err := replay.RecordQuery("tenant", "run", "second", []CitationEvidence{entry})
		if err != nil || next[0].Label != "[2]" {
			t.Fatal(next, err)
		}
		if _, ok := replay.Get("other", "run", "[1]", ""); ok {
			t.Fatal("cross tenant evidence")
		}
		if _, ok := replay.Get("tenant", "run", "[1]", "wrong"); ok {
			t.Fatal("wrong document bound")
		}
	})
}

func TestEvidenceRejectsFalseLiveFreshnessAndBounds(t *testing.T) {
	s := NewEvidenceStore()
	at := time.Now()
	entry := CitationEvidence{Source: "docs", DocumentID: "doc", Title: "Live", Text: "result", Mode: Live, CopyAt: &at}
	if _, err := s.RecordQuery("tenant", "run", "q", []CitationEvidence{entry}); !errors.Is(err, ErrInvalid) {
		t.Fatal("live result accepted retained-copy time", err)
	}
	entry.CopyAt = nil
	for i := range MaxEvidencePerRun {
		if _, err := s.RecordQuery("tenant", "run", fmt.Sprintf("query-%d", i), []CitationEvidence{entry}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RecordQuery("tenant", "run", "overflow", []CitationEvidence{entry}); !errors.Is(err, ErrExists) {
		t.Fatal("run evidence unbounded", err)
	}
}

func TestSourceRunReferencesFilterHiddenBeforeBound(t *testing.T) {
	s := NewEvidenceStore()
	entry := CitationEvidence{Source: "docs", DocumentID: "doc", Text: "actual", Mode: Indexed}
	for _, id := range []string{"a-hidden", "b-hidden", "z-visible"} {
		if _, err := s.RecordQuery("tenant", id, "rag:1", []CitationEvidence{entry}); err != nil {
			t.Fatal(err)
		}
	}
	refs := s.RunReferences("tenant", "docs", 1, func(id string, _ CitationEvidence) bool { return id == "z-visible" })
	if len(refs) != 1 || refs[0] != "z-visible" {
		t.Fatal("hidden references consumed public bound", refs)
	}
	if len(s.RunReferences("other", "docs", 50, func(string, CitationEvidence) bool { return true })) != 0 {
		t.Fatal("cross tenant relationships exposed")
	}
	if len(s.RunReferences("tenant", "docs", 50, nil)) != 0 {
		t.Fatal("missing visibility accepted")
	}
}
