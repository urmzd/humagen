// Package operations records durable agent controls, tightening action policies,
// assignments and handoff contracts. It never starts a run or grants a tool.
package operations

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

var (
	ErrInvalid    = errors.New("operations: invalid")
	ErrConflict   = errors.New("operations: version conflict")
	ErrNotFound   = errors.New("operations: not found")
	ErrPaused     = errors.New("operations: agent is paused")
	ErrDenied     = errors.New("operations: action denied")
	ErrEscalation = errors.New("operations: handoff needs a person: cycle or more than three hops")
)

// MetaForceAsk is an internal tightening marker, preserved in durable run tags.
const MetaForceAsk = "humagen.operations.ask"

// MetaTools is a JSON array of exact tool names restricting a pinned skill.
// An absent key preserves grants; an explicit empty array allows no actions.
const MetaTools = "humagen.operations.tools"

const MaxRecords = 10000
const MaxResults = 200

type Level string

const (
	Unspecified Level = ""
	Allow       Level = "allow"
	Ask         Level = "ask"
	Never       Level = "never"
)

type Category string

const (
	ReadContext      Category = "read_context"
	WriteFiles       Category = "write_files"
	PublishArtifacts Category = "publish_artifacts"
	ExternalMessages Category = "external_messages"
	CallAgents       Category = "call_agents"
	Other            Category = "other"
)

var Categories = []Category{ReadContext, WriteFiles, PublishArtifacts, ExternalMessages, CallAgents, Other}

type Scope struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
}

func (s Scope) key() string { return s.Kind + ":" + s.Name }
func (s Scope) valid() bool {
	switch s.Kind {
	case "workspace":
		return s.Name == ""
	case "project", "agent", "personal":
		return identifier(s.Name)
	}
	return false
}

