package spec

import (
	"strings"
	"testing"
)

// A config written before the platform's agents carried their own models,
// and before people picked agents rather than models, still loads: the
// fields it no longer reads are dropped, each with a note, and never written
// back.
func TestAnOldConfigWithRetiredFieldsLoadsWithNotes(t *testing.T) {
	old := []byte(`api_version: humagen/v1
kind: System
spec:
  execution:
    concurrency: 4
    builtin: {provider: gemini, model: gemini-3-flash-preview}
---
api_version: humagen/v1
kind: Platform
metadata: {name: default}
spec:
  agents:
    - spec:
        name: picker
        provider: anthropic
        model: claude-sonnet-5-5
        system_prompt: Answer.
        choices: {models: [anthropic/claude-opus-5-5], efforts: [low, high]}
`)
	got, err := Parse(old)
	if err != nil {
		t.Fatalf("an old config = %v", err)
	}
	if got.System.Spec.Execution.Concurrency != 4 || got.System.Spec.Execution.RetiredBuiltin != nil {
		t.Fatalf("execution = %+v", got.System.Spec.Execution)
	}
	if a := got.Platforms[0].Spec.Agents[0]; a.Spec == nil || a.Spec.Name != "picker" {
		t.Fatalf("the agent = %+v", a)
	}
	notes := strings.Join(got.Notes, "\n")
	for _, want := range []string{"execution.builtin is no longer read", "agents[0].spec.choices is no longer read"} {
		if !strings.Contains(notes, want) {
			t.Errorf("no note %q in:\n%s", want, notes)
		}
	}
	out, err := Marshal(Config{System: got.System, Platforms: got.Platforms})
	if err != nil {
		t.Fatal(err)
	}
	if s := string(out); strings.Contains(s, "builtin") || strings.Contains(s, "choices") {
		t.Fatalf("a retired field was written back:\n%s", s)
	}
	// Anything else the file does not know still stops it.
	if _, err := Parse([]byte(strings.Replace(string(old), "concurrency: 4", "concurency: 4", 1))); err == nil {
		t.Fatal("a misspelled field loaded")
	}
	if _, err := Parse([]byte(strings.Replace(string(old), "system_prompt: Answer.", "system_prompt: Answer.\n        promt: x", 1))); err == nil {
		t.Fatal("a misspelled agent field loaded")
	}
}
