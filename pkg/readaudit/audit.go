// Package readaudit records reads that require project governance.
package readaudit

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/state"
)

const namespace = "read-audit"

// MaxRecords bounds retained records per tenant. At capacity, exceptional reads fail closed.
const MaxRecords = 100000

// Record identifies a reader, the place read, the route and the instant.
type Record struct {
	Subject string    `json:"subject"`
	Project string    `json:"project"`
	Thread  string    `json:"thread,omitempty"`
	Route   string    `json:"route"`
	At      time.Time `json:"at"`
}
type entry struct {
	key    string
	record Record
}

// Store is a write-through record store, serialized with retention.
type Store struct {
	mu   sync.Mutex
	st   state.Store
	rows map[string][]entry
	seq  uint64
}

// Open replays retained records. A nil backend keeps records in memory.
func Open(st state.Store) (*Store, error) {
	s := &Store{st: st, rows: map[string][]entry{}}
	if st == nil {
		return s, nil
	}
	rows, err := st.Load(context.Background(), namespace)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		var v Record
		if err := json.Unmarshal(r.Value, &v); err != nil {
			return nil, err
		}
		if !valid(r.Tenant, v) || len(s.rows[r.Tenant]) >= MaxRecords {
			return nil, errors.New("read audit: invalid retained records")
		}
		s.rows[r.Tenant] = append(s.rows[r.Tenant], entry{r.Key, v})
		seq, err := strconv.ParseUint(r.Key, 10, 64)
		if err != nil {
			return nil, err
		}
		s.seq = max(s.seq, seq)
	}
	return s, nil
}

func valid(tenant string, r Record) bool {
	return tenant != "" && len(tenant) <= 512 && !strings.ContainsRune(tenant, 0) &&
		r.Subject != "" && r.Route != "" && !r.At.IsZero() &&
		len(r.Subject) <= 512 && len(r.Project) <= 256 && len(r.Thread) <= 256 && len(r.Route) <= 512 &&
		utf8.ValidString(tenant) && utf8.ValidString(r.Subject) && utf8.ValidString(r.Project) &&
		utf8.ValidString(r.Thread) && utf8.ValidString(r.Route)
}

// Add writes before permitting the read. Invalid or unrecordable reads fail closed.
func (s *Store) Add(tenant string, r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !valid(tenant, r) {
		return errors.New("read audit: invalid record")
	}
	if len(s.rows[tenant]) >= MaxRecords {
		return errors.New("read audit: capacity reached")
	}
	key := fmt.Sprintf("%020d", s.seq+1)
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if s.st != nil {
		if err := s.st.Write(context.Background(), namespace, state.Put(tenant, key, b)); err != nil {
			return err
		}
	}
	s.seq++
	s.rows[tenant] = append(s.rows[tenant], entry{key, r})
	return nil
}

// List returns newest first within a window of at most 31 days and at most 500 rows.
func (s *Store) List(tenant string, from, to time.Time, limit int) []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := recordHeap{}
	if !from.Before(to) || to.Sub(from) > 31*24*time.Hour {
		return []Record{}
	}
	limit = min(max(limit, 1), 500)
	for _, e := range s.rows[tenant] {
		if !e.record.At.Before(from) && e.record.At.Before(to) {
			if len(out) < limit {
				heap.Push(&out, e.record)
			} else if e.record.At.After(out[0].At) {
				out[0] = e.record
				heap.Fix(&out, 0)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

// Retain removes old records in bounded writes, committing each batch before changing memory.
func (s *Store) Retain(tenant string, cut time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		ops := []state.Op{}
		removed := map[string]bool{}
		for _, e := range s.rows[tenant] {
			if e.record.At.Before(cut) {
				ops = append(ops, state.Delete(tenant, e.key))
				removed[e.key] = true
				if len(ops) == 5000 {
					break
				}
			}
		}
		if len(ops) == 0 {
			return nil
		}
		if s.st != nil {
			if err := s.st.Write(context.Background(), namespace, ops...); err != nil {
				return err
			}
		}
		held := s.rows[tenant]
		kept := held[:0]
		for _, e := range s.rows[tenant] {
			if !removed[e.key] {
				kept = append(kept, e)
			}
		}
		clear(held[len(kept):])
		s.rows[tenant] = kept
	}
}

// Tenants lists workspaces with retained audit records.
func (s *Store) Tenants() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for t := range s.rows {
		out = append(out, t)
	}
	return out
}

// recordHeap keeps only the requested newest records while scanning the bounded tenant bucket.
type recordHeap []Record

func (h recordHeap) Len() int           { return len(h) }
func (h recordHeap) Less(i, j int) bool { return h[i].At.Before(h[j].At) }
func (h recordHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *recordHeap) Push(v any)        { *h = append(*h, v.(Record)) }
func (h *recordHeap) Pop() any {
	last := len(*h) - 1
	v := (*h)[last]
	*h = (*h)[:last]
	return v
}
