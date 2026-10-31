package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/state"
)

// namespace is where operations are kept. The tenant is the shard key; in
// it:
//
//	control:<agent>           an agent's control (agent "" is the workspace's)
//	policy:<kind>:<name>      an action policy
//	assignment:<id>           an assignment, so assignments page in ID order
//	handoff:<id>              a handoff, likewise
//	#count                    how many of the four above the tenant holds
//	#notice                   where the next page of Due's notices starts
//	run:<run>                 the keys of the records a run may settle
//
// The tenant "" holds the directory of tenants (state.Tenants) and the
// converted marker.
const namespace = "operations"

const (
	countKey  = "#count"
	noticeKey = "#notice"
	// converted marks, in the tenant "", that every record a run may settle
	// is in its run's index.
	converted = "converted:v1"
	// pageSize is how many records one page of a scan reads.
	pageSize = 500
)

func runKey(run string) string { return "run:" + run }

// recordPrefixes are the keys that count toward MaxRecords.
var recordPrefixes = []string{"assignment:", "control:", "handoff:", "policy:"}

// Store keeps operations in a state.Store, read and written per request.
type Store struct {
	st      state.Store
	tenants *state.Tenants
}

// Open returns a store on st, which may be shared with other processes. A nil
// st keeps operations in this process's memory.
//
// Records a release before the run index kept are added to it the first time
// any process opens the store; a process that opens it at the same time adds
// the same entries.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	s := &Store{st: st}
	if err := s.convert(); err != nil {
		return nil, fmt.Errorf("operations: convert: %w", err)
	}
	d, err := state.OpenTenants(st, namespace)
	if err != nil {
		return nil, fmt.Errorf("operations: tenants: %w", err)
	}
	s.tenants = d
	return s, nil
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// lookup reads one record into v, and its version.
func (s *Store) lookup(t, k string, v any) (int64, error) {
	c, cancel := ctx()
	defer cancel()
	r, err := s.st.Lookup(c, namespace, t, k)
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(r.Value, v); err != nil {
		return 0, fmt.Errorf("%w: %q: %v", ErrInvalid, k, err)
	}
	return r.Version, nil
}

// check holds a record read back to what a write accepts. A malformed record
// is refused rather than read as missing, so a damaged restriction never
// reads as none.
func check(t, k string, r record) (record, error) {
	n := 0
	for _, set := range []bool{r.Control != nil, r.Policy != nil, r.Assignment != nil, r.Handoff != nil} {
		if set {
			n++
		}
	}
	if n != 1 {
		return record{}, fmt.Errorf("%w: malformed record %q", ErrInvalid, k)
	}
	if r.Control != nil && (!validReminder(r.Control.ReminderAfterSeconds) || !validReminder(r.Control.BlockedReminderAfterSeconds)) {
		return record{}, fmt.Errorf("%w: malformed reminder control", ErrInvalid)
	}
	if r.Policy != nil && (!validPolicy(*r.Policy) || k != "policy:"+r.Policy.Scope.key()) {
		return record{}, fmt.Errorf("%w: malformed policy", ErrInvalid)
	}
	if r.Invocation != nil && (r.Assignment == nil || !validInvocation(t, *r.Assignment, r.Invocation.Event) || r.Assignment.TagID != r.Invocation.Event.ID || runtime.RunID(t, r.Invocation.Event.ID) != r.Assignment.RunID) {
		return record{}, fmt.Errorf("%w: malformed invocation", ErrInvalid)
	}
	if r.HandoffRequester != "" && (r.Handoff == nil || !identifier(r.HandoffRequester)) {
		return record{}, fmt.Errorf("%w: malformed handoff requester", ErrInvalid)
	}
	if r.Handoff != nil {
		r.Handoff.Requester = r.HandoffRequester
	}
	return r, nil
}

func decode(t string, rec state.Record) (record, error) {
	var r record
	if err := json.Unmarshal(rec.Value, &r); err != nil {
		return record{}, fmt.Errorf("%w: %q: %v", ErrInvalid, rec.Key, err)
	}
	return check(t, rec.Key, r)
}

