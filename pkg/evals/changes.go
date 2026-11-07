package evals

import (
	"crypto/rand"
	"encoding/json"
	"github.com/urmzd/humagen/pkg/state"
	"time"
)

// Change is the retained scorecard for one proposed definition or shared prompt.
// Candidate is private configuration; callers must apply the agent's read policy.
type Change struct {
	ID             string          `json:"id"`
	Agent          string          `json:"agent"`
	Kind           string          `json:"kind"`
	ContextHash    string          `json:"context_hash"`
	BeforeHash     string          `json:"before_hash"`
	AfterHash      string          `json:"after_hash"`
	Candidate      json.RawMessage `json:"candidate"`
	Scores         []ChangeScore   `json:"scores"`
	Evaluated      bool            `json:"evaluated"`
	Passed         bool            `json:"passed"`
	Error          string          `json:"error,omitempty"`
	OverrideBy     string          `json:"override_by,omitempty"`
	OverrideReason string          `json:"override_reason,omitempty"`
	At             time.Time       `json:"at"`
}

func (s *Store) SaveChange(tenant string, c Change) (Change, error) {
	if tenant == "" || c.Agent == "" || c.At.IsZero() || len(c.Candidate) > 512000 {
		return c, ErrInvalid
	}
	if c.ID == "" {
		c.ID = "change-" + state.Seq(c.At.UnixNano()) + "-" + rand.Text()
	}
	return c, s.changes.Put(tenant, c.Agent+"/"+c.ID, c)
}
func (s *Store) Change(tenant, agent, id string) (Change, error) {
	r, err := s.changes.Lookup(tenant, agent+"/"+id)
	return r.Value, err
}
func (s *Store) Changes(tenant, agent string) ([]Change, error) {
	rows, _, err := s.changes.List(tenant, state.Query{Prefix: agent + "/", Limit: 200, Desc: true})
	if err != nil {
		return nil, err
	}
	out := make([]Change, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Value)
	}
	return out, nil
}

// EachTenant visits the indexed tenants that have saved eval suites.
func (s *Store) EachTenant(fn func(string) error) error { return s.tenants.Each(fn) }
