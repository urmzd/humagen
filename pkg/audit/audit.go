// Package audit keeps bounded, durable attempts and their observed outcomes.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds nothing in memory, so any number of processes on one database agree,
// and it does not need pkg/state's single-writer lock.
package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/state"
)

const (
	MaxRecords = 100000
	MaxRows    = 500
	MaxWindow  = 31 * 24 * time.Hour
)

// namespace is where attempts are kept. The tenant is the shard key; within
// it:
//
//	<id>             a record, by its ID (a_<random>, or a<seq> from an
//	                 older release)
//	t:<at>:<id>      the same record, ordered by when it was attempted, which
//	                 a window and retention page
//	!n               how many records the tenant holds
//	!next            a counter an older release kept; no longer written
//
// <at> is the instant as a key that sorts in time order. A record and its
// copy under t: change in one write, so they always agree. IDs are random,
// so two processes beginning at the same instant never write one key.
const (
	namespace   = "audit"
	indexPrefix = "t:"
	countKey    = "!n"
	// converted marks, in the tenant "", that records kept before the
	// index existed have their copy under t:.
	converted = "converted:v1"
	// retainBatch is the most records one retention write removes.
	retainBatch = 500
)

var (
	ErrInvalid  = errors.New("audit: invalid")
	ErrNotFound = errors.New("audit: not found")
	ErrConflict = errors.New("audit: already completed")
	ErrCapacity = errors.New("audit: retention capacity reached")
)

type Outcome string

const (
	Pending   Outcome = "pending"
	Succeeded Outcome = "succeeded"
	Failed    Outcome = "failed"
)

// Change contains only an explicit safe snapshot, never a request body.
// Subject scopes a personal record; Actor is always supplied separately.
// Scope is workspace, project, personal or agent. An omitted scope is inferred
// from Subject, Agent and Project, in that order, else workspace.
type Change struct {
	Route    string            `json:"route,omitempty"`
	Rule     string            `json:"rule,omitempty"`
	Action   string            `json:"action"`
	Resource string            `json:"resource"`
	ID       string            `json:"resource_id"`
	Scope    string            `json:"scope"`
	Project  string            `json:"project,omitempty"`
	Thread   string            `json:"thread,omitempty"`
	Subject  string            `json:"subject,omitempty"`
	Agent    string            `json:"agent,omitempty"`
	Before   map[string]string `json:"before,omitempty"`
	After    map[string]string `json:"after,omitempty"`
}