// get reads one record and its version; ok is false when there is none.
func (s *Store) get(t, k string) (r record, version int64, ok bool, err error) {
	c, cancel := ctx()
	defer cancel()
	rec, err := s.st.Lookup(c, namespace, t, k)
	if errors.Is(err, state.ErrNotFound) {
		return record{}, 0, false, nil
	}
	if err != nil {
		return record{}, 0, false, err
	}
	r, err = decode(t, rec)
	if err != nil {
		return record{}, 0, false, err
	}
	return r, rec.Version, true, nil
}

// batch is one write's ops, built as JSON.
type batch struct {
	ops []state.Op
	err error
}

// put adds a put at version, or a create when version is 0.
func (b *batch) put(t, k string, v any, version int64) {
	raw, err := json.Marshal(v)
	if err != nil {
		b.err = errors.Join(b.err, err)
		return
	}
	if version == 0 {
		b.ops = append(b.ops, state.Create(t, k, raw))
		return
	}
	b.ops = append(b.ops, state.PutAt(t, k, raw, version))
}

func (b *batch) commit(st state.Store) error {
	if b.err != nil {
		return b.err
	}
	if len(b.ops) == 0 {
		return nil
	}
	c, cancel := ctx()
	defer cancel()
	return st.Write(c, namespace, b.ops...)
}

// settleRun is the run that may settle a record, or "" when none may.
func settleRun(r record) string {
	if a := r.Assignment; a != nil && a.RunID != "" && (a.State == InProgress || (a.State == NeedsPerson && a.DeadlineMissed)) {
		return a.RunID
	}
	if h := r.Handoff; h != nil && h.RunID != "" && (h.State == InProgress || h.State == Accepted || (h.State == NeedsPerson && h.DeadlineMissed)) {
		return h.RunID
	}
	return ""
}

// countRecords counts a tenant's records, a page at a time, stopping past
// the cap.
func (s *Store) countRecords(t string) (int, error) {
	n := 0
	for _, prefix := range recordPrefixes {
		after := ""
		for n <= MaxRecords {
			c, cancel := ctx()
			page, next, err := s.st.List(c, namespace, t, state.Query{Prefix: prefix, After: after, Limit: state.MaxList})
			cancel()
			if err != nil {
				return 0, err
			}
			n += len(page)
			if next == "" {
				break
			}
			after = next
		}
	}
	return n, nil
}

// grow adds one record to a tenant's count in b, refusing past MaxRecords. A
// tenant written before the count was kept is counted once.
func (s *Store) grow(t string, b *batch) error {
	var n int
	at, err := s.lookup(t, countKey, &n)
	if errors.Is(err, state.ErrNotFound) {
		if n, err = s.countRecords(t); err != nil {
			return err
		}
		at = 0
	} else if err != nil {
		return err
	}
	if n >= MaxRecords {
		return fmt.Errorf("%w: record limit reached", ErrInvalid)
	}
	b.put(t, countKey, n+1, at)
	return nil
}

// index adds a record's key to its run's index in b.
func (s *Store) index(t string, b *batch, run, k string) error {
	var keys []string
	at, err := s.lookup(t, runKey(run), &keys)
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return err
	}
	if slices.Contains(keys, k) {
		return nil
	}
	b.put(t, runKey(run), append(keys, k), at)
	return nil
}

// write changes one record by compare-and-set: fn is given the record as it
// stands and returns what to write, or state.ErrUnchanged to write nothing.
// A new record counts toward the tenant's cap, and a record a run may now
// settle joins the run's index, in the same write. When another writer moved
// any of them, fn runs again on what is there.
func (s *Store) write(t, k string, fn func(old record, ok bool) (record, error)) (record, error) {
	if err := s.tenants.Mark(t); err != nil {
		return record{}, err
	}
	var out record
	err := state.Retry(func() error {
		old, at, ok, err := s.get(t, k)
		if err != nil {
			return err
		}
		r, err := fn(clone(old), ok)
		if errors.Is(err, state.ErrUnchanged) {
			out = old
			return nil
		}
		if err != nil {
			return err
		}
		if r.Handoff != nil {
			r.HandoffRequester = r.Handoff.Requester
		}
		b := &batch{}
		b.put(t, k, r, at)
		if !ok {
			if err := s.grow(t, b); err != nil {
				return err
			}
		}
		if run := settleRun(r); run != "" && (!ok || settleRun(old) != run) {
			if err := s.index(t, b, run, k); err != nil {
				return err
			}
		}
		if err := b.commit(s.st); err != nil {
			return err
		}
		out = r
		return nil
	})
	if err != nil {
		return record{}, err
	}
	return clone(out), nil
}

