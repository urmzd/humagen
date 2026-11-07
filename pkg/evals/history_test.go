package evals

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

func TestHistoryRetainsExecutionAccessAcrossSaveRestartAndInterruption(t *testing.T) {
	backend := state.NewMemory()
	s, err := Open(backend)
	if err != nil {
		t.Fatal(err)
	}
	clock := now
	s.now = func() time.Time { return clock }
	r := Retrieval{Input: "question", Sources: []string{"private"}, Blocks: []ContextBlock{{Text: "retained"}}}.Seal()
	suite := agentSuite(Case{ID: "c", Input: "question", Checks: []Check{{Contains, "answer"}}, Trace: &Trace{Run: "original", Retrieval: &r}})
	if err := s.Put("acme", suite, false); err != nil {
		t.Fatal(err)
	}
	first, err := s.Begin("acme", suite, 1, "ana", clock)
	if err != nil {
		t.Fatal(err)
	}
	first.Finish([]Result{{CaseID: "c", Output: "private answer", Passed: true}}, nil, clock)
	if err := s.Save("acme", first); err != nil {
		t.Fatal(err)
	}
	second, err := s.Begin("acme", suite, 1, "ana", clock)
	if err != nil {
		t.Fatal(err)
	}
	// Replacing the suite before the next Begin also exercises stale-run cleanup.
	suite.Cases[0].Trace = nil
	if err := s.Put("acme", suite, true); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(staleAfter + time.Second)
	if _, err := s.Begin("acme", suite, 2, "ana", clock); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(backend)
	if err != nil {
		t.Fatal(err)
	}
	history, err := reopened.History("acme", suite.Name)
	if err != nil || len(history) != 3 {
		t.Fatalf("history = %+v, %v", history, err)
	}
	if history[0].Access == nil || len(history[0].Access.Replay) != 0 {
		t.Fatal("plain execution lost known empty provenance")
	}
	for _, record := range history[1:] {
		if record.Access == nil || len(record.Access.Replay) != 1 {
			t.Fatalf("original provenance lost: %+v", record)
		}
		source := record.Access.Replay[0]
		if source.Run != "original" || !source.Retrieval || !source.RequiresEvidence || len(source.Sources) != 1 || source.Sources[0] != "private" {
			t.Fatalf("original provenance changed: %+v", source)
		}
	}
	if history[1].Run.ID != second.ID || history[1].Run.Status != Errored || history[2].Run.Results[0].Output != "private answer" {
		t.Fatalf("unexpected retained results: %+v", history)
	}
	if _, err := reopened.History("other", suite.Name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("history crossed tenants: %v", err)
	}
}

func TestHistoryDoesNotInventLegacyProvenanceFromCurrentSuite(t *testing.T) {
	s := NewStore()
	suite := agentSuite(Case{ID: "c", Input: "hi", Checks: []Check{{Contains, "answer"}}})
	if err := s.Put("acme", suite, false); err != nil {
		t.Fatal(err)
	}
	run := Run{ID: "ev-1", Suite: suite.Name, Target: Agent, Subject: suite.Subject, Status: Passed, Results: []Result{{Output: "unknown origin"}}}
	if err := s.kept.Put("acme", runKey(run), stored{Run: &run}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("acme", run); err != nil {
		t.Fatal(err)
	}
	history, err := s.History("acme", suite.Name)
	if err != nil || len(history) != 1 || history[0].Access != nil {
		t.Fatalf("legacy provenance invented: %+v %v", history, err)
	}
}
