package wire

import (
	"encoding/json"

	"github.com/urmzd/saige/flow/turn"
)

// EvaluationDefinition crosses the same spec seam without publishing a
// revision. The candidate remains immutable and content addressed.
func EvaluationDefinition(spec AgentSpec) (turn.Pinned, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return turn.Pinned{}, err
	}
	var converted turn.Spec
	if err := json.Unmarshal(data, &converted); err != nil {
		return turn.Pinned{}, err
	}
	return turn.Pinned{Name: spec.Name, Version: "candidate-" + spec.Hash(), Digest: spec.Hash(), Spec: converted}, nil
}
