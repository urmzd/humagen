// Package marks holds what one person has marked on the conversations they
// read: the ones they put away (archived, for them alone), when they last
// read each, and the one-time tips they dismissed.
//
// Marks belong to a person in a tenant. Nobody else reads or writes them,
// and they change nothing about the conversations themselves: archiving one
// here hides it from that person's own lists, and anyone else still sees it.
// Pins stay in pkg/prefs, where the sidebar has always kept them.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds nothing in memory, so any number of processes on one database agree.
package marks

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// ErrInvalid is a mark that cannot be kept.
var ErrInvalid = errors.New("marks: invalid")

// Bounds on one person's marks.
const (
	// MaxArchived bounds the conversations one person keeps archived.
	MaxArchived = 500
	// MaxRead bounds the conversations whose last read is kept. Past it the
	// oldest is forgotten, and that conversation reads as read once more.
	MaxRead = 500
	// MaxTips bounds the tips one person has dismissed.
	MaxTips = 32
	// maxID bounds a conversation's id and a tip's name, in bytes.
	maxID = 128
)

// Marks is one person's marks.
type Marks struct {
	// Archived is the conversations the person put away, most recent first.
	Archived []string `json:"archived"`
	// Read is when the person last read each conversation, by its id.
	Read map[string]time.Time `json:"read"`
	// Tips is the one-time tips the person dismissed, by name.
	Tips []string `json:"tips"`
	// Since is when the person first marked anything, zero before that. A
	// conversation that has not changed since then reads as read, so the
	// first mark does not make every older conversation unread.
	Since time.Time `json:"since,omitzero"`
}

func normal(m Marks) Marks {
	m.Archived = slices.Clone(m.Archived)
	if m.Archived == nil {
		m.Archived = []string{}
	}
	m.Read = maps.Clone(m.Read)
	if m.Read == nil {
		m.Read = map[string]time.Time{}
	}
	m.Tips = slices.Clone(m.Tips)
	if m.Tips == nil {
		m.Tips = []string{}
	}
	return m
}

// Store holds every person's marks in the namespace "marks", one record per
// person keyed by subject in their tenant.
type Store struct {
	kept *state.Table[Marks]
}

// NewStore returns an empty store in this process's memory.
func NewStore() *Store {
	s, _ := Open(nil)
	return s
}

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps marks in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{kept: state.NewTable[Marks](st, "marks")}, nil
}

// Get returns a person's marks, empty when they have marked nothing.
func (s *Store) Get(tenant, subject string) (Marks, error) {
	it, err := s.kept.Lookup(tenant, subject)
	if errors.Is(err, state.ErrNotFound) {
		return normal(Marks{}), nil
	}
	if err != nil {
		return Marks{}, fmt.Errorf("marks: read: %w", err)
	}
	return normal(it.Value), nil
}

func (s *Store) change(tenant, subject string, fn func(m *Marks) error) (Marks, error) {
	if tenant == "" || subject == "" {
		return Marks{}, fmt.Errorf("%w: marks belong to a person", ErrInvalid)
	}
	out, err := s.kept.Update(tenant, subject, func(cur Marks, ok bool) (Marks, error) {
		next := normal(cur)
		if !ok || next.Since.IsZero() {
			next.Since = time.Now().UTC().Truncate(time.Millisecond)
		}
		if err := fn(&next); err != nil {
			return Marks{}, err
		}
		return next, nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			return Marks{}, err
		}
		return Marks{}, fmt.Errorf("marks: write: %w", err)
	}
	return normal(out), nil
}

// ValidID reports whether id can name a conversation or a tip here.
func ValidID(id string) bool {
	return id != "" && len(id) <= maxID && !strings.ContainsAny(id, "\r\n\t<> /")
}

// SetArchived archives a conversation for one person, or brings it back.
// Archiving one already archived moves it to the front.
func (s *Store) SetArchived(tenant, subject, id string, on bool) (Marks, error) {
	if !ValidID(id) {
		return Marks{}, fmt.Errorf("%w: a conversation is named by its id", ErrInvalid)
	}
	return s.change(tenant, subject, func(m *Marks) error {
		was := slices.Index(m.Archived, id)
		switch {
		case !on && was < 0:
			return state.ErrUnchanged
		case !on:
			m.Archived = slices.Delete(m.Archived, was, was+1)
		case was == 0:
			return state.ErrUnchanged
		case was > 0:
			m.Archived = slices.Insert(slices.Delete(m.Archived, was, was+1), 0, id)
		case len(m.Archived) >= MaxArchived:
			return fmt.Errorf("%w: at most %d conversations can be archived; bring one back first", ErrInvalid, MaxArchived)
		default:
			m.Archived = slices.Insert(m.Archived, 0, id)
		}
		return nil
	})
}

// MarkRead records that a person read a conversation at a time. An earlier
// time than the one kept changes nothing. Past MaxRead the conversation read
// longest ago is forgotten.
func (s *Store) MarkRead(tenant, subject, id string, at time.Time) (Marks, error) {
	if !ValidID(id) {
		return Marks{}, fmt.Errorf("%w: a conversation is named by its id", ErrInvalid)
	}
	at = at.UTC().Truncate(time.Millisecond)
	return s.change(tenant, subject, func(m *Marks) error {
		if was, ok := m.Read[id]; ok && !at.After(was) {
			return state.ErrUnchanged
		}
		m.Read[id] = at
		for len(m.Read) > MaxRead {
			oldest, first := "", time.Time{}
			for k, v := range m.Read {
				if oldest == "" || v.Before(first) || (v.Equal(first) && k < oldest) {
					oldest, first = k, v
				}
			}
			delete(m.Read, oldest)
		}
		return nil
	})
}

// DismissTip records that a person dismissed a one-time tip.
func (s *Store) DismissTip(tenant, subject, name string) (Marks, error) {
	if !ValidID(name) {
		return Marks{}, fmt.Errorf("%w: a tip is named by its name", ErrInvalid)
	}
	return s.change(tenant, subject, func(m *Marks) error {
		if slices.Contains(m.Tips, name) {
			return state.ErrUnchanged
		}
		if len(m.Tips) >= MaxTips {
			return fmt.Errorf("%w: at most %d tips are kept", ErrInvalid, MaxTips)
		}
		m.Tips = append(m.Tips, name)
		return nil
	})
}
