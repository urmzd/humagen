package operations

import "time"

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

// AssignmentPage bounds internal reconciliation as well as public lists.
func (s *Store) AssignmentPage(t, after string, limit int, keep func(Assignment) bool) ([]Assignment, string) {
	if limit < 1 || limit > MaxResults {
		limit = MaxResults
	}
	all := s.Assignments(t, limit, func(a Assignment) bool { return a.ID > after && keep(a) })
	next := ""
	if len(all) == limit {
		next = all[len(all)-1].ID
	}
	return all, next
}

// ReserveSource keeps the consumed source version and deterministic run together.
func (s *Store) ReserveSource(t, id, run, tag string, version int, sourceVersion uint64, at time.Time) (Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.records[key{t, "assignment:" + id}]
	if r.Assignment == nil {
		return Assignment{}, ErrNotFound
	}
	a := *r.Assignment
	if a.State == InProgress && a.RunID == run {
		return a, nil
	}
	if a.Trigger != SourceTrigger || a.SourceVersion == sourceVersion || a.Version != version || !transition(a.State, InProgress) {
		return Assignment{}, ErrConflict
	}
	a.State, a.RunID, a.TagID, a.SourceVersion, a.Version, a.UpdatedAt = InProgress, run, tag, sourceVersion, a.Version+1, at
	return a, s.put(t, "assignment:"+id, record{Assignment: &a})
}
