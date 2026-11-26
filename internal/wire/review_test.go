package wire

import (
	"errors"
	"testing"
)

// An agent's review policy is kept with its revision and read from an
// .agent.md file's metadata, changes the hash only when set, and takes only
// the values the host acts on.
func TestAnAgentsReviewPolicyIsKeptWithItsRevision(t *testing.T) {
	spec := AgentSpec{Name: "gatekeeper", Provider: "anthropic", Model: "claude-sonnet-5-5", SystemPrompt: "Review.", Review: Review{Approve: " When_Clean ", BlockOn: "Warning"}}
	plain := spec
	plain.Review = Review{}
	if spec.Normalize().Hash() == plain.Normalize().Hash() {
		t.Fatal("a review policy kept the hash")
	}
	rec, err := RecordOf(spec.Normalize(), 1)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := RevisionOf(rec, 1)
	if err != nil || rev.Spec.Review != (Review{Approve: ReviewApproveWhenClean, BlockOn: "warning"}) {
		t.Fatalf("read back %+v, %v", rev.Spec.Review, err)
	}
	if rec, err = RecordOf(plain.Normalize(), 2); err != nil {
		t.Fatal(err)
	}
	if rev, err = RevisionOf(rec, 2); err != nil || rev.Spec.Review != (Review{}) {
		t.Fatalf("no policy = %+v, %v", rev.Spec.Review, err)
	}
	file := "---\napiVersion: saige/v1\nname: gatekeeper\nversion: 1.0.0\nmodel:\n  use: anthropic/claude-sonnet-5-5\nmetadata:\n  humagen_review_approve: when_clean\n  humagen_review_block_on: info\n---\nReview.\n"
	if s, err := SpecOfAgentFile(file, DefinitionPolicy{}); err != nil || s.Review != (Review{Approve: ReviewApproveWhenClean, BlockOn: "info"}) {
		t.Fatalf("an .agent.md file's policy = %+v, %v", s.Review, err)
	}
	for _, bad := range []Review{{Approve: "always"}, {Approve: "yes"}, {BlockOn: "high"}, {BlockOn: "critical"}} {
		s := plain
		s.Review = bad
		if err := s.Validate(); !errors.Is(err, ErrInvalidAgent) {
			t.Errorf("review %+v = %v", bad, err)
		}
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("a valid policy = %v", err)
	}
}
