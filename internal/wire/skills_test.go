package wire

import (
	"errors"
	"slices"
	"testing"
)

// The skills an agent may load are kept with its revision, change its hash
// only when set, and load nothing by default.
func TestAnAgentsLoadableSkillsAreKeptWithItsRevision(t *testing.T) {
	spec := AgentSpec{Name: "reviewer", Provider: "anthropic", Model: "claude-sonnet-5-5", SystemPrompt: "Review.",
		Skills: &Skills{Loadable: []string{"secure-review", " go-style "}}}
	plain := spec
	plain.Skills = nil
	if spec.Normalize().Hash() == plain.Normalize().Hash() {
		t.Fatal("loadable skills kept the hash")
	}
	if empty := (AgentSpec{Name: "reviewer", Provider: "anthropic", Model: "claude-sonnet-5-5", SystemPrompt: "Review.", Skills: &Skills{}}); empty.Normalize().Hash() != plain.Normalize().Hash() {
		t.Fatal("an empty list changed the hash")
	}
	rec, err := RecordOf(spec.Normalize(), 1)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := RevisionOf(rec, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Spec.Skills == nil || !slices.Equal(rev.Spec.Skills.Loadable, []string{"go-style", "secure-review"}) {
		t.Fatalf("read back %+v", rev.Spec.Skills)
	}
	if !rev.Spec.Loads("go-style") || rev.Spec.Loads("other") || plain.Loads("go-style") {
		t.Fatal("Loads answers wrongly")
	}
	if rec, err = RecordOf(plain.Normalize(), 2); err != nil {
		t.Fatal(err)
	}
	if rev, err = RevisionOf(rec, 2); err != nil || rev.Spec.Skills != nil {
		t.Fatalf("no skills = %+v, %v", rev.Spec.Skills, err)
	}
	every := plain
	every.Skills = &Skills{Loadable: []string{AnySkill}}
	if !every.Loads("anything") || every.Validate() != nil {
		t.Fatalf("* = %v", every.Validate())
	}
	for _, bad := range [][]string{{"*", "go-style"}, {"Go Style"}, {"a", "a"}, {""}} {
		s := plain
		s.Skills = &Skills{Loadable: bad}
		if err := s.Validate(); !errors.Is(err, ErrInvalidAgent) {
			t.Errorf("loadable %q = %v", bad, err)
		}
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("valid loadable skills = %v", err)
	}
}