type Control struct {
	Agent     string    `json:"agent,omitempty"`
	Paused    bool      `json:"paused"`
	Manager   string    `json:"manager,omitempty"`
	StopRules string    `json:"stop_rules,omitempty"`
	Version   int       `json:"version"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Policy struct {
	Scope     Scope              `json:"scope"`
	Rules     map[Category]Level `json:"rules"`
	Version   int                `json:"version"`
	UpdatedBy string             `json:"updated_by"`
	UpdatedAt time.Time          `json:"updated_at"`
}
type WorkState string

const (
	Requested   WorkState = "requested"
	Accepted    WorkState = "accepted"
	InProgress  WorkState = "in_progress"
	Watching    WorkState = "watching"
	Result      WorkState = "result"
	Blocked     WorkState = "blocked"
	NeedsPerson WorkState = "needs_person"
	Cancelled   WorkState = "cancelled"
)

type Assignment struct {
	ID        string    `json:"id"`
	Agent     string    `json:"agent"`
	Project   string    `json:"project"`
	Thread    string    `json:"thread,omitempty"`
	Owner     string    `json:"owner"`
	Goal      string    `json:"goal"`
	DoneWhen  string    `json:"done_when"`
	DueAt     time.Time `json:"due_at,omitempty"`
	Schedule  string    `json:"schedule,omitempty"`
	Cron      string    `json:"cron,omitempty"`
	State     WorkState `json:"state"`
	Blocker   string    `json:"blocker,omitempty"`
	RunID     string    `json:"run_id,omitempty"`
	Version   int       `json:"version"`
	UpdatedBy string    `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Input struct {
	Type      string `json:"type"`
	Reference string `json:"reference"`
}
type Handoff struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Project   string    `json:"project"`
	Thread    string    `json:"thread,omitempty"`
	Owner     string    `json:"owner,omitempty"`
	Goal      string    `json:"goal"`
	DoneWhen  string    `json:"done_when"`
	Inputs    []Input   `json:"inputs"`
	Chain     []string  `json:"chain"`
	DueAt     time.Time `json:"due_at,omitempty"`
	State     WorkState `json:"state"`
	Blocker   string    `json:"blocker,omitempty"`
	RunID     string    `json:"run_id,omitempty"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type record struct {
	Control    *Control    `json:"control,omitempty"`
	Policy     *Policy     `json:"policy,omitempty"`
	Assignment *Assignment `json:"assignment,omitempty"`
	Handoff    *Handoff    `json:"handoff,omitempty"`
}
type key struct{ tenant, id string }
type Store struct {
	mu          sync.RWMutex
	table       *state.Table[record]
	records     map[key]record
	noticeAfter map[string]string
}

func Open(st state.Store) (*Store, error) {
	s := &Store{table: state.NewTable[record](st, "operations"), records: map[key]record{}, noticeAfter: map[string]string{}}
	err := s.table.Load(func(t, k string, r record) error {
		n := 0
		if r.Control != nil {
			n++
		}
		if r.Policy != nil {
			n++
		}
		if r.Assignment != nil {
			n++
		}
		if r.Handoff != nil {
			n++
		}
		if n != 1 {
			return fmt.Errorf("%w: malformed record", ErrInvalid)
		}
		s.records[key{t, k}] = clone(r)
		return nil
	})
	return s, err
}
func clone(r record) record {
	if r.Control != nil {
		v := *r.Control
		r.Control = &v
	}
	if r.Policy != nil {
		v := *r.Policy
		v.Rules = maps.Clone(v.Rules)
		r.Policy = &v
	}
	if r.Assignment != nil {
		v := *r.Assignment
		r.Assignment = &v
	}
	if r.Handoff != nil {
		v := *r.Handoff
		v.Inputs = slices.Clone(v.Inputs)
		v.Chain = slices.Clone(v.Chain)
		r.Handoff = &v
	}
	return r
}
func identifier(v string) bool {
	return strings.TrimSpace(v) != "" && len(v) <= 256 && !strings.ContainsAny(v, "\x00\r\n")
}
func text(v string, max int) bool {
	return strings.TrimSpace(v) != "" && len(v) <= max && !strings.ContainsRune(v, 0)
}
func (s *Store) put(t, id string, r record) error {
	if _, ok := s.records[key{t, id}]; !ok {
		n := 0
		for k := range s.records {
			if k.tenant == t {
				n++
			}
		}
		if n >= MaxRecords {
			return fmt.Errorf("%w: record limit reached", ErrInvalid)
		}
	}
	if err := s.table.Put(t, id, r); err != nil {
		return err
	}
	s.records[key{t, id}] = clone(r)
	return nil
}
func (s *Store) Control(t, agent string) Control {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r := s.records[key{t, "control:" + agent}]; r.Control != nil {
		return *r.Control
	}
	return Control{Agent: agent}
}
func (s *Store) SetControl(t, agent string, c Control, version int, by string, at time.Time) (Control, error) {
	if agent != "" && !identifier(agent) || len(c.Manager) > 256 || len(c.StopRules) > 4000 {
		return Control{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.records[key{t, "control:" + agent}]
	v := 0
	if r.Control != nil {
		v = r.Control.Version
	}
	if version != v {
		return Control{}, ErrConflict
	}
	c.Agent, c.Version, c.UpdatedBy, c.UpdatedAt = agent, v+1, by, at
	return c, s.put(t, "control:"+agent, record{Control: &c})
}
func (s *Store) Paused(t, agent string) bool {
	return s.Control(t, "").Paused || s.Control(t, agent).Paused
}
func (s *Store) Policy(t string, scope Scope) Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r := clone(s.records[key{t, "policy:" + scope.key()}]); r.Policy != nil {
		return *r.Policy
	}
	return Policy{Scope: scope, Rules: map[Category]Level{}}
}
func (s *Store) SetPolicy(t string, p Policy, version int, by string, at time.Time) (Policy, error) {
	if !p.Scope.valid() || len(p.Rules) > len(Categories) {
		return Policy{}, ErrInvalid
	}
	for c, l := range p.Rules {
		if !slices.Contains(Categories, c) || (l != Allow && l != Ask && l != Never) {
			return Policy{}, ErrInvalid
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.records[key{t, "policy:" + p.Scope.key()}]
	v := 0
	if r.Policy != nil {
		v = r.Policy.Version
	}
	if version != v {
		return Policy{}, ErrConflict
	}
	p.Version, p.UpdatedBy, p.UpdatedAt = v+1, by, at
	p.Rules = maps.Clone(p.Rules)
	return p, s.put(t, "policy:"+p.Scope.key(), record{Policy: &p})
}

// Decision returns only a tightening rule. Unspecified is not permission to act.
func (s *Store) Decision(t string, scopes []Scope, c Category) Level {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := Unspecified
	for _, scope := range scopes {
		if r := s.records[key{t, "policy:" + scope.key()}]; r.Policy != nil {
			l := r.Policy.Rules[c]
			if rank(l) > rank(out) {
				out = l
			}
		}
	}
	return out
}
func rank(l Level) int {
	switch l {
	case Allow:
		return 1
	case Ask:
		return 2
	case Never:
		return 3
	}
	return 0
}
func validState(s WorkState) bool {
	return slices.Contains([]WorkState{Requested, Accepted, InProgress, Watching, Result, Blocked, NeedsPerson, Cancelled}, s)
}
func transition(a, b WorkState) bool {
	if a == b {
		return true
	}
	switch a {
	case Requested:
		return b == Accepted || b == Watching || b == InProgress || b == Cancelled || b == NeedsPerson
	case Accepted, Watching, Blocked, NeedsPerson:
		return b == InProgress || b == Watching || b == Cancelled || b == Accepted || b == NeedsPerson || b == Blocked
	case InProgress:
		return b == Result || b == Blocked || b == NeedsPerson || b == Watching
	case Result:
		return b == Accepted || b == Watching
	}
	return false
}
func validAssignment(a Assignment) bool {
	return identifier(a.ID) && identifier(a.Agent) && identifier(a.Owner) && len(a.Project) <= 256 && len(a.Thread) <= 256 && text(a.Goal, 8000) && text(a.DoneWhen, 4000) && validState(a.State) && len(a.Blocker) <= 4000 && len(a.Cron) <= 128 && (a.State != Blocked || text(a.Blocker, 4000))
}
func (s *Store) Assignment(t, id string) (Assignment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r := s.records[key{t, "assignment:" + id}]
	if r.Assignment == nil {
		return Assignment{}, false
	}
	return *r.Assignment, true
}
func (s *Store) SetAssignment(t string, a Assignment, version int, by string, at time.Time) (Assignment, error) {
	if !validAssignment(a) {
		return Assignment{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.records[key{t, "assignment:" + a.ID}]
	v := 0
	if r.Assignment != nil {
		old := r.Assignment
		v = old.Version
		if old.State == InProgress || !transition(old.State, a.State) {
			return Assignment{}, ErrConflict
		}
		a.CreatedAt = old.CreatedAt
		a.RunID = old.RunID
	} else {
		if a.State != Requested {
			return Assignment{}, ErrInvalid
		}
		a.CreatedAt = at
		a.RunID = ""
	}
	if version != v {
		return Assignment{}, ErrConflict
	}
	a.Version, a.UpdatedBy, a.UpdatedAt = v+1, by, at
	return a, s.put(t, "assignment:"+a.ID, record{Assignment: &a})
}
func (s *Store) Assignments(t string, limit int, sees func(Assignment) bool) []Assignment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Assignment{}
	for k, r := range s.records {
		if k.tenant == t && r.Assignment != nil && sees != nil && sees(*r.Assignment) {
			out = append(out, *r.Assignment)
		}
	}
	slices.SortFunc(out, func(a, b Assignment) int { return strings.Compare(a.ID, b.ID) })
	if limit < 1 || limit > MaxResults {
		limit = MaxResults
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Reserve records the deterministic run identity before the platform admits it.
func (s *Store) Reserve(t, id, run string, version int, at time.Time) (Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.records[key{t, "assignment:" + id}]
	if r.Assignment == nil {
		return Assignment{}, ErrNotFound
	}
	a := *r.Assignment
	if a.State == InProgress {
		if a.RunID == run {
			return a, nil
		}
		return Assignment{}, ErrConflict
	}
	if a.Version != version || !transition(a.State, InProgress) || !identifier(run) {
		return Assignment{}, ErrConflict
	}
	a.State, a.RunID, a.UpdatedAt, a.Version = InProgress, run, at, a.Version+1
	return a, s.put(t, "assignment:"+id, record{Assignment: &a})
}
func (s *Store) Settle(t, run string, success bool, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.table.Batch()
	changes := map[key]record{}
	for k, r := range s.records {
		if k.tenant != t {
			continue
		}
		r = clone(r)
		changed := false
		if a := r.Assignment; a != nil && a.RunID == run && a.State == InProgress {
			a.State = Result
			if a.Cron != "" {
				a.State = Watching
			}
			if !success {
				a.State = Blocked
				a.Blocker = "The recorded run did not complete successfully."
			}
			a.Version++
			a.UpdatedAt = at
			changed = true
		}
		if h := r.Handoff; h != nil && h.RunID == run && (h.State == InProgress || h.State == Accepted) {
			h.State = Result
			if !success {
				h.State = Blocked
				h.Blocker = "The recorded run did not complete successfully."
			}
			h.Version++
			h.UpdatedAt = at
			changed = true
		}
		if changed {
			b.Put(t, k.id, r)
			changes[k] = r
		}
	}
	if err := b.Commit(); err != nil {
		return err
	}
	maps.Copy(s.records, changes)
	return nil
}
func (s *Store) SetHandoff(t string, h Handoff, version int, at time.Time) (Handoff, error) {
	if len(h.Chain) > 4 {
		return Handoff{}, ErrEscalation
	}
	seen := map[string]bool{}
	for _, a := range h.Chain {
		if seen[a] {
			return Handoff{}, ErrEscalation
		}
		seen[a] = true
	}
	if !identifier(h.ID) || !identifier(h.From) || !identifier(h.To) || !text(h.Goal, 8000) || !text(h.DoneWhen, 4000) || len(h.Inputs) > 32 || (len(h.Chain) < 2 && h.State != NeedsPerson) || !validState(h.State) || len(h.Blocker) > 4000 {
		return Handoff{}, ErrInvalid
	}
	for _, i := range h.Inputs {
		if !slices.Contains([]string{"run", "artifact", "thread", "text"}, i.Type) || !text(i.Reference, 4000) {
			return Handoff{}, ErrInvalid
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.records[key{t, "handoff:" + h.ID}]
	v := 0
	if r.Handoff != nil {
		old := r.Handoff
		v = old.Version
		if !transition(old.State, h.State) {
			return Handoff{}, ErrConflict
		}
		h.CreatedAt = old.CreatedAt
	} else {
		h.CreatedAt = at
	}
	if version != v {
		return Handoff{}, ErrConflict
	}
	h.Version, h.UpdatedAt = v+1, at
	h.Inputs = slices.Clone(h.Inputs)
	h.Chain = slices.Clone(h.Chain)
	return h, s.put(t, "handoff:"+h.ID, record{Handoff: &h})
}
func (s *Store) Handoffs(t string, limit int, sees func(Handoff) bool) []Handoff {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Handoff{}
	for k, r := range s.records {
		if k.tenant == t && r.Handoff != nil && sees != nil && sees(*r.Handoff) {
			out = append(out, *clone(r).Handoff)
		}
	}
	slices.SortFunc(out, func(a, b Handoff) int { return strings.Compare(a.ID, b.ID) })
	if limit < 1 || limit > MaxResults {
		limit = MaxResults
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Store) Handoff(t, id string) (Handoff, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r := clone(s.records[key{t, "handoff:" + id}])
	if r.Handoff == nil {
		return Handoff{}, false
	}
	return *r.Handoff, true
}

// Notice is unresolved work needing a person. It carries only its own place.
type Notice struct{ ID, Project, Thread, Run, Agent, Owner, Reason string }

// Due records missed deadlines before returning notices. Repeated scans return
// the same notice so the inbox can recover a crash between the two stores.
func (s *Store) Due(t string, at time.Time) ([]Notice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.table.Batch()
	after := s.noticeAfter[t]
	next := after
	changes := map[key]record{}
	out := []Notice{}
	keys := []key{}
	for k := range s.records {
		if k.tenant == t {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b key) int { return strings.Compare(a.id, b.id) })
	for _, k := range keys {
		r := clone(s.records[k])
		var n Notice
		changed := false
		if a := r.Assignment; a != nil {
			if !a.DueAt.IsZero() && at.After(a.DueAt) && a.State != Result && a.State != Cancelled && a.State != NeedsPerson {
				a.State = NeedsPerson
				a.Blocker = "The assignment deadline passed. A person must decide what happens next."
				a.Version++
				a.UpdatedAt = at
				changed = true
			}
			if a.State == NeedsPerson || a.State == Blocked {
				n = Notice{ID: a.ID, Project: a.Project, Thread: a.Thread, Run: a.RunID, Agent: a.Agent, Owner: a.Owner, Reason: a.Blocker}
			}
		}
		if h := r.Handoff; h != nil {
			if !h.DueAt.IsZero() && at.After(h.DueAt) && h.State != Result && h.State != Cancelled && h.State != NeedsPerson {
				h.State = NeedsPerson
				h.Blocker = "The handoff deadline passed. A person must choose the next owner."
				h.Version++
				h.UpdatedAt = at
				changed = true
			}
			if h.State == NeedsPerson || h.State == Blocked {
				n = Notice{ID: h.ID, Project: h.Project, Thread: h.Thread, Run: h.RunID, Agent: h.From, Owner: h.Owner, Reason: h.Blocker}
			}
		}
		if changed {
			b.Put(t, k.id, r)
			changes[k] = r
		}
		if n.ID != "" && len(out) < MaxResults && k.id > s.noticeAfter[t] {
			out = append(out, n)
			next = k.id
		}
	}
	if err := b.Commit(); err != nil {
		return nil, err
	}
	maps.Copy(s.records, changes)
	if len(out) < MaxResults {
		next = ""
	}
	s.noticeAfter[t] = next
	return out, nil
}

func (s *Store) Tenants() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{}
	out := []string{}
	for k := range s.records {
		if !seen[k.tenant] {
			seen[k.tenant] = true
			out = append(out, k.tenant)
		}
	}
	slices.Sort(out)
	return out
}

// BlockAssignment records a dependency or access refusal without inventing a
// terminal run outcome. Its schedule must be paused by the composition root.
func (s *Store) BlockAssignment(t, id, reason string, at time.Time) error {
	if !text(reason, 4000) {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.records[key{t, "assignment:" + id}]
	if r.Assignment == nil {
		return ErrNotFound
	}
	a := *r.Assignment
	if a.State == Cancelled || a.State == Result {
		return ErrConflict
	}
	a.State, a.Blocker, a.UpdatedAt, a.Version = Blocked, reason, at, a.Version+1
	return s.put(t, "assignment:"+id, record{Assignment: &a})
}
