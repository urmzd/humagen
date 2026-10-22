package feedback_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/state"
)

func TestFeedbackSurvivesRestartAndScopesPeopleAndTenants(t *testing.T) {
	st := state.NewMemory()
	s, err := feedback.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	original := feedback.Record{Run: "run", Subject: "ana", Rating: "down"}
	if _, err := s.Save("one", original); err != nil {
		t.Fatal(err)
	}
	s, err = feedback.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	updated := original
	updated.Comment = "Missing the source"
	updated.Reason = "incomplete"
	got, err := s.Save("one", updated)
	if err != nil {
		t.Fatal(err)
	}
	if got.Run != original.Run || got.Comment != updated.Comment {
		t.Fatalf("lost feedback context: %+v", got)
	}
	records, err := st.Load(t.Context(), "feedback")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("stored %d records", len(records))
	}
	var stored map[string]any
	if err := json.Unmarshal(records[0].Value, &stored); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"prompt", "answer"} {
		if _, exists := stored[field]; exists {
			t.Fatalf("feedback duplicates %s", field)
		}
	}
	if stored["run"] != original.Run {
		t.Fatal("feedback lost its source reference")
	}
	other := original
	other.Subject = "olu"
	other.Rating = "up"
	if _, err := s.Save("one", other); err != nil {
		t.Fatal(err)
	}
	if got := s.List("two", "run", 0); len(got) != 0 {
		t.Fatal("another tenant's feedback escaped")
	}
	if got := s.List("one", "run", 1); len(got) != 1 || got[0].Subject != "olu" || got[0].Seq != 2 {
		t.Fatalf("cursor: %+v", got)
	}
	if _, err := s.Save("one", feedback.Record{Run: "run", Subject: "ana", Rating: "invalid"}); err == nil {
		t.Fatal("invalid rating accepted")
	}
}

// A run's feedback pages by a number, never by a subject, which may hold an
// address, and the number means the same record after a restart, even for a
// record kept before records were numbered.
func TestFeedbackPagesByANumberThatSurvivesARestart(t *testing.T) {
	st := state.NewMemory()
	// Kept before records were numbered: they are numbered in the order they
	// were last rated, zed first.
	for i, who := range []string{"zed@example.com", "amy@example.com"} {
		at := time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC)
		if err := state.NewTable[feedback.Record](st, "feedback").Put("one", "k-"+who, feedback.Record{Run: "run", Subject: who, Rating: "up", UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	s, err := feedback.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	first := s.List("one", "run", 0)
	if len(first) != 2 || first[0].Seq == 0 || first[1].Seq <= first[0].Seq || first[0].Subject != "zed@example.com" {
		t.Fatalf("old records are not numbered: %+v", first)
	}
	for i := range feedback.MaxPage + 1 {
		if _, err := s.Save("one", feedback.Record{Run: "run", Subject: fmt.Sprintf("p%03d@example.com", i), Rating: "down"}); err != nil {
			t.Fatal(err)
		}
	}
	s, err = feedback.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	again := s.List("one", "run", 0)
	if len(again) != feedback.MaxPage || again[0] != first[0] || again[1] != first[1] {
		t.Fatalf("a restart renumbered the records: %+v then %+v", first, again[:2])
	}
	rest := s.List("one", "run", again[len(again)-1].Seq)
	if len(rest) != 3 || rest[2].Subject != fmt.Sprintf("p%03d@example.com", feedback.MaxPage) {
		t.Fatalf("the next page: %+v", rest)
	}
	// Rating again keeps a record's place.
	if _, err := s.Save("one", feedback.Record{Run: "run", Subject: first[0].Subject, Rating: "down"}); err != nil {
		t.Fatal(err)
	}
	if got := s.List("one", "run", 0)[0]; got.Subject != first[0].Subject || got.Seq != first[0].Seq {
		t.Fatalf("a new rating moved the record: %+v", got)
	}
}
