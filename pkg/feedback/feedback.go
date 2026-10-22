// Package feedback keeps ratings and comments linked to their source runs.
package feedback

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/warp/pkg/state"
)

// Record is one person's latest feedback on a run. The run owns its content.
type Record struct {
	Run       string    `json:"run"`
	Subject   string    `json:"subject"`
	Rating    string    `json:"rating"`
	Reason    string    `json:"reason,omitempty"`
	Comment   string    `json:"comment,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store writes through before updating its in-memory projection.
type Store struct {
	mu   sync.RWMutex
	by   map[string]map[string]Record
	kept *state.Table[Record]
}

func Open(st state.Store) (*Store, error) {
	s := &Store{by: map[string]map[string]Record{}, kept: state.NewTable[Record](st, "feedback")}
	err := s.kept.Load(func(tenant, key string, r Record) error { s.put(tenant, key, r); return nil })
	return s, err
}

func (s *Store) put(tenant, key string, r Record) {
	if s.by[tenant] == nil {
		s.by[tenant] = map[string]Record{}
	}
	s.by[tenant][key] = r
}

// Save replaces a rating. Repeating a rating preserves existing comments.
func (s *Store) Save(tenant string, r Record) (Record, error) {
	if tenant == "" || r.Run == "" || r.Subject == "" || (r.Rating != "up" && r.Rating != "down") || len(r.Comment) > 4000 || len(r.Reason) > 80 {
		return Record{}, errors.New("feedback: invalid record")
	}
	r.Comment, r.Reason = strings.TrimSpace(r.Comment), strings.TrimSpace(r.Reason)
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(r.Run+"\x00"+r.Subject)))
	if old, ok := s.by[tenant][key]; ok && old.Rating == r.Rating {
		if r.Comment == "" {
			r.Comment = old.Comment
		}
		if r.Reason == "" {
			r.Reason = old.Reason
		}
	}
	r.UpdatedAt = time.Now().UTC()
	if err := s.kept.Put(tenant, key, r); err != nil {
		return Record{}, err
	}
	s.put(tenant, key, r)
	return r, nil
}

// List returns at most 200 ratings for a run, ordered by subject, after the cursor.
func (s *Store) List(tenant, run, before string) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Record{}
	for _, r := range s.by[tenant] {
		if r.Run == run && (before == "" || r.Subject > before) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b Record) int { return strings.Compare(a.Subject, b.Subject) })
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}
