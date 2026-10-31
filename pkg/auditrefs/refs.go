// Package auditrefs assigns stable opaque IDs to retained legacy audit sources.
//
// It reads and writes the database per request (pkg/state, per request): a
// tenant's seed is created once, by whichever process asks first, and never
// changes, so the seeds this process has read are a cache that may be
// dropped at any time. Any number of processes on one database agree, and
// it does not need pkg/state's single-writer lock.
package auditrefs

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"

	"github.com/urmzd/humagen/pkg/state"
)

// namespace holds one record per tenant, under seedKey: the tenant's seed.
const (
	namespace = "audit.refs"
	seedKey   = "seed"
)

// Store derives a tenant's references from its seed.
type Store struct {
	kept *state.Table[[]byte]
	// seeds are the seeds read so far. A seed never changes once written.
	seeds sync.Map
}

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps seeds in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{kept: state.NewTable[[]byte](st, namespace)}, nil
}

// seed returns a tenant's seed, creating it when the tenant has none. Two
// processes creating one at once keep the one written first.
func (s *Store) seed(tenant string) ([]byte, error) {
	if v, ok := s.seeds.Load(tenant); ok {
		return v.([]byte), nil
	}
	var seed []byte
	err := state.Retry(func() error {
		it, err := s.kept.Lookup(tenant, seedKey)
		if err == nil {
			seed = it.Value
			return nil
		}
		if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		fresh := make([]byte, 32)
		if _, err := rand.Read(fresh); err != nil {
			return err
		}
		b := s.kept.Batch()
		b.Create(tenant, seedKey, fresh)
		if err := b.Commit(); err != nil {
			return err
		}
		seed = fresh
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(seed) != 32 {
		return nil, errors.New("audit refs: invalid")
	}
	s.seeds.Store(tenant, seed)
	return seed, nil
}

// ID returns the opaque reference of one legacy event.
func (s *Store) ID(tenant, domain, event string) (string, error) {
	if tenant == "" || domain == "" || len(domain) > 64 || len(event) > 2048 {
		return "", errors.New("audit refs: invalid")
	}
	seed, err := s.seed(tenant)
	if err != nil {
		return "", err
	}
	digest := hmac.New(sha256.New, seed)
	digest.Write([]byte(domain))
	digest.Write([]byte{0})
	digest.Write([]byte(event))
	return "legacy_" + hex.EncodeToString(digest.Sum(nil)[:16]), nil
}
