// Package contextflags keeps security classification independently of journal
// retention and before a run can be dispatched to a model.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds nothing in memory, so any number of processes on one database agree,
// and it does not need pkg/state's single-writer lock.
package contextflags

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/urmzd/humagen/pkg/state"
)

const MaxRuns = 100000

var ErrInvalid = errors.New("context flags: invalid")
var ErrCapacity = errors.New("context flags: capacity reached")

// namespace is where flags are kept. Within a tenant:
//
//	<run>   true: the run used private context
//	#count  how many runs the tenant has flagged
//
// The count is written with every new flag, at the version read, so the cap
// holds whichever process marks. A run id never starts with "#". A tenant
// kept before the count existed is counted once, on its next new flag.
const namespace = "context.flags"

const count = "#count"

type Store struct {
	st state.Store
}

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps flags in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{st: st}, nil
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// counted reads a tenant's count and its version, 0 when it has none yet:
// then the tenant's flags are counted, at most MaxRuns of them.
func (s *Store) counted(c context.Context, tenant string) (int, int64, error) {
	r, err := s.st.Lookup(c, namespace, tenant, count)
	if err == nil {
		var n int
		if err := json.Unmarshal(r.Value, &n); err != nil {
			return 0, 0, err
		}
		return n, r.Version, nil
	}
	if !errors.Is(err, state.ErrNotFound) {
		return 0, 0, err
	}
	n, after := 0, ""
	for {
		page, next, err := s.st.List(c, namespace, tenant, state.Query{After: after, Limit: state.MaxList})
		if err != nil {
			return 0, 0, err
		}
		for _, r := range page {
			if !strings.HasPrefix(r.Key, "#") {
				n++
			}
		}
		if n > MaxRuns {
			return 0, 0, ErrCapacity
		}
		if next == "" {
			return n, 0, nil
		}
		after = next
	}
}

// Mark flags a run as having used private context. Marking one already
// flagged writes nothing.
func (s *Store) Mark(tenant, id string) error {
	if tenant == "" || id == "" || len(id) > 256 || strings.HasPrefix(id, "#") {
		return ErrInvalid
	}
	return state.Retry(func() error {
		c, cancel := ctx()
		defer cancel()
		if _, err := s.st.Lookup(c, namespace, tenant, id); err == nil {
			return nil
		} else if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		n, version, err := s.counted(c, tenant)
		if err != nil {
			return err
		}
		if n >= MaxRuns {
			return ErrCapacity
		}
		raw, _ := json.Marshal(n + 1)
		head := state.PutAt(tenant, count, raw, version)
		if version == 0 {
			head = state.Create(tenant, count, raw)
		}
		return s.st.Write(c, namespace, state.Create(tenant, id, []byte("true")), head)
	})
}

// Has reports whether a run used private context. It fails closed: a flag
// that cannot be read reports true, so the run is treated as private.
func (s *Store) Has(tenant, id string) bool {
	if tenant == "" || id == "" || strings.HasPrefix(id, "#") {
		return false
	}
	c, cancel := ctx()
	defer cancel()
	r, err := s.st.Lookup(c, namespace, tenant, id)
	if errors.Is(err, state.ErrNotFound) {
		return false
	}
	if err != nil {
		return true
	}
	var private bool
	return json.Unmarshal(r.Value, &private) != nil || private
}
