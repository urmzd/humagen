package inbox

import (
	"slices"
	"strconv"
	"time"
)

// markOf reads one reader's marks on a message.
func (x *tx) markOf(id, subject string) (mark, error) {
	k := mark{Subject: subject}
	_, err := x.get(markKey(id, subject), &k)
	return k, err
}

func (x *tx) putMark(id string, k mark) {
	if k.empty() {
		x.del(markKey(id, k.Subject))
		return
	}
	x.put(markKey(id, k.Subject), k)
}

// marks reads every reader's marks on a message.
func (x *tx) marks(id string) ([]mark, error) {
	items, err := x.list(markPrefix(id), maxMarks)
	if err != nil {
		return nil, err
	}
	out := make([]mark, 0, len(items))
	for _, it := range items {
		var k mark
		if err := unmarshal(it, &k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}

// record keeps a change that took effect, as the next change about its
// message.
func (x *tx) record(m *Message, c Change) {
	m.changes++
	x.put(changeKey(m.ID, m.changes), storedOf(c))
}

// apply is the only thing that alters what the inbox holds. It reads and
// writes through x, so a change that loses to another writer is applied
// again to what that writer left. It reports the message the change is
// about and whether everyone sees it changed.
func (x *tx) apply(c Change) (*Message, bool, error) {
	if c.What == Raised {
		return x.raise(c)
	}
	m, err := x.message(c.Message)
	if m == nil || err != nil {
		return nil, false, err
	}
	if m.Broadcast && (c.What == DidDismiss || c.What == DidSnooze || c.What == DidUnhide) {
		return m, false, x.hideFor(m, c)
	}
	switch c.What {
	case Reminded:
		if m.reminded || m.Broadcast || settled(m) || c.At.Before(m.UpdatedAt) || (m.State == Snoozed && c.At.Before(m.SnoozedUntil)) {
			return m, false, nil
		}
		if err := x.leave(m); err != nil {
			return nil, false, err
		}
		marks, err := x.marks(m.ID)
		if err != nil {
			return nil, false, err
		}
		for _, k := range marks {
			if k.Read {
				k.Read = false
				x.putMark(m.ID, k)
			}
		}
		m.State, m.SnoozedUntil, m.reminded = Open, time.Time{}, true
		if err := x.join(m); err != nil {
			return nil, false, err
		}
	case MarkRead, MarkUnread:
		k, err := x.markOf(m.ID, c.By)
		if err != nil {
			return nil, false, err
		}
		if k.Read == (c.What == MarkRead) {
			return m, false, nil
		}
		wasOut := isOut(m, k)
		k.Read = c.What == MarkRead
		x.putMark(m.ID, k)
		if err := x.remark(m, k, wasOut); err != nil {
			return nil, false, err
		}
		x.record(m, c)
		// Reading changes nothing anyone else sees.
		return m, false, nil
	case Delivered:
		ok, err := x.deliver(m, c)
		if err != nil || !ok {
			return m, false, err
		}
		x.record(m, c)
		return m, false, nil
	case DidResolve:
		i := slices.IndexFunc(m.Parts, func(p Part) bool { return p.ID == c.Part })
		if i < 0 || m.Parts[i].State != PartOpen {
			return m, false, nil
		}
		m.Parts[i].State = PartResolved
		m.Parts[i].Outcome = &Outcome{Action: c.Action, By: c.By, At: c.At, Note: c.Note}
		if !slices.ContainsFunc(m.Parts, func(p Part) bool { return p.State == PartOpen }) {
			if err := x.restate(m, Resolved, time.Time{}); err != nil {
				return nil, false, err
			}
		}
	case DidDismiss, DidWithdraw:
		if settled(m) {
			return m, false, nil
		}
		// A withdrawal from before it recorded who reads as the system's.
		if c.What == DidWithdraw && c.By == "" {
			c.By = "system"
		}
		if err := x.restate(m, Dismissed, time.Time{}); err != nil {
			return nil, false, err
		}
	case DidSnooze:
		if settled(m) {
			return m, false, nil
		}
		if err := x.restate(m, Snoozed, c.Until); err != nil {
			return nil, false, err
		}
	default:
		return m, false, nil
	}
	m.UpdatedAt = c.At
	x.record(m, c)
	x.notices = append(x.notices, Notice{ID: m.ID, Kind: m.Kind, State: m.State, UpdatedAt: m.UpdatedAt})
	return m, true, nil
}

// raise applies a Raised change. A change replayed from an older record
// names the id it was given, which is kept.
func (x *tx) raise(c Change) (*Message, bool, error) {
	if c.Raised == nil {
		return nil, false, nil
	}
	var ref keyRef
	if ok, err := x.get(keyKey(c.Raised.Key), &ref); err != nil {
		return nil, false, err
	} else if ok && ref.ID != "" {
		prior, err := x.message(ref.ID)
		if err != nil {
			return nil, false, err
		}
		if prior != nil && !settled(prior) {
			return nil, false, nil
		}
	}
	var h head
	if _, err := x.get(headKey, &h); err != nil {
		return nil, false, err
	}
	id := ""
	if n, ok := num(c.Message); ok {
		h.Next, id = max(h.Next, n), c.Message
	} else {
		h.Next++
		id = "m-" + strconv.FormatInt(h.Next, 10)
	}
	x.put(headKey, h)
	m := c.Raised.clone()
	m.ID, m.reminded, m.changes = id, false, 0
	m.Tenant, m.At, m.UpdatedAt, m.State, m.Unread, m.Hidden, m.Deliveries, m.SnoozedUntil = x.tenant, c.At, c.At, Open, false, false, nil, time.Time{}
	for i := range m.Parts {
		m.Parts[i].ID = "p" + strconv.Itoa(i+1)
		m.Parts[i].Actions, m.Parts[i].Outcome = nil, nil
		if m.Parts[i].State == "" {
			m.Parts[i].State = PartOpen
		}
	}
	x.raised(&m)
	x.put(keyKey(m.Key), keyRef{Key: m.Key, ID: m.ID})
	if m.Kind == KindAnnouncement && m.Broadcast {
		var p postedRec
		day := dayKey(dayOf(c.At))
		if _, err := x.get(day, &p); err != nil {
			return nil, false, err
		}
		if p.By == nil {
			p.By = map[string]int{}
		}
		p.Total++
		p.By[m.Source.Project]++
		x.put(day, p)
	}
	if err := x.join(&m); err != nil {
		return nil, false, err
	}
	c.Message = m.ID
	x.record(&m, c)
	x.notices = append(x.notices, Notice{ID: m.ID, Kind: m.Kind, State: m.State, UpdatedAt: m.UpdatedAt})
	return &m, true, nil
}

// restate moves a message to a state everyone sees, and its count with it.
func (x *tx) restate(m *Message, st State, until time.Time) error {
	if err := x.leave(m); err != nil {
		return err
	}
	m.State, m.SnoozedUntil = st, until
	return x.join(m)
}

// hideFor records one reader dismissing, snoozing or unhiding a broadcast
// for themselves. It changes nothing anyone else sees, so it tells no
// watcher. A broadcast already closed for everyone, a dismiss or snooze of
// one the reader dismissed, a snooze to the time it is already snoozed
// until, an unhide of one the reader never set aside, or a change with no
// reader, is not recorded.
func (x *tx) hideFor(m *Message, c Change) error {
	if c.By == "" || settled(m) {
		return nil
	}
	k, err := x.markOf(m.ID, c.By)
	if err != nil {
		return err
	}
	wasOut := isOut(m, k)
	if c.What == DidUnhide {
		if k.Hide == nil {
			return nil
		}
		k.Hide = nil
	} else {
		if k.Hide != nil && k.Hide.State == Dismissed {
			return nil
		}
		h := hide{State: Dismissed}
		if c.What == DidSnooze {
			h = hide{State: Snoozed, Until: c.Until}
		}
		if k.Hide != nil && k.Hide.State == h.State && k.Hide.Until.Equal(h.Until) {
			return nil
		}
		k.Hide = &h
	}
	x.putMark(m.ID, k)
	if err := x.remark(m, k, wasOut); err != nil {
		return err
	}
	x.record(m, c)
	return nil
}

// stateFor is a message's state for one reader at a moment, given their
// marks: a broadcast as the reader set it aside, and a snooze that has run
// out open. A zero now leaves a snooze as it is. own reports that the state
// is the reader's own hide or snooze of a broadcast, which nobody else has.
func stateFor(m *Message, k mark, now time.Time) (st State, until time.Time, own bool) {
	st, until = m.State, m.SnoozedUntil
	if k.Hide != nil && m.Broadcast && (st == Open || st == Snoozed) {
		st, until, own = k.Hide.State, k.Hide.Until, true
	}
	if st == Snoozed && !now.IsZero() && !now.Before(until) {
		return Open, time.Time{}, false
	}
	return st, until, own
}

// view is a message as one reader sees it.
func view(m *Message, k mark, now time.Time) Message {
	out := m.clone()
	out.State, out.SnoozedUntil, out.Hidden = stateFor(m, k, now)
	out.Unread = !k.Read
	return out
}
