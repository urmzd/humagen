// Package contextflags keeps security classification independently of journal
// retention and before a run can be dispatched to a model.
package contextflags

import (
	"errors"
	"sync"

	"github.com/urmzd/humagen/pkg/state"
)

const MaxRuns = 100000

var ErrInvalid = errors.New("context flags: invalid")
var ErrCapacity = errors.New("context flags: capacity reached")

type Store struct {
	mu   sync.Mutex
	by   map[string]map[string]bool
	kept *state.Table[bool]
}

func Open(st state.Store) (*Store, error) {
	s := &Store{by: map[string]map[string]bool{}, kept: state.NewTable[bool](st, "context.flags")}
	err := s.kept.Load(func(tenant, id string, private bool) error {
		if tenant == "" || id == "" || !private {
			return ErrInvalid
		}
		if s.by[tenant] == nil {
			s.by[tenant] = map[string]bool{}
		}
		if len(s.by[tenant]) >= MaxRuns {
			return ErrCapacity
		}
		s.by[tenant][id] = true
		return nil
	})
	return s, err
}
func (s *Store) Mark(tenant, id string) error {
	if tenant == "" || id == "" || len(id) > 256 {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.by[tenant][id] {
		return nil
	}
	if len(s.by[tenant]) >= MaxRuns {
		return ErrCapacity
	}
	if err := s.kept.Put(tenant, id, true); err != nil {
		return err
	}
	if s.by[tenant] == nil {
		s.by[tenant] = map[string]bool{}
	}
	s.by[tenant][id] = true
	return nil
}
func (s *Store) Has(tenant, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.by[tenant][id]
}
