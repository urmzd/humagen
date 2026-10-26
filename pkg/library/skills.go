// Package library keeps bounded immutable skill revisions. Published revisions
// are resolved by the platform into its existing pinned skill composition.
package library

import (
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
)

const MaxSkills = 32
const MaxVersions = 32
const MaxScopes = 1000

var (
	ErrInvalid  = errors.New("library: invalid")
	ErrNotFound = errors.New("library: not found")
	ErrConflict = errors.New("library: version conflict")
)

type Scope struct {
	Kind    string `json:"kind"`
	Project string `json:"project,omitempty"`
	Subject string `json:"subject,omitempty"`
}

func (s Scope) Key() string {
	value := s.Project
	if s.Kind == "personal" {
		value = s.Subject
	}
	return s.Kind + ":" + base64.RawURLEncoding.EncodeToString([]byte(value))
}
func (s Scope) Valid() bool {
	switch s.Kind {
	case "workspace":
		return s.Project == "" && s.Subject == ""
	case "project":
		return s.Project != "" && len(s.Project) <= 512 && s.Subject == ""
	case "personal":
		return s.Subject != "" && len(s.Subject) <= 512 && s.Project == ""
	}
	return false
}

type Definition struct {
	projects.Skill
	Inputs  string   `json:"inputs,omitempty"`
	Outputs string   `json:"outputs,omitempty"`
	Tools   []string `json:"tools"`
}

func (d Definition) Valid() bool {
	if !projects.ValidSkill(d.Name) || strings.TrimSpace(d.Description) == "" || strings.TrimSpace(d.Instructions) == "" || utf8.RuneCountInString(d.Description) > projects.MaxSkillDescription || utf8.RuneCountInString(d.Instructions) > projects.MaxSkillInstructions || len(d.ArgumentHint) > projects.MaxArgumentHint || len(d.Inputs) > 4000 || len(d.Outputs) > 4000 || len(d.Tools) > 32 {
		return false
	}
	for _, value := range []string{d.Description, d.Instructions, d.ArgumentHint, d.Inputs, d.Outputs} {
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return false
		}
	}
	seen := map[string]bool{}
	for _, tool := range d.Tools {
		if tool == "" || len(tool) > 128 || strings.ContainsAny(tool, "\x00\r\n") || seen[tool] {
			return false
		}
		seen[tool] = true
	}
	return true
}

