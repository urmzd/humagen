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
	by map[string]map[[2]string][]byte // namespace -> (tenant, key) -> value
}

// NewMemory returns an empty store.
func NewMemory() *Memory { return &Memory{by: map[string]map[[2]string][]byte{}} }

var _ Store = (*Memory)(nil)

// Get implements Store.
func (m *Memory) Get(ctx context.Context, namespace, tenant, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.by[namespace][[2]string{tenant, key}]
	if !ok {
		return nil, ErrNotFound
	}
	return bytes.Clone(v), nil
}

// Load implements Store.
func (m *Memory) Load(ctx context.Context, namespace string) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Record, 0, len(m.by[namespace]))
	for k, v := range m.by[namespace] {
		out = append(out, Record{Tenant: k[0], Key: k[1], Value: bytes.Clone(v)})
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
	if ns == nil {
		ns = map[[2]string][]byte{}
		m.by[namespace] = ns
	}
	for _, op := range ops {
		k := [2]string{op.Tenant, op.Key}
		if op.Value == nil {
			delete(ns, k)
			continue
		}
		ns[k] = bytes.Clone(op.Value)
	}
	return nil
}
