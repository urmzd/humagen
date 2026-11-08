package evals

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
)

// Trace contains recorded tool outcomes. Replaying it never reaches a live tool.
// The original run reference is provenance, not authority to read another run.
type Trace struct {
	Run       string       `json:"run"`
	Tools     []ToolResult `json:"tools"`
	Retrieval *Retrieval   `json:"retrieval,omitempty"`
}
type ToolResult struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Result    string          `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
}

func (t Trace) Validate() error {
	if t.Run == "" || len(t.Tools) > 100 {
		return fmt.Errorf("%w: a trace needs a run and at most 100 tool outcomes", ErrInvalid)
	}
	total := 0
	if t.Retrieval != nil {
		if err := t.Retrieval.Validate(); err != nil {
			return err
		}
		raw, _ := json.Marshal(t.Retrieval)
		total += len(raw)
	}
	for _, step := range t.Tools {
		var args map[string]any
		if step.Name == "" || json.Unmarshal(step.Arguments, &args) != nil || args == nil {
			return fmt.Errorf("%w: malformed recorded tool arguments", ErrInvalid)
		}
		total += len(step.Arguments) + len(step.Result) + len(step.Error)
	}
	if total > 512000 {
		return fmt.Errorf("%w: trace exceeds 512000 bytes", ErrInvalid)
	}
	return nil
}

// Replay consumes matching recorded outcomes once. Calls may run concurrently;
// outcomes for equal arguments retain their original order.
type Replay struct {
	mu      sync.Mutex
	steps   []ToolResult
	used    []bool
	problem error
}

func NewReplay(t Trace) (*Replay, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return &Replay{steps: slices.Clone(t.Tools), used: make([]bool, len(t.Tools))}, nil
}
func (r *Replay) Execute(name string, args map[string]any) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, _ := json.Marshal(args)
	for i, step := range r.steps {
		if r.used[i] || step.Name != name {
			continue
		}
		var obj map[string]any
		_ = json.Unmarshal(step.Arguments, &obj)
		want, _ := json.Marshal(obj)
		if string(key) != string(want) {
			continue
		}
		r.used[i] = true
		if step.Error != "" {
			return step.Result, fmt.Errorf("recorded tool failure: %s", step.Error)
		}
		return step.Result, nil
	}
	r.problem = fmt.Errorf("evals: no recorded result for tool %s and these arguments", name)
	return "", r.problem
}
func (r *Replay) Error() error { r.mu.Lock(); defer r.mu.Unlock(); return r.problem }
