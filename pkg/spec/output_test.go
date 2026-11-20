package spec

import (
	"path/filepath"
	"strings"
	"testing"
)

// A file may give the approval agent its own answer schema, with fields of
// the workspace's own beside decision and reason; one without decision is
// refused when the file is read, so config check finds it before a start.
func TestTheApprovalAgentsSchemaIsCheckedWhenTheFileIsRead(t *testing.T) {
	file := func(schema string) []byte {
		return []byte(`api_version: humagen/v1
kind: Platform
metadata: {name: default}
spec:
  agents:
    - spec:
        name: approver
        provider: anthropic
        model: claude-sonnet-5-5
        system_prompt: Decide each call.
        output:
          schema: ` + schema + `
`)
	}
	check := func(schema string) error {
		path := filepath.Join(t.TempDir(), "humagen.yaml")
		writeFile(t, path, string(file(schema)))
		cfg, err := Load(path)
		if err != nil {
			return err
		}
		return cfg.Check(Kinds{})
	}
	custom := `{type: object, required: [decision, reason, risk], properties: {decision: {type: string, enum: [allow, ask, deny]}, reason: {type: string}, risk: {type: string, enum: [low, medium, high]}, notify: {type: array, items: {type: string}}}}`
	if err := check(custom); err != nil {
		t.Fatalf("a schema with the core = %v", err)
	}
	got, err := Parse(file(custom))
	if err != nil {
		t.Fatal(err)
	}
	if a := got.Platforms[0].Spec.Agents[0].Spec; a.Output == nil || !strings.Contains(string(a.Output.Schema), `"risk"`) {
		t.Fatalf("the schema = %+v", a.Output)
	}
	missing := `{type: object, required: [reason, risk], properties: {reason: {type: string}, risk: {type: string}}}`
	if err := check(missing); err == nil || !strings.Contains(err.Error(), "agents[0].spec.output") || !strings.Contains(err.Error(), "decision") {
		t.Fatalf("a schema without decision = %v", err)
	}
}
