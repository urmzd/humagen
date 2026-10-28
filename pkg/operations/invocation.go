package operations

import (
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
)

const MetaAssignment = "humagen.assignment"

func validInvocation(t string, a Assignment, tag event.Envelope) bool {
	return tag.Tenant == t && wire.Origin(tag) == "cron" && wire.AgentOf(tag) == a.Agent && a.Cron != "" && wire.Meta(tag)[MetaAssignment] == a.ID && wire.SurfaceOf(tag).CronSchedule == a.Schedule && tag.Validate() == nil
}

func cloneInvocation(tag event.Envelope) event.Envelope { return wire.Clone(tag) }

// Invocation returns only the exact stored pre-acceptance clock occurrence.
// It is internal recovery state, never part of a reader's assignment DTO.
func (s *Store) Invocation(t, id string) (event.Envelope, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r := s.records[key{t, "assignment:" + id}]
	if r.Invocation == nil {
		return event.Envelope{}, false
	}
	return cloneInvocation(r.Invocation.Event), true
}

// ReserveInvocation keeps the complete clock tag in the same write as its
// run reservation. A retry may reuse it only through ordinary fresh admission.
func (s *Store) ReserveInvocation(t, id string, tag event.Envelope, version int, at time.Time) (Assignment, error) {
	if tag.Tenant != t || wire.Origin(tag) != "cron" || tag.Validate() != nil {
		return Assignment{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.records[key{t, "assignment:" + id}]
	if r.Assignment == nil {
		return Assignment{}, ErrNotFound
	}
	a := *r.Assignment
	run := runtime.RunID(t, tag.ID)
	if a.State == InProgress {
		if a.RunID == run && r.Invocation != nil {
			return a, nil
		}
		return Assignment{}, ErrConflict
	}
	if a.Version != version || !validInvocation(t, a, tag) || !transition(a.State, InProgress) {
		return Assignment{}, ErrConflict
	}
	tag = cloneInvocation(tag)
	a.State, a.RunID, a.TagID, a.Version, a.UpdatedAt = InProgress, run, tag.ID, a.Version+1, at
	return a, s.put(t, "assignment:"+id, record{Assignment: &a, Invocation: &legacy.Stored{Event: tag}})
}
