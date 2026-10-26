package runs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

var ErrLearningConflict = errors.New("runs: the outcome changed; review it again")

// LearningSnapshot identifies the exact retained run reviewed by a human.
// It reveals neither accepted tags nor private execution metadata.
func LearningSnapshot(run Run) string {
	raw, err := json.Marshal(run)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// GuardLearning serializes a learning write with run mutations. The callback
// may write only its destination store; it must not read or mutate this index.
func (x *Index) GuardLearning(tenant, id, snapshot string, commit func(Run) error) error {
	x.mu.RLock()
	defer x.mu.RUnlock()
	run, ok := x.runs[id]
	if !ok || run.Tenant != tenant {
		return ErrNotFound
	}
	if run.Status != Completed || run.Outcome == nil || snapshot == "" || LearningSnapshot(*run) != snapshot {
		return ErrLearningConflict
	}
	return commit(*run)
}
