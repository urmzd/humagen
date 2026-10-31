// Package sourcelinks matches resource metadata to explicit project-link rules.
// A match never grants source access or permissions on the external service.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds no rules or suggestions in memory, so any number of processes on one
// database agree, and it does not need pkg/state's single-writer lock.
package sourcelinks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

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

// Store keeps each tenant's rules in a state.Store, one record per rule
// keyed by its ID, and reads them per request.
type Store struct {
	st state.Store
}

// rulesNamespace holds rules, keyed by ID, and in each tenant the head:
// a stamp every new rule moves, so two processes adding rules at once
// cannot both count under MaxRules.
const (
	rulesNamespace = "source_link_rules"
	ruleHead       = "#head"
)

// New returns a store in this process's memory.
func New() *Store { s, _ := Open(nil); return s }

// Open returns a store on st, which may be shared with other processes. A nil
// st keeps rules in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{st: st}, nil
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
	err := state.Retry(func() error {
		var old Rule
		at, err := lookup(s.st, rulesNamespace, tenant, rule.ID, &old)
		exists := err == nil
		if err != nil && !errors.Is(err, state.ErrNotFound) {
			return err
		}
		if guard && ((expected == nil && exists) || (expected != nil && (!exists || old != *expected))) {
			return ErrConflict
		}
		if exists && old.CopiedFrom != "" && !reflect.DeepEqual(old, rule) {
			return fmt.Errorf("%w: copied rules remain off", ErrInvalid)
		}
		w := &write{}
		if exists {
			w.put(tenant, rule.ID, rule, at)
			return w.commit(s.st, rulesNamespace)
		}
		rules, head, err := s.all(tenant)
		if err != nil {
			return err
		}
		if len(rules) >= MaxRules {
			return ErrLimit
		}
		w.put(tenant, rule.ID, rule, state.Missing)
		w.bump(tenant, ruleHead, head)
		return w.commit(s.st, rulesNamespace)
	})
	if err != nil {
		return Rule{}, err
	}
	return rule, nil
}

// all reads a tenant's rules by ID, and the version of its head (0 when it
// has none). The head sorts before every rule ID and the bound fits one
// page, so both are read at once.
func (s *Store) all(tenant string) ([]Rule, int64, error) {
	recs, err := all(s.st, rulesNamespace, tenant, "", MaxRules+1)
	if err != nil {
		return nil, 0, err
	}
	var head int64
	out := []Rule{}
	for _, r := range recs {
		if r.Key == ruleHead {
			head = r.Version
			continue
		}
		var rule Rule
		if err := json.Unmarshal(r.Value, &rule); err != nil {
			return nil, 0, fmt.Errorf("source links: %q: %w", r.Key, err)
		}
		out = append(out, rule)
	}
	return out, head, nil
}

// List returns a tenant's rules by ID.
func (s *Store) List(tenant string) ([]Rule, error) {
	rules, _, err := s.all(tenant)
	return rules, err
}
func (s *Store) Get(tenant, id string) (Rule, error) {
	if id == ruleHead {
		return Rule{}, ErrNotFound
	}
	var rule Rule
	if _, err := lookup(s.st, rulesNamespace, tenant, id, &rule); errors.Is(err, state.ErrNotFound) {
		return Rule{}, ErrNotFound
	} else if err != nil {
		return Rule{}, err
	}
	return rule, nil
}
func (s *Store) Delete(tenant, id string) error { return s.delete(tenant, id, nil) }
func (s *Store) DeleteIfCurrent(tenant, id string, expected Rule) error {
	return s.delete(tenant, id, &expected)
}
func (s *Store) delete(tenant, id string, expected *Rule) error {
	if id == ruleHead {
		return ErrNotFound
	}
	return state.Retry(func() error {
		var existing Rule
		at, err := lookup(s.st, rulesNamespace, tenant, id, &existing)
		ok := err == nil
		if err != nil && !errors.Is(err, state.ErrNotFound) {
			return err
		}
		if expected != nil && (!ok || existing != *expected) {
			return ErrConflict
		}
		if !ok {
			return ErrNotFound
		} else if existing.CopiedFrom != "" {
			return fmt.Errorf("%w: copied rules remain off", ErrConflict)
		}
		w := &write{}
		w.ops = append(w.ops, state.DeleteAt(tenant, id, at))
		return w.commit(s.st, rulesNamespace)
	})
}

// Match evaluates at most 200 rules and 400 metadata values. Ambiguity is
// calculated across all destinations before a caller filters its visible view.
func (s *Store) Match(tenant string, resource Resource) ([]Match, error) {
	return s.MatchCandidate(tenant, resource, nil)
}

// MatchCandidate previews one unsaved rule without writing state or applying a link.
func (s *Store) MatchCandidate(tenant string, resource Resource, candidate *Rule) ([]Match, error) {
	if err := checkResource(resource); err != nil {
		return nil, err
	}
	rules, err := s.List(tenant)
	if err != nil {
		return nil, err
	}
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
	return state.Retry(func() error {
		have, head, err := s.all(tenant)
		if err != nil {
			return err
		}
		byID := make(map[string]Rule, len(have))
		for _, rule := range have {
			byID[rule.ID] = rule
		}
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
			if existing, ok := byID[rule.ID]; ok {
				if !reflect.DeepEqual(existing, rule) {
					return ErrInvalid
				}
				continue
			}
			staged[rule.ID] = rule
		}
		if len(staged) == 0 {
			return nil
		}
		if len(have)+len(staged) > MaxRules {
			return ErrLimit
		}
		w := &write{}
		for id, rule := range staged {
			w.put(tenant, id, rule, state.Missing)
		}
		w.bump(tenant, ruleHead, head)
		return w.commit(s.st, rulesNamespace)
	})
}

// checkResource bounds resource metadata before it is matched or kept.
func checkResource(resource Resource) error {
	if len(resource.Provider) == 0 || len(resource.Provider) > 64 || resource.Attributes == nil || len(resource.ID) == 0 || len(resource.ID) > 256 || len(resource.Attributes) > 8 {
		return ErrInvalid
	}
	for key, values := range resource.Attributes {
		if len(key) > 64 || len(values) > 50 {
			return ErrInvalid
		}
		for _, value := range values {
			if len(value) > 512 {
				return ErrInvalid
			}
		}
	}
	return nil
}
