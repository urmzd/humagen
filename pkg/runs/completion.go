package runs

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// A terminal transition writes this record in the same transaction. It remains
// until every completion action has succeeded, including across a process crash.
type completion struct {
	Run   string          `json:"run"`
	Done  map[string]bool `json:"done,omitempty"`
	Owner string          `json:"owner,omitempty"`
	Until time.Time       `json:"until,omitzero"`
}

func completionKey(seq int64) string { return "z:" + state.Seq(seq) }

// PendingCompletions rotates a bounded page per tenant so a failing old run
// cannot starve later completions. A restart begins from the oldest page again.
func (x *Index) PendingCompletions() ([]Run, error) {
	x.completionMu.Lock()
	defer x.completionMu.Unlock()
	if x.completionAfter == nil {
		x.completionAfter = map[string]string{}
	}
	var out []Run
	err := x.tenants.Each(func(tenant string) error {
		recs, next, err := x.page(tenant, state.Query{Prefix: "z:", After: x.completionAfter[tenant], Limit: 100})
		if err != nil {
			return err
		}
		seqs := make([]int64, len(recs))
		for i, r := range recs {
			seqs[i] = seqOf(r.Key)
		}
		rs, err := x.all(tenant, seqs)
		if err != nil {
			return err
		}
		x.completionAfter[tenant] = next
		for _, r := range rs {
			out = append(out, citationSnapshot(r))
		}
		return nil
	})
	return out, err
}

// Completion holds a fenced, renewable lease over a run's completion actions.
// Actions must be idempotent: a crash can happen after an effect and before Ack.
type Completion struct {
	x                  *Index
	tenant, key, owner string
	done               map[string]bool
}

const completionLease = 2 * time.Minute

// ClaimCompletion claims outstanding work, or returns nil if done or leased.
func (x *Index) ClaimCompletion(ctx context.Context, run Run, now time.Time) (*Completion, error) {
	seq, err := x.seq(run.Tenant, run.ID)
	if err != nil {
		return nil, err
	}
	if seq == 0 {
		return nil, ErrNotFound
	}
	c := &Completion{x: x, tenant: run.Tenant, key: completionKey(seq), owner: rand.Text()}
	claimed := false
	err = state.Retry(func() error {
		rec, err := x.st.Lookup(ctx, namespace, c.tenant, c.key)
		if errors.Is(err, state.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var v completion
		if err = json.Unmarshal(rec.Value, &v); err != nil {
			return err
		}
		if v.Owner != "" && now.Before(v.Until) {
			return nil
		}
		v.Owner, v.Until = c.owner, now.Add(completionLease)
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if err = x.st.Write(ctx, namespace, state.PutAt(c.tenant, c.key, raw, rec.Version)); err != nil {
			return err
		}
		c.done, claimed = v.Done, true
		return nil
	})
	if !claimed {
		return nil, err
	}
	return c, err
}

// Done reports whether an earlier attempt acknowledged the named action.
func (c *Completion) Done(action string) bool { return c.done[action] }

func (c *Completion) change(ctx context.Context, fn func(*completion)) error {
	return state.Retry(func() error {
		rec, err := c.x.st.Lookup(ctx, namespace, c.tenant, c.key)
		if err != nil {
			return err
		}
		var v completion
		if err = json.Unmarshal(rec.Value, &v); err != nil {
			return err
		}
		if v.Owner != c.owner {
			return errors.New("runs: completion lease lost")
		}
		fn(&v)
		var op state.Op
		if v.Run == "" {
			op = state.DeleteAt(c.tenant, c.key, rec.Version)
		} else {
			raw, err := json.Marshal(v)
			if err != nil {
				return err
			}
			op = state.PutAt(c.tenant, c.key, raw, rec.Version)
		}
		return c.x.st.Write(ctx, namespace, op)
	})
}

// Renew extends the lease before performing an action.
func (c *Completion) Renew(ctx context.Context, now time.Time) error {
	return c.change(ctx, func(v *completion) { v.Until = now.Add(completionLease) })
}

// Ack keeps the success of an action before the next one starts.
func (c *Completion) Ack(ctx context.Context, action string) error {
	return c.change(ctx, func(v *completion) {
		if v.Done == nil {
			v.Done = map[string]bool{}
		}
		v.Done[action] = true
	})
}

// Release makes failed work available immediately to the reconciler.
func (c *Completion) Release(ctx context.Context) error {
	return c.change(ctx, func(v *completion) { v.Owner = ""; v.Until = time.Time{} })
}

// Finish removes pending work only after all actions were acknowledged.
func (c *Completion) Finish(ctx context.Context) error {
	return c.change(ctx, func(v *completion) { v.Run = "" })
}
