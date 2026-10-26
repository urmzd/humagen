package memory

import (
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"
)

var ErrReviewConflict = errors.New("memory: this text already has a different review state")

// RememberDecision records exactly the text a human reviewed, atomically. A
// pending duplicate never becomes approved through an unrelated learning write.
func (s *Store) RememberDecision(tenant, owner, text, reviewer string, at time.Time, trustedRun ...string) (Note, error) {
	if tenant == "" || owner == "" || reviewer == "" || at.IsZero() {
		return Note{}, ErrInvalid
	}
	if len(trustedRun) > 1 || len(trustedRun) == 1 && (trustedRun[0] == "" || len(trustedRun[0]) > 255) {
		return Note{}, ErrInvalid
	}
	reviewed := strings.TrimSpace(text)
	text, err := clean(text)
	if err != nil {
		return Note{}, err
	}
	if text != reviewed {
		return Note{}, fmt.Errorf("%w: review the memory as one line without list markers", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(tenant, owner)
	for _, note := range s.by[k] {
		if strings.EqualFold(note.Text, text) {
			if note.Text != text || note.Status != Approved || note.Kind != Decision || !note.ExpiresAt.IsZero() && !note.ExpiresAt.After(at) {
				return Note{}, ErrReviewConflict
			}
			return note, nil
		}
	}
	notes, err := makeRoom(append([]Note(nil), s.by[k]...), Approved)
	if err != nil {
		return Note{}, err
	}
	note := Note{ID: "m_" + rand.Text(), Text: text, Details: Details{Kind: Decision}, Source: Person, Status: Approved, By: reviewer, At: at, ReviewedBy: reviewer, ReviewedAt: at}
	origins := maps.Clone(s.origins[k])
	if len(trustedRun) == 1 {
		if origins == nil {
			origins = map[string]string{}
		}
		origins[note.ID] = trustedRun[0]
	}
	if err := s.setOrigins(tenant, owner, append(notes, note), s.next+1, origins); err != nil {
		return Note{}, err
	}
	return note, nil
}
