package memory_test

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestRememberDecisionIsAtomicAndNeverApprovesPendingDuplicates(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		blobs := blob.NewMemory()
		fault := statetest.NewFaulty(st)
		store, err := memory.OpenOn(fault, blobs)
		if err != nil {
			t.Fatal(err)
		}
		owner := memory.PersonalOwner("ada")
		at := time.Now()
		if _, err := store.RememberDecision("acme", owner, "Keep\nTuesday", "ada", at); !errors.Is(err, memory.ErrInvalid) {
			t.Fatal("whole reviewed text was silently rewritten", err)
		}
		pending, err := store.AddDetailed("acme", owner, "Keep Tuesday", memory.Consolidated, memory.Pending, "private-run", at, memory.Details{Kind: memory.Decision})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.RememberDecision("acme", owner, "keep tuesday", "ada", at); !errors.Is(err, memory.ErrReviewConflict) {
			t.Fatal("pending duplicate bypassed review", err)
		}
		if notes := store.List("acme", owner); len(notes) != 1 || notes[0].Status != memory.Pending || notes[0].ID != pending.ID {
			t.Fatal("review refusal changed memory")
		}
		fault.Fail(statetest.ErrInjected)
		if _, err := store.RememberDecision("acme", owner, "Use a clear owner", "ada", at); !errors.Is(err, statetest.ErrInjected) {
			t.Fatal("failed reviewed write succeeded", err)
		}
		if len(store.List("acme", owner)) != 1 {
			t.Fatal("failed write became visible")
		}
		fault.Fail(nil)
		note, err := store.RememberDecision("acme", owner, "Use a clear owner", "ada", at)
		if err != nil {
			t.Fatal(err)
		}
		if repeat, err := store.RememberDecision("acme", owner, "Use a clear owner", "other-reviewer", at.Add(time.Hour)); err != nil || repeat.ID != note.ID || repeat.ReviewedBy != "ada" {
			t.Fatal("idempotent retry forged review", err)
		}
		again, err := memory.OpenOn(st, blobs)
		if err != nil {
			t.Fatal(err)
		}
		if notes := again.List("acme", owner); len(notes) != 2 || notes[1].ReviewedBy != "ada" || notes[1].Kind != memory.Decision || notes[1].Status != memory.Approved {
			t.Fatalf("review lost on restart: %+v", notes)
		}
	})
}
