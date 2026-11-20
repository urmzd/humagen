package wire

import (
	"errors"
	"testing"
)

// An agent's reasoning effort is kept with its revision, changes its hash
// only when set, and must be one word.
func TestAnAgentsEffortIsKeptWithItsRevision(t *testing.T) {
	spec := AgentSpec{Name: "thinker", Provider: "anthropic", Model: "claude-sonnet-5-5", SystemPrompt: "Think.", Effort: " High "}
	plain := spec
	plain.Effort = ""
	if spec.Normalize().Hash() == plain.Normalize().Hash() {
		t.Fatal("effort kept the hash")
	}
	rec, err := RecordOf(spec.Normalize(), 1)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := RevisionOf(rec, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Spec.Effort != "high" {
		t.Fatalf("read back effort %q", rev.Spec.Effort)
	}
	// With a package too, both are kept in the one metadata block.
	spec.Package = &PackageBase{Name: "humagen/core", Version: "sha256:x"}
	rec, err = RecordOf(spec.Normalize(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if rev, err = RevisionOf(rec, 2); err != nil || rev.Spec.Effort != "high" || rev.Spec.Package == nil || rev.Spec.Package.Name != "humagen/core" {
		t.Fatalf("with a package = %+v, %v", rev.Spec, err)
	}
	// A revision with no effort reads back with none.
	if rec, err = RecordOf(plain.Normalize(), 3); err != nil {
		t.Fatal(err)
	}
	if rev, err = RevisionOf(rec, 3); err != nil || rev.Spec.Effort != "" {
		t.Fatalf("no effort = %q, %v", rev.Spec.Effort, err)
	}
	for _, bad := range []string{"very high", "high!", "abcdefghijklmnopq"} {
		s := plain
		s.Effort = bad
		if err := s.Validate(); !errors.Is(err, ErrInvalidAgent) {
			t.Errorf("effort %q = %v", bad, err)
		}
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("a valid effort = %v", err)
	}
}
