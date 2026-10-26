// Package audit keeps bounded, durable attempts and their observed outcomes.
package audit

import (
	"container/heap"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/state"
)

const (
	MaxRecords = 100000
	MaxRows    = 500
	MaxWindow  = 31 * 24 * time.Hour
	namespace  = "audit"
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
type Store struct {
	mu   sync.Mutex
	st   state.Store
	rows map[string]map[string]Record
	next map[string]uint64
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
func Open(st state.Store) (*Store, error) {
	s := &Store{st: st, rows: map[string]map[string]Record{}, next: map[string]uint64{}}
	if st == nil {
		return s, nil
	}
	records, err := st.Load(context.Background(), namespace)
	if err != nil {
		return nil, err
	}
	for _, row := range records {
		if row.Key == "!next" {
			var seq uint64
			if err := json.Unmarshal(row.Value, &seq); err != nil {
				return nil, err
			}
			s.next[row.Tenant] = max(s.next[row.Tenant], seq)
			continue
		}
		var r Record
		if err := json.Unmarshal(row.Value, &r); err != nil {
			return nil, err
		}
		checked, err := clean(r.Change)
		if err != nil || r.ID != row.Key || !validIdentity(row.Tenant) || !validIdentity(r.Actor) || r.At.IsZero() || (r.Outcome != Pending && r.Outcome != Succeeded && r.Outcome != Failed) || (r.Outcome == Pending && !r.FinishedAt.IsZero()) || (r.Outcome != Pending && (r.FinishedAt.IsZero() || r.FinishedAt.Before(r.At))) {
			return nil, ErrInvalid
		}
		r.Change = checked
		if s.rows[row.Tenant] == nil {
			s.rows[row.Tenant] = map[string]Record{}
		}
		if len(s.rows[row.Tenant]) >= MaxRecords {
			return nil, ErrCapacity
		}
		s.rows[row.Tenant][r.ID] = r
		if !opaqueIDPattern.MatchString(r.ID) {
			seq, err := strconv.ParseUint(strings.TrimPrefix(r.ID, "a"), 10, 64)
			if err != nil {
				return nil, ErrInvalid
			}
			s.next[row.Tenant] = max(s.next[row.Tenant], seq)
		}
	}
	return s, nil
}
func (s *Store) write(ops ...state.Op) error {
	if s.st == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	return s.st.Write(ctx, namespace, ops...)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.rows[tenant]) >= MaxRecords {
		return Attempt{}, ErrCapacity
	}
	seq := s.next[tenant] + 1
	if seq == 0 {
		return Attempt{}, ErrCapacity
	}
	// Public IDs must not reveal the number of hidden personal records. The
	// internal counter remains durable for legacy replay and never leaves here.
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return Attempt{}, err
	}
	id := "a_" + hex.EncodeToString(nonce[:])
	if _, exists := s.rows[tenant][id]; exists {
		return Attempt{}, ErrConflict
	}
	r := Record{Change: checked, ID: id, Actor: actor, At: at.UTC(), Outcome: Pending}
	raw, _ := json.Marshal(r)
	counter, _ := json.Marshal(seq)
	if err := s.write(state.Put(tenant, "!next", counter), state.Put(tenant, id, raw)); err != nil {
		return Attempt{}, err
	}
	if s.rows[tenant] == nil {
		s.rows[tenant] = map[string]Record{}
	}
	s.rows[tenant][id] = r
	s.next[tenant] = seq
	return Attempt{ID: id}, nil
}

// Finish records an observed result once. A failed write leaves Pending intact.
// Error strings are deliberately not stored: upstream responses can hold secrets.
func (s *Store) Finish(tenant, id string, result error, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[tenant][id]
	if !ok {
		return ErrNotFound
	}
	next := Succeeded
	if result != nil {
		next = Failed
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
	raw, _ := json.Marshal(r)
	if err := s.write(state.Put(tenant, id, raw)); err != nil {
		return err
	}
	s.rows[tenant][id] = r
	return nil
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
// returned counts nor which visible rows fit in the bounded result.
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
	s.mu.Lock()
	rows := make([]Record, 0, len(s.rows[tenant]))
	for _, r := range s.rows[tenant] {
		rows = append(rows, r)
	}
	s.mu.Unlock()
	found := recordHeap{}
	for _, r := range rows {
		if q.Actor != "" && r.Actor != q.Actor || q.Resource != "" && r.Resource != q.Resource || r.At.Before(q.From) || !r.At.Before(q.To) || visible != nil && !visible(clone(r)) {
			continue
		}
		if len(found) < limit {
			heap.Push(&found, r)
		} else if newer(r, found[0]) {
			found[0] = r
			heap.Fix(&found, 0)
		}
	}
	sort.Slice(found, func(i, j int) bool { return newer(found[i], found[j]) })
	out := make([]Record, len(found))
	for i, r := range found {
		out[i] = clone(r)
	}
	return out, nil
}
func newer(a, b Record) bool {
	if a.At.Equal(b.At) {
		return a.ID > b.ID
	}
	return a.At.After(b.At)
}

type recordHeap []Record

func (h recordHeap) Len() int           { return len(h) }
func (h recordHeap) Less(i, j int) bool { return newer(h[j], h[i]) }
func (h recordHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *recordHeap) Push(v any)        { *h = append(*h, v.(Record)) }
func (h *recordHeap) Pop() any          { n := len(*h) - 1; r := (*h)[n]; *h = (*h)[:n]; return r }
func (s *Store) Tenants() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Sorted(maps.Keys(s.rows))
	return out
}

// Retain uses the inbox retention cutoff. Pending attempts expire by their
// start time too; their unknown outcome is never converted into success.
func (s *Store) Retain(tenant string, cut time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		ids := []string{}
		ops := []state.Op{}
		for id, r := range s.rows[tenant] {
			if r.At.Before(cut) {
				ids = append(ids, id)
				ops = append(ops, state.Delete(tenant, id))
				if len(ids) == 5000 {
					break
				}
			}
		}
		if len(ids) == 0 {
			return nil
		}
		if err := s.write(ops...); err != nil {
			return err
		}
		for _, id := range ids {
			delete(s.rows[tenant], id)
		}
	}
}

// RedactedSnapshot adapts a legacy explicit snapshot through the same bounds
// and credential redaction as newly recorded changes.
func RedactedSnapshot(fields map[string]string) (map[string]string, error) { return safeFields(fields) }