// scan calls fn for every record under prefix, a page at a time, after a
// key (exclusive, "" for the first). fn returning false stops the scan. The
// tenant's cap bounds it.
func (s *Store) scan(t, prefix, after string, fn func(k string, r record) (bool, error)) error {
	for {
		c, cancel := ctx()
		page, next, err := s.st.List(c, namespace, t, state.Query{Prefix: prefix, After: after, Limit: pageSize})
		cancel()
		if err != nil {
			return err
		}
		for _, rec := range page {
			r, err := decode(t, rec)
			if err != nil {
				return err
			}
			more, err := fn(rec.Key, r)
			if err != nil || !more {
				return err
			}
		}
		if next == "" {
			return nil
		}
		after = next
	}
}

// convert adds every record a run may settle to its run's index.
func (s *Store) convert() error {
	var done int
	if _, err := s.lookup("", converted, &done); err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	c, cancel := ctx()
	recs, err := s.st.Load(c, namespace)
	cancel()
	if err != nil {
		return err
	}
	for _, rec := range recs {
		if rec.Tenant == "" || !strings.HasPrefix(rec.Key, "assignment:") && !strings.HasPrefix(rec.Key, "handoff:") {
			continue
		}
		r, err := decode(rec.Tenant, rec)
		if err != nil {
			// A malformed record is refused when it is read; it settles
			// nothing.
			continue
		}
		run := settleRun(r)
		if run == "" {
			continue
		}
		err = state.Retry(func() error {
			b := &batch{}
			if err := s.index(rec.Tenant, b, run, rec.Key); err != nil {
				return err
			}
			return b.commit(s.st)
		})
		if err != nil {
			return err
		}
	}
	b := &batch{}
	b.put("", converted, 1, 0)
	if err := b.commit(s.st); err != nil && !errors.Is(err, state.ErrConflict) {
		return err
	}
	return nil
}

// EachTenant calls fn for every tenant that holds operations, a page at a
// time.
func (s *Store) EachTenant(fn func(tenant string) error) error { return s.tenants.Each(fn) }

func (s *Store) Control(t, agent string) (Control, error) {
	r, _, _, err := s.get(t, "control:"+agent)
	if err != nil {
		return Control{}, err
	}
	if r.Control != nil {
		return *r.Control, nil
	}
	return Control{Agent: agent}, nil
}

func (s *Store) SetControl(t, agent string, c Control, version int, by string, at time.Time) (Control, error) {
	if agent != "" && !identifier(agent) || len(c.Manager) > 256 || len(c.StopRules) > 4000 || !validReminder(c.ReminderAfterSeconds) || !validReminder(c.BlockedReminderAfterSeconds) {
		return Control{}, ErrInvalid
	}
	r, err := s.write(t, "control:"+agent, func(old record, _ bool) (record, error) {
		v := 0
		if old.Control != nil {
			v = old.Control.Version
		}
		if version != v {
			return record{}, ErrConflict
		}
		c := c
		c.Agent, c.Version, c.UpdatedBy, c.UpdatedAt = agent, v+1, by, at
		return record{Control: &c}, nil
	})
	if err != nil {
		return Control{}, err
	}
	return *r.Control, nil
}

