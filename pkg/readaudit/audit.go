// Package readaudit records reads that require project governance.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds nothing in memory, so any number of processes on one database agree,
// and it does not need pkg/state's single-writer lock.
package readaudit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/state"
)

// namespace is where reads are recorded. The tenant is the shard key; within
// it:
//
//	r:<at>:<unique>  a record, ordered by when the read happened
//	count            how many records the tenant holds
//
// <at> is the instant as a key that sorts in time order, so a window is a
// range of keys, and <unique> is random, so two processes recording at the
// same instant never write one key.
const namespace = "read-audit"

const (
	recordPrefix = "r:"
	countKey     = "count"
	// converted marks, in the tenant "", that records kept before this
	// layout, one per global sequence number, have been moved into it.
	converted = "converted:v1"
	// retainBatch is the most records one retention write removes.
	retainBatch = 1000
)

// MaxRecords bounds retained records per tenant. At capacity, exceptional reads fail closed.
const MaxRecords = 100000

// MaxRows is the most records one List returns.
const MaxRows = 500

// MaxWindow is the longest window one List reads.
const MaxWindow = 31 * 24 * time.Hour

// Record identifies a reader, the place read, the route and the instant.
type Record struct {
	Subject string    `json:"subject"`
	Project string    `json:"project"`
	Thread  string    `json:"thread,omitempty"`
	Route   string    `json:"route"`
	At      time.Time `json:"at"`
}

// Store records reads in a state.Store.
type Store struct {
	st      state.Store
	tenants *state.Tenants
}

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps records in this process's memory.
//
// Records kept by a release before this layout are moved into it the first
// time any process opens the store; a process that opens it at the same
// time finds them moved.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	s := &Store{st: st}
	if err := s.convert(); err != nil {
		return nil, fmt.Errorf("read audit: convert: %w", err)
	}
	tenants, err := state.OpenTenants(st, namespace)
	if err != nil {
		return nil, fmt.Errorf("read audit: tenants: %w", err)
	}
	s.tenants = tenants
	return s, nil
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// timeKey formats an instant as a key that sorts in time order, the sign
// bit flipped so instants before 1970 sort first.
func timeKey(t time.Time) string {
	return fmt.Sprintf("%020d", uint64(t.UnixNano())^(1<<63))
}

func recordKey(at time.Time, unique string) string {
	return recordPrefix + timeKey(at) + ":" + unique
}

func unique() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func valid(tenant string, r Record) bool {
	return tenant != "" && len(tenant) <= 512 && !strings.ContainsRune(tenant, 0) &&
		r.Subject != "" && r.Route != "" && !r.At.IsZero() &&
		len(r.Subject) <= 512 && len(r.Project) <= 256 && len(r.Thread) <= 256 && len(r.Route) <= 512 &&
		utf8.ValidString(tenant) && utf8.ValidString(r.Subject) && utf8.ValidString(r.Project) &&
		utf8.ValidString(r.Thread) && utf8.ValidString(r.Route)
}

// convert moves records kept under a global sequence number into the layout
// by time. Each record moves in one write conditional on its old key, so a
// record two processes move at once is moved once.
func (s *Store) convert() error {
	c, cancel := ctx()
	_, err := s.st.Lookup(c, namespace, "", converted)
	cancel()
	if err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	c, cancel = ctx()
	recs, err := s.st.Load(c, namespace)
	cancel()
	if err != nil {
		return err
	}
	for _, rec := range recs {
		if rec.Tenant == "" || strings.HasPrefix(rec.Key, recordPrefix) || rec.Key == countKey {
			continue
		}
		var r Record
		if err := json.Unmarshal(rec.Value, &r); err != nil {
			return fmt.Errorf("%q/%q: %w", rec.Tenant, rec.Key, err)
		}
		if !valid(rec.Tenant, r) {
			return fmt.Errorf("%q/%q: invalid retained record", rec.Tenant, rec.Key)
		}
		c, cancel := ctx()
		err := s.st.Write(c, namespace,
			state.DeleteAt(rec.Tenant, rec.Key, rec.Version),
			state.Create(rec.Tenant, recordKey(r.At, "s"+rec.Key), rec.Value))
		cancel()
		if err != nil && !errors.Is(err, state.ErrConflict) {
			return err
		}
	}
	c, cancel = ctx()
	defer cancel()
	err = s.st.Write(c, namespace, state.Create("", converted, []byte("1")))
	if err != nil && !errors.Is(err, state.ErrConflict) {
		return err
	}
	return nil
}

