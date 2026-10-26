package operations

import (
	"maps"
	"slices"
	"time"

	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
)

const MetaAssignment = "humagen.assignment"

func validInvocation(t string, a Assignment, tag envelope.Tag) bool {
	return tag.Tenant == t && tag.Origin == "cron" && tag.Agent == a.Agent && a.Cron != "" && tag.Meta[MetaAssignment] == a.ID && tag.Meta["cron_schedule"] == a.Schedule && tag.Validate() == nil
}

func cloneInvocation(tag envelope.Tag) envelope.Tag {
	tag.Meta = maps.Clone(tag.Meta)
	tag.Deliver = slices.Clone(tag.Deliver)
	for i := range tag.Deliver {
		tag.Deliver[i].Kinds = slices.Clone(tag.Deliver[i].Kinds)
	}
	return tag
}

// Invocation returns only the exact stored pre-acceptance clock occurrence.
// It is internal recovery state, never part of a reader's assignment DTO.
func (s *Store) Invocation(t, id string) (envelope.Tag, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r := s.records[key{t, "assignment:" + id}]
	if r.Invocation == nil {
		return envelope.Tag{}, false
	}
	return cloneInvocation(*r.Invocation), true
}

// ReserveInvocation keeps the complete clock tag in the same write as its
// run reservation. A retry may reuse it only through ordinary fresh admission.
func (s *Store) ReserveInvocation(t, id string, tag envelope.Tag, version int, at time.Time) (Assignment, error) {
	if tag.Tenant != t || tag.Origin != "cron" || tag.Validate() != nil {
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
	return a, s.put(t, "assignment:"+id, record{Assignment: &a, Invocation: &tag})
}
