package threads

import (
	"fmt"
	"time"
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
	s.mu.Lock()
	defer s.mu.Unlock()
	th, err := s.writable(tenantName, id)
	if err != nil {
		return Thread{}, err
	}
	rec := th.rec.clone()
	rec.Archive = a.clone()
	b := s.batch()
	b.put(tenantName, threadKey(id), rec)
	if err := b.commit(); err != nil {
		return Thread{}, err
	}
	th.rec = rec
	s.tenants[tenantName].tell(Notice{ID: id, At: a.atOr(rec.UpdatedAt)})
	return rec.clone(), nil
}

func (a *Archive) atOr(t time.Time) time.Time {
	if a == nil {
		return t
	}
	return a.At
}

// Purge deletes a conversation's messages and keeps its record as a
// tombstone, its title cleared, marked deleted by who at at.
func (s *Store) Purge(tenantName, id, by string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	th, err := s.writable(tenantName, id)
	if err != nil {
		return err
	}
	rec := th.rec.clone()
	if rec.Archive == nil {
		return fmt.Errorf("%w: only an archived conversation is deleted", ErrInvalid)
	}
	when := at
	rec.Archive.Deleted, rec.Archive.DeletedBy = &when, by
	rec.Title = ""
	b := s.batch()
	for n := range th.msgs {
		b.b.Delete(tenantName, messageKey(id, n+1))
	}
	b.put(tenantName, threadKey(id), rec)
	if err := b.commit(); err != nil {
		return err
	}
	tn := s.tenants[tenantName]
	for _, m := range th.msgs {
		tn.search.Delete(m.Thread + "\x00" + m.ID)
	}
	th.msgs, th.surfaces = nil, map[string]int{}
	th.rec = rec
	tn.tell(Notice{ID: id, At: at})
	return nil
}