// Paused reports whether the workspace or the agent is paused. A read that
// fails returns its error, never false.
func (s *Store) Paused(t, agent string) (bool, error) {
	for _, a := range []string{"", agent} {
		c, err := s.Control(t, a)
		if err != nil {
			return false, err
		}
		if c.Paused {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) Policy(t string, scope Scope) (Policy, error) {
	r, _, _, err := s.get(t, "policy:"+scope.key())
	if err != nil {
		return Policy{}, err
	}
	if r.Policy != nil {
		return *r.Policy, nil
	}
	return Policy{Scope: scope, Rules: map[Category]Level{}}, nil
}

func (s *Store) SetPolicy(t string, p Policy, version int, by string, at time.Time) (Policy, error) {
	if !validPolicy(p) {
		return Policy{}, ErrInvalid
	}
	r, err := s.write(t, "policy:"+p.Scope.key(), func(old record, _ bool) (record, error) {
		v := 0
		if old.Policy != nil {
			v = old.Policy.Version
		}
		if version != v {
			return record{}, ErrConflict
		}
		p := p
		p.Version, p.UpdatedBy, p.UpdatedAt = v+1, by, at
		p.Rules = maps.Clone(p.Rules)
		if p.Rules == nil {
			p.Rules = map[Category]Level{}
		}
		return record{Policy: &p}, nil
	})
	if err != nil {
		return Policy{}, err
	}
	return *r.Policy, nil
}

// Decision returns only a tightening rule. Unspecified is not permission to
// act. A read that fails returns its error, never a level.
func (s *Store) Decision(t string, scopes []Scope, c Category) (Level, error) {
	out := Unspecified
	for _, scope := range scopes {
		r, _, _, err := s.get(t, "policy:"+scope.key())
		if err != nil {
			return Unspecified, err
		}
		if r.Policy != nil {
			if l := r.Policy.Rules[c]; rank(l) > rank(out) {
				out = l
			}
		}
	}
	return out, nil
}

func (s *Store) Assignment(t, id string) (Assignment, bool, error) {
	r, _, _, err := s.get(t, "assignment:"+id)
	if err != nil || r.Assignment == nil {
		return Assignment{}, false, err
	}
	return *r.Assignment, true, nil
}

func (s *Store) SetAssignment(t string, a Assignment, version int, by string, at time.Time) (Assignment, error) {
	if a.Trigger == "" {
		if a.Cron != "" {
			a.Trigger = CronTrigger
		} else {
			a.Trigger = ManualTrigger
		}
	}
	if !validAssignment(a) {
		return Assignment{}, ErrInvalid
	}
	r, err := s.write(t, "assignment:"+a.ID, func(r record, _ bool) (record, error) {
		a := a
		v := 0
		if old := r.Assignment; old != nil {
			v = old.Version
			if old.State == InProgress || !transition(old.State, a.State) {
				return record{}, ErrConflict
			}
			a.CreatedAt = old.CreatedAt
			a.RunID = old.RunID
			a.SourceVersion = old.SourceVersion
			a.TagID = old.TagID
			a.DeadlineMissed = old.DeadlineMissed
			if !a.DueAt.Equal(old.DueAt) && (a.DueAt.IsZero() || a.DueAt.After(at)) {
				a.DeadlineMissed = false
				if old.DeadlineMissed && a.State != Blocked {
					a.Blocker = ""
				}
			}
		} else {
			if a.State != Requested {
				return record{}, ErrInvalid
			}
			a.CreatedAt = at
			a.RunID = ""
		}
		if version != v {
			return record{}, ErrConflict
		}
		a.Version, a.UpdatedBy, a.UpdatedAt = v+1, by, at
		return record{Assignment: &a}, nil
	})
	if err != nil {
		return Assignment{}, err
	}
	return *r.Assignment, nil
}

func (s *Store) Assignments(t string, limit int, sees func(Assignment) bool) ([]Assignment, error) {
	out, _, err := s.AssignmentPage(t, "", limit, sees)
	return out, err
}

// Reserve records the deterministic run identity before the platform admits it.
func (s *Store) Reserve(t, id, run string, version int, at time.Time) (Assignment, error) {
	return s.reserve(t, id, func(r record) (record, error) {
		a := *r.Assignment
		if a.State == InProgress {
			if a.RunID == run {
				return record{}, state.ErrUnchanged
			}
			return record{}, ErrConflict
		}
		if a.Version != version || !transition(a.State, InProgress) || !identifier(run) {
			return record{}, ErrConflict
		}
		a.State, a.RunID, a.UpdatedAt, a.Version = InProgress, run, at, a.Version+1
		return record{Assignment: &a}, nil
	})
}

// reserve changes an assignment that must exist.
func (s *Store) reserve(t, id string, fn func(r record) (record, error)) (Assignment, error) {
	r, err := s.write(t, "assignment:"+id, func(r record, _ bool) (record, error) {
		if r.Assignment == nil {
			return record{}, ErrNotFound
		}
		return fn(r)
	})
	if err != nil {
		return Assignment{}, err
	}
	return *r.Assignment, nil
}

// Settle ends what a terminal run was recorded for: the records in its
// index, in one write that also drops the index.
func (s *Store) Settle(t, run string, success bool, at time.Time) error {
	return state.Retry(func() error {
		var keys []string
		iv, err := s.lookup(t, runKey(run), &keys)
		if errors.Is(err, state.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		b := &batch{}
		for _, k := range keys {
			r, v, ok, err := s.get(t, k)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			changed := false
			if a := r.Assignment; a != nil && a.RunID == run && (a.State == InProgress || (a.State == NeedsPerson && a.DeadlineMissed)) {
				a.State = Result
				a.Blocker = ""
				if (a.Cron != "" || a.Trigger == SourceTrigger) && !a.DeadlineMissed {
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
			if h := r.Handoff; h != nil && h.RunID == run && (h.State == InProgress || h.State == Accepted || (h.State == NeedsPerson && h.DeadlineMissed)) {
				h.State = Result
				if !success {
					h.State = Blocked
					h.Blocker = "The recorded run did not complete successfully."
				}
				h.Version++
				h.UpdatedAt = at
				r.HandoffRequester = h.Requester
				changed = true
			}
			if changed {
				b.put(t, k, r, v)
			}
		}
		b.ops = append(b.ops, state.DeleteAt(t, runKey(run), iv))
		return b.commit(s.st)
	})
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
	if h.Requester != "" && !identifier(h.Requester) {
		return Handoff{}, ErrInvalid
	}
	for _, i := range h.Inputs {
		if !slices.Contains([]string{"run", "artifact", "thread", "text"}, i.Type) || !text(i.Reference, 4000) {
			return Handoff{}, ErrInvalid
		}
	}
	r, err := s.write(t, "handoff:"+h.ID, func(r record, _ bool) (record, error) {
		h := h
		v := 0
		if old := r.Handoff; old != nil {
			h.Requester = old.Requester
			v = old.Version
			if !transition(old.State, h.State) {
				return record{}, ErrConflict
			}
			h.CreatedAt = old.CreatedAt
			h.DeadlineMissed = old.DeadlineMissed
			if !h.DueAt.Equal(old.DueAt) && (h.DueAt.IsZero() || h.DueAt.After(at)) {
				h.DeadlineMissed = false
			}
		} else {
			h.CreatedAt = at
		}
		if version != v {
			return record{}, ErrConflict
		}
		h.Version, h.UpdatedAt = v+1, at
		h.Inputs = slices.Clone(h.Inputs)
		h.Chain = slices.Clone(h.Chain)
		return record{Handoff: &h}, nil
	})
	if err != nil {
		return Handoff{}, err
	}
	return *r.Handoff, nil
}

// Handoffs returns up to limit handoffs sees keeps, in ID order.
func (s *Store) Handoffs(t string, limit int, sees func(Handoff) bool) ([]Handoff, error) {
	if limit < 1 || limit > MaxResults {
		limit = MaxResults
	}
	out := []Handoff{}
	err := s.scan(t, "handoff:", "", func(_ string, r record) (bool, error) {
		if r.Handoff != nil && sees != nil && sees(*r.Handoff) {
			out = append(out, *r.Handoff)
		}
		return len(out) < limit, nil
	})
	return out, err
}

func (s *Store) Handoff(t, id string) (Handoff, bool, error) {
	r, _, _, err := s.get(t, "handoff:"+id)
	if err != nil || r.Handoff == nil {
		return Handoff{}, false, err
	}
	return *r.Handoff, true, nil
}

// Notice is unresolved work needing a person. It carries only its own place.
type Notice struct{ Kind, ID, Project, Thread, Run, Agent, Owner, Reason string }

// missDeadline marks a record whose deadline passed, and reports whether it
// changed it.
func missDeadline(r *record, at time.Time) bool {
	if a := r.Assignment; a != nil && !a.DueAt.IsZero() && at.After(a.DueAt) && a.State != Result && a.State != Cancelled && a.State != NeedsPerson {
		a.State = NeedsPerson
		a.DeadlineMissed = true
		a.Blocker = "The assignment deadline passed. A person must decide what happens next."
		a.Version++
		a.UpdatedAt = at
		return true
	}
	if h := r.Handoff; h != nil && !h.DueAt.IsZero() && at.After(h.DueAt) && h.State != Result && h.State != Cancelled && h.State != NeedsPerson {
		h.State = NeedsPerson
		h.DeadlineMissed = true
		h.Blocker = "The handoff deadline passed. A person must choose the next owner."
		h.Version++
		h.UpdatedAt = at
		return true
	}
	return false
}

func notice(r record) Notice {
	if a := r.Assignment; a != nil && (a.State == NeedsPerson || a.State == Blocked) {
		return Notice{Kind: "assignment", ID: a.ID, Project: a.Project, Thread: a.Thread, Run: a.RunID, Agent: a.Agent, Owner: a.Owner, Reason: a.Blocker}
	}
	if h := r.Handoff; h != nil && (h.State == NeedsPerson || h.State == Blocked) {
		return Notice{Kind: "handoff", ID: h.ID, Project: h.Project, Thread: h.Thread, Run: h.RunID, Agent: h.From, Owner: h.Owner, Reason: h.Blocker}
	}
	return Notice{}
}

// Due records missed deadlines before returning notices. Repeated scans return
// the same notice so the inbox can recover a crash between the two stores.
// Notices come at most MaxResults at a time; where the next page starts is
// stored, so every process continues from it.
func (s *Store) Due(t string, at time.Time) ([]Notice, error) {
	var after string
	nv, err := s.lookup(t, noticeKey, &after)
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return nil, err
	}
	next := after
	out := []Notice{}
	for _, prefix := range []string{"assignment:", "handoff:"} {
		err := s.scan(t, prefix, "", func(k string, r record) (bool, error) {
			if missDeadline(&r, at) {
				var err error
				r, err = s.write(t, k, func(r record, ok bool) (record, error) {
					if !ok || !missDeadline(&r, at) {
						return record{}, state.ErrUnchanged
					}
					return r, nil
				})
				if err != nil {
					return false, err
				}
			}
			if n := notice(r); n.ID != "" && len(out) < MaxResults && k > after {
				out = append(out, n)
				next = k
			}
			return true, nil
		})
		if err != nil {
			return nil, err
		}
	}
	if len(out) < MaxResults {
		next = ""
	}
	if next != after || nv == 0 && next != "" {
		b := &batch{}
		b.put(t, noticeKey, next, nv)
		// Another process scanning at once moved the cursor as well; either
		// position returns every notice in time.
		if err := b.commit(s.st); err != nil && !errors.Is(err, state.ErrConflict) {
			return nil, err
		}
	}
	return out, nil
}

// BlockAssignment records a dependency or access refusal without inventing a
// terminal run outcome. Its schedule must be paused by the composition root.
func (s *Store) BlockAssignment(t, id, reason string, at time.Time) error {
	if !text(reason, 4000) {
		return ErrInvalid
	}
	_, err := s.reserve(t, id, func(r record) (record, error) {
		a := *r.Assignment
		if a.State == Cancelled || a.State == Result {
			return record{}, ErrConflict
		}
		a.State, a.Blocker, a.UpdatedAt, a.Version = Blocked, reason, at, a.Version+1
		return record{Assignment: &a}, nil
	})
	return err
}

// HasConversation bounds a publication dependency check by the tenant record
// cap. Even terminal contracts retain raw inputs that require summary review.
func (s *Store) HasConversation(t, thread string) (bool, error) {
	if thread == "" {
		return false, nil
	}
	found := false
	for _, prefix := range []string{"assignment:", "handoff:"} {
		err := s.scan(t, prefix, "", func(_ string, r record) (bool, error) {
			if a := r.Assignment; a != nil && (a.Thread == thread || a.RunID == thread) {
				found = true
			}
			if h := r.Handoff; h != nil && (h.Thread == thread || h.RunID == thread) {
				found = true
			}
			return !found, nil
		})
		if err != nil || found {
			return found, err
		}
	}
	return false, nil
}
