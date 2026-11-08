package feedback_test

import (
	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/state"
	"testing"
)

func TestNativeRemovalCannotEraseNewerSameRatingOrReplayAddition(t *testing.T) {
	st := state.NewMemory()
	s, _ := feedback.Open(st)
	r := feedback.Record{Run: "r", Subject: "u", FindingID: "f", Rating: "up"}
	if _, err := s.SaveNative("t", "old", r); err != nil {
		t.Fatal(err)
	}
	newer, err := s.SaveNative("t", "new", r)
	if err != nil {
		t.Fatal(err)
	}
	s, _ = feedback.Open(st)
	if _, _, err = s.RemoveNativeSource("t", "remove-old", "old", r); err != nil {
		t.Fatal(err)
	}
	got, err := s.Own("t", "r", "u", "f")
	if err != nil || !got.UpdatedAt.Equal(newer.UpdatedAt) {
		t.Fatal("stale same-rating removal", got, err)
	}
	if _, _, err = s.RemoveNativeSource("t", "remove-new", "new", r); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Own("t", "r", "u", "f"); err == nil {
		t.Fatal("matching assessment remains")
	}
	if _, err = s.SaveNative("t", "new", r); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Own("t", "r", "u", "f"); err == nil {
		t.Fatal("stale addition resurrected withdrawn rating")
	}
	if _, err = s.Save("t", r); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.RemoveNativeSource("t", "another-removal", "new", r); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Own("t", "r", "u", "f"); err != nil {
		t.Fatal("old native source erased console rating", err)
	}
}
