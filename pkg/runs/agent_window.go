package runs

import (
	"slices"
	"time"
)

// AgentWindow returns at most limit visible runs of one agent in a bounded
// time window, newest first. Visibility is applied before the row cap.
func (x *Index) AgentWindow(tenant, agent string, from, to time.Time, limit int, visible func(Run) bool) []Run {
	if to.Before(from) || to.Sub(from) > 31*24*time.Hour || visible == nil {
		return []Run{}
	}
	if limit < 1 || limit > 200 {
		limit = 200
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	out := []Run{}
	for _, id := range slices.Backward(x.order) {
		r := x.runs[id]
		if r.Tenant != tenant || r.Agent != agent || r.CreatedAt.Before(from) || r.CreatedAt.After(to) || !visible(*r) {
			continue
		}
		out = append(out, *r)
		if len(out) == limit {
			break
		}
	}
	return out
}
