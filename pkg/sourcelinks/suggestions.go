package sourcelinks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
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

// Suggestions keeps each tenant's suggestions in a state.Store and reads
// them per request.
type Suggestions struct {
	st state.Store
}

// suggestionsNamespace is where suggestions are kept. The tenant is the shard
// key; within it:
//
//	link-<hex>             a suggestion, by its ID
//	n                      how many suggestions the tenant holds
//	r:<resource>:<id>      a suggestion of a client's resource, for Observe
//	d:<destination>:<id>   a suggestion without a client, for Detach
//
// A suggestion is never deleted, and what its index keys name is part of its
// ID, so an index key, once written, stays true. Every write that adds a
// suggestion moves n, so two processes adding at once cannot both count
// under MaxSuggestions.
const (
	suggestionsNamespace = "source_link_suggestions"
	suggestionCount      = "n"
	suggestionPrefix     = "link-"
)

func digest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}
func resourcePrefix(client, knowledge string, resource Resource) string {
	return "r:" + digest(client, knowledge, resource.Provider, resource.ID) + ":"
}
func destinationPrefix(knowledge, project, channel string) string {
	return "d:" + digest(knowledge, project, channel) + ":"
}

// index adds the index keys of a new suggestion.
func (w *write) index(tenant string, row Suggestion) {
	w.put(tenant, resourcePrefix(row.Client, row.Knowledge, row.Resource)+row.ID, 1, 0)
	if row.Client == "" {
		w.put(tenant, destinationPrefix(row.Knowledge, row.Project, row.Channel)+row.ID, 1, 0)
	}
}

// NewSuggestions returns suggestions in this process's memory.
func NewSuggestions() *Suggestions { s, _ := OpenSuggestions(nil); return s }

// OpenSuggestions returns suggestions on st, which may be shared with other
// processes. A nil st keeps them in this process's memory.
//
// Suggestions kept by a release before the count and index keys are indexed
// the first time a process writes to their tenant.
func OpenSuggestions(st state.Store) (*Suggestions, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &Suggestions{st: st}, nil
}

// count returns how many suggestions a tenant holds and the version of its
// count. A tenant without a count has its suggestions indexed first: the
// index keys, which say only what is true, and then the count, created by
// one process.
func (s *Suggestions) count(tenant string) (int, int64, error) {
	for {
		var n int
		at, err := lookup(s.st, suggestionsNamespace, tenant, suggestionCount, &n)
		if err == nil {
			return n, at, nil
		}
		if !errors.Is(err, state.ErrNotFound) {
			return 0, 0, err
		}
		rows, err := s.rows(tenant)
		if err != nil {
			return 0, 0, err
		}
		for chunk := range slices.Chunk(rows, state.MaxList/2) {
			w := &write{}
			for _, row := range chunk {
				w.index(tenant, row.Suggestion)
			}
			if err := w.commit(s.st, suggestionsNamespace); err != nil {
				return 0, 0, err
			}
		}
		w := &write{}
		w.put(tenant, suggestionCount, len(rows), state.Missing)
		if err := w.commit(s.st, suggestionsNamespace); err != nil && !errors.Is(err, state.ErrConflict) {
			return 0, 0, err
		}
	}
}

type versioned struct {
	Suggestion
	version int64
}

// rows reads every suggestion of a tenant, at most MaxSuggestions.
func (s *Suggestions) rows(tenant string) ([]versioned, error) {
	recs, err := all(s.st, suggestionsNamespace, tenant, suggestionPrefix, MaxSuggestions)
	if err != nil {
		return nil, err
	}
	out := make([]versioned, 0, len(recs))
	for _, r := range recs {
		var row Suggestion
		if err := json.Unmarshal(r.Value, &row); err != nil {
			return nil, fmt.Errorf("source links: %q: %w", r.Key, err)
		}
		out = append(out, versioned{row, r.Version})
	}
	return out, nil
}

