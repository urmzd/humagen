// Package sourcelinks matches resource metadata to explicit project-link rules.
// A match never grants source access or permissions on the external service.
package sourcelinks

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/state"
)

type Mode string

const (
	Suggest Mode = "suggest"
	Auto    Mode = "auto_link"
	Off     Mode = "off"
)
const MaxRules = 200

var (
	ErrInvalid  = errors.New("source links: invalid")
	ErrNotFound = errors.New("source links: not found")
	ErrLimit    = errors.New("source links: limit reached")
	ErrConflict = errors.New("source links: changed")
)

// Rule holds no connection credential, external authority or live binding.
// CopiedFrom is an immutable operation key; copied rules always remain off.
type Rule struct {
	ID         string `json:"id"`
	Provider   string `json:"provider"`
	Attribute  string `json:"attribute"`
	Pattern    string `json:"pattern"`
	Project    string `json:"project"`
	Channel    string `json:"channel,omitempty"`
	Mode       Mode   `json:"mode"`
	CopiedFrom string `json:"copied_from,omitempty"`
}

// Resource is bounded metadata from a verified connector or an explicit preview.
// It contains no document text and conveys no new access to its identifier.
type Resource struct {
	Provider   string              `json:"provider"`
	ID         string              `json:"id"`
	Attributes map[string][]string `json:"attributes"`
}

type Match struct {
	Project string   `json:"project"`
	Channel string   `json:"channel,omitempty"`
	Mode    Mode     `json:"mode"`
	Rules   []string `json:"rules"`
}

type Store struct {
	mu   sync.RWMutex
	by   map[string]map[string]Rule
	kept *state.Table[Rule]
}

func New() *Store { return &Store{by: map[string]map[string]Rule{}} }
func Open(st state.Store) (*Store, error) {
	s := New()
	s.kept = state.NewTable[Rule](st, "source_link_rules")
	err := s.kept.Load(func(tenant, key string, rule Rule) error {
		if rule.ID != key {
			return ErrInvalid
		}
		if err := check(rule); err != nil {
			return err
		}
		if s.by[tenant] == nil {
			s.by[tenant] = map[string]Rule{}
		}
		s.by[tenant][key] = rule
		return nil
	})
	return s, err
}

func check(rule Rule) error {
	if !knowledge.ValidName(rule.ID) || len(rule.Pattern) == 0 || len(rule.Pattern) > 256 || strings.TrimSpace(rule.Project) == "" || len(rule.Project) > 64 || len(rule.Channel) > 40 || len(rule.CopiedFrom) > 200 {
		return ErrInvalid
	}
	if rule.Mode != Suggest && rule.Mode != Auto && rule.Mode != Off {
		return ErrInvalid
	}
	if rule.CopiedFrom != "" && rule.Mode != Off {
		return ErrInvalid
	}
	attributes := map[string][]string{"github": {"repository", "topic", "label", "path", "contributor"}, "slack": {"channel", "channel_prefix"}, "drive": {"folder"}, "notion": {"parent"}}
	if !slices.Contains(attributes[rule.Provider], rule.Attribute) {
		return ErrInvalid
	}
	return nil
}
func (s *Store) Put(tenant string, rule Rule) (Rule, error) { return s.put(tenant, rule, nil, false) }

