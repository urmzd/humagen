// Package callpolicy is what an agent says about being called by other
// agents: who may call it, and which of those calls a person approves first.
//
// The callee decides. A caller still needs the callee in its own spawn
// allowlist, so a policy can only narrow what a caller's spec allows, never
// widen it. An agent with no stored policy admits every caller and needs no
// approval, which is how calls behaved before policies existed.
//
// The store is in memory, per tenant, and bounded: a tenant holds at most
// MaxPolicies policies, each list at most MaxNames entries and
// MaxApprovers approvers.
package callpolicy

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// Errors a caller can match.
var (
	ErrInvalid = errors.New("callpolicy: invalid")
	ErrLimit   = errors.New("callpolicy: limit")
)

// Callers is which agents may call an agent.
type Callers string

const (
	// Anyone admits every agent whose spec lists this one.
	Anyone Callers = "anyone"
	// Listed admits only the agents named in Allow.
	Listed Callers = "listed"
	// Nobody admits no agent.
	Nobody Callers = "none"
)

// Approval is which admitted calls wait for a person.
type Approval string

const (
	// Never lets every admitted call through.
	Never Approval = "never"
	// Always holds every call for a person.
	Always Approval = "always"
	// Unlisted holds calls from every caller not named in Trusted.
	Unlisted Approval = "unlisted"
)

// Bounds.
const (
	// MaxNames is the most entries in allow or trusted.
	MaxNames = 100
	// MaxApprovers is the most approvers: what one approval question may
	// carry (fiber's agentrt.MaxApprovers).
	MaxApprovers = 32
	// MaxNameLength is the longest name or approver a list may hold.
	MaxNameLength = 200
	// MaxPolicies is the most policies one tenant may hold: one per agent.
	MaxPolicies = 1000
	// DefaultExpiry is how long a call waits for approval when the policy
	// sets no time.
	DefaultExpiry = 24 * time.Hour
	// MinExpiry and MaxExpiry bound how long a call may wait for approval.
	MinExpiry = time.Minute
	MaxExpiry = 7 * 24 * time.Hour
)

