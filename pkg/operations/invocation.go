package operations

import (
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/state"
)

const MetaAssignment = "humagen.assignment"

func validInvocation(t string, a Assignment, tag event.Envelope) bool {
	return tag.Tenant == t && wire.Origin(tag) == "cron" && wire.AgentOf(tag) == a.Agent && a.Cron != "" && wire.Meta(tag)[MetaAssignment] == a.ID && wire.SurfaceOf(tag).CronSchedule == a.Schedule && tag.Validate() == nil
}

func cloneInvocation(tag event.Envelope) event.Envelope { return wire.Clone(tag) }

// Invocation returns only the exact stored pre-acceptance clock occurrence.
// It is internal recovery state, never part of a reader's assignment DTO.
func (s *Store) Invocation(t, id string) (event.Envelope, bool, error) {
	r, _, _, err := s.get(t, "assignment:"+id)
	if err != nil || r.Invocation == nil {
		return event.Envelope{}, false, err
	}
	return cloneInvocation(r.Invocation.Event), true, nil
}

// ReserveInvocation keeps the complete clock tag in the same write as its
// run reservation. A retry may reuse it only through ordinary fresh admission.
func (s *Store) ReserveInvocation(t, id string, tag event.Envelope, version int, at time.Time) (Assignment, error) {
	if tag.Tenant != t || wire.Origin(tag) != "cron" || tag.Validate() != nil {
		return Assignment{}, ErrInvalid
	}
	run := runtime.RunID(t, tag.ID)
	return s.reserve(t, id, func(r record) (record, error) {
		a := *r.Assignment
		if a.State == InProgress {
			if a.RunID == run && r.Invocation != nil {
				return record{}, state.ErrUnchanged
			}
			return record{}, ErrConflict
		}
		if a.Version != version || !validInvocation(t, a, tag) || !transition(a.State, InProgress) {
			return record{}, ErrConflict
		}
		tag := cloneInvocation(tag)
		a.State, a.RunID, a.TagID, a.Version, a.UpdatedAt = InProgress, run, tag.ID, a.Version+1, at
		return record{Assignment: &a, Invocation: &legacy.Stored{Event: tag}}, nil
	})
}
