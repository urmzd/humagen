package runs

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// Search finds original requests and recorded answers, never prepared private
// context, reasoning or tool payloads. Replayed runs rebuild the derived index.
func (x *Index) Search(tenant, query string, since time.Time, limit int, keep func(Run) bool) []Run {
	x.mu.RLock()
	defer x.mu.RUnlock()
	ids := x.search.Search(query, 1000, func(id string) bool {
		r := x.runs[id]
		return r != nil && r.Tenant == tenant && r.Parent == "" && !r.CreatedAt.Before(since) && (keep == nil || keep(*r))
	})
	out := make([]Run, 0, len(ids))
	for _, id := range ids {
		out = append(out, citationSnapshot(x.runs[id]))
	}
	slices.SortFunc(out, func(a, b Run) int { return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), strings.Compare(a.ID, b.ID)) })
	return out[:min(len(out), min(max(limit, 1), 1000))]
}
