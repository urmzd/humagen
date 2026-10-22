// Package feedback keeps ratings and comments linked to their source runs.
package feedback

import (
	"cmp"
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
// Subject is the key the platform keeps the person by, which may hold an
// address, so it is never handed out: a route names the rater as the
// platform calls them. Seq orders a run's feedback and pages it, and is not a
// subject.
type Record struct {
	Run       string    `json:"run"`
	Subject   string    `json:"subject"`
	Seq       int64     `json:"seq,omitempty"`
	Rating    string    `json:"rating"`
	Reason    string    `json:"reason,omitempty"`
	Comment   string    `json:"comment,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store writes through before updating its in-memory projection.
type Store struct {
	mu   sync.RWMutex
	by   map[string]map[string]Record
	seq  map[string]int64
	kept *state.Table[Record]
}

// MaxPage is the most records one page of a run's feedback holds.
const MaxPage = 200

// Open loads what was kept. A record kept before records were numbered is
// given the next number in the order it was last rated, and kept with it, so a
// cursor still means the same record after a restart.
func Open(st state.Store) (*Store, error) {
	s := &Store{by: map[string]map[string]Record{}, seq: map[string]int64{}, kept: state.NewTable[Record](st, "feedback")}
	type unnumbered struct {
		tenant, key string
		at          time.Time
	}
	var old []unnumbered
	err := s.kept.Load(func(tenant, key string, r Record) error {
		s.put(tenant, key, r)
		if r.Seq == 0 {
			old = append(old, unnumbered{tenant, key, r.UpdatedAt})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(old, func(a, b unnumbered) int {
		return cmp.Or(strings.Compare(a.tenant, b.tenant), a.at.Compare(b.at), strings.Compare(a.key, b.key))
	})
	for _, u := range old {
		r := s.by[u.tenant][u.key]
		s.seq[u.tenant]++
		r.Seq = s.seq[u.tenant]
		if err := s.kept.Put(u.tenant, u.key, r); err != nil {
			return nil, err
		}
		s.by[u.tenant][u.key] = r
	}
	return s, nil
}

func (s *Store) put(tenant, key string, r Record) {
	if s.by[tenant] == nil {
		s.by[tenant] = map[string]Record{}
	}
	s.by[tenant][key] = r
	s.seq[tenant] = max(s.seq[tenant], r.Seq)
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
	old, ok := s.by[tenant][key]
	if ok {
		r.Seq = old.Seq
	} else {
		r.Seq = s.seq[tenant] + 1
	}
	if ok && old.Rating == r.Rating {
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

// List returns at most MaxPage ratings for a run, in the order they were
// first given, after the one numbered after (0 for the first page).
func (s *Store) List(tenant, run string, after int64) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Record{}
	for _, r := range s.by[tenant] {
		if r.Run == run && r.Seq > after {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b Record) int { return cmp.Compare(a.Seq, b.Seq) })
	if len(out) > MaxPage {
		out = out[:MaxPage]
	}
	return out
}
