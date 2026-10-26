package sourcelinks

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/urmzd/humagen/pkg/state"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

const MaxSuggestions = 10000

// Suggestion stores verified metadata and an explicit relation, never message
// text, provider credentials or authority to read an external resource.
type Suggestion struct {
	ID        string    `json:"id"`
	Client    string    `json:"client"`
	Resource  Resource  `json:"resource"`
	Project   string    `json:"project"`
	Channel   string    `json:"channel,omitempty"`
	Knowledge string    `json:"knowledge,omitempty"`
	Rules     []string  `json:"rules"`
	State     string    `json:"state"`
	Automatic bool      `json:"automatic"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Suggestions struct {
	mu   sync.RWMutex
	by   map[string]map[string]Suggestion
	kept *state.Table[Suggestion]
}

func NewSuggestions() *Suggestions { return &Suggestions{by: map[string]map[string]Suggestion{}} }
func OpenSuggestions(st state.Store) (*Suggestions, error) {
	s := NewSuggestions()
	s.kept = state.NewTable[Suggestion](st, "source_link_suggestions")
	err := s.kept.Load(func(tenant, key string, row Suggestion) error {
		if row.ID != key || row.Version < 1 || !slices.Contains([]string{"suggested", "linked", "ignored"}, row.State) || len(s.by[tenant]) >= MaxSuggestions {
			return ErrInvalid
		}
		if _, err := New().Match(tenant, row.Resource); err != nil {
			return err
		}
		if row.Project == "" || len(row.Project) > 64 || len(row.Channel) > 40 || len(row.Client) > 64 || len(row.Knowledge) > 64 || len(row.Rules) > MaxRules {
			return ErrInvalid
		}
		if s.by[tenant] == nil {
			s.by[tenant] = map[string]Suggestion{}
		}
		s.by[tenant][key] = cloneSuggestion(row)
		return nil
	})
	return s, err
}
func cloneSuggestion(row Suggestion) Suggestion {
	row.Rules = slices.Clone(row.Rules)
	row.Resource.Attributes = maps.Clone(row.Resource.Attributes)
	for k, v := range row.Resource.Attributes {
		row.Resource.Attributes[k] = slices.Clone(v)
	}
	return row
}
func suggestionID(client string, resource Resource, m Match, knowledge string) string {
	sum := sha256.Sum256([]byte(client + "\x00" + resource.Provider + "\x00" + resource.ID + "\x00" + m.Project + "\x00" + m.Channel + "\x00" + knowledge))
	return "link-" + hex.EncodeToString(sum[:16])
}

// Observe is called only after an actual provider read or verified ingress.
// Replays deduplicate by resource and destination. Ignore and manual Link are
// durable choices; an automatic relation remains subject to current rules.
func (s *Suggestions) Observe(tenant, client, knowledge string, resource Resource, matches []Match, at time.Time) ([]Suggestion, error) {
	if len(client) > 64 || len(knowledge) > 64 || len(matches) > MaxRules {
		return nil, ErrInvalid
	}
	if _, err := New().Match(tenant, resource); err != nil {
		return nil, err
	}
	resource = cloneSuggestion(Suggestion{Resource: resource}).Resource
	for key, values := range resource.Attributes {
		slices.Sort(values)
		resource.Attributes[key] = slices.Compact(values)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	batch := s.kept.Batch()
	out := []Suggestion{}
	changed := []Suggestion{}
	added := 0
	for _, m := range matches {
		if m.Project == "" || len(m.Project) > 64 || len(m.Channel) > 40 || len(m.Rules) > MaxRules {
			return nil, ErrInvalid
		}
		id := suggestionID(client, resource, m, knowledge)
		old, exists := s.by[tenant][id]
		if !exists {
			added++
			if len(s.by[tenant])+added > MaxSuggestions {
				return nil, ErrLimit
			}
		}
		row := old
		if !exists {
			row = Suggestion{ID: id, Client: client, Resource: resource, Project: m.Project, Channel: m.Channel, Knowledge: knowledge, State: "suggested", Rules: []string{}, Version: 0, CreatedAt: at}
		}
		row.Resource = resource
		row.Rules = slices.Clone(m.Rules)
		slices.Sort(row.Rules)
		if m.Mode == Auto && row.State != "ignored" && (row.State == "suggested" || row.Automatic) {
			row.State = "linked"
			row.Automatic = true
		}
		if !exists || !reflect.DeepEqual(cloneSuggestion(old), cloneSuggestion(row)) {
			row.Version++
			row.UpdatedAt = at
			batch.Put(tenant, id, cloneSuggestion(row))
			changed = append(changed, row)
		}
		out = append(out, cloneSuggestion(row))
	}
	seen := map[string]bool{}
	for _, row := range out {
		seen[row.ID] = true
	}
	for _, old := range s.by[tenant] {
		if seen[old.ID] || old.Client != client || old.Knowledge != knowledge || old.Resource.Provider != resource.Provider || old.Resource.ID != resource.ID {
			continue
		}
		row := cloneSuggestion(old)
		row.Resource = resource
		if reflect.DeepEqual(old.Resource, resource) {
			continue
		}
		row.Version++
		row.UpdatedAt = at
		batch.Put(tenant, row.ID, cloneSuggestion(row))
		changed = append(changed, row)
	}
	if err := batch.Commit(); err != nil {
		return nil, err
	}
	if s.by[tenant] == nil {
		s.by[tenant] = map[string]Suggestion{}
	}
	for _, row := range changed {
		s.by[tenant][row.ID] = cloneSuggestion(row)
	}
	return out, nil
}
func (s *Suggestions) Get(tenant, id string) (Suggestion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row, ok := s.by[tenant][id]
	if !ok {
		return Suggestion{}, ErrNotFound
	}
	return cloneSuggestion(row), nil
}

// List applies visibility before its bound. No total or hidden count is exposed.
func (s *Suggestions) List(tenant, client, status string, limit int, visible func(Suggestion) bool) []Suggestion {
	if s == nil {
		return []Suggestion{}
	}
	limit = min(max(limit, 1), 200)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Suggestion{}
	for _, row := range s.by[tenant] {
		if client != "" && row.Client != client || status != "" && row.State != status || visible != nil && !visible(cloneSuggestion(row)) {
			continue
		}
		copy := cloneSuggestion(row)
		pos, _ := slices.BinarySearchFunc(out, copy, func(a, b Suggestion) int { return strings.Compare(a.ID, b.ID) })
		if pos >= limit {
			continue
		}
		out = slices.Insert(out, pos, copy)
		if len(out) > limit {
			out = out[:limit]
		}
	}
	return out
}
func (s *Suggestions) Decide(tenant, id, action string, version int, at time.Time) (Suggestion, error) {
	if action != "link" && action != "ignore" {
		return Suggestion{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.by[tenant][id]
	if !ok {
		return Suggestion{}, ErrNotFound
	}
	if row.Version != version {
		return Suggestion{}, ErrConflict
	}
	row.State = "linked"
	if action == "ignore" {
		row.State = "ignored"
	}
	row.Automatic = false
	row.Version++
	row.UpdatedAt = at
	if err := s.kept.Put(tenant, id, row); err != nil {
		return Suggestion{}, err
	}
	s.by[tenant][id] = cloneSuggestion(row)
	return cloneSuggestion(row), nil
}

// Attach records a person's explicit context attachment in one write. It says
// nothing about provider connection health or a successful content import.
func (s *Suggestions) Attach(tenant, knowledge string, resource Resource, project, channel string, at time.Time) (Suggestion, error) {
	if project == "" || len(project) > 64 || len(channel) > 40 || len(knowledge) == 0 || len(knowledge) > 64 {
		return Suggestion{}, ErrInvalid
	}
	if _, err := New().Match(tenant, resource); err != nil {
		return Suggestion{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := suggestionID("", resource, Match{Project: project, Channel: channel}, knowledge)
	row, exists := s.by[tenant][id]
	if !exists && len(s.by[tenant]) >= MaxSuggestions {
		return Suggestion{}, ErrLimit
	}
	if exists && row.State == "linked" && !row.Automatic {
		return cloneSuggestion(row), nil
	}
	if !exists {
		row = Suggestion{ID: id, Resource: resource, Project: project, Channel: channel, Knowledge: knowledge, Rules: []string{}, CreatedAt: at}
	}
	row.State = "linked"
	row.Automatic = false
	row.Version++
	row.UpdatedAt = at
	if err := s.kept.Put(tenant, id, row); err != nil {
		return Suggestion{}, err
	}
	if s.by[tenant] == nil {
		s.by[tenant] = map[string]Suggestion{}
	}
	s.by[tenant][id] = cloneSuggestion(row)
	return cloneSuggestion(row), nil
}

func (s *Suggestions) Detach(tenant, knowledge, project, channel string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	batch := s.kept.Batch()
	changed := []Suggestion{}
	for _, row := range s.by[tenant] {
		if row.Knowledge != knowledge || row.Project != project || row.Channel != channel || row.Client != "" || row.State != "linked" {
			continue
		}
		row.State = "ignored"
		row.Automatic = false
		row.Version++
		row.UpdatedAt = at
		batch.Put(tenant, row.ID, row)
		changed = append(changed, row)
	}
	if len(changed) == 0 {
		return ErrNotFound
	}
	if err := batch.Commit(); err != nil {
		return err
	}
	for _, row := range changed {
		s.by[tenant][row.ID] = cloneSuggestion(row)
	}
	return nil
}
