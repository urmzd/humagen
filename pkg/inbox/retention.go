package inbox

import (
	"fmt"
	"time"

	"github.com/urmzd/humagen/pkg/state"
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

// maxCut is the most records one step of Retain deletes, and maxDrop the
// most messages one step drops. A test lowers them to take many steps.
var (
	maxCut  = 5000
	maxDrop = 100
)

// cutKey is the key an older release kept a tenant's cut under, beside its
// changes.
const cutKey = "cut"

// cut is what an older release's retention kept of the messages it
// dropped: the number of the last id issued, the keys of what was posted
// once, and messages whose records were still being deleted.
type cut struct {
	Next    int      `json:"next"`
	Gone    []string `json:"gone,omitempty"`
	Pending []string `json:"pending,omitempty"`
}

// settledBefore reports whether retention drops a message: resolved,
// dismissed or withdrawn for everyone before a time, with no question in it
// that a run still waits on.
func settledBefore(m *Message, before time.Time) bool {
	return retainable(m) && m.UpdatedAt.Before(before)
}

// Retain drops what was settled for everyone before a time from a tenant's
// inbox, and returns how many messages it dropped. A message goes when it
// was resolved, dismissed or withdrawn before it and no question in it is
// one a run still waits on, with every record kept about it: its raise, its
// outcomes, each reader's marks and each client's receipts. Nothing open or
// snoozed goes, and a broadcast that readers hid for themselves stays while
// it is open for anyone. What must not happen twice outlives it: no message
// id is issued again, and the key of an announcement posted once (PostOnce)
// is never posted again. The receipts and keys of what is kept still find
// it.
//
// It pages the index of settled messages, oldest first, and works in
// steps: one write drops up to maxDrop messages, each at the version it
// read, and marks each for cleanup ("g:"); later writes delete what remains
// of them, at most maxCut records each. Two processes retaining one tenant
// at once drop each message once. A step that cannot be written returns
// the error, and the next pass carries on.
func (s *Store) Retain(tenantName string, before time.Time) (int, error) {
	dropped := 0
	for {
		more, err := s.clean(tenantName)
		if err != nil {
			return dropped, err
		}
		if more {
			continue
		}
		n, more, err := s.dropStep(tenantName, before)
		dropped += n
		if err != nil || !more {
			if err == nil {
				err = s.prunePosts(tenantName, before)
			}
			return dropped, err
		}
	}
}

// dropStep drops up to maxDrop messages settled before a time, and reports
// whether more may be left.
func (s *Store) dropStep(tenantName string, before time.Time) (int, bool, error) {
	floor := "e:" + state.Seq(stamp(before))
	var ids []string
	capped, err := scan(s.st, tenantName, "e:", "", false, max(maxDrop, 1), func(r state.Record) (bool, error) {
		if r.Key >= floor {
			return false, nil
		}
		ids = append(ids, idOfKey(r.Key))
		return true, nil
	})
	if err != nil {
		return 0, false, fmt.Errorf("inbox: retain: %w", err)
	}
	if len(ids) == 0 {
		return 0, false, nil
	}
	n := 0
	err = state.Retry(func() error {
		n = 0
		x := newTx(s.st, tenantName)
		for _, id := range ids {
			ok, err := x.drop(id, before)
			if err != nil {
				return err
			}
			if ok {
				n++
			}
		}
		return x.commit()
	})
	if err != nil {
		return 0, false, fmt.Errorf("inbox: retain: %w", err)
	}
	return n, capped || n > 0, nil
}

// drop takes one message out of the inbox, when it is still settled before
// a time, and marks what remains of it for cleanup.
func (x *tx) drop(id string, before time.Time) (bool, error) {
	m, err := x.message(id)
	if err != nil {
		return false, err
	}
	if m == nil {
		// Dropped by another process; its index entry went with it.
		return false, nil
	}
	if !settledBefore(m, before) {
		return false, nil
	}
	at := stamp(m.At)
	x.del(allKey(at, id))
	x.del(liveKey(at, id))
	x.del(retainKey(m))
	x.del(ptrKey(id))
	var ref keyRef
	if ok, err := x.get(keyKey(m.Key), &ref); err != nil {
		return false, err
	} else if ok && ref.ID == id {
		if m.Once {
			x.put(keyKey(m.Key), keyRef{Key: m.Key, Gone: true})
		} else {
			x.del(keyKey(m.Key))
		}
	}
	for client, d := range m.Deliveries {
		if err := x.unpost(client, d, id); err != nil {
			return false, err
		}
	}
	x.put(garbageKey(id), struct{}{})
	// The message is gone: flush writes nothing more of it.
	delete(x.msgs, id)
	return true, nil
}

// clean deletes up to maxCut records of messages already dropped, and
// reports whether it deleted any.
func (s *Store) clean(tenantName string) (bool, error) {
	var gone []string
	if _, err := scan(s.st, tenantName, "g:", "", false, 10, func(r state.Record) (bool, error) {
		gone = append(gone, idOfKey(r.Key))
		return true, nil
	}); err != nil {
		return false, fmt.Errorf("inbox: retain: %w", err)
	}
	if len(gone) == 0 {
		return false, nil
	}
	budget := max(maxCut, 2)
	var ops []state.Op
	for _, id := range gone {
		for _, prefix := range []string{changePrefix(id), markPrefix(id)} {
			if budget-len(ops) <= 1 {
				break
			}
			if _, err := scan(s.st, tenantName, prefix, "", false, budget-len(ops)-1, func(r state.Record) (bool, error) {
				ops = append(ops, state.Delete(tenantName, r.Key))
				return true, nil
			}); err != nil {
				return false, fmt.Errorf("inbox: retain: %w", err)
			}
		}
		if budget-len(ops) <= 1 {
			break
		}
		// Everything of it is in this write: its mark goes too.
		ops = append(ops, state.Delete(tenantName, garbageKey(id)))
	}
	c, cancel := ctx()
	defer cancel()
	if err := s.st.Write(c, namespace, ops...); err != nil {
		return false, fmt.Errorf("inbox: retain: %w", err)
	}
	return true, nil
}

// prunePosts deletes the day counts of announcements from before a time,
// which no cap reads again.
func (s *Store) prunePosts(tenantName string, before time.Time) error {
	floor := dayKey(dayOf(before).AddDate(0, 0, -1))
	var ops []state.Op
	if _, err := scan(s.st, tenantName, "p:", "", false, state.MaxList, func(r state.Record) (bool, error) {
		if r.Key >= floor {
			return false, nil
		}
		ops = append(ops, state.Delete(tenantName, r.Key))
		return true, nil
	}); err != nil {
		return fmt.Errorf("inbox: retain: %w", err)
	}
	if len(ops) == 0 {
		return nil
	}
	c, cancel := ctx()
	defer cancel()
	if err := s.st.Write(c, namespace, ops...); err != nil {
		return fmt.Errorf("inbox: retain: %w", err)
	}
	return nil
}
