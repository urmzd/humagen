package state

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Timeout bounds one write or load a Table makes. The stores that use a
// Table are called without a context, from request handlers and from the
// platform's event loop alike, so the bound is here.
const Timeout = 30 * time.Second

// Table is one namespace of a Store holding values of one type as JSON. A nil
// Table keeps nothing and never fails, which is how a store built without a
// Store behaves as it always has.
type Table[T any] struct {
	store     Store
	namespace string
}

// NewTable returns a namespace of st. A nil st gives a nil Table.
func NewTable[T any](st Store, namespace string) *Table[T] {
	if st == nil {
		return nil
	}
	return &Table[T]{store: st, namespace: namespace}
}

// Enabled reports whether the table writes anywhere.
func (t *Table[T]) Enabled() bool { return t != nil }

// Put stores a value under a key.
func (t *Table[T]) Put(tenant, key string, v T) error {
	if t == nil {
		return nil
	}
	b := t.Batch()
	b.Put(tenant, key, v)
	return b.Commit()
}

// Delete removes a key.
func (t *Table[T]) Delete(tenant, key string) error {
	if t == nil {
		return nil
	}
	b := t.Batch()
	b.Delete(tenant, key)
	return b.Commit()
}

// Load calls fn for every record in the namespace, in the order Store.Load
// gives.
func (t *Table[T]) Load(fn func(tenant, key string, v T) error) error {
	if t == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	recs, err := t.store.Load(ctx, t.namespace)
	if err != nil {
		return err
	}
	for _, r := range recs {
		var v T
		if err := json.Unmarshal(r.Value, &v); err != nil {
			return fmt.Errorf("state: %s %q/%q: %w", t.namespace, r.Tenant, r.Key, err)
		}
		if err := fn(r.Tenant, r.Key, v); err != nil {
			return err
		}
	}
	return nil
}

// Item is one record of a Table, read per request.
type Item[T any] struct {
	Key     string
	Value   T
	Version int64
}

// Lookup reads one record and its version, or ErrNotFound. A nil Table has
// none.
func (t *Table[T]) Lookup(tenant, key string) (Item[T], error) {
	if t == nil {
		return Item[T]{}, ErrNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	r, err := t.store.Lookup(ctx, t.namespace, tenant, key)
	if err != nil {
		return Item[T]{}, err
	}
	return t.decode(r)
}

// List reads one page of a tenant's records, and the cursor of the next (see
// Store.List). A nil Table has none.
func (t *Table[T]) List(tenant string, q Query) ([]Item[T], string, error) {
	if t == nil {
		return nil, "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	recs, next, err := t.store.List(ctx, t.namespace, tenant, q)
	if err != nil {
		return nil, "", err
	}
	out := make([]Item[T], 0, len(recs))
	for _, r := range recs {
		it, err := t.decode(r)
		if err != nil {
			return nil, "", err
		}
		out = append(out, it)
	}
	return out, next, nil
}

func (t *Table[T]) decode(r Record) (Item[T], error) {
	it := Item[T]{Key: r.Key, Version: r.Version}
	if err := json.Unmarshal(r.Value, &it.Value); err != nil {
		return Item[T]{}, fmt.Errorf("state: %s %q/%q: %w", t.namespace, r.Tenant, r.Key, err)
	}
	return it, nil
}

// Batch starts a set of changes written together.
func (t *Table[T]) Batch() *Batch[T] { return &Batch[T]{t: t} }

// Batch is changes to one Table, written all or none by Commit. A batch from
// a nil Table collects nothing and commits without error.
type Batch[T any] struct {
	t   *Table[T]
	ops []Op
	err error
}

// Put adds a put.
func (b *Batch[T]) Put(tenant, key string, v T) {
	if b.t == nil || b.err != nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		b.err = fmt.Errorf("state: %s %q/%q: %w", b.t.namespace, tenant, key, err)
		return
	}
	b.ops = append(b.ops, Put(tenant, key, raw))
}

// Delete adds a delete.
func (b *Batch[T]) Delete(tenant, key string) {
	if b.t == nil {
		return
	}
	b.ops = append(b.ops, Delete(tenant, key))
}

// Create adds a put that requires the key to have no record.
func (b *Batch[T]) Create(tenant, key string, v T) { b.putIf(tenant, key, v, Missing) }

// PutAt adds a put that requires the record to be at version.
func (b *Batch[T]) PutAt(tenant, key string, v T, version int64) {
	if b.t == nil {
		return
	}
	if version < 1 {
		b.fail(fmt.Errorf("%w: state: %s %q/%q: a version is at least 1", ErrInvalid, b.t.namespace, tenant, key))
		return
	}
	b.putIf(tenant, key, v, version)
}

// DeleteAt adds a delete that requires the record to be at version.
func (b *Batch[T]) DeleteAt(tenant, key string, version int64) {
	if b.t == nil {
		return
	}
	if version < 1 {
		b.fail(fmt.Errorf("%w: state: %s %q/%q: a version is at least 1", ErrInvalid, b.t.namespace, tenant, key))
		return
	}
	b.ops = append(b.ops, DeleteAt(tenant, key, version))
}

func (b *Batch[T]) putIf(tenant, key string, v T, version int64) {
	n := len(b.ops)
	b.Put(tenant, key, v)
	if len(b.ops) > n {
		b.ops[n].If = version
	}
}

func (b *Batch[T]) fail(err error) {
	if b.err == nil {
		b.err = err
	}
}

// Len is how many changes the batch holds.
func (b *Batch[T]) Len() int { return len(b.ops) }

// Commit writes the batch. A conditional change whose record moved returns
// ErrConflict, and nothing is written.
func (b *Batch[T]) Commit() error {
	if b.t == nil {
		return nil
	}
	if b.err != nil {
		return b.err
	}
	if len(b.ops) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	return b.t.store.Write(ctx, b.t.namespace, b.ops...)
}

// Seq formats a sequence number as a key that sorts in numeric order.
func Seq(n int64) string { return fmt.Sprintf("%020d", n) }
