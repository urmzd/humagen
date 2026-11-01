package runs

import "time"

// AgentWindow returns at most limit visible runs of one agent in a bounded
// time window, newest first. Visibility is applied before the row cap.
func (x *Index) AgentWindow(tenant, agent string, from, to time.Time, limit int, visible func(Run) bool) []Run {
	if to.Before(from) || to.Sub(from) > 31*24*time.Hour || visible == nil {
		return []Run{}
	}
	if limit < 1 || limit > 200 {
		limit = 200
	}
	out := []Run{}
	err := x.scan(tenant, func(r *Run) bool {
		if r.CreatedAt.Before(from.Add(-scanSlack)) {
			return false
		}
		if r.Agent != agent || r.CreatedAt.Before(from) || r.CreatedAt.After(to) || !visible(*r) {
			return true
		}
		out = append(out, citationSnapshot(r))
		return len(out) < limit
	})
	if err != nil {
		x.log.Error("runs: could not read an agent's runs", "tenant", tenant, "agent", agent, "error", err)
	}
	return out
}