// get reads one suggestion and its version, or ErrNotFound.
func (s *Suggestions) get(tenant, id string) (Suggestion, int64, error) {
	if !strings.HasPrefix(id, suggestionPrefix) {
		return Suggestion{}, 0, ErrNotFound
	}
	var row Suggestion
	at, err := lookup(s.st, suggestionsNamespace, tenant, id, &row)
	if errors.Is(err, state.ErrNotFound) {
		return Suggestion{}, 0, ErrNotFound
	}
	return row, at, err
}

// indexed reads the IDs under one index prefix.
func (s *Suggestions) indexed(tenant, prefix string) ([]string, error) {
	recs, err := all(s.st, suggestionsNamespace, tenant, prefix, MaxSuggestions)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, strings.TrimPrefix(r.Key, prefix))
	}
	return out, nil
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
	if err := checkResource(resource); err != nil {
		return nil, err
	}
	for _, m := range matches {
		if m.Project == "" || len(m.Project) > 64 || len(m.Channel) > 40 || len(m.Rules) > MaxRules {
			return nil, ErrInvalid
		}
	}
	resource = cloneSuggestion(Suggestion{Resource: resource}).Resource
	for key, values := range resource.Attributes {
		slices.Sort(values)
		resource.Attributes[key] = slices.Compact(values)
	}
	var out []Suggestion
	err := state.Retry(func() error {
		out = []Suggestion{}
		count, countAt, err := s.count(tenant)
		if err != nil {
			return err
		}
		w := &write{}
		// staged are the rows this write changes, and versions the version
		// each was read at: state.Missing for a new one.
		staged := map[string]Suggestion{}
		versions := map[string]int64{}
		added := 0
		for _, m := range matches {
			id := suggestionID(client, resource, m, knowledge)
			old, exists := staged[id]
			if !exists {
				row, version, err := s.get(tenant, id)
				switch {
				case err == nil:
					old, exists = row, true
					versions[id] = version
				case errors.Is(err, ErrNotFound):
					versions[id] = state.Missing
				default:
					return err
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
				if !exists {
					added++
				}
				staged[id] = cloneSuggestion(row)
			}
			out = append(out, cloneSuggestion(row))
		}
		if count+added > MaxSuggestions {
			return ErrLimit
		}
		// Other destinations of the same resource follow its latest metadata.
		others, err := s.indexed(tenant, resourcePrefix(client, knowledge, resource))
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, row := range out {
			seen[row.ID] = true
		}
		for _, id := range others {
			if seen[id] {
				continue
			}
			old, version, err := s.get(tenant, id)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if old.Client != client || old.Knowledge != knowledge || old.Resource.Provider != resource.Provider || old.Resource.ID != resource.ID || reflect.DeepEqual(old.Resource, resource) {
				continue
			}
			row := cloneSuggestion(old)
			row.Resource = resource
			row.Version++
			row.UpdatedAt = at
			staged[id] = row
			versions[id] = version
		}
		for id, row := range staged {
			w.put(tenant, id, row, versions[id])
			if versions[id] == state.Missing {
				w.index(tenant, row)
			}
		}
		if added > 0 {
			w.put(tenant, suggestionCount, count+added, countAt)
		}
		return w.commit(s.st, suggestionsNamespace)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Suggestions) Get(tenant, id string) (Suggestion, error) {
	row, _, err := s.get(tenant, id)
	if err != nil {
		return Suggestion{}, err
	}
	return cloneSuggestion(row), nil
}

// List applies visibility before its bound. No total or hidden count is exposed.
// It reads the tenant's suggestions in ID order until it has limit of them.
func (s *Suggestions) List(tenant, client, status string, limit int, visible func(Suggestion) bool) ([]Suggestion, error) {
	if s == nil {
		return []Suggestion{}, nil
	}
	limit = min(max(limit, 1), 200)
	out := []Suggestion{}
	after := ""
	for range MaxSuggestions/state.MaxList + 1 {
		c, cancel := ctx()
		page, next, err := s.st.List(c, suggestionsNamespace, tenant, state.Query{Prefix: suggestionPrefix, After: after, Limit: state.MaxList})
		cancel()
		if err != nil {
			return nil, err
		}
		for _, r := range page {
			var row Suggestion
			if err := json.Unmarshal(r.Value, &row); err != nil {
				return nil, fmt.Errorf("source links: %q: %w", r.Key, err)
			}
			if client != "" && row.Client != client || status != "" && row.State != status || visible != nil && !visible(cloneSuggestion(row)) {
				continue
			}
			out = append(out, cloneSuggestion(row))
			if len(out) == limit {
				return out, nil
			}
		}
		if next == "" {
			break
		}
		after = next
	}
	return out, nil
}
func (s *Suggestions) Decide(tenant, id, action string, version int, at time.Time) (Suggestion, error) {
	if action != "link" && action != "ignore" {
		return Suggestion{}, ErrInvalid
	}
	var out Suggestion
	err := state.Retry(func() error {
		row, kept, err := s.get(tenant, id)
		if err != nil {
			return err
		}
		if row.Version != version {
			return ErrConflict
		}
		row.State = "linked"
		if action == "ignore" {
			row.State = "ignored"
		}
		row.Automatic = false
		row.Version++
		row.UpdatedAt = at
		w := &write{}
		w.put(tenant, id, row, kept)
		if err := w.commit(s.st, suggestionsNamespace); err != nil {
			return err
		}
		out = row
		return nil
	})
	if err != nil {
		return Suggestion{}, err
	}
	return cloneSuggestion(out), nil
}

// Attach records a person's explicit context attachment in one write. It says
// nothing about provider connection health or a successful content import.
func (s *Suggestions) Attach(tenant, knowledge string, resource Resource, project, channel string, at time.Time) (Suggestion, error) {
	if project == "" || len(project) > 64 || len(channel) > 40 || len(knowledge) == 0 || len(knowledge) > 64 {
		return Suggestion{}, ErrInvalid
	}
	if err := checkResource(resource); err != nil {
		return Suggestion{}, err
	}
	id := suggestionID("", resource, Match{Project: project, Channel: channel}, knowledge)
	var out Suggestion
	err := state.Retry(func() error {
		row, version, err := s.get(tenant, id)
		exists := err == nil
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if exists && row.State == "linked" && !row.Automatic {
			out = row
			return nil
		}
		w := &write{}
		if !exists {
			count, countAt, err := s.count(tenant)
			if err != nil {
				return err
			}
			if count >= MaxSuggestions {
				return ErrLimit
			}
			row = Suggestion{ID: id, Resource: resource, Project: project, Channel: channel, Knowledge: knowledge, Rules: []string{}, CreatedAt: at}
			w.put(tenant, suggestionCount, count+1, countAt)
			w.index(tenant, row)
			version = state.Missing
		}
		row.State = "linked"
		row.Automatic = false
		row.Version++
		row.UpdatedAt = at
		w.put(tenant, id, row, version)
		if err := w.commit(s.st, suggestionsNamespace); err != nil {
			return err
		}
		out = row
		return nil
	})
	if err != nil {
		return Suggestion{}, err
	}
	return cloneSuggestion(out), nil
}

func (s *Suggestions) Detach(tenant, knowledge, project, channel string, at time.Time) error {
	return state.Retry(func() error {
		if _, _, err := s.count(tenant); err != nil {
			return err
		}
		ids, err := s.indexed(tenant, destinationPrefix(knowledge, project, channel))
		if err != nil {
			return err
		}
		w := &write{}
		for _, id := range ids {
			row, version, err := s.get(tenant, id)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if row.Knowledge != knowledge || row.Project != project || row.Channel != channel || row.Client != "" || row.State != "linked" {
				continue
			}
			row.State = "ignored"
			row.Automatic = false
			row.Version++
			row.UpdatedAt = at
			w.put(tenant, row.ID, row, version)
		}
		if len(w.ops) == 0 {
			return ErrNotFound
		}
		return w.commit(s.st, suggestionsNamespace)
	})
}
