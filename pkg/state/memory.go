package state

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"sync"
)

// Memory is a Store in the process's memory. It is lost with the process, and
// exists so that a store built on it behaves exactly as it does on Postgres,
// and so that a test can rebuild a store from what an earlier one wrote.
type Memory struct {
	mu sync.RWMutex
	by map[string]map[[2]string]entry // namespace -> (tenant, key) -> record
}

type entry struct {
	value   []byte
	version int64
}

// NewMemory returns an empty store.
func NewMemory() *Memory { return &Memory{by: map[string]map[[2]string]entry{}} }

var _ Store = (*Memory)(nil)

// Get implements Store.
func (m *Memory) Get(ctx context.Context, namespace, tenant, key string) ([]byte, error) {
	r, err := m.Lookup(ctx, namespace, tenant, key)
	return r.Value, err
}

// Lookup implements Store.
func (m *Memory) Lookup(ctx context.Context, namespace, tenant, key string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.by[namespace][[2]string{tenant, key}]
	if !ok {
		return Record{}, ErrNotFound
	}
	return Record{Tenant: tenant, Key: key, Value: bytes.Clone(e.value), Version: e.version}, nil
}

// List implements Store.
func (m *Memory) List(ctx context.Context, namespace, tenant string, q Query) ([]Record, string, error) {
	limit, err := checkQuery(namespace, q)
	if err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Record
	for k, e := range m.by[namespace] {
		if k[0] != tenant || !strings.HasPrefix(k[1], q.Prefix) {
			continue
		}
		if q.After != "" && (!q.Desc && k[1] <= q.After || q.Desc && k[1] >= q.After) {
			continue
		}
		out = append(out, Record{Tenant: tenant, Key: k[1], Value: e.value, Version: e.version})
	}
	slices.SortFunc(out, func(a, b Record) int {
		if q.Desc {
			return strings.Compare(b.Key, a.Key)
		}
		return strings.Compare(a.Key, b.Key)
	})
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = out[limit-1].Key
	}
	for i := range out {
		out[i].Value = bytes.Clone(out[i].Value)
	}
	return out, next, nil
}

// Load implements Store.
func (m *Memory) Load(ctx context.Context, namespace string) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Record, 0, len(m.by[namespace]))
	for k, e := range m.by[namespace] {
		out = append(out, Record{Tenant: k[0], Key: k[1], Value: bytes.Clone(e.value), Version: e.version})
	}
	slices.SortFunc(out, func(a, b Record) int {
		if c := strings.Compare(a.Tenant, b.Tenant); c != 0 {
			return c
		}
		return strings.Compare(a.Key, b.Key)
	})
	return out, nil
}

// Write implements Store.
func (m *Memory) Write(ctx context.Context, namespace string, ops ...Op) error {
	if err := checkNames(namespace, ops); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ns := m.by[namespace]
	// The ops apply in order to a copy of the keys they touch, so a later op
	// sees an earlier one and a failed condition leaves nothing behind.
	staged := map[[2]string]*entry{}
	read := func(k [2]string) *entry {
		if e, ok := staged[k]; ok {
			return e
		}
		if e, ok := ns[k]; ok {
			return &entry{value: e.value, version: e.version}
		}
		return nil
	}
	for _, op := range ops {
		k := [2]string{op.Tenant, op.Key}
		cur := read(k)
		switch {
		case op.If == Missing && cur != nil,
			op.If > 0 && (cur == nil || cur.version != op.If):
			return ErrConflict
		}
		if op.Value == nil {
			staged[k] = nil
			continue
		}
		next := &entry{value: bytes.Clone(op.Value), version: 1}
		if cur != nil {
			next.version = cur.version + 1
		}
		staged[k] = next
	}
	if ns == nil {
		ns = map[[2]string]entry{}
		m.by[namespace] = ns
	}
	for k, e := range staged {
		if e == nil {
			delete(ns, k)
			continue
		}
		ns[k] = *e
	}
	return nil
}
