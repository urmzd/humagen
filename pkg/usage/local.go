package usage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/state"
)

// The counts of a ledger without shared counters are records in its store
// (see keys.go), changed by compare-and-set: an entry is one Write of the
// entry and every count it moves, each at the version it was read at, and a
// Write that lost to another writer is read and made again (state.Retry).

// change is one try at a Write in one tenant: what it read, at which
// version, and what it will write.
type change struct {
	st     state.Store
	tenant string
	// read is the version each key was read at, Missing when it had no
	// record.
	read map[string]int64
	// staged is what will be written, nil for a delete, in the order keys
	// were first staged.
	staged map[string][]byte
	order  []string
}

func newChange(st state.Store, tenant string) *change {
	return &change{st: st, tenant: tenant, read: map[string]int64{}, staged: map[string][]byte{}}
}

// get reads a key into v, through what is staged, and reports whether it
// has a value.
func (c *change) get(key string, v any) (bool, error) {
	if raw, ok := c.staged[key]; ok {
		if raw == nil {
			return false, nil
		}
		return true, json.Unmarshal(raw, v)
	}
	ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	r, err := c.st.Lookup(ctx, namespace, c.tenant, key)
	if errors.Is(err, state.ErrNotFound) {
		c.read[key] = state.Missing
		return false, nil
	}
	if err != nil {
		return false, err
	}
	c.read[key] = r.Version
	return true, json.Unmarshal(r.Value, v)
}

func (c *change) stage(key string, raw []byte) {
	if _, ok := c.staged[key]; !ok {
		c.order = append(c.order, key)
	}
	c.staged[key] = raw
}

func (c *change) put(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.stage(key, raw)
	return nil
}

func (c *change) del(key string) { c.stage(key, nil) }

// ops are the staged writes, each at the version its key was read at.
func (c *change) ops() []state.Op {
	out := make([]state.Op, 0, len(c.order))
	for _, k := range c.order {
		raw := c.staged[k]
		v, read := c.read[k]
		switch {
		case raw == nil && read && v == state.Missing:
			// Deleting what was not there.
		case raw == nil && read:
			out = append(out, state.DeleteAt(c.tenant, k, v))
		case raw == nil:
			out = append(out, state.Delete(c.tenant, k))
		case read && v == state.Missing:
			out = append(out, state.Create(c.tenant, k, raw))
		case read:
			out = append(out, state.PutAt(c.tenant, k, raw, v))
		default:
			out = append(out, state.Put(c.tenant, k, raw))
		}
	}
	return out
}

func (c *change) commit(extra ...state.Op) error {
	ops := append(c.ops(), extra...)
	if len(ops) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	return c.st.Write(ctx, namespace, ops...)
}

// next is the next place for an entry in the tenant.
func (c *change) next() (int64, error) {
	var h headRec
	if _, err := c.get(headKey, &h); err != nil {
		return 0, err
	}
	h.N++
	return h.N, c.put(headKey, h)
}

// entry stages an entry at the next place.
func (c *change) entry(e Entry, added *Spend) error {
	seq, err := c.next()
	if err != nil {
		return err
	}
	ops, err := entryOps(e, added, seq)
	if err != nil {
		return err
	}
	for _, op := range ops {
		c.stage(op.Key, op.Value)
	}
	return nil
}

// admit stages an admission's counts: a run started in each of its scopes
// on its day, and, when the run is not already in flight, one more in
// flight in each.
func (c *change) admit(e Entry) error {
	a := admissionOf(e)
	var cur activeRun
	inFlightAlready, err := c.get(activeKey(e.RunID), &cur)
	if err != nil {
		return err
	}
	day := dayKey(e.At)
	for _, k := range a.scopes() {
		var d dayCount
		if _, err := c.get(dayCountKey(k, day), &d); err != nil {
			return err
		}
		d.Runs++
		if err := c.put(dayCountKey(k, day), d); err != nil {
			return err
		}
		if inFlightAlready {
			continue
		}
		var n inFlight
		if _, err := c.get(inFlightKey(k), &n); err != nil {
			return err
		}
		n.N++
		if err := c.put(inFlightKey(k), n); err != nil {
			return err
		}
	}
	if inFlightAlready {
		return nil
	}
	return c.put(activeKey(e.RunID), activeRun{Project: e.Project, Within: e.Within, Agent: e.Agent, Client: e.Client})
}

// end stages a run's leaving flight, and reports false when it was not in
// flight: an end seen twice is kept once.
func (c *change) end(run string) (bool, error) {
	var cur activeRun
	ok, err := c.get(activeKey(run), &cur)
	if err != nil || !ok {
		return false, err
	}
	a := admission{project: cur.Project, within: cur.Within, agent: cur.Agent, client: cur.Client}
	for _, k := range a.scopes() {
		var n inFlight
		if _, err := c.get(inFlightKey(k), &n); err != nil {
			return false, err
		}
		n.N = max(n.N-1, 0)
		if err := c.put(inFlightKey(k), n); err != nil {
			return false, err
		}
	}
	c.del(activeKey(run))
	return true, nil
}

