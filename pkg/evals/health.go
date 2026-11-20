package evals

import (
	"fmt"
	"github.com/urmzd/humagen/pkg/signals"
)

// Limits leaves cost and duration disabled at zero. Tool errors always count.
type Limits struct {
	MaxSpend        int64 `json:"max_spend,omitempty"`
	MaxDurationMS   int64 `json:"max_duration_ms,omitempty"`
	RequireEvidence bool  `json:"require_evidence,omitempty"`
}

func (l Limits) Validate() error {
	if l.MaxSpend < 0 || l.MaxDurationMS < 0 {
		return fmt.Errorf("%w: evaluation limits cannot be negative", ErrInvalid)
	}
	return nil
}
func CheckSignals(record signals.Record, limits Limits, completed, evidence bool) []Verdict {
	out := []Verdict{{Check: "completed", Passed: completed, Reason: chooseReason(completed, "The run did not complete")}, {Check: "tool_errors", Passed: record.ToolErrors == 0, Reason: chooseReason(record.ToolErrors == 0, fmt.Sprintf("%d tools failed", record.ToolErrors))}}
	if limits.MaxSpend > 0 {
		ok := record.Spend <= limits.MaxSpend
		out = append(out, Verdict{Check: "cost", Passed: ok, Reason: chooseReason(ok, "Run cost exceeded the workspace limit")})
	}
	if limits.MaxDurationMS > 0 {
		ok := record.LatencyMS-record.WaitingMS <= limits.MaxDurationMS
		out = append(out, Verdict{Check: "duration", Passed: ok, Reason: chooseReason(ok, "Execution time exceeded the workspace limit")})
	}
	if limits.RequireEvidence {
		out = append(out, Verdict{Check: "evidence", Passed: evidence, Reason: chooseReason(evidence, "The answer has no linked evidence")})
	}
	return out
}
