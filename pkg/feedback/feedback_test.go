package feedback_test

import (
	"encoding/json"
	"testing"

	"github.com/urmzd/warp/pkg/feedback"
	"github.com/urmzd/warp/pkg/state"
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
	if got := s.List("two", "run", ""); len(got) != 0 {
		t.Fatal("another tenant's feedback escaped")
	}
	if got := s.List("one", "run", "ana"); len(got) != 1 || got[0].Subject != "olu" {
		t.Fatalf("cursor: %+v", got)
	}
	if _, err := s.Save("one", feedback.Record{Run: "run", Subject: "ana", Rating: "invalid"}); err == nil {
		t.Fatal("invalid rating accepted")
	}
}
