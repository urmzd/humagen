package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/turn"
	"github.com/urmzd/orchagent/flow/turn/native"
	types "github.com/urmzd/saige/agent/types"
)

type replayRuntime struct {
	turn.Runtime
	replay *evals.Replay
}

func (r *replayRuntime) Turn(ctx context.Context, def turn.Pinned, in turn.Input, emit func(context.Context, event.Body) error) (turn.Result, error) {
	out, err := r.Runtime.Turn(ctx, def, in, emit)
	if replayErr := r.replay.Error(); replayErr != nil {
		return out, replayErr
	}
	return out, err
}

type replayTools struct {
	trace  evals.Trace
	replay *evals.Replay
}

func (r replayTools) Lookup(name string) (types.Tool, error) {
	if native.IsToolGroup(name) {
		return nil, fmt.Errorf("replay requires explicit recorded tool names")
	}
	props := map[string]types.PropertyDef{}
	for _, step := range r.trace.Tools {
		if step.Name == name {
			var args map[string]any
			_ = json.Unmarshal(step.Arguments, &args)
			for key, value := range args {
				props[key] = replayProperty(value)
			}
		}
	}
	return &types.ToolFunc{Def: types.ToolDef{Name: name, Description: "Replay a recorded tool result. New arguments fail this evaluation.", Parameters: types.ParameterSchema{Type: "object", Properties: props}}, Fn: func(_ context.Context, args map[string]any) (string, error) { return r.replay.Execute(name, args) }}, nil
}
func replayProperty(v any) types.PropertyDef {
	p := types.PropertyDef{}
	switch v := v.(type) {
	case string:
		p.Type = "string"
	case bool:
		p.Type = "boolean"
	case float64:
		p.Type = "number"
	case nil:
		p.Type = "string"
		p.Nullable = true
	case []any:
		p.Type = "array"
		item := types.PropertyDef{Type: "string"}
		if len(v) > 0 {
			item = replayProperty(v[0])
		}
		p.Items = &item
	case map[string]any:
		p.Type = "object"
		p.Properties = map[string]types.PropertyDef{}
		for k, val := range v {
			p.Properties[k] = replayProperty(val)
		}
	}
	return p
}

// RecordedEvalCase snapshots inputs and outcomes only after checking the source
// run's visibility in the builder's conversation. Truncated arguments cannot replay.
func (p *Platform) recordedEvalCase(tenant, own, runID, id string, checks []evals.Check) (evals.Case, error) {
	run, err := p.Runs.Get(tenant, runID)
	if err != nil || run.UsesPrivateContext() || !p.openWork(tenant, own)(run) {
		return evals.Case{}, ErrForbidden
	}
	events, err := p.Journal.Events(tenant, []string{runID})
	if err != nil {
		return evals.Case{}, err
	}
	trace := evals.Trace{Run: runID, Tools: []evals.ToolResult{}}
	trace.Retrieval, err = p.recordedRetrievalFor(tenant, own, runID, run.Text)
	if err != nil {
		return evals.Case{}, err
	}
	if trace.Retrieval == nil {
		rev, err := p.Catalog().Get(context.Background(), tenant, run.Agent, run.Rev)
		if err != nil || len(rev.Spec.Sources) > 0 || len(runs.SourcesOf(wire.Meta(run.Tag()))) > 0 || len(run.Citations) > 0 {
			return evals.Case{}, fmt.Errorf("exact recorded retrieval context is unavailable for this run")
		}
	}
	slices.SortStableFunc(events, func(a, b event.Envelope) int { return a.Time.Compare(b.Time) })
	for _, ev := range events {
		if done, ok := ev.Data.(event.ToolDone); ok {
			if done.ArgumentsBytes > len(done.Arguments) || len(done.Arguments) == 0 {
				return evals.Case{}, fmt.Errorf("recorded tool arguments were omitted; this run cannot replay")
			}
			trace.Tools = append(trace.Tools, evals.ToolResult{Name: done.Name, Arguments: done.Arguments, Result: done.Result, Error: done.Error})
		}
	}
	if err := trace.Validate(); err != nil {
		return evals.Case{}, err
	}
	return evals.Case{ID: id, Input: run.Text, Checks: checks, Trace: &trace}, nil
}
