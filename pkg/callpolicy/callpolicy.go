// Package callpolicy is what an agent says about being called by other
// agents: who may call it, and which of those calls a person approves first.
//
// The callee decides. A caller still needs the callee in its own spawn
// allowlist, so a policy can only narrow what a caller's spec allows, never
// widen it. An agent with no stored policy admits every caller and needs no
// approval, which is how calls behaved before policies existed.
//
// The store is per tenant and bounded: a tenant holds at most MaxPolicies
// policies, each list at most MaxNames entries and MaxApprovers approvers.
// It reads and writes the database per request (pkg/state, per request): it
// holds nothing in memory, so any number of processes on one database agree,
// and it does not need pkg/state's single-writer lock.
package callpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
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
	if err := p.ValidateShape(known); err != nil {
		return err
	}
	return p.withinLimits(Policy{})
}

// withinLimits checks how many names each list holds. A list of allow or
// trusted holds at most MaxNames, or more only when every name past them is
// one stored already holds in that list: names kept for writers who were
// not shown them (PutKeeping), which a writer shown them saves as read.
func (p Policy) withinLimits(stored Policy) error {
	for _, list := range []struct {
		field         string
		names, stored []string
		max           int
	}{{"allow", p.Allow, stored.Allow, MaxNames}, {"trusted", p.Trusted, stored.Trusted, MaxNames}, {"approvers", p.Approvers, nil, MaxApprovers}} {
		if len(list.names) <= list.max {
			continue
		}
		if list.stored == nil || slices.ContainsFunc(list.names, func(n string) bool { return !slices.Contains(list.stored, n) }) {
			return fmt.Errorf("%w: %s holds at most %d names", ErrInvalid, list.field, list.max)
		}
	}
	return nil
}

