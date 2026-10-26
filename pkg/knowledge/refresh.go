package knowledge

import (
	"slices"
	"strings"
	"time"
)

type RefreshCandidate struct {
	Tenant string
	Source Source
	DueAt  time.Time
}

// RefreshDue returns at most 200 eligible copies, ordered by oldest due time.
// Failed refreshes stop until an operator repairs and retries them.
func (s *Store) RefreshDue(at time.Time, limit int) []RefreshCandidate {
	return s.RefreshDueEligible(at, limit, nil)
}

// RefreshDueEligible filters standing authorization before the result bound, so
// revoked owners cannot starve other tenants' due copies. The filter must not
// call back into this store.
func (s *Store) RefreshDueEligible(at time.Time, limit int, eligible func(string, Source) bool) []RefreshCandidate {
	limit = min(max(limit, 1), 200)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []RefreshCandidate{}
	less := func(a, b RefreshCandidate) int {
		if n := a.DueAt.Compare(b.DueAt); n != 0 {
			return n
		}
		return strings.Compare(a.Tenant+"\x00"+a.Source.Name, b.Tenant+"\x00"+b.Source.Name)
	}
	for tenant, all := range s.tenants {
		for _, src := range all {
			lc := src.Lifecycle
			if lc == nil || lc.SyncEverySeconds == 0 || lc.Snapshot || lc.Provider == "manual" || lc.Mode == Live || lc.Status == NeedsAttention || active(lc) != "" {
				continue
			}
			since := lc.ConnectedAt
			if lc.LastGoodAt != nil {
				since = *lc.LastGoodAt
			}
			due := since.Add(time.Duration(lc.SyncEverySeconds) * time.Second)
			if at.Before(due) {
				continue
			}
			if eligible != nil && !eligible(tenant, cloneSource(src.Source)) {
				continue
			}
			candidate := RefreshCandidate{tenant, cloneSource(src.Source), due}
			pos, _ := slices.BinarySearchFunc(out, candidate, less)
			if pos >= limit {
				continue
			}
			out = slices.Insert(out, pos, candidate)
			if len(out) > limit {
				out = out[:limit]
			}
		}
	}
	return out
}