// Policy is one callee's rules for being called.
type Policy struct {
	// Agent is the callee the policy belongs to.
	Agent string `json:"agent"`
	// Callers is anyone, listed or none.
	Callers Callers `json:"callers"`
	// Allow names the callers admitted when Callers is listed.
	Allow []string `json:"allow"`
	// Approval is never, always or unlisted.
	Approval Approval `json:"approval"`
	// Trusted names the callers whose calls skip approval when Approval is
	// unlisted.
	Trusted []string `json:"trusted"`
	// Approvers are who may approve a held call: subjects, and this
	// workspace's roles as role:<name>. Empty means the platform's fallback
	// approvers, never the whole workspace.
	Approvers []string `json:"approvers"`
	// SelfApproval lets the person whose request led to a call approve it.
	SelfApproval bool `json:"self_approval"`
	// ExpiresSeconds is how long a held call waits before it is denied.
	// Zero means DefaultExpiry.
	ExpiresSeconds int `json:"expires_seconds"`

	// Version counts changes to the policy, from 1. Zero is the default
	// policy of an agent that has none stored.
	Version   int       `json:"version"`
	UpdatedBy string    `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
}

// Default is the policy of an agent that has none stored: any caller its own
// spec allows, without approval.
func Default(agent string) Policy {
	return Policy{
		Agent: agent, Callers: Anyone, Allow: []string{}, Approval: Never, Trusted: []string{},
		Approvers: []string{}, ExpiresSeconds: int(DefaultExpiry / time.Second),
	}
}

// Admits reports whether the policy lets caller call its agent.
func (p Policy) Admits(caller string) bool {
	switch p.Callers {
	case Anyone:
		return true
	case Listed:
		return slices.Contains(p.Allow, caller)
	}
	return false
}

// NeedsApproval reports whether a call from caller waits for a person. It
// says nothing about whether the caller is admitted. An unknown mode needs
// approval: a policy that cannot be read is not a reason to skip a person.
func (p Policy) NeedsApproval(caller string) bool {
	switch p.Approval {
	case Never:
		return false
	case Unlisted:
		return !slices.Contains(p.Trusted, caller)
	}
	return true
}

// Expiry is how long a held call waits before it is denied.
func (p Policy) Expiry() time.Duration {
	if p.ExpiresSeconds <= 0 {
		return DefaultExpiry
	}
	return time.Duration(p.ExpiresSeconds) * time.Second
}

// Normalize trims every name, drops nothing, and fills empty lists, so a
// policy reads the same however it was written.
func (p Policy) Normalize() Policy {
	trim := func(in []string) []string {
		out := make([]string, 0, len(in))
		for _, s := range in {
			out = append(out, strings.TrimSpace(s))
		}
		return out
	}
	p.Agent = strings.TrimSpace(p.Agent)
	p.Allow, p.Trusted, p.Approvers = trim(p.Allow), trim(p.Trusted), trim(p.Approvers)
	if p.ExpiresSeconds == 0 {
		p.ExpiresSeconds = int(DefaultExpiry / time.Second)
	}
	return p
}

// Validate checks a normalized policy on its own. known reports whether an
// agent exists in the policy's tenant; every agent a list names must.
func (p Policy) Validate(known func(string) bool) error {
	switch {
	case p.Agent == "":
		return fmt.Errorf("%w: a policy names the agent it belongs to", ErrInvalid)
	case p.Callers != Anyone && p.Callers != Listed && p.Callers != Nobody:
		return fmt.Errorf("%w: callers is anyone, listed or none", ErrInvalid)
	case p.Approval != Never && p.Approval != Always && p.Approval != Unlisted:
		return fmt.Errorf("%w: approval is never, always or unlisted", ErrInvalid)
	case p.Callers != Listed && len(p.Allow) > 0:
		return fmt.Errorf("%w: allow applies only when callers is listed", ErrInvalid)
	case p.Approval != Unlisted && len(p.Trusted) > 0:
		return fmt.Errorf("%w: trusted applies only when approval is unlisted", ErrInvalid)
	case p.Approval == Never && (len(p.Approvers) > 0 || p.SelfApproval):
		return fmt.Errorf("%w: approvers and self_approval apply only when calls need approval", ErrInvalid)
	case p.ExpiresSeconds < int(MinExpiry/time.Second) || p.ExpiresSeconds > int(MaxExpiry/time.Second):
		return fmt.Errorf("%w: expires_seconds is from %d to %d", ErrInvalid, int(MinExpiry/time.Second), int(MaxExpiry/time.Second))
	}
	for _, list := range []struct {
		field  string
		names  []string
		agents bool
		max    int
	}{{"allow", p.Allow, true, MaxNames}, {"trusted", p.Trusted, true, MaxNames}, {"approvers", p.Approvers, false, MaxApprovers}} {
		if len(list.names) > list.max {
			return fmt.Errorf("%w: %s holds at most %d names", ErrInvalid, list.field, list.max)
		}
		seen := map[string]bool{}
		for _, n := range list.names {
			switch {
			case n == "":
				return fmt.Errorf("%w: %s has an empty name", ErrInvalid, list.field)
			case len(n) > MaxNameLength:
				return fmt.Errorf("%w: a name in %s is longer than %d characters", ErrInvalid, list.field, MaxNameLength)
			case seen[n]:
				return fmt.Errorf("%w: %s names %q twice", ErrInvalid, list.field, n)
			case !list.agents && (n == "role:" || strings.TrimSpace(strings.TrimPrefix(n, "role:")) == ""):
				return fmt.Errorf("%w: %s has a role with no name", ErrInvalid, list.field)
			case list.agents && (known == nil || !known(n)):
				return fmt.Errorf("%w: %s names %q, which is not an agent", ErrInvalid, list.field, n)
			}
			seen[n] = true
		}
	}
	return nil
}

// Store keeps each tenant's policies, by agent.
type Store struct {
	mu      sync.Mutex
	tenants map[string]map[string]Policy
}

// NewStore returns an empty store.
func NewStore() *Store { return &Store{tenants: map[string]map[string]Policy{}} }

func clone(p Policy) Policy {
	p.Allow, p.Trusted, p.Approvers = slices.Clone(p.Allow), slices.Clone(p.Trusted), slices.Clone(p.Approvers)
	return p
}

// Get returns an agent's stored policy, or its default and false.
func (s *Store) Get(tenant, agent string) (Policy, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.tenants[tenant][agent]; ok {
		return clone(p), true
	}
	return Default(agent), false
}

// Put stores a policy, replacing the agent's last one. It normalizes and
// validates the policy first; known reports whether an agent exists.
func (s *Store) Put(tenant string, p Policy, known func(string) bool, by string, at time.Time) (Policy, error) {
	p = p.Normalize()
	if err := p.Validate(known); err != nil {
		return Policy{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tenants[tenant]
	if t == nil {
		t = map[string]Policy{}
		s.tenants[tenant] = t
	}
	prev, exists := t[p.Agent]
	if !exists && len(t) >= MaxPolicies {
		return Policy{}, fmt.Errorf("%w: a workspace holds at most %d call policies", ErrLimit, MaxPolicies)
	}
	p.Version = prev.Version + 1
	p.UpdatedBy, p.UpdatedAt = by, at.UTC()
	t[p.Agent] = clone(p)
	return clone(p), nil
}

// Delete drops an agent's policy, so it reads as the default again.
func (s *Store) Delete(tenant, agent string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tenants[tenant], agent)
}
