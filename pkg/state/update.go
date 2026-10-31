package state

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
)

// MaxTries bounds how often Retry runs a change that lost to another writer.
const MaxTries = 64

// ErrBusy reports a change that lost every try to other writers on the same
// records.
var ErrBusy = errors.New("state: too many writers on one record")

// ErrUnchanged is what an Update's function returns to write nothing. Update
// then returns the value as it was read, and no error.
var ErrUnchanged = errors.New("state: unchanged")

// Retry runs try until it returns anything but ErrConflict, at most MaxTries
// times, and returns ErrBusy when every try conflicted. Each try after the
// first waits a little longer, at random, so writers on one record spread
// out. try reads what it changes afresh every time: a conflict means what it
// read has moved on.
func Retry(try func() error) error {
	for n := range MaxTries {
		if n > 0 {
			time.Sleep(time.Duration(rand.Int64N(int64(min(n, 20)) * int64(time.Millisecond))))
		}
		err := try()
		if !errors.Is(err, ErrConflict) {
			return err
		}
	}
	return ErrBusy
}

// Update reads one record, passes fn its value and whether it exists, and
// writes what fn returns at the version it read: created when it was
// missing. When another writer moved the record in between, it reads again
// and calls fn again, so fn must not keep anything from a call that did not
// write. fn returning ErrUnchanged writes nothing. A nil Table writes nothing
// and calls fn once.
func (t *Table[T]) Update(tenant, key string, fn func(v T, ok bool) (T, error)) (T, error) {
	var out T
	if t == nil {
		var zero T
		v, err := fn(zero, false)
		if errors.Is(err, ErrUnchanged) {
			return zero, nil
		}
		return v, err
	}
	err := Retry(func() error {
		cur, err := t.Lookup(tenant, key)
		ok := err == nil
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		v, err := fn(cur.Value, ok)
		if errors.Is(err, ErrUnchanged) {
			out = cur.Value
			return nil
		}
		if err != nil {
			return err
		}
		b := t.Batch()
		if ok {
			b.PutAt(tenant, key, v, cur.Version)
		} else {
			b.Create(tenant, key, v)
		}
		if err := b.Commit(); err != nil {
			return err
		}
		out = v
		return nil
	})
	return out, err
}

// ErrTooMany reports a read that would return more records than its caller
// allows.
var ErrTooMany = errors.New("state: too many records")

// All reads every record of a tenant under a prefix, page by page, and
// fails with ErrTooMany rather than return more than most. A store calls it
// only for a collection it caps on write, so the bound is never met in use.
func (t *Table[T]) All(tenant, prefix string, most int) ([]Item[T], error) {
	if t == nil {
		return nil, nil
	}
	if most <= 0 {
		return nil, fmt.Errorf("%w: state: All names its bound", ErrInvalid)
	}
	var out []Item[T]
	after := ""
	for {
		page, next, err := t.List(tenant, Query{Prefix: prefix, After: after, Limit: min(most+1-len(out), MaxList)})
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(out) > most {
			return nil, fmt.Errorf("%w: %s %q under %q holds more than %d", ErrTooMany, t.namespace, tenant, prefix, most)
		}
		if next == "" {
			return out, nil
		}
		after = next
	}
}

// directory is the tenant that holds a namespace's directory of tenants, and
// tenantKey the prefix of each entry in it.
const (
	directory = ""
	tenantKey = "tenant:"
	// filled marks, in the directory, that tenants written before the
	// directory existed are in it.
	filled = "directory:v1"
)

// Tenants is the directory of the tenants a namespace holds records for: one
// entry per tenant, in the tenant "", which a background loop pages instead
// of reading the namespace across tenants. A store marks a tenant before its
// first write for it. A tenant with an entry and no records is possible, after
// a write that failed, and reads as a tenant with nothing in it.
type Tenants struct {
	st        Store
	namespace string
	// seen are tenants this process has marked: a cache that saves a write,
	// which may be dropped at any time.
	seen sync.Map
}

// OpenTenants returns a namespace's directory. The first time any process
// opens it, it adds every tenant the namespace already holds records for,
// reading the namespace once; a process that opens it at the same time adds
// the same entries.
func OpenTenants(st Store, namespace string) (*Tenants, error) {
	d := &Tenants{st: st, namespace: namespace}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	if _, err := st.Lookup(ctx, namespace, directory, filled); err == nil {
		return d, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	recs, err := st.Load(ctx, namespace)
	if err != nil {
		return nil, err
	}
	var ops []Op
	last := ""
	for _, r := range recs {
		if r.Tenant == directory || r.Tenant == last {
			continue
		}
		last = r.Tenant
		ops = append(ops, Put(directory, tenantKey+r.Tenant, []byte{}))
	}
	ops = append(ops, Put(directory, filled, []byte{}))
	for len(ops) > 0 {
		n := min(len(ops), MaxList)
		if err := st.Write(ctx, namespace, ops[:n]...); err != nil {
			return nil, err
		}
		ops = ops[n:]
	}
	return d, nil
}

// Mark adds a tenant to the directory. Marking one already there writes
// nothing.
func (d *Tenants) Mark(tenant string) error {
	if tenant == directory {
		return nil
	}
	if _, ok := d.seen.Load(tenant); ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	err := d.st.Write(ctx, d.namespace, Create(directory, tenantKey+tenant, []byte{}))
	if err != nil && !errors.Is(err, ErrConflict) {
		return err
	}
	d.seen.Store(tenant, true)
	return nil
}

// Page returns up to limit tenants after a cursor, in order, and the cursor
// of the next page, "" when none follow.
func (d *Tenants) Page(after string, limit int) ([]string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	q := Query{Prefix: tenantKey, Limit: limit}
	if after != "" {
		q.After = tenantKey + after
	}
	recs, next, err := d.st.List(ctx, d.namespace, directory, q)
	if err != nil {
		return nil, "", err
	}
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, strings.TrimPrefix(r.Key, tenantKey))
	}
	return out, strings.TrimPrefix(next, tenantKey), nil
}

// Each calls fn for every tenant in the directory, a page at a time.
func (d *Tenants) Each(fn func(tenant string) error) error {
	after := ""
	for {
		page, next, err := d.Page(after, MaxList)
		if err != nil {
			return err
		}
		for _, t := range page {
			if err := fn(t); err != nil {
				return err
			}
		}
		if next == "" {
			return nil
		}
		after = next
	}
}
