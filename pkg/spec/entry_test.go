package spec

import "testing"

func TestEntryAgentDefaultAndBrand(t *testing.T) {
	s := System{}
	s.Defaults()
	if s.Spec.Execution.EntryAgent != "humagen" {
		t.Fatal(s.Spec.Execution.EntryAgent)
	}
	s.Spec.Execution.EntryAgent = "codex"
	s.Defaults()
	if s.Spec.Execution.EntryAgent != "codex" {
		t.Fatal(s.Spec.Execution.EntryAgent)
	}
}