// spend stages a Spent or Settled entry's counts and reports what it adds,
// and false when its key was already counted.
func (c *change) spend(e Entry) (Spend, bool, error) {
	var k keyRec
	if seen, err := c.get(spentKey(e.Key), &k); err != nil || seen {
		return Spend{}, false, err
	}
	if err := c.put(spentKey(e.Key), keyRec{At: e.At}); err != nil {
		return Spend{}, false, err
	}
	var r runRec
	if _, err := c.get(runKey(e.RunID), &r); err != nil {
		return Spend{}, false, err
	}
	before := r.counted()
	if e.Kind == Spent {
		r.Live = r.Live.add(*e.Spend)
	} else {
		r.Settled = *e.Spend
	}
	if e.At.After(r.Last) {
		r.Last = e.At
	}
	added := r.counted().beyond(before)
	if err := c.put(runKey(e.RunID), r); err != nil {
		return Spend{}, false, err
	}
	if added.IsZero() {
		return added, true, nil
	}
	day := dayKey(e.At)
	for _, s := range admissionOf(e).scopes() {
		var d dayCount
		if _, err := c.get(dayCountKey(s, day), &d); err != nil {
			return Spend{}, false, err
		}
		d.Spend = d.Spend.add(added)
		if err := c.put(dayCountKey(s, day), d); err != nil {
			return Spend{}, false, err
		}
	}
	return added, true, nil
}

// guard is a precondition a conversion adds to a Write: the ops that make
// the Write happen once, or false when it has already happened.
type guard func() ([]state.Op, bool, error)

// recordLocal records an entry in the store's counts. It reports what the
// entry added, and false for a key already counted, or a guard that says
// the entry is already kept.
func recordLocal(st state.Store, e Entry, g guard) (Spend, bool, error) {
	var added Spend
	var ok bool
	err := state.Retry(func() error {
		added, ok = Spend{}, false
		var extra []state.Op
		if g != nil {
			ops, goOn, err := g()
			if err != nil || !goOn {
				return err
			}
			extra = ops
		}
		c := newChange(st, e.Tenant)
		var counted *Spend
		switch e.Kind {
		case Admitted:
			if err := c.admit(e); err != nil {
				return err
			}
		case Ended:
			if was, err := c.end(e.RunID); err != nil || !was {
				return err
			}
		case Spent, Settled:
			a, fresh, err := c.spend(e)
			if err != nil || !fresh {
				return err
			}
			added, counted = a, &a
		}
		if err := c.entry(e, counted); err != nil {
			return err
		}
		if err := c.commit(extra...); err != nil {
			return err
		}
		ok = true
		return nil
	})
	if err != nil {
		return Spend{}, false, err
	}
	return added, ok, nil
}

// admitLocal takes an admission's quotas and records it, as one Write: a
// quota is read at the version the admission writes, so another admission
// in between makes this one read again.
func admitLocal(st state.Store, e Entry, quotas []Quota) (bool, *Quota, error) {
	var full *Quota
	var ok bool
	err := state.Retry(func() error {
		full, ok = nil, false
		c := newChange(st, e.Tenant)
		today := dayKey(e.At)
		for _, q := range quotas {
			k := scopeKey(q.Scope)
			var used int
			if q.Kind == QuotaConcurrent {
				var n inFlight
				if _, err := c.get(inFlightKey(k), &n); err != nil {
					return err
				}
				used = n.N
				// Written back as it is, so the Write fails if it moved.
				if err := c.put(inFlightKey(k), n); err != nil {
					return err
				}
			} else {
				days, err := dayCounts(st, e.Tenant, k)
				if err != nil {
					return err
				}
				since := dayKey(q.Since)
				for day, d := range days {
					if day >= since && day != today {
						used += d.Runs
					}
				}
				var d dayCount
				if _, err := c.get(dayCountKey(k, today), &d); err != nil {
					return err
				}
				if today >= since {
					used += d.Runs
				}
				if err := c.put(dayCountKey(k, today), d); err != nil {
					return err
				}
			}
			if used+1 > q.Max {
				full = &q
				return nil
			}
		}
		if err := c.admit(e); err != nil {
			return err
		}
		if err := c.entry(e, nil); err != nil {
			return err
		}
		if err := c.commit(); err != nil {
			return err
		}
		ok = true
		return nil
	})
	if err != nil {
		return false, nil, err
	}
	return ok, full, nil
}

// dayCounts reads a scope's counts by day. A scope has one per day the
// ledger keeps, so the read is bounded by retention.
func dayCounts(st state.Store, tenant, scope string) (map[string]dayCount, error) {
	prefix := dayCountPrefix(scope)
	items, err := state.NewTable[dayCount](st, namespace).All(tenant, prefix, maxDays)
	if err != nil {
		return nil, err
	}
	out := make(map[string]dayCount, len(items))
	for _, it := range items {
		if day, ok := dayOf(it.Key, prefix); ok {
			out[day] = it.Value
		}
	}
	return out, nil
}

// usageLocal is what a scope has used since the start of a UTC day, from
// the store's counts.
func usageLocal(st state.Store, tenant string, scope policy.Scope, since time.Time) (policy.Usage, error) {
	var u policy.Usage
	k := scopeKey(scope)
	days, err := dayCounts(st, tenant, k)
	if err != nil {
		return u, err
	}
	from := dayKey(since)
	var sum Spend
	for day, d := range days {
		if day >= from {
			u.Runs += d.Runs
			sum = sum.add(d.Spend)
		}
	}
	n, err := state.NewTable[inFlight](st, namespace).Lookup(tenant, inFlightKey(k))
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return u, err
	}
	u.Concurrent = n.Value.N
	u.Spend, u.Tokens, u.ModelCalls, u.UnpricedCalls, u.OtherCosts = sum.Micros, sum.Tokens, sum.ModelCalls, sum.Unpriced, sorted(sum.Other)
	return u, nil
}

func runSpendLocal(st state.Store, tenant, run string) (Spend, error) {
	r, err := state.NewTable[runRec](st, namespace).Lookup(tenant, runKey(run))
	if errors.Is(err, state.ErrNotFound) {
		return Spend{}, nil
	}
	if err != nil {
		return Spend{}, err
	}
	return r.Value.counted(), nil
}