// ValidateShape is Validate without the limits on how many names each list
// holds, which PutKeeping checks against the stored policy.
func (p Policy) ValidateShape(known func(string) bool) error {
	switch {
	case p.Agent == "":
		return fmt.Errorf("%w: a policy names the agent it belongs to", ErrInvalid)
	case strings.HasPrefix(p.Agent, "!"):
		return fmt.Errorf("%w: an agent's name cannot start with \"!\"", ErrInvalid)
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

// namespace is where policies are kept. The tenant is the shard key; within
// it:
//
//	<agent>   the agent's policy
//	!count    how many policies the tenant holds, which no agent's name is
//
// A write that adds or removes a policy changes the count at the version it
// read in the same write. A tenant written before the count existed has it
// counted from its policies on its first such write.
const namespace = "callpolicy"

// countKey is the key of a tenant's count of policies.
const countKey = "!count"

// Store keeps each tenant's policies, by agent, in a state.Store read and
// written per request.
type Store struct {
	st state.Store
}

// NewStore returns an empty store in this process's memory.
func NewStore() *Store { return &Store{st: state.NewMemory()} }

// Open returns a store on st, which may be shared with other processes. A nil
// st keeps policies in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{st: st}, nil
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

func clone(p Policy) Policy {
	p.Allow, p.Trusted, p.Approvers = slices.Clone(p.Allow), slices.Clone(p.Trusted), slices.Clone(p.Approvers)
	return p
}

// lookup reads one record into v and returns its version, 0 when it has none.
func (s *Store) lookup(tenant, key string, v any) (int64, error) {
	c, cancel := ctx()
	defer cancel()
	r, err := s.st.Lookup(c, namespace, tenant, key)
	if errors.Is(err, state.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("callpolicy: read: %w", err)
	}
	if err := json.Unmarshal(r.Value, v); err != nil {
		return 0, fmt.Errorf("callpolicy: %q/%q: %w", tenant, key, err)
	}
	return r.Version, nil
}

// all reads every policy a tenant holds, page by page, never more than
// MaxPolicies of them.
func (s *Store) all(tenant string) ([]Policy, error) {
	var out []Policy
	after := ""
	for {
		c, cancel := ctx()
		recs, next, err := s.st.List(c, namespace, tenant, state.Query{After: after, Limit: state.MaxList})
		cancel()
		if err != nil {
			return nil, fmt.Errorf("callpolicy: read: %w", err)
		}
		for _, r := range recs {
			if r.Key == countKey {
				continue
			}
			var p Policy
			if err := json.Unmarshal(r.Value, &p); err != nil {
				return nil, fmt.Errorf("callpolicy: %q/%q: %w", tenant, r.Key, err)
			}
			out = append(out, p)
		}
		if len(out) > MaxPolicies {
			return nil, fmt.Errorf("%w: %q holds more than %d call policies", state.ErrTooMany, tenant, MaxPolicies)
		}
		if next == "" {
			return out, nil
		}
		after = next
	}
}

// count reads a tenant's count of policies and its version, counting the
// policies when the tenant has none stored (version 0).
func (s *Store) count(tenant string) (int, int64, error) {
	var n int
	at, err := s.lookup(tenant, countKey, &n)
	if err != nil || at > 0 {
		return n, at, err
	}
	all, err := s.all(tenant)
	return len(all), 0, err
}

// write is one write's ops, built as JSON.
type write struct {
	tenant string
	ops    []state.Op
	err    error
}

// put adds a put at version: 0 for a record that has none.
func (w *write) put(key string, v any, version int64) {
	raw, err := json.Marshal(v)
	if err != nil {
		w.err = errors.Join(w.err, err)
		return
	}
	if version == 0 {
		version = state.Missing
	}
	w.ops = append(w.ops, state.PutAt(w.tenant, key, raw, version))
}

func (w *write) commit(st state.Store) error {
	if w.err != nil {
		return w.err
	}
	c, cancel := ctx()
	defer cancel()
	err := st.Write(c, namespace, w.ops...)
	if err != nil && !errors.Is(err, state.ErrConflict) {
		return fmt.Errorf("callpolicy: write: %w", err)
	}
	return err
}

// Get returns an agent's stored policy, or its default and false.
func (s *Store) Get(tenant, agent string) (Policy, bool, error) {
	if agent == "" || strings.HasPrefix(agent, "!") {
		return Default(agent), false, nil
	}
	var p Policy
	at, err := s.lookup(tenant, agent, &p)
	if err != nil {
		return Policy{}, false, err
	}
	if at == 0 {
		return Default(agent), false, nil
	}
	return p, true, nil
}

// save writes p over what the agent has stored, read afresh on every try:
// fill is given the stored policy (the default when there is none) and
// whether there is one, and finishes p.
func (s *Store) save(tenant string, p Policy, by string, at time.Time, fill func(p *Policy, stored Policy, exists bool) error) (Policy, error) {
	in := p
	var out Policy
	err := state.Retry(func() error {
		p := clone(in)
		var prev Policy
		v, err := s.lookup(tenant, p.Agent, &prev)
		if err != nil {
			return err
		}
		exists := v > 0
		stored := Default(p.Agent)
		if exists {
			stored = prev
		}
		w := &write{tenant: tenant}
		if !exists {
			n, cv, err := s.count(tenant)
			if err != nil {
				return err
			}
			if n >= MaxPolicies {
				return fmt.Errorf("%w: a workspace holds at most %d call policies", ErrLimit, MaxPolicies)
			}
			w.put(countKey, n+1, cv)
		}
		if fill != nil {
			if err := fill(&p, stored, exists); err != nil {
				return err
			}
		}
		p.Version = prev.Version + 1
		p.UpdatedBy, p.UpdatedAt = by, at.UTC()
		w.put(p.Agent, p, v)
		if err := w.commit(s.st); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return Policy{}, err
	}
	return clone(out), nil
}

// Put stores a policy, replacing the agent's last one. It normalizes and
// validates the policy first; known reports whether an agent exists.
func (s *Store) Put(tenant string, p Policy, known func(string) bool, by string, at time.Time) (Policy, error) {
	p = p.Normalize()
	if err := p.Validate(known); err != nil {
		return Policy{}, err
	}
	return s.save(tenant, p, by, at, nil)
}

// PutKeeping is Put for a writer who is not shown every agent the stored
// policy names. p is normalized and validated on its own, and against the
// policy stored when it writes its lists are held to MaxNames as the writer
// wrote them, or past it only with names the stored lists hold
// (withinLimits). Then, keep is given the stored policy (the default when
// there is none) and returns the names to keep in allow and in trusted,
// which are added after p's own when p's mode still has that list and p does
// not name them already. A kept name counts toward no limit, since its
// writer was not shown it; a list names each agent once, so it never holds
// more than the workspace's agents. The write is at the version of the
// policy keep was given, and done again from a fresh read when another
// writer moved it, so a name another writer keeps between the read and the
// write is never lost.
func (s *Store) PutKeeping(tenant string, p Policy, known func(string) bool, keep func(stored Policy) (allow, trusted []string), by string, at time.Time) (Policy, error) {
	p = p.Normalize()
	if err := p.ValidateShape(known); err != nil {
		return Policy{}, err
	}
	return s.save(tenant, p, by, at, func(p *Policy, stored Policy, _ bool) error {
		if err := p.withinLimits(stored); err != nil {
			return err
		}
		allow, trusted := keep(clone(stored))
		add := func(into, from []string) []string {
			for _, n := range from {
				if !slices.Contains(into, n) {
					into = append(into, n)
				}
			}
			return into
		}
		if p.Callers == Listed {
			p.Allow = add(p.Allow, allow)
		}
		if p.Approval == Unlisted {
			p.Trusted = add(p.Trusted, trusted)
		}
		return nil
	})
}

// Delete drops an agent's policy, so it reads as the default again.
func (s *Store) Delete(tenant, agent string) error {
	if agent == "" || strings.HasPrefix(agent, "!") {
		return nil
	}
	return state.Retry(func() error {
		var p Policy
		v, err := s.lookup(tenant, agent, &p)
		if err != nil || v == 0 {
			return err
		}
		n, cv, err := s.count(tenant)
		if err != nil {
			return err
		}
		w := &write{tenant: tenant}
		w.put(countKey, max(n-1, 0), cv)
		w.ops = append(w.ops, state.DeleteAt(tenant, agent, v))
		return w.commit(s.st)
	})
}

// Snapshots returns at most 500 current policy snapshots in a time window.
// It is not a revision history: an older overwritten policy is unavailable.
// Visibility is applied before the bounded newest selection.
func (s *Store) Snapshots(tenant string, from, to time.Time, limit int, visible func(Policy) bool) ([]Policy, error) {
	all, err := s.all(tenant)
	if err != nil {
		return nil, err
	}
	limit = min(max(limit, 1), 500)
	out := []Policy{}
	for _, p := range all {
		if p.UpdatedAt.Before(from) || !p.UpdatedAt.Before(to) || visible != nil && !visible(clone(p)) {
			continue
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Policy) int {
		if n := b.UpdatedAt.Compare(a.UpdatedAt); n != 0 {
			return n
		}
		return strings.Compare(a.Agent, b.Agent)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