// Record is an attempt and what the writer could durably observe afterward.
// Pending includes a process lost between a domain commit and Finish. It never
// asserts that the change succeeded or failed.
type Record struct {
	Change
	ID         string    `json:"id"`
	Actor      string    `json:"actor"`
	At         time.Time `json:"at"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
	Outcome    Outcome   `json:"outcome"`
}

// Attempt identifies a pending record without retaining credential material.
type Attempt struct {
	ID string `json:"id"`
}
type Query struct {
	Actor    string
	Resource string
	From, To time.Time
	Limit    int
}

// Store keeps audit records in a state.Store.
type Store struct {
	st      state.Store
	tenants *state.Tenants
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
var opaqueIDPattern = regexp.MustCompile(`^a_[0-9a-f]{32}$`)
var secrets = regexp.MustCompile(`(?i)(bearer\s+[^\s]+|(?:sk-ant-|sk-|ghp_|gho_|github_pat_|xox[baprs]-)[a-z0-9_-]{8,}|(?:[?&](?:token|api_key|key|secret|password)=)[^&\s]+|://[^\s/@:]+:[^\s/@]+@)`)

func safeFields(fields map[string]string) (map[string]string, error) {
	if len(fields) > 32 {
		return nil, ErrInvalid
	}
	if fields == nil {
		return nil, nil
	}
	out := make(map[string]string, len(fields))
	for key, value := range fields {
		if key == "" || len(key) > 64 || !utf8.ValidString(key) || !utf8.ValidString(value) || strings.ContainsRune(key+value, 0) {
			return nil, ErrInvalid
		}
		sensitive := false
		for _, word := range []string{"password", "secret", "token", "credential", "api_key", "private_key", "authorization"} {
			sensitive = sensitive || strings.Contains(strings.ToLower(key), word)
		}
		if sensitive {
			out[key] = "[redacted]"
			continue
		}
		value = secrets.ReplaceAllString(value, "[redacted]")
		if text := []rune(value); len(text) > 500 {
			value = string(text[:500]) + "..."
		}
		out[key] = value
	}
	raw, _ := json.Marshal(out)
	if len(raw) > 8192 {
		return nil, ErrInvalid
	}
	return out, nil
}
func clean(change Change) (Change, error) {
	if !namePattern.MatchString(change.Action) || !namePattern.MatchString(change.Resource) || change.ID == "" {
		return Change{}, ErrInvalid
	}
	for _, value := range []string{change.ID, change.Project, change.Thread, change.Subject, change.Agent, change.Route, change.Rule} {
		if len(value) > 512 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return Change{}, ErrInvalid
		}
	}
	if change.Scope == "" {
		switch {
		case change.Subject != "":
			change.Scope = "personal"
		case change.Agent != "":
			change.Scope = "agent"
		case change.Project != "":
			change.Scope = "project"
		default:
			change.Scope = "workspace"
		}
	}
	switch change.Scope {
	case "workspace", "project":
	case "personal":
		if change.Subject == "" {
			return Change{}, ErrInvalid
		}
	case "agent":
		if change.Agent == "" {
			return Change{}, ErrInvalid
		}
	default:
		return Change{}, ErrInvalid
	}
	var err error
	if change.Before, err = safeFields(change.Before); err != nil {
		return Change{}, err
	}
	if change.After, err = safeFields(change.After); err != nil {
		return Change{}, err
	}
	return change, nil
}
func validIdentity(value string) bool {
	return value != "" && len(value) <= 512 && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func clone(r Record) Record { r.Before = maps.Clone(r.Before); r.After = maps.Clone(r.After); return r }

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps records in this process's memory.
//
// Records kept by a release before the index are given their copy under t:
// the first time any process opens the store; a process that opens it at
// the same time finds them done.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	s := &Store{st: st}
	if err := s.convert(); err != nil {
		return nil, err
	}
	tenants, err := state.OpenTenants(st, namespace)
	if err != nil {
		return nil, err
	}
	s.tenants = tenants
	return s, nil
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// timeKey formats an instant as a key that sorts in time order, the sign
// bit flipped so instants before 1970 sort first.
func timeKey(t time.Time) string {
	return fmt.Sprintf("%020d", uint64(t.UnixNano())^(1<<63))
}

func indexKey(r Record) string { return indexPrefix + timeKey(r.At) + ":" + r.ID }

// check holds a stored record to what Begin and Finish write.
func check(tenant, key string, r Record) (Record, error) {
	checked, err := clean(r.Change)
	if err != nil || r.ID != key || !validIdentity(tenant) || !validIdentity(r.Actor) || r.At.IsZero() || (r.Outcome != Pending && r.Outcome != Succeeded && r.Outcome != Failed) || (r.Outcome == Pending && !r.FinishedAt.IsZero()) || (r.Outcome != Pending && (r.FinishedAt.IsZero() || r.FinishedAt.Before(r.At))) {
		return Record{}, ErrInvalid
	}
	if !opaqueIDPattern.MatchString(r.ID) {
		if _, err := strconv.ParseUint(strings.TrimPrefix(r.ID, "a"), 10, 64); err != nil {
			return Record{}, ErrInvalid
		}
	}
	r.Change = checked
	return r, nil
}

// convert gives every record kept before the index its copy under t:. Each
// record is done in one write conditional on the record, so a record two
// processes do at once is done once, and one removed meanwhile gets none.
func (s *Store) convert() error {
	c, cancel := ctx()
	_, err := s.st.Lookup(c, namespace, "", converted)
	cancel()
	if err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	c, cancel = ctx()
	rows, err := s.st.Load(c, namespace)
	cancel()
	if err != nil {
		return err
	}
	indexed := map[string]bool{}
	for _, row := range rows {
		if row.Tenant != "" && strings.HasPrefix(row.Key, indexPrefix) {
			indexed[row.Tenant+"\x00"+row.Key] = true
		}
	}
	for _, row := range rows {
		if row.Tenant == "" || strings.HasPrefix(row.Key, "!") || strings.HasPrefix(row.Key, indexPrefix) {
			continue
		}
		var r Record
		if err := json.Unmarshal(row.Value, &r); err != nil {
			return err
		}
		if _, err := check(row.Tenant, row.Key, r); err != nil {
			return err
		}
		if indexed[row.Tenant+"\x00"+indexKey(r)] {
			continue
		}
		c, cancel := ctx()
		err := s.st.Write(c, namespace,
			state.PutAt(row.Tenant, row.Key, row.Value, row.Version),
			state.Create(row.Tenant, indexKey(r), row.Value))
		cancel()
		if err != nil && !errors.Is(err, state.ErrConflict) {
			return err
		}
	}
	c, cancel = ctx()
	defer cancel()
	err = s.st.Write(c, namespace, state.Create("", converted, []byte("1")))
	if err != nil && !errors.Is(err, state.ErrConflict) {
		return err
	}
	return nil
}

// count reads how many records a tenant holds, and the version of that
// count: 0 when none is kept yet, in which case it counts the index,
// reading at most MaxRecords.
func (s *Store) count(tenant string) (int, int64, error) {
	c, cancel := ctx()
	r, err := s.st.Lookup(c, namespace, tenant, countKey)
	cancel()
	if err == nil {
		var n int
		if err := json.Unmarshal(r.Value, &n); err != nil {
			return 0, 0, err
		}
		return n, r.Version, nil
	}
	if !errors.Is(err, state.ErrNotFound) {
		return 0, 0, err
	}
	n := 0
	after := ""
	for {
		c, cancel := ctx()
		recs, next, err := s.st.List(c, namespace, tenant, state.Query{Prefix: indexPrefix, After: after, Limit: state.MaxList})
		cancel()
		if err != nil {
			return 0, 0, err
		}
		n += len(recs)
		if next == "" || n > MaxRecords {
			return n, 0, nil
		}
		after = next
	}
}

// countOp writes a tenant's count at the version read.
func countOp(tenant string, n int, version int64) state.Op {
	raw, _ := json.Marshal(n)
	if version == 0 {
		return state.Create(tenant, countKey, raw)
	}
	return state.PutAt(tenant, countKey, raw, version)
}

func (s *Store) write(ops ...state.Op) error {
	c, cancel := ctx()
	defer cancel()
	return s.st.Write(c, namespace, ops...)
}

// Begin must succeed before a caller performs its already-authorized mutation.
func (s *Store) Begin(tenant, actor string, change Change, at time.Time) (Attempt, error) {
	checked, err := clean(change)
	if err != nil {
		return Attempt{}, err
	}
	if !validIdentity(tenant) || !validIdentity(actor) || at.IsZero() {
		return Attempt{}, ErrInvalid
	}
	if err := s.tenants.Mark(tenant); err != nil {
		return Attempt{}, err
	}
	var out Attempt
	err = state.Retry(func() error {
		n, version, err := s.count(tenant)
		if err != nil {
			return err
		}
		if n >= MaxRecords {
			return ErrCapacity
		}
		// Public IDs must not reveal the number of hidden personal records.
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		r := Record{Change: checked, ID: "a_" + hex.EncodeToString(nonce[:]), Actor: actor, At: at.UTC(), Outcome: Pending}
		raw, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if err := s.write(countOp(tenant, n+1, version), state.Create(tenant, r.ID, raw), state.Create(tenant, indexKey(r), raw)); err != nil {
			return err
		}
		out = Attempt{ID: r.ID}
		return nil
	})
	if err != nil {
		return Attempt{}, err
	}
	return out, nil
}

// Finish records an observed result once. A failed write leaves Pending intact.
// Error strings are deliberately not stored: upstream responses can hold secrets.
func (s *Store) Finish(tenant, id string, result error, at time.Time) error {
	if id == "" || strings.HasPrefix(id, "!") || strings.HasPrefix(id, indexPrefix) {
		return ErrNotFound
	}
	next := Succeeded
	if result != nil {
		next = Failed
	}
	return state.Retry(func() error {
		c, cancel := ctx()
		row, err := s.st.Lookup(c, namespace, tenant, id)
		cancel()
		if errors.Is(err, state.ErrNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var r Record
		if err := json.Unmarshal(row.Value, &r); err != nil {
			return err
		}
		if r.Outcome != Pending {
			if r.Outcome == next {
				return nil
			}
			return ErrConflict
		}
		if at.IsZero() || at.Before(r.At) {
			return ErrInvalid
		}
		r.Outcome = next
		r.FinishedAt = at.UTC()
		raw, err := json.Marshal(r)
		if err != nil {
			return err
		}
		return s.write(state.PutAt(tenant, id, raw, row.Version), state.Put(tenant, indexKey(r), raw))
	})
}

// Record appends an event whose outcome is already known. Use Begin/Finish
// around a mutation when a failure to record the attempt must prevent it.
func (s *Store) Record(tenant, actor string, change Change, at time.Time) error {
	a, err := s.Begin(tenant, actor, change, at)
	if err != nil {
		return err
	}
	return s.Finish(tenant, a.ID, nil, at)
}

// List filters before choosing the newest rows, so hidden records affect neither
// returned counts nor which visible rows fit in the bounded result. It pages
// the window from its newest record down, so it reads no more than the
// tenant holds, which MaxRecords bounds.
func (s *Store) List(tenant string, q Query, visible func(Record) bool) ([]Record, error) {
	if len(q.Actor) > 512 || !utf8.ValidString(q.Actor) || strings.ContainsRune(q.Actor, 0) || q.Resource != "" && !namePattern.MatchString(q.Resource) {
		return nil, ErrInvalid
	}
	if q.From.IsZero() || !q.From.Before(q.To) || q.To.Sub(q.From) > MaxWindow {
		return nil, ErrInvalid
	}
	limit := min(max(q.Limit, 1), MaxRows)
	if q.Limit == 0 {
		limit = 100
	}
	out := []Record{}
	floor := indexPrefix + timeKey(q.From)
	after := indexPrefix + timeKey(q.To)
	for {
		c, cancel := ctx()
		rows, next, err := s.st.List(c, namespace, tenant, state.Query{Prefix: indexPrefix, After: after, Limit: state.MaxList, Desc: true})
		cancel()
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.Key < floor {
				return out, nil
			}
			var r Record
			if err := json.Unmarshal(row.Value, &r); err != nil {
				return nil, err
			}
			if q.Actor != "" && r.Actor != q.Actor || q.Resource != "" && r.Resource != q.Resource || visible != nil && !visible(clone(r)) {
				continue
			}
			out = append(out, r)
			if len(out) == limit {
				return out, nil
			}
		}
		if next == "" {
			return out, nil
		}
		after = next
	}
}

// EachTenant calls fn for every workspace that has recorded an attempt, a
// page at a time.
func (s *Store) EachTenant(fn func(tenant string) error) error { return s.tenants.Each(fn) }

// Retain uses the inbox retention cutoff. Pending attempts expire by their
// start time too; their unknown outcome is never converted into success.
// Each write removes records oldest first, at the versions of their copies
// read, and lowers the count with them, so two processes retaining at once
// remove each record once.
func (s *Store) Retain(tenant string, cut time.Time) error {
	end := indexPrefix + timeKey(cut)
	for {
		done := false
		err := state.Retry(func() error {
			c, cancel := ctx()
			rows, _, err := s.st.List(c, namespace, tenant, state.Query{Prefix: indexPrefix, Limit: retainBatch})
			cancel()
			if err != nil {
				return err
			}
			var ops []state.Op
			for _, row := range rows {
				if row.Key >= end {
					break
				}
				id := row.Key[strings.LastIndexByte(row.Key, ':')+1:]
				ops = append(ops, state.DeleteAt(tenant, row.Key, row.Version), state.Delete(tenant, id))
			}
			if len(ops) == 0 {
				done = true
				return nil
			}
			n, version, err := s.count(tenant)
			if err != nil {
				return err
			}
			return s.write(append(ops, countOp(tenant, max(n-len(ops)/2, 0), version))...)
		})
		if err != nil || done {
			return err
		}
	}
}

// RedactedSnapshot adapts a legacy explicit snapshot through the same bounds
// and credential redaction as newly recorded changes.
func RedactedSnapshot(fields map[string]string) (map[string]string, error) { return safeFields(fields) }
