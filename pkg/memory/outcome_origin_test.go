package memory_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestReviewedOriginIsAtomicPrivateAndNeverInferredOrCopied(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		blobs := blob.NewMemory()
		fault := statetest.NewFaulty(st)
		s, err := memory.OpenOn(fault, blobs)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Now()
		owner := memory.PersonalOwner("ada")
		fault.Fail(statetest.ErrInjected)
		if _, err := s.RememberDecision("tenant", owner, "Keep Tuesday", "ada", at, "private-origin"); !errors.Is(err, statetest.ErrInjected) {
			t.Fatal(err)
		}
		if len(memory.OriginsOf(t, s, "tenant", []string{"private-origin", "different-run"})) != 0 {
			t.Fatal("failed origin became visible")
		}
		fault.Fail(nil)
		legacy, err := s.RememberDecision("tenant", owner, "Legacy reviewed memory", "ada", at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.RememberDecision("tenant", owner, legacy.Text, "ada", at, "private-origin"); err != nil {
			t.Fatal(err)
		}
		if len(memory.OriginsOf(t, s, "tenant", []string{"private-origin"})) != 0 {
			t.Fatal("legacy memory origin inferred on retry")
		}
		n, err := s.RememberDecision("tenant", owner, "Keep Tuesday", "ada", at, "private-origin")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(n)
		if strings.Contains(string(body), "private-origin") || n.By != "ada" {
			t.Fatal("private origin exposed or human attribution replaced")
		}
		if _, err := s.RememberDecision("tenant", owner, n.Text, "ada", at, "different-run"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AddDetailed("tenant", memory.ProjectOwner("public"), n.Text, memory.Person, memory.Pending, "ada", at, n.Details); err != nil {
			t.Fatal(err)
		}
		replay, err := memory.OpenOn(st, blobs)
		if err != nil {
			t.Fatal(err)
		}
		origins := memory.OriginsOf(t, replay, "tenant", []string{"private-origin", "different-run"})
		if len(origins) != 1 || origins[0].Run != "private-origin" || origins[0].Owner != owner || origins[0].Note.ID != n.ID {
			t.Fatalf("origin lost, rewritten or copied: %+v", origins)
		}
		if len(memory.OriginsOf(t, replay, "other", []string{"private-origin"})) != 0 {
			t.Fatal("cross-tenant origin")
		}
		if err := replay.Remove("tenant", owner, n.ID); err != nil {
			t.Fatal(err)
		}
		if len(memory.OriginsOf(t, replay, "tenant", []string{"private-origin", "different-run"})) != 0 {
			t.Fatal("deleted memory retained dependency")
		}
	})
}
