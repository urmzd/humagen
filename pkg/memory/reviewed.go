package memory

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
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
	id := "m_" + rand.Text()
	run := ""
	if len(trustedRun) == 1 {
		run = trustedRun[0]
		// The origin is kept before the note, so the note is never held
		// without it; one kept for a note that is not written names nothing.
		if _, err := s.kept.Update(tenant, owner, func(cur stored, _ bool) (stored, error) {
			cur.Origins = setIn(cur.Origins, id, run)
			cur.Exported = true
			return cur, nil
		}); err != nil {
			return Note{}, fmt.Errorf("memory: write: %w", err)
		}
	}
	var note Note
	_, err = s.change(tenant, owner, func(notes []Note) ([]Note, error) {
		for _, n := range notes {
			if strings.EqualFold(n.Text, text) {
				if n.Text != text || n.Status != Approved || n.Kind != Decision || !n.ExpiresAt.IsZero() && !n.ExpiresAt.After(at) {
					return nil, ErrReviewConflict
				}
				note = n
				return notes, nil
			}
		}
		next := nextSeq(notes)
		notes, err := makeRoom(notes, Approved)
		if err != nil {
			return nil, err
		}
		note = Note{ID: id, Text: text, Details: Details{Kind: Decision}, Source: Person, Status: Approved, By: reviewer, At: at, ReviewedBy: reviewer, ReviewedAt: at, seq: next}
		return append(notes, note), nil
	})
	if run != "" && (err != nil || note.ID != id) {
		// Best effort: the origin of a note that was not written.
		_, _ = s.kept.Update(tenant, owner, func(cur stored, _ bool) (stored, error) {
			if _, ok := cur.Origins[id]; !ok {
				return cur, state.ErrUnchanged
			}
			delete(cur.Origins, id)
			return cur, nil
		})
	}
	if err != nil {
		return Note{}, err
	}
	return note, nil
}
