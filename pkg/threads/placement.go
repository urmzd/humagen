package threads

import (
	"github.com/urmzd/humagen/pkg/state"
	"time"
)

// MaxMoves bounds the immutable placement history of one conversation.
const MaxMoves = 100

// PlacementChange is internal history, committed atomically with the thread.
// It is intentionally absent from public thread responses.
type PlacementChange struct {
	By          string    `json:"by,omitempty"`
	Thread      string    `json:"thread"`
	Version     int       `json:"version"`
	FromProject string    `json:"from_project"`
	FromChannel string    `json:"from_channel"`
	Project     string    `json:"project"`
	Channel     string    `json:"channel"`
	At          time.Time `json:"at"`
}

type loadedPlacement struct {
	tenant string
	change PlacementChange
}

func placementKey(thread string, version int) string {
	return keyPlacement + thread + ":" + state.Seq(int64(version))
}

// UpdateMovingAs records the actor with an atomic private-conversation move.
func (s *Store) UpdateMovingAs(tenant, id string, version int, by string, at time.Time, fn func(*Thread) error) (Thread, error) {
	return s.update(tenant, id, version, at, func(t *Thread) error {
		if err := fn(t); err != nil {
			return err
		}
		t.movedBy = by
		return nil
	}, true)
}
