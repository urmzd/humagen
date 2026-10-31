package projects

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// EnsurePersonal provisions exactly one durable private project per human
// subject. The opaque identity contains no subject or display name.
func (s *Store) EnsurePersonal(tenantName, subject string, at time.Time) (Project, error) {
	if subject == "" || subject == Everyone {
		return Project{}, fmt.Errorf("%w: a personal project needs a human subject", ErrInvalid)
	}
	if err := validWho(subject); err != nil {
		return Project{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return retry(func() (Project, error) { return s.ensurePersonal(tenantName, subject, at) })
}

// ensurePersonal is one attempt at EnsurePersonal.
func (s *Store) ensurePersonal(tenantName, subject string, at time.Time) (Project, error) {
	tx, err := s.begin(tenantName)
	if err != nil {
		return Project{}, err
	}
	for _, p := range tx.t.projects {
		if p.PersonalOwner == subject {
			return p.clone(), nil
		}
	}
	// Project caps bound this scan; a random identity avoids revealing a subject
	// through a guessed name or permitting a caller to preclaim a personal home.
	var id [24]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Project{}, err
	}
	name := "personal-" + hex.EncodeToString(id[:])
	p := Project{Name: name, Title: "Personal", Kind: Team, PersonalOwner: subject, Members: []Entry{{Who: subject, Role: Owner}}, Experts: []Expert{}}
	return s.create(tenantName, p, Writer{By: subject}, at, subject, true)
}
