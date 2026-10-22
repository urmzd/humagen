package inbox

import (
	"fmt"
	"slices"
	"time"

	"github.com/urmzd/warp/pkg/state"
)

// Retention bounds.
const (
	// DefaultRetention is how long what was settled is kept: 90 days.
	DefaultRetention = 90 * 24 * time.Hour
	// MinRetention is the shortest retention a workspace may set: 7 days,
	// so a week of what was settled is always there to look back on, and
	// the day's announcements always count toward the day's caps.
	MinRetention = 7 * 24 * time.Hour
)

// maxCut is the most operations one step of Retain writes, cut included. A
// test lowers it to take many steps.
var maxCut = 5000

// cutKey is the key a tenant's cut is kept under, beside its changes, whose
// keys are all digits and sort before it.
const cutKey = "cut"

// cut is what retention keeps of the messages it dropped: the number of the
// last id issued, so that none is issued again, and the keys of what was
// posted once (PostOnce), so that none is posted again.
type cut struct {
	Next int      `json:"next"`
	Gone []string `json:"gone,omitempty"`
	// Pending messages are already absent. Replay skips their records until
	// bounded cleanup deletes the last of them and clears Pending.
	Pending []string `json:"pending,omitempty"`
}

// restore takes up what a tenant's cut keeps.
func (t *tenant) restore(c cut) {
	t.cut = cut{Next: c.Next, Gone: slices.Clone(c.Gone), Pending: slices.Clone(c.Pending)}
	t.next = max(t.next, c.Next)
}

// settledBefore reports whether retention drops a message: resolved,
// dismissed or withdrawn for everyone before a time, with no question in it
// that a run still waits on.
func settledBefore(m *Message, before time.Time) bool {
	if m.State != Resolved && m.State != Dismissed || !m.UpdatedAt.Before(before) {
		return false
	}
	return !slices.ContainsFunc(m.Parts, func(p Part) bool { return p.Question != nil && p.State == PartOpen })
}

// Retain drops what was settled for everyone before a time from a tenant's
// inbox, and from its record in the state.Store, and returns how many
// messages it dropped. A message goes when it was resolved, dismissed or
// withdrawn before it and no question in it is one a run still waits on,
// with every change recorded about it: its raise, its outcomes, each
// reader's marks and each client's receipts. Nothing open or snoozed goes,
// and a broadcast that readers hid for themselves stays while it is open for
// anyone. What must not happen twice outlives it: no message id is issued
// again, and the key of an announcement posted once (PostOnce) is never
// posted again. The receipts and keys of what is kept still find it.
//
// It works in steps of at most maxCut operations, including the cut. Each
// step deletes records and writes the cut in one write, all or none, and
// changes the inbox only once that is written, so replay rebuilds what the
// inbox holds. A step that cannot be written leaves the inbox as that step
// found it and returns the error. A cut names a message whose remaining
// records await cleanup, so replay never brings it back between steps.
func (s *Store) Retain(tenantName string, before time.Time) (int, error) {
	dropped := 0
	for {
		n, more, err := s.retainStep(tenantName, before)
		dropped += n
		if err != nil || !more {
			return dropped, err
		}
	}
}

// retainStep drops what one step of Retain may, and reports whether more is
// left to drop.
func (s *Store) retainStep(name string, before time.Time) (int, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tenants[name]
	if t == nil {
		return 0, false, nil
	}
	if len(t.garbage) > 0 {
		return s.cleanStep(name, t)
	}
	records := map[string]int{}
	var order []*Message
	for _, m := range t.messages {
		if settledBefore(m, before) {
			records[m.ID] = 0
			order = append(order, m)
		}
	}
	if len(order) == 0 {
		return 0, false, nil
	}
	for _, c := range t.log {
		if _, ok := records[c.Message]; ok {
			records[c.Message]++
		}
	}
	drop := map[string]bool{}
	next := cut{Next: t.next, Gone: slices.Clone(t.cut.Gone)}
	size, more := 0, false
	for _, m := range order {
		if size > 0 && size+records[m.ID] >= maxCut {
			more = true
			break
		}
		size += records[m.ID]
		drop[m.ID] = true
		if m.Once && !slices.Contains(next.Gone, m.Key) {
			next.Gone = append(next.Gone, m.Key)
		}
	}
	slices.Sort(next.Gone)
	b := s.kept.Batch()
	var garbage []int64
	deletes := 0
	for i, c := range t.log {
		if drop[c.Message] {
			if deletes < maxCut-1 {
				b.Delete(name, state.Seq(t.seqs[i]))
				deletes++
			} else {
				garbage = append(garbage, t.seqs[i])
				if !slices.Contains(next.Pending, c.Message) {
					next.Pending = append(next.Pending, c.Message)
				}
			}
		}
	}
	b.Put(name, cutKey, storedChange{Cut: &next})
	if err := b.Commit(); err != nil {
		return 0, false, fmt.Errorf("inbox: retain: %w", err)
	}
	t.drop(drop)
	t.cut = next
	t.garbage = garbage
	return len(drop), more || len(garbage) > 0, nil
}

// cleanStep deletes another bounded slice of a cut message's history. The
// durable pending ids make every intermediate state replayable.
func (s *Store) cleanStep(name string, t *tenant) (int, bool, error) {
	n := min(len(t.garbage), maxCut-1)
	next := t.cut
	if n == len(t.garbage) {
		next.Pending = nil
	}
	b := s.kept.Batch()
	for _, seq := range t.garbage[:n] {
		b.Delete(name, state.Seq(seq))
	}
	b.Put(name, cutKey, storedChange{Cut: &next})
	if err := b.Commit(); err != nil {
		return 0, false, fmt.Errorf("inbox: retain: %w", err)
	}
	t.cut = next
	t.garbage = t.garbage[n:]
	return 0, true, nil
}

// drop takes messages out of a tenant, with every change about them. Each
// is settled, so it is in no count.
func (t *tenant) drop(ids map[string]bool) {
	rekey := map[string]bool{}
	kept := make([]*Message, 0, len(t.messages)-len(ids))
	for _, m := range t.messages {
		if !ids[m.ID] {
			kept = append(kept, m)
			continue
		}
		t.leave(m)
		delete(t.byID, m.ID)
		if t.byKey[m.Key] == m {
			delete(t.byKey, m.Key)
			rekey[m.Key] = true
		}
		delete(t.read, m.ID)
		delete(t.hidden, m.ID)
	}
	t.messages = kept
	// A key is the latest kept message's raised under it, if any is.
	for _, m := range t.messages {
		if rekey[m.Key] {
			t.byKey[m.Key] = m
		}
	}
	log, seqs := make([]Change, 0, len(t.log)), make([]int64, 0, len(t.seqs))
	for i, c := range t.log {
		if !ids[c.Message] {
			log, seqs = append(log, c), append(seqs, t.seqs[i])
		}
	}
	t.log, t.seqs = log, seqs
	t.rebuildPosts()
}
