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

// GuardLearning commits a learning write only while the run's outcome is the
// one reviewed: it reads the run, and calls commit when its snapshot still
// matches. The callback may write only its destination store; it must not read
// or mutate this index.
func (x *Index) GuardLearning(tenant, id, snapshot string, commit func(Run) error) error {
	run, _, err := x.get(tenant, id)
	if err != nil {
		return err
	}
	if run.Status != Completed || run.Outcome == nil || snapshot == "" || LearningSnapshot(*run) != snapshot {
		return ErrLearningConflict
	}
	return commit(*run)
}
