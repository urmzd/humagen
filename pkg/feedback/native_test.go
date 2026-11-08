package feedback_test

import (
	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/state"
	"sync"
	"testing"
)

func TestNativeFeedbackReplayCannotOverwriteNewerRating(t *testing.T) {
	st := state.NewMemory()
	s, _ := feedback.Open(st)
	r := feedback.Record{Run: "r", Subject: "u", FindingID: "f", Rating: "up"}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := s.SaveNative("t", "same", r); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	first, err := s.Own("t", "r", "u", "f")
	if err != nil {
		t.Fatal(err)
	}
	r.Rating = "down"
	if _, err = s.SaveNative("t", "new", r); err != nil {
		t.Fatal(err)
	}
	s, _ = feedback.Open(st)
	r.Rating = "up"
	duplicate, err := s.SaveNative("t", "same", r)
	if err != nil || !duplicate.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatal(duplicate, err)
	}
	got, err := s.Own("t", "r", "u", "f")
	if err != nil || got.Rating != "down" {
		t.Fatal("replay replaced latest rating", got, err)
	}
	if _, err = s.RemoveNative("t", "remove-stale", r); err != nil {
		t.Fatal(err)
	}
	got, err = s.Own("t", "r", "u", "f")
	if err != nil || got.Rating != "down" {
		t.Fatal("stale removal cleared newer rating", got, err)
	}
	r.Rating = "down"
	if _, err = s.RemoveNative("t", "remove-down", r); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveNative("t", "readd", r); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RemoveNative("t", "remove-down", r); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Own("t", "r", "u", "f"); err != nil {
		t.Fatal("duplicate removal cleared re-added rating", err)
	}
}
