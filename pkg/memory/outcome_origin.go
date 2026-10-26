package memory

import (
	"slices"
	"strings"
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
func (s *Store) ReviewedOrigins(tenant string, runs []string, owners ...map[string]bool) []ReviewedOrigin {
	s.mu.RLock()
	defer s.mu.RUnlock()
	allowed := map[string]bool{}
	for _, id := range runs[:min(len(runs), 200)] {
		allowed[id] = true
	}
	keys := []string{}
	for k := range s.origins {
		if strings.HasPrefix(k, tenant+"\x00") {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	result := []ReviewedOrigin{}
	for _, k := range keys {
		owner := strings.TrimPrefix(k, tenant+"\x00")
		if len(owners) > 0 && !owners[0][owner] {
			continue
		}
		for _, note := range s.by[k] {
			run := s.origins[k][note.ID]
			if run != "" && allowed[run] {
				result = append(result, ReviewedOrigin{Owner: owner, Note: note, Run: run})
				if len(result) == 50 {
					return result
				}
			}
		}
	}
	return result
}
