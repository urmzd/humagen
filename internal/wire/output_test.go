package wire

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/approve"
)

const riskSchema = `{"type":"object","required":["decision","reason","risk"],"properties":{"decision":{"type":"string","enum":["allow","ask","deny"]},"reason":{"type":"string"},"risk":{"type":"string","enum":["low","medium","high"]},"conditions":{"type":"array","items":{"type":"string"}},"notify":{"type":"array","items":{"type":"string"}},"expires_in":{"type":"integer"}}}`

// An agent's output schema is kept with its revision in a canonical form,
// so the same schema written another way is the same definition, and a
// different schema is an edit.
func TestAnAgentsOutputSchemaIsKeptWithItsRevision(t *testing.T) {
	spec := AgentSpec{Name: "triage", Provider: "anthropic", Model: "claude-sonnet-5-5", SystemPrompt: "Sort it.",
		Output: &Output{Schema: json.RawMessage(`{ "properties": {"label": {"type": "string"}}, "type": "object", "required": ["label"] }`)}}
	same := spec
	same.Output = &Output{Schema: json.RawMessage(`{"type":"object","required":["label"],"properties":{"label":{"type":"string"}}}`)}
	if spec.Normalize().Hash() != same.Normalize().Hash() {
		t.Fatal("the same schema written another way changed the hash")
	}
	plain := spec
	plain.Output = nil
	if spec.Normalize().Hash() == plain.Normalize().Hash() {
		t.Fatal("an output schema kept the hash")
	}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	rec, err := RecordOf(spec.Normalize(), 1)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := RevisionOf(rec, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Spec.Output == nil || string(rev.Spec.Output.Schema) != string(same.Normalize().Output.Schema) {
		t.Fatalf("read back output %+v", rev.Spec.Output)
	}
	if rev.Spec.Normalize().Hash() != spec.Normalize().Hash() {
		t.Fatal("the stored revision is not the definition written")
	}
	// A revision with no output reads back with none.
	if rec, err = RecordOf(plain.Normalize(), 2); err != nil {
		t.Fatal(err)
	}
	if rev, err = RevisionOf(rec, 2); err != nil || rev.Spec.Output != nil {
		t.Fatalf("no output = %+v, %v", rev.Spec.Output, err)
	}
	clone := spec.Clone()
	clone.Output.Schema[0] = '['
	if spec.Output.Schema[0] != '{' {
		t.Fatal("a clone shares its output schema")
	}
}

func TestAnOutputSchemaDescribesAnObjectTheRuntimeCanAskFor(t *testing.T) {
	base := AgentSpec{Name: "triage", Provider: "anthropic", Model: "claude-sonnet-5-5", SystemPrompt: "Sort it."}
	for name, schema := range map[string]string{
		"empty":            ``,
		"not JSON":         `{"type":`,
		"not an object":    `{"type":"string"}`,
		"no type":          `{"properties":{"a":{"type":"string"}}}`,
		"over the bound":   `{"type":"object","description":"` + strings.Repeat("x", MaxOutputSchema) + `"}`,
		"not a JSON value": `object`,
	} {
		s := base
		s.Output = &Output{Schema: json.RawMessage(schema)}
		if err := s.Validate(); !errors.Is(err, ErrInvalidAgent) {
			t.Errorf("%s: %v", name, err)
		}
		if err := s.ValidateOutput(); !errors.Is(err, ErrInvalidAgent) {
			t.Errorf("%s alone: %v", name, err)
		}
	}
}

// The approval agent's answer is read as a decision, so its schema keeps
// decision and reason; anything else in it is the workspace's own.
func TestTheApprovalAgentsSchemaKeepsTheCore(t *testing.T) {
	s := AgentSpec{Name: approve.Agent, Provider: "anthropic", Model: "claude-sonnet-5-5", SystemPrompt: "Decide."}
	for _, ok := range []string{string(approve.DefaultSchema), riskSchema} {
		s.Output = &Output{Schema: json.RawMessage(ok)}
		if err := s.Validate(); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
	s.Output = &Output{Schema: json.RawMessage(`{"type":"object","required":["reason","risk"],"properties":{"reason":{"type":"string"},"risk":{"type":"string"}}}`)}
	if err := s.Validate(); !errors.Is(err, ErrInvalidAgent) || !strings.Contains(err.Error(), "decision") {
		t.Fatalf("a schema without decision = %v", err)
	}
	// Another agent may answer in any object it likes.
	s.Name = "triage"
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}
