package evals

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

func TestStyleChecksDoNotUseAModelAndRespectDetailedRequests(t *testing.T) {
	for _, tc := range []struct {
		text             string
		detailed, passed bool
	}{
		{"The build passed.", false, true},
		{strings.Repeat("word ", 121), false, false},
		{strings.Repeat("word ", 121), true, true},
		{"Great question! The build passed.", true, false},
		{"", false, true},
	} {
		if got := CheckStyle(tc.text, 120, tc.detailed); got.Passed != tc.passed {
			t.Fatalf("%q: %+v", tc.text, got)
		}
	}
}

func TestOnlineResultsAndFeedbackCandidatesSurviveReopenWithoutCrossingTenants(t *testing.T) {
	st := state.NewMemory()
	s, _ := Open(st)
	result := CheckStyle("Sure! Done.", 120, false)
	result.Run, result.Agent, result.At = "r1", "writer", time.Now()
	if err := s.SaveOnline("acme", result); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.SaveCandidate("acme", "ada", Candidate{Run: "r1", Agent: "writer", Input: "Write a summary", Reason: "too_long", At: result.At}); err != nil {
			t.Fatal(err)
		}
	}
	s, _ = Open(st)
	if got, err := s.Online("acme", "r1"); err != nil || got.Passed {
		t.Fatalf("lost online result: %+v %v", got, err)
	}
	if _, err := s.Online("other", "r1"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("tenant leak: %v", err)
	}
	if candidates, err := s.Candidates("acme", "writer", 50); err != nil || len(candidates) != 1 {
		t.Fatalf("duplicate/lost candidates: %+v %v", candidates, err)
	}
	if candidates, err := s.Candidates("other", "writer", 50); err != nil || len(candidates) != 0 {
		t.Fatalf("tenant leak: %+v %v", candidates, err)
	}
}

func TestAnEvalGateRejectsRegressionsAndUnexecutedCases(t *testing.T) {
	suite := agentSuite(Case{ID: "summary", Input: "Summarize", Checks: []Check{{Kind: Contains, Value: "evidence"}}})
	answer := func(text string) Answerer { return func(context.Context, Case) (string, error) { return text, nil } }
	if score, err := CheckChange(t.Context(), suite, answer("evidence"), answer("no findings")); !errors.Is(err, ErrRegression) || score.After != 0 || score.Before != 100 {
		t.Fatalf("regression accepted: %+v %v", score, err)
	}
	if score, err := CheckChange(t.Context(), suite, answer("bad"), answer("evidence")); err != nil || !score.Passed {
		t.Fatalf("improvement refused: %+v %v", score, err)
	}
	if _, err := CheckChange(t.Context(), suite, answer("evidence"), func(context.Context, Case) (string, error) { return "", errors.New("provider down") }); !errors.Is(err, ErrRegression) {
		t.Fatalf("unrun check passed: %v", err)
	}
}

func TestRecordedToolsReplayOnceAndRefuseUnrecordedArguments(t *testing.T) {
	trace := Trace{Run: "source", Tools: []ToolResult{{Name: "search", Arguments: []byte(`{"q":"original"}`), Result: "known evidence"}}}
	replay, err := NewReplay(trace)
	if err != nil {
		t.Fatal(err)
	}
	got, err := replay.Execute("search", map[string]any{"q": "original"})
	if err != nil || got != "known evidence" {
		t.Fatalf("replay: %q %v", got, err)
	}
	if _, err = replay.Execute("search", map[string]any{"q": "original"}); err == nil {
		t.Fatal("recorded side effect replayed twice")
	}
	replay, _ = NewReplay(trace)
	if _, err = replay.Execute("search", map[string]any{"q": "new"}); err == nil || replay.Error() == nil {
		t.Fatal("unrecorded call passed")
	}
}
