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
func (s *Store) RefreshDue(at time.Time, limit int) ([]RefreshCandidate, error) {
	return s.RefreshDueEligible(at, limit, nil)
}

// RefreshDueEligible filters standing authorization before the result bound, so
// revoked owners cannot starve other tenants' due copies. It reads every
// tenant's sources, a tenant at a time through the directory, each tenant
// capped at MaxSources.
func (s *Store) RefreshDueEligible(at time.Time, limit int, eligible func(string, Source) bool) ([]RefreshCandidate, error) {
	limit = min(max(limit, 1), 200)
	out := []RefreshCandidate{}
	less := func(a, b RefreshCandidate) int {
		if n := a.DueAt.Compare(b.DueAt); n != 0 {
			return n
		}
		return strings.Compare(a.Tenant+"\x00"+a.Source.Name, b.Tenant+"\x00"+b.Source.Name)
	}
	err := s.dir.Each(func(tenant string) error {
		all, _, err := s.sources(tenant)
		if err != nil {
			return err
		}
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
			if eligible != nil && !eligible(tenant, cloneSource(src)) {
				continue
			}
			candidate := RefreshCandidate{tenant, cloneSource(src), due}
			pos, _ := slices.BinarySearchFunc(out, candidate, less)
			if pos >= limit {
				continue
			}
			out = slices.Insert(out, pos, candidate)
			if len(out) > limit {
				out = out[:limit]
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
