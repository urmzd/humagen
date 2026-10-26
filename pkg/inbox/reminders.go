package inbox

import (
	"sort"
	"time"
)

// Remind updates one still-pending item once. The same cause keeps its ID,
// audience and parts. Its write-through change resets read marks and survives
// replay; settling and retention remove it with the ordinary message.
func (s *Store) Remind(tenant, id string, at time.Time) (Message, bool, error) {
	return s.do(Change{Tenant: tenant, Message: id, What: Reminded, By: "system", At: at})
}

// OpenPage bounds reconciliation and rotates past long-lived pending items.
func (s *Store) OpenPage(tenant, after string, limit int) ([]Message, string) {
	if limit < 1 || limit > 200 {
		limit = 200
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	selected := make([]*Message, 0, limit)
	for _, message := range s.of(tenant).messages {
		if message.ID <= after || (message.State != Open && message.State != Snoozed) {
			continue
		}
		i := sort.Search(len(selected), func(i int) bool { return selected[i].ID >= message.ID })
		if i >= limit {
			continue
		}
		if len(selected) < limit {
			selected = append(selected, nil)
		}
		copy(selected[i+1:], selected[i:])
		selected[i] = message
	}
	out := make([]Message, 0, len(selected))
	for _, message := range selected {
		out = append(out, message.clone())
	}
	next := ""
	if len(out) == limit {
		next = out[len(out)-1].ID
	}
	return out, next
}
