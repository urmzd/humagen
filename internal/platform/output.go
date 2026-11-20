package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/approve"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
)

// outputSchema is the output schema a revision's runs answer under: its
// spec's (wire.AgentSpec.Output), or for an approval agent whose spec has
// none, such as a revision a workspace saved before schemas existed, the
// default it ships with. Nil is a free-text answer.
func outputSchema(rev wire.Revision) json.RawMessage {
	if rev.Spec.Output != nil {
		return rev.Spec.Output.Schema
	}
	if rev.Spec.Name == BuiltinApprover {
		return approve.DefaultSchema
	}
	return nil
}

// keepOutput keeps a completed run's answer as structured data (Output)
// when its agent's revision declares an output schema and the answer holds
// to it, checked against the whole schema: the model was asked for the
// schema in the form a provider takes, which may leave some of it out. An
// answer that does not hold is not kept, and is logged; whoever reads the
// output treats it as missing.
func (p *Platform) keepOutput(run runs.Run) (runs.Run, error) {
	if run.Status != runs.Completed || len(run.Output) > 0 {
		return run, nil
	}
	rev, err := p.Catalog().Get(context.Background(), run.Tenant, run.Agent, run.Rev)
	if err != nil {
		return run, nil
	}
	schema := outputSchema(rev)
	if schema == nil {
		return run, nil
	}
	answer, ok := structured(run.Answer)
	if !ok {
		p.log.Warn("platform: a run's answer held no JSON object for its agent's output schema", "tenant", run.Tenant, "run", run.ID, "agent", run.Agent)
		return run, nil
	}
	if err := (call.JSONSchemaFormat{Schema: schema}).ValidateOutput(string(answer)); err != nil {
		p.log.Warn("platform: a run's answer did not hold to its agent's output schema", "tenant", run.Tenant, "run", run.ID, "agent", run.Agent, "error", err)
		return run, nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, answer); err != nil {
		return run, nil
	}
	if err := p.Runs.SetOutput(run.Tenant, run.ID, compact.Bytes()); err != nil {
		return run, err
	}
	run.Output = compact.Bytes()
	return run, nil
}

// maxCandidates bounds the objects structured tries.
const maxCandidates = 256

// structured is the JSON object an answer ends with. An agent with no tools
// answers with the object alone; one with tools may say something first,
// and its structured reply follows what it said, so its answer ends with
// the object. Only the whole of the last object is a valid suffix: an
// object inside it is followed by the rest of it.
func structured(answer string) ([]byte, bool) {
	s := strings.TrimSpace(answer)
	if !strings.HasSuffix(s, "}") {
		return nil, false
	}
	i := strings.LastIndexByte(s, '{')
	for n := 0; i >= 0 && n < maxCandidates; n++ {
		if json.Valid([]byte(s[i:])) {
			return []byte(s[i:]), true
		}
		i = strings.LastIndexByte(s[:i], '{')
	}
	return nil, false
}