// PutIfCurrent binds an authorized snapshot to the durable write. Nil requires a new ID.
func (s *Store) PutIfCurrent(tenant string, rule Rule, expected *Rule) (Rule, error) {
	return s.put(tenant, rule, expected, true)
}
func (s *Store) put(tenant string, rule Rule, expected *Rule, guard bool) (Rule, error) {
	if rule.Mode == "" {
		rule.Mode = Suggest
	}
	if err := check(rule); err != nil {
		return Rule{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.by[tenant][rule.ID]
	if guard && ((expected == nil && exists) || (expected != nil && (!exists || old != *expected))) {
		return Rule{}, ErrConflict
	}
	if exists && old.CopiedFrom != "" && !reflect.DeepEqual(old, rule) {
		return Rule{}, fmt.Errorf("%w: copied rules remain off", ErrInvalid)
	}
	if !exists && len(s.by[tenant]) >= MaxRules {
		return Rule{}, ErrLimit
	}
	if err := s.kept.Put(tenant, rule.ID, rule); err != nil {
		return Rule{}, err
	}
	if s.by[tenant] == nil {
		s.by[tenant] = map[string]Rule{}
	}
	s.by[tenant][rule.ID] = rule
	return rule, nil
}
func (s *Store) List(tenant string) []Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Rule{}
	for _, rule := range s.by[tenant] {
		out = append(out, rule)
	}
	slices.SortFunc(out, func(a, b Rule) int { return strings.Compare(a.ID, b.ID) })
	return out
}
func (s *Store) Get(tenant, id string) (Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rule, ok := s.by[tenant][id]
	if !ok {
		return Rule{}, ErrNotFound
	}
	return rule, nil
}
func (s *Store) Delete(tenant, id string) error { return s.delete(tenant, id, nil) }
func (s *Store) DeleteIfCurrent(tenant, id string, expected Rule) error {
	return s.delete(tenant, id, &expected)
}
func (s *Store) delete(tenant, id string, expected *Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected != nil {
		if actual, ok := s.by[tenant][id]; !ok || actual != *expected {
			return ErrConflict
		}
	}
	if existing, ok := s.by[tenant][id]; !ok {
		return ErrNotFound
	} else if existing.CopiedFrom != "" {
		return fmt.Errorf("%w: copied rules remain off", ErrConflict)
	}
	if err := s.kept.Delete(tenant, id); err != nil {
		return err
	}
	delete(s.by[tenant], id)
	return nil
}

// Match evaluates at most 200 rules and 400 metadata values. Ambiguity is
// calculated across all destinations before a caller filters its visible view.
func (s *Store) Match(tenant string, resource Resource) ([]Match, error) {
	return s.MatchCandidate(tenant, resource, nil)
}

// MatchCandidate previews one unsaved rule without writing state or applying a link.
func (s *Store) MatchCandidate(tenant string, resource Resource, candidate *Rule) ([]Match, error) {
	rules := s.List(tenant)
	if candidate != nil {
		copy := *candidate
		copy.ID = "preview-candidate"
		copy.CopiedFrom = ""
		if copy.Mode == "" {
			copy.Mode = Suggest
		}
		if err := check(copy); err != nil {
			return nil, err
		}
		rules = append(rules, copy)
	}
	if len(resource.Provider) == 0 || len(resource.Provider) > 64 || resource.Attributes == nil || len(resource.ID) == 0 || len(resource.ID) > 256 || len(resource.Attributes) > 8 {
		return nil, ErrInvalid
	}
	for key, values := range resource.Attributes {
		if len(key) > 64 || len(values) > 50 {
			return nil, ErrInvalid
		}
		for _, value := range values {
			if len(value) > 512 {
				return nil, ErrInvalid
			}
		}
	}
	byDestination := map[[2]string]Match{}
	for _, rule := range rules {
		if rule.Provider != resource.Provider || rule.Mode == Off {
			continue
		}
		matched := false
		for _, value := range resource.Attributes[rule.Attribute] {
			if rule.Attribute == "path" || rule.Attribute == "channel_prefix" {
				matched = strings.HasPrefix(value, rule.Pattern)
			} else {
				matched = value == rule.Pattern
			}
			if matched {
				break
			}
		}
		if !matched {
			continue
		}
		key := [2]string{rule.Project, rule.Channel}
		entry, exists := byDestination[key]
		if !exists {
			entry = Match{Project: rule.Project, Channel: rule.Channel, Mode: rule.Mode, Rules: []string{}}
		} else if rule.Mode == Suggest {
			entry.Mode = Suggest
		}
		entry.Rules = append(entry.Rules, rule.ID)
		byDestination[key] = entry
	}
	out := []Match{}
	for _, match := range byDestination {
		if len(byDestination) > 1 {
			match.Mode = Suggest
		}
		out = append(out, match)
	}
	slices.SortFunc(out, func(a, b Match) int {
		if n := strings.Compare(a.Project, b.Project); n != 0 {
			return n
		}
		return strings.Compare(a.Channel, b.Channel)
	})
	return out, nil
}

// RestoreDisabled copies an already recorded fork snapshot. Later edits to the
// original rules cannot alter a replay. The write is atomic and idempotent.
func (s *Store) RestoreDisabled(tenant, destination, operation string, rules []Rule) error {
	if len(operation) == 0 || len(operation) > 200 || len(rules) > MaxRules {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	staged := map[string]Rule{}
	for _, rule := range rules {
		sum := sha256.Sum256([]byte(operation + "\x00" + rule.ID))
		rule.ID = "copy-" + hex.EncodeToString(sum[:16])
		rule.Project = destination
		rule.Mode = Off
		rule.CopiedFrom = operation
		if err := check(rule); err != nil {
			return err
		}
		if existing, ok := s.by[tenant][rule.ID]; ok {
			if !reflect.DeepEqual(existing, rule) {
				return ErrInvalid
			}
			continue
		}
		staged[rule.ID] = rule
	}
	if len(s.by[tenant])+len(staged) > MaxRules {
		return ErrLimit
	}
	b := s.kept.Batch()
	for id, rule := range staged {
		b.Put(tenant, id, rule)
	}
	if err := b.Commit(); err != nil {
		return err
	}
	if s.by[tenant] == nil {
		s.by[tenant] = map[string]Rule{}
	}
	for id, rule := range staged {
		s.by[tenant][id] = rule
	}
	return nil
}
