package threads

import (
	"fmt"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// DeletionDelay is how long an archived conversation waits before it joins
// the deletion queue. Nothing is deleted until an admin approves it.
const DeletionDelay = 30 * 24 * time.Hour

// Archive is a conversation put away by a person. It is hidden from lists
// and joins the deletion queue at DeleteAfter, or at once when Requested.
// Deleted marks a deletion an admin approved: the messages are gone and the
// record stays as a tombstone so the conversation never shows again.
type Archive struct {
	By          string     `json:"by"`
	At          time.Time  `json:"at"`
	DeleteAfter time.Time  `json:"delete_after"`
	Requested   bool       `json:"requested,omitempty"`
	Deleted     *time.Time `json:"deleted,omitempty"`
	DeletedBy   string     `json:"deleted_by,omitempty"`
}

func (a *Archive) clone() *Archive {
	if a == nil {
		return nil
	}
	c := *a
	if a.Deleted != nil {
		at := *a.Deleted
		c.Deleted = &at
	}
	return &c
}

// Due reports whether an archived conversation waits on an admin.
func (a *Archive) Due(now time.Time) bool {
	return a != nil && a.Deleted == nil && (a.Requested || !now.Before(a.DeleteAfter))
}

func sameArchive(a, b *Archive) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	case (a.Deleted == nil) != (b.Deleted == nil):
		return false
	case a.Deleted != nil && !a.Deleted.Equal(*b.Deleted):
		return false
	}
	return a.By == b.By && a.At.Equal(b.At) && a.DeleteAfter.Equal(b.DeleteAfter) && a.Requested == b.Requested && a.DeletedBy == b.DeletedBy
}

// SetArchive archives a conversation, or brings it back with a nil a. A
// deleted conversation cannot change. Neither its version nor when it was
// last active changes, so it keeps its place when it comes back.
func (s *Store) SetArchive(tenantName, id string, a *Archive) (Thread, error) {
	h, err := s.edit(tenantName, id, func(next *head, _ *write) error {
		next.Archive = a.clone()
		return nil
	})
	if err != nil {
		return Thread{}, err
	}
	s.tell(tenantName, Notice{ID: id, At: a.atOr(h.UpdatedAt)})
	return h.Thread, nil
}

func (a *Archive) atOr(t time.Time) time.Time {
	if a == nil {
		return t
	}
	return a.At
}

// Purge deletes a conversation's messages and keeps its record as a
// tombstone, its title cleared, marked deleted by who at at. The messages go
// in the write that marks the record; the word index entries of what they
// said follow, a page at a time, and one left behind finds nothing, as a
// search reads the record and the messages it names.
func (s *Store) Purge(tenantName, id, by string, at time.Time) error {
	var said []string
	_, err := s.edit(tenantName, id, func(next *head, w *write) error {
		if next.Archive == nil {
			return fmt.Errorf("%w: only an archived conversation is deleted", ErrInvalid)
		}
		seen := map[string]bool{}
		said = said[:0]
		for after := 0; after < next.Head; {
			page, more, err := s.page(tenantName, id, after, state.MaxList)
			if err != nil {
				return err
			}
			for _, m := range page {
				if m.SurfaceID != "" {
					w.del(tenantName, postedKey(id, m.SurfaceID))
				}
				for _, word := range words(m.Text) {
					if !seen[word] {
						seen[word] = true
						said = append(said, word)
					}
				}
			}
			if !more || len(page) == 0 {
				break
			}
			after = seqOf(page[len(page)-1].ID, next.Head)
		}
		for n := 1; n <= next.Head; n++ {
			w.del(tenantName, messageKey(id, n))
		}
		when := at
		next.Archive = next.Archive.clone()
		next.Archive.Deleted, next.Archive.DeletedBy = &when, by
		next.Title, next.Head, next.First = "", 0, ""
		return nil
	})
	if err != nil {
		return err
	}
	s.tell(tenantName, Notice{ID: id, At: at})
	for len(said) > 0 {
		n := min(len(said), state.MaxList)
		w := s.write()
		for _, word := range said[:n] {
			w.del(tenantName, wordKey(word, id))
		}
		if err := w.commit(s); err != nil {
			return err
		}
		said = said[n:]
	}
	return nil
}
