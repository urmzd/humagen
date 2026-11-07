package evals

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
)

func TestRetrievalSnapshotIsImmutableTenantScopedAndDurable(t *testing.T) {
	db := state.NewMemory()
	s, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	r := Retrieval{Input: "ask", Query: "ask", Sources: []string{"manual"}, Prompt: "exact bytes", Blocks: []ContextBlock{}}.Seal()
	if err = s.RecordRetrieval("acme", "run", 1, r); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordRetrieval("acme", "run", 1, r); err != nil {
		t.Fatal(err)
	}
	changed := r
	changed.Prompt = "different"
	changed = changed.Seal()
	if err = s.RecordRetrieval("acme", "run", 1, changed); !errors.Is(err, ErrExists) {
		t.Fatalf("snapshot replaced: %v", err)
	}
	reopened, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.LastRetrieval("acme", "run")
	if err != nil || got.Digest != r.Digest {
		t.Fatalf("snapshot lost: %+v %v", got, err)
	}
	if _, err = reopened.LastRetrieval("other", "run"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant snapshot: %v", err)
	}
	empty := Retrieval{Input: "ask", Sources: []string{"manual"}, Blocks: []ContextBlock{}}.Seal()
	if err = empty.Validate(); err != nil {
		t.Fatalf("recorded empty context refused: %v", err)
	}
}
