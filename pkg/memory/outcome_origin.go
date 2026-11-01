package memory

import (
	"fmt"
	"maps"
	"slices"

	"github.com/urmzd/humagen/pkg/state"
)

// ReviewedOrigin is private store provenance, never part of a Note response.
// Only a trusted outcome-learning write supplies Run. Ordinary creation,
// promotion and an idempotent duplicate cannot add or replace it.
type ReviewedOrigin struct {
	Owner string `json:"-"`
	Note  Note   `json:"-"`
	Run   string `json:"-"`
}

// ReviewedOrigins returns at most 50 records matching at most 200 trusted runs.
// The caller must independently confine the owner and origin run before use.
// It reads what is kept beside the files for the first state.MaxList owners
// of the tenant, by name, and the notes of those with a matching origin.
func (s *Store) ReviewedOrigins(tenant string, runs []string, owners ...map[string]bool) ([]ReviewedOrigin, error) {
	allowed := map[string]bool{}
	for _, id := range runs[:min(len(runs), 200)] {
		allowed[id] = true
	}
	result := []ReviewedOrigin{}
	if tenant == "" {
		return result, nil
	}
	page, _, err := s.kept.List(tenant, state.Query{Limit: state.MaxList})
	if err != nil {
		return nil, fmt.Errorf("memory: read: %w", err)
	}
	for _, it := range page {
		owner := it.Key
		if len(owners) > 0 && !owners[0][owner] {
			continue
		}
		if !slices.ContainsFunc(slices.Collect(maps.Values(it.Value.Origins)), func(run string) bool { return allowed[run] }) {
			continue
		}
		notes, err := s.read(tenant, owner)
		if err != nil {
			return nil, err
		}
		for _, note := range notes {
			run := it.Value.Origins[note.ID]
			if run != "" && allowed[run] {
				result = append(result, ReviewedOrigin{Owner: owner, Note: note, Run: run})
				if len(result) == 50 {
					return result, nil
				}
			}
		}
	}
	return result, nil
}