type Provenance struct {
	Scope   Scope  `json:"scope"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}
type Revision struct {
	Definition
	Scope       Scope       `json:"scope"`
	Version     int         `json:"version"`
	By          string      `json:"by"`
	At          time.Time   `json:"at"`
	PublishedAt time.Time   `json:"published_at,omitzero"`
	PublishedBy string      `json:"published_by,omitempty"`
	Tests       []string    `json:"tests"`
	From        *Provenance `json:"from,omitempty"`
}

func clone(r Revision) Revision {
	r.Tools = slices.Clone(r.Tools)
	r.Tests = slices.Clone(r.Tests)
	if r.From != nil {
		from := *r.From
		r.From = &from
	}
	return r
}

type Store struct {
	mu   sync.RWMutex
	rows map[string]map[string]map[string][]Revision
	kept *state.Table[[]Revision]
}

func Open(st state.Store) (*Store, error) {
	s := &Store{rows: map[string]map[string]map[string][]Revision{}, kept: state.NewTable[[]Revision](st, "library.skills")}
	err := s.kept.Load(func(tenant, key string, revisions []Revision) error {
		if len(revisions) == 0 || len(revisions) > MaxVersions {
			return ErrInvalid
		}
		first := revisions[0]
		if key != first.Scope.Key()+"/"+first.Name || !first.Scope.Valid() {
			return ErrInvalid
		}
		for i, r := range revisions {
			if !r.Definition.Valid() || r.Scope != first.Scope || r.Name != first.Name || r.Version != i+1 || r.By == "" || r.At.IsZero() || len(r.Tests) > 10 {
				return ErrInvalid
			}
		}
		group, err := s.group(tenant, first.Scope)
		if err != nil {
			return err
		}
		if len(group) >= MaxSkills {
			return ErrInvalid
		}
		group[first.Name] = revisions
		return nil
	})
	return s, err
}
func (s *Store) group(tenant string, scope Scope) (map[string][]Revision, error) {
	if s.rows[tenant] == nil {
		s.rows[tenant] = map[string]map[string][]Revision{}
	}
	key := scope.Key()
	if s.rows[tenant][key] == nil {
		if len(s.rows[tenant]) >= MaxScopes {
			return nil, ErrInvalid
		}
		s.rows[tenant][key] = map[string][]Revision{}
	}
	return s.rows[tenant][key], nil
}
func (s *Store) put(tenant string, scope Scope, name string, rows []Revision) error {
	return s.kept.Put(tenant, scope.Key()+"/"+name, rows)
}

// Draft appends an immutable revision; expected is the latest version read.
// Provenance is validated by the platform before entering this store.
func (s *Store) Draft(tenant, by string, scope Scope, d Definition, expected int, from *Provenance, at time.Time) (Revision, error) {
	if tenant == "" || by == "" || !scope.Valid() || !d.Valid() || at.IsZero() {
		return Revision{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	group, err := s.group(tenant, scope)
	if err != nil {
		return Revision{}, err
	}
	previous := group[d.Name]
	if len(previous) != expected {
		return Revision{}, ErrConflict
	}
	if len(previous) >= MaxVersions || len(previous) == 0 && len(group) >= MaxSkills {
		return Revision{}, ErrInvalid
	}
	if d.Tools == nil {
		d.Tools = []string{}
	}
	r := clone(Revision{Definition: d, Scope: scope, Version: len(previous) + 1, By: by, At: at, Tests: []string{}, From: from})
	next := append(slices.Clone(previous), r)
	if err := s.put(tenant, scope, d.Name, next); err != nil {
		return Revision{}, err
	}
	group[d.Name] = next
	return clone(r), nil
}
func (s *Store) Get(tenant string, scope Scope, name string, version int) (Revision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := s.rows[tenant][scope.Key()][name]
	if version == 0 {
		version = len(rows)
	}
	if version < 1 || version > len(rows) {
		return Revision{}, ErrNotFound
	}
	return clone(rows[version-1]), nil
}
func (s *Store) List(tenant string, scope Scope) []Revision {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Revision{}
	for _, rows := range s.rows[tenant][scope.Key()] {
		out = append(out, clone(rows[len(rows)-1]))
	}
	slices.SortFunc(out, func(a, b Revision) int { return strings.Compare(a.Name, b.Name) })
	return out
}
func (s *Store) History(tenant string, scope Scope, name string) []Revision {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Revision{}
	for _, r := range s.rows[tenant][scope.Key()][name] {
		out = append(out, clone(r))
	}
	return out
}
func (s *Store) Published(tenant string, scope Scope, name string) (Revision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := s.rows[tenant][scope.Key()][name]
	for i := len(rows) - 1; i >= 0; i-- {
		if !rows[i].PublishedAt.IsZero() {
			return clone(rows[i]), true
		}
	}
	return Revision{}, false
}

// Tested records the durable run reference, never a client-supplied success flag.
func (s *Store) Tested(tenant string, scope Scope, name string, version int, run string) error {
	if run == "" || len(run) > 128 {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.rows[tenant][scope.Key()][name]
	if version < 1 || version > len(rows) {
		return ErrNotFound
	}
	r := clone(rows[version-1])
	if slices.Contains(r.Tests, run) {
		return nil
	}
	if len(r.Tests) >= 10 {
		return ErrInvalid
	}
	r.Tests = append(r.Tests, run)
	next := slices.Clone(rows)
	next[version-1] = r
	if err := s.put(tenant, scope, name, next); err != nil {
		return err
	}
	s.rows[tenant][scope.Key()][name] = next
	return nil
}
func (s *Store) Publish(tenant, by string, scope Scope, name string, version int, at time.Time) (Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.rows[tenant][scope.Key()][name]
	if version < 1 || version > len(rows) {
		return Revision{}, ErrNotFound
	}
	r := clone(rows[version-1])
	if !r.PublishedAt.IsZero() {
		return r, nil
	}
	// The platform checks that a recorded test actually completed successfully.
	if len(r.Tests) == 0 || by == "" || at.IsZero() {
		return Revision{}, fmt.Errorf("%w: test the revision before publishing", ErrConflict)
	}
	r.PublishedAt = at
	r.PublishedBy = by
	next := slices.Clone(rows)
	next[version-1] = r
	if err := s.put(tenant, scope, name, next); err != nil {
		return Revision{}, err
	}
	s.rows[tenant][scope.Key()][name] = next
	return clone(r), nil
}
