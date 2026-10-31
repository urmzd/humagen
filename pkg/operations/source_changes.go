package operations

import (
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// SourceChanges reads resident durable source metadata, never the network.
// Versions advance only on changed retained content and must not be reused.
type SourceChanges interface {
	SourceChangeVersion(tenant, name string) (version uint64, fresh bool)
}

const (
	ManualTrigger = "manual"
	CronTrigger   = "cron"
	SourceTrigger = "source_change"
)

// AssignmentPage bounds internal reconciliation as well as public lists: up
// to limit assignments keep keeps, in ID order, after the one named after
// ("" for the first), and the ID to continue from when the page is full.
func (s *Store) AssignmentPage(t, after string, limit int, keep func(Assignment) bool) ([]Assignment, string, error) {
	if limit < 1 || limit > MaxResults {
		limit = MaxResults
	}
	from := ""
	if after != "" {
		from = "assignment:" + after
	}
	out := []Assignment{}
	err := s.scan(t, "assignment:", from, func(_ string, r record) (bool, error) {
		if r.Assignment != nil && keep != nil && keep(*r.Assignment) {
			out = append(out, *r.Assignment)
		}
		return len(out) < limit, nil
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) == limit {
		next = out[len(out)-1].ID
	}
	return out, next, nil
}

// ReserveSource keeps the consumed source version and deterministic run together.
func (s *Store) ReserveSource(t, id, run, tag string, version int, sourceVersion uint64, at time.Time) (Assignment, error) {
	return s.reserve(t, id, func(r record) (record, error) {
		a := *r.Assignment
		if a.State == InProgress && a.RunID == run {
			return record{}, state.ErrUnchanged
		}
		if a.Trigger != SourceTrigger || a.SourceVersion == sourceVersion || a.Version != version || !transition(a.State, InProgress) {
			return record{}, ErrConflict
		}
		a.State, a.RunID, a.TagID, a.SourceVersion, a.Version, a.UpdatedAt = InProgress, run, tag, sourceVersion, a.Version+1, at
		return record{Assignment: &a}, nil
	})
}
