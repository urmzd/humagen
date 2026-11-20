package feedback_test

import (
	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/state"
	"testing"
)

func TestFindingFeedbackIsIndependentAndClearsOnlyItsOwner(t *testing.T) {
	st := state.NewMemory()
	a, err := feedback.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	b, err := feedback.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []feedback.Record{{Run: "r", Subject: "ada", Rating: "up"}, {Run: "r", Subject: "ada", FindingID: "f1", Rating: "down"}, {Run: "r", Subject: "ben", FindingID: "f1", Rating: "up"}} {
		if _, err := a.Save("acme", r); err != nil {
			t.Fatal(err)
		}
	}
	before, err := a.ChangedAt("acme", "r", "ada")
	if err != nil || before.IsZero() {
		t.Fatalf("missing feedback change timestamp: %v %v", before, err)
	}
	if err := b.Remove("acme", "r", "ada", "f1"); err != nil {
		t.Fatal(err)
	}
	after, err := a.ChangedAt("acme", "r", "ada")
	if err != nil || !after.After(before) {
		t.Fatalf("removal did not advance timestamp: %v -> %v (%v)", before, after, err)
	}
	foreign, err := a.ChangedAt("elsewhere", "r", "ada")
	if err != nil || !foreign.IsZero() {
		t.Fatalf("change timestamp crossed tenant: %v %v", foreign, err)
	}
	if _, err := a.Own("acme", "r", "ada"); err != nil {
		t.Fatalf("cleared whole answer: %v", err)
	}
	if _, err := a.Own("acme", "r", "ben", "f1"); err != nil {
		t.Fatalf("cleared another person's finding: %v", err)
	}
	rows, err := b.OwnAll("acme", "r", "ada")
	if err != nil || len(rows) != 1 || rows[0].FindingID != "" {
		t.Fatalf("own records: %+v %v", rows, err)
	}
	if _, err := a.Own("elsewhere", "r", "ben", "f1"); err == nil {
		t.Fatal("cross tenant read")
	}
}
