package threads

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// Search returns matching messages from a bounded time window. Each thread's
// word index is updated only after a successful write and rebuilt on Open.
func (s *Store) Search(tenantName, query string, since time.Time, limit int, keep func(Thread) bool) []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tn := s.tenants[tenantName]
	if tn == nil {
		return nil
	}
	limit = min(max(limit, 1), 1000)
	out := []Message{}
	ids := tn.search.Search(query, limit, func(id string) bool {
		thread, mid, _ := strings.Cut(id, "\x00")
		th := tn.threads[thread]
		i := th.message(mid)
		return i >= 0 && !th.msgs[i].At.Before(since) && (keep == nil || keep(th.rec))
	})
	for _, id := range ids {
		thread, mid, _ := strings.Cut(id, "\x00")
		th := tn.threads[thread]
		if i := th.message(mid); i >= 0 {
			out = append(out, th.msgs[i].clone())
		}
	}
	slices.SortFunc(out, func(a, b Message) int {
		return cmp.Or(b.At.Compare(a.At), strings.Compare(a.Thread+":"+a.ID, b.Thread+":"+b.ID))
	})
	return out[:min(len(out), limit)]
}
