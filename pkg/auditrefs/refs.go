// Package auditrefs assigns stable opaque IDs to retained legacy audit sources.
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

type Store struct {
	mu    sync.Mutex
	seeds map[string][]byte
	kept  *state.Table[[]byte]
}

func Open(st state.Store) (*Store, error) {
	s := &Store{seeds: map[string][]byte{}, kept: state.NewTable[[]byte](st, "audit.refs")}
	err := s.kept.Load(func(tenant, key string, seed []byte) error {
		if tenant == "" || key != "seed" || len(seed) != 32 {
			return errors.New("audit refs: invalid")
		}
		s.seeds[tenant] = seed
		return nil
	})
	return s, err
}
func (s *Store) ID(tenant, domain, event string) (string, error) {
	if tenant == "" || domain == "" || len(domain) > 64 || len(event) > 2048 {
		return "", errors.New("audit refs: invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seed := s.seeds[tenant]
	if len(seed) == 0 {
		seed = make([]byte, 32)
		if _, err := rand.Read(seed); err != nil {
			return "", err
		}
		if err := s.kept.Put(tenant, "seed", seed); err != nil {
			return "", err
		}
		s.seeds[tenant] = seed
	}
	digest := hmac.New(sha256.New, seed)
	digest.Write([]byte(domain))
	digest.Write([]byte{0})
	digest.Write([]byte(event))
	return "legacy_" + hex.EncodeToString(digest.Sum(nil)[:16]), nil
}