// count reads how many records a tenant holds, and the version of that
// count: 0 when none is kept yet, in which case it counts them, reading at
// most MaxRecords.
func (s *Store) count(tenant string) (int, int64, error) {
	c, cancel := ctx()
	r, err := s.st.Lookup(c, namespace, tenant, countKey)
	cancel()
	if err == nil {
		var n int
		if err := json.Unmarshal(r.Value, &n); err != nil {
			return 0, 0, fmt.Errorf("read audit: count: %w", err)
		}
		return n, r.Version, nil
	}
	if !errors.Is(err, state.ErrNotFound) {
		return 0, 0, err
	}
	n := 0
	after := ""
	for {
		c, cancel := ctx()
		recs, next, err := s.st.List(c, namespace, tenant, state.Query{Prefix: recordPrefix, After: after, Limit: state.MaxList})
		cancel()
		if err != nil {
			return 0, 0, err
		}
		n += len(recs)
		if next == "" || n > MaxRecords {
			return n, 0, nil
		}
		after = next
	}
}

// countOp writes a tenant's count at the version read.
func countOp(tenant string, n int, version int64) state.Op {
	raw, _ := json.Marshal(n)
	if version == 0 {
		return state.Create(tenant, countKey, raw)
	}
	return state.PutAt(tenant, countKey, raw, version)
}

// Add writes before permitting the read. Invalid or unrecordable reads fail closed.
func (s *Store) Add(tenant string, r Record) error {
	if !valid(tenant, r) {
		return errors.New("read audit: invalid record")
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := s.tenants.Mark(tenant); err != nil {
		return err
	}
	return state.Retry(func() error {
		n, version, err := s.count(tenant)
		if err != nil {
			return err
		}
		if n >= MaxRecords {
			return errors.New("read audit: capacity reached")
		}
		id, err := unique()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		return s.st.Write(c, namespace, countOp(tenant, n+1, version), state.Create(tenant, recordKey(r.At, id), b))
	})
}

// List returns newest first within a window of at most 31 days and at most 500 rows.
func (s *Store) List(tenant string, from, to time.Time, limit int) ([]Record, error) {
	return s.ListVisible(tenant, from, to, limit, nil)
}

// ListVisible applies confinement before selecting the bounded newest rows.
// It pages the window from its newest record down, so it reads no more than
// the tenant holds, which MaxRecords bounds.
func (s *Store) ListVisible(tenant string, from, to time.Time, limit int, visible func(Record) bool) ([]Record, error) {
	out := []Record{}
	if !from.Before(to) || to.Sub(from) > MaxWindow {
		return out, nil
	}
	limit = min(max(limit, 1), MaxRows)
	floor := recordPrefix + timeKey(from)
	after := recordPrefix + timeKey(to)
	for {
		c, cancel := ctx()
		recs, next, err := s.st.List(c, namespace, tenant, state.Query{Prefix: recordPrefix, After: after, Limit: state.MaxList, Desc: true})
		cancel()
		if err != nil {
			return nil, err
		}
		for _, rec := range recs {
			if rec.Key < floor {
				return out, nil
			}
			var r Record
			if err := json.Unmarshal(rec.Value, &r); err != nil {
				return nil, fmt.Errorf("read audit: %q: %w", rec.Key, err)
			}
			if visible != nil && !visible(r) {
				continue
			}
			out = append(out, r)
			if len(out) == limit {
				return out, nil
			}
		}
		if next == "" {
			return out, nil
		}
		after = next
	}
}

// Retain removes records from before cut, oldest first, in bounded writes.
// Each write removes its records at the versions read and lowers the count
// with them, so two processes retaining at once remove each record once.
func (s *Store) Retain(tenant string, cut time.Time) error {
	end := recordPrefix + timeKey(cut)
	for {
		done := false
		err := state.Retry(func() error {
			c, cancel := ctx()
			recs, _, err := s.st.List(c, namespace, tenant, state.Query{Prefix: recordPrefix, Limit: retainBatch})
			cancel()
			if err != nil {
				return err
			}
			var ops []state.Op
			for _, rec := range recs {
				if rec.Key >= end {
					break
				}
				ops = append(ops, state.DeleteAt(tenant, rec.Key, rec.Version))
			}
			if len(ops) == 0 {
				done = true
				return nil
			}
			n, version, err := s.count(tenant)
			if err != nil {
				return err
			}
			ops = append(ops, countOp(tenant, max(n-len(ops), 0), version))
			c, cancel = ctx()
			defer cancel()
			return s.st.Write(c, namespace, ops...)
		})
		if err != nil || done {
			return err
		}
	}
}

// EachTenant calls fn for every workspace that has recorded a read, a page
// at a time.
func (s *Store) EachTenant(fn func(tenant string) error) error { return s.tenants.Each(fn) }
