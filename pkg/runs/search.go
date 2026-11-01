package runs

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/internal/textindex"
)

// Search finds original requests and recorded answers, never prepared private
// context, reasoning or tool payloads. It reads a tenant's runs from the
// newest back to since, at most MaxScan of them, and returns the turns whose
// words hold every word of the query, newest first.
func (x *Index) Search(tenant, query string, since time.Time, limit int, keep func(Run) bool) []Run {
	limit = min(max(limit, 1), 1000)
	var out []Run
	err := x.scan(tenant, func(r *Run) bool {
		if r.CreatedAt.Before(since.Add(-scanSlack)) {
			return false
		}
		if r.Parent == "" && !r.CreatedAt.Before(since) && textindex.Matches(r.Text+"\n"+r.Answer, query) && (keep == nil || keep(*r)) {
			out = append(out, citationSnapshot(r))
		}
		return len(out) < 1000
	})
	if err != nil {
		x.log.Error("runs: could not search runs", "tenant", tenant, "error", err)
	}
	slices.SortFunc(out, func(a, b Run) int { return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), strings.Compare(a.ID, b.ID)) })
	return out[:min(len(out), limit)]
}
