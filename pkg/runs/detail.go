package runs

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/humagen/fiber/pkg/event"
)

// Outcome is the agent's authored result, never an action approval or proof
// of external execution. Tool and call records remain the execution truth.
type Outcome struct {
	Kind       string   `json:"kind"`
	Summary    string   `json:"summary"`
	Decisions  []string `json:"decisions,omitempty"`
	NextAction string   `json:"next_action,omitempty"`
}

// Validate refuses ambiguous kinds and oversized authored result fields.
func (o Outcome) Validate() error {
	if o.Kind != "recommendation" && o.Kind != "decision" && o.Kind != "update" {
		return errors.New("outcome: kind must be recommendation, decision or update")
	}
	if strings.TrimSpace(o.Summary) == "" || len(o.Summary) > 4000 || len(o.NextAction) > 2000 || len(o.Decisions) > 20 || !utf8.ValidString(o.Summary+o.NextAction) {
		return errors.New("outcome: a summary up to 4000 bytes, next action up to 2000 bytes and at most 20 decisions are required")
	}
	for _, d := range o.Decisions {
		if len(d) > 1000 || !utf8.ValidString(d) {
			return errors.New("outcome: each decision must be valid text of at most 1000 bytes")
		}
	}
	return nil
}

func (x *Index) observeDetail(e event.Envelope) Run {
	if e.Type != event.TypeAction {
		return Run{}
	}
	action, ok := e.Data.(event.Action)
	if !ok || action.Name != "record_outcome" || action.Error != "" || len(action.Data) > 30000 {
		return Run{}
	}
	var outcome Outcome
	if json.Unmarshal(action.Data, &outcome) != nil || outcome.Validate() != nil {
		return Run{}
	}
	out, _ := x.update("outcome", e.Tenant, e.Run, func(t *txn, r *Run) (Run, bool) {
		if r.Status.Terminal() {
			return Run{}, false
		}
		r.Outcome = &outcome
		t.touch(r)
		return *r, true
	})
	return out
}
