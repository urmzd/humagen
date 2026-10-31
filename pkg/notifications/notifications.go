// Package notifications keeps an opaque native-console delivery projection of
// the inbox. It never stores a subject line, body, project name or principal.
//
// It reads and writes the database per request (pkg/state, per request): it
// reads one person's records for each change and writes them at the
// versions it read, retrying when another process moved them, so any number
// of processes on one database agree, and it does not need pkg/state's
// single-writer lock. Background retention pages the tenants from a
// state.Tenants directory.
package notifications

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/state"
)

const MaxEntries = 10000
const MaxDeliveries = 50
const MaxDeliveryItems = 100
const DigestEvery = time.Hour
const namespace = "notifications"
const emailNamespace = "email_notifications"

var ErrInvalid = errors.New("notifications: invalid")
var ErrNotFound = errors.New("notifications: not found")
var ErrCapacity = errors.New("notifications: capacity reached")

type Candidate struct {
	Message string    `json:"message"`
	Kind    string    `json:"kind"`
	Stamp   time.Time `json:"stamp"`
}
type Entry struct {
	PolicyMode prefs.Delivery `json:"policy_mode,omitempty"`
	Candidate
	Subject      string         `json:"subject"`
	Due          time.Time      `json:"due,omitzero"`
	DeliveredAt  time.Time      `json:"delivered_at,omitzero"`
	Delivery     string         `json:"delivery,omitempty"`
	Mode         prefs.Delivery `json:"mode"`
	Suppressed   bool           `json:"suppressed,omitempty"`
	Acknowledged bool           `json:"acknowledged,omitempty"`
}
type Record struct {
	Entry
	Cursor string `json:"cursor,omitempty"`
	Meta   bool   `json:"meta,omitempty"`
}
type Delivery struct {
	ID       string
	Mode     prefs.Delivery
	At       time.Time
	Messages []string
}

// Store keeps one namespace of delivery state. It holds nothing in memory
// but the tenant directory's cache, which may be dropped at any time.
type Store struct {
	namespace string
	st        state.Store
	tenants   *state.Tenants
}

func key(subject, message string) string {
	return prefix(subject) + base64.RawURLEncoding.EncodeToString([]byte(message))
}

// prefix is what every key of one person's records starts with: their own
// rows and their meta record, which holds the cursor.
func prefix(subject string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(subject)) + "/"
}
func valid(subject string, c Candidate) bool {
	return subject != "" && len(subject) <= 512 && c.Message != "" && len(c.Message) <= 256 && len(c.Kind) <= 64 && !c.Stamp.IsZero() && !strings.ContainsRune(subject+c.Message+c.Kind, 0)
}

// Open returns the native-console store on st, which may be shared with
// other processes. A nil st keeps it in this process's memory.
func Open(st state.Store) (*Store, error) { return open(st, namespace) }

// OpenEmail keeps independent delivery and acknowledgment state for email.
func OpenEmail(st state.Store) (*Store, error) { return open(st, emailNamespace) }

func open(st state.Store, space string) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	tenants, err := state.OpenTenants(st, space)
	if err != nil {
		return nil, fmt.Errorf("notifications: tenants: %w", err)
	}
	return &Store{namespace: space, st: st, tenants: tenants}, nil
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// row is one stored entry and the version it was read at.
type row struct {
	Entry
	version int64
}

// person is one person's records as read: their rows by message, and their
// meta record's cursor and version, zero when they have none.
type person struct {
	rows    map[string]row
	cursor  string
	version int64
}

// read reads one person's records, a page at a time. A person holds at most
// MaxEntries rows and one meta record, and a read past that is ErrCapacity.
func (s *Store) read(tenant, subject string) (person, error) {
	out := person{rows: map[string]row{}}
	pre := prefix(subject)
	after := ""
	count := 0
	for {
		c, cancel := ctx()
		recs, next, err := s.st.List(c, s.namespace, tenant, state.Query{Prefix: pre, After: after, Limit: state.MaxList})
		cancel()
		if err != nil {
			return person{}, err
		}
		for _, record := range recs {
			count++
			if count > MaxEntries+1 {
				return person{}, ErrCapacity
			}
			var r Record
			if err := json.Unmarshal(record.Value, &r); err != nil {
				return person{}, err
			}
			if r.Subject != subject {
				return person{}, ErrInvalid
			}
			if r.Meta {
				if record.Key != key(subject, "") || len(r.Cursor) > 1024 {
					return person{}, ErrInvalid
				}
				out.cursor, out.version = r.Cursor, record.Version
				continue
			}
			if !valid(r.Subject, r.Candidate) || record.Key != key(r.Subject, r.Message) || r.Mode != prefs.Immediate && r.Mode != prefs.Digest && r.Mode != prefs.Off || r.PolicyMode != "" && r.PolicyMode != prefs.Immediate && r.PolicyMode != prefs.Digest && r.PolicyMode != prefs.Off {
				return person{}, ErrInvalid
			}
			out.rows[r.Message] = row{Entry: r.Entry, version: record.Version}
		}
		if next == "" {
			return out, nil
		}
		after = next
	}
}

// Cursor is a person's backfill cursor, "" when they have none.
func (s *Store) Cursor(tenant, subject string) (string, error) {
	c, cancel := ctx()
	defer cancel()
	record, err := s.st.Lookup(c, s.namespace, tenant, key(subject, ""))
	if errors.Is(err, state.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var r Record
	if err := json.Unmarshal(record.Value, &r); err != nil {
		return "", err
	}
	return r.Cursor, nil
}

// write applies ops to one tenant, marking it in the directory first. A
// conditional op whose record moved returns state.ErrConflict.
func (s *Store) write(tenant string, ops []state.Op) error {
	if len(ops) == 0 {
		return nil
	}
	if err := s.tenants.Mark(tenant); err != nil {
		return err
	}
	c, cancel := ctx()
	defer cancel()
	return s.st.Write(c, s.namespace, ops...)
}

// entryOp writes an entry at the version it was read: created when version
// is zero.
func entryOp(tenant string, e Entry, version int64) state.Op {
	raw, _ := json.Marshal(Record{Entry: e})
	if version == 0 {
		return state.Create(tenant, key(e.Subject, e.Message), raw)
	}
	return state.PutAt(tenant, key(e.Subject, e.Message), raw, version)
}

// metaOp writes a person's cursor at the version it was read.
func metaOp(tenant, subject, cursor string, version int64) state.Op {
	raw, _ := json.Marshal(Record{Entry: Entry{Subject: subject}, Meta: true, Cursor: cursor})
	if version == 0 {
		return state.Create(tenant, key(subject, ""), raw)
	}
	return state.PutAt(tenant, key(subject, ""), raw, version)
}

// QuietUntil returns the local end of the current quiet period. It uses the
// configured IANA zone, including overnight windows and daylight-saving days.
func QuietUntil(p prefs.Personal, at time.Time) time.Time {
	if p.QuietStart == "" || p.QuietEnd == "" {
		return time.Time{}
	}
	location, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return at.Add(DigestEvery)
	}
	local := at.In(location)
	clock := local.Format("15:04")
	quiet := clock >= p.QuietStart && clock < p.QuietEnd
	overnight := p.QuietStart > p.QuietEnd
	if overnight {
		quiet = clock >= p.QuietStart || clock < p.QuietEnd
	}
	if !quiet {
		return time.Time{}
	}
	end, err := time.Parse("15:04", p.QuietEnd)
	if err != nil {
		return at.Add(DigestEvery)
	}
	day := local.Day()
	if overnight && clock >= p.QuietStart {
		day++
	}
	return time.Date(local.Year(), local.Month(), day, end.Hour(), end.Minute(), 0, 0, location)
}
func due(p prefs.Personal, kind string, at time.Time) time.Time {
	if quiet := QuietUntil(p, at); !quiet.IsZero() {
		return quiet
	}
	if p.Notifications[kind] == prefs.Immediate {
		return at
	}
	next := at.Truncate(DigestEvery).Add(DigestEvery)
	if quiet := QuietUntil(p, next); !quiet.IsZero() {
		return quiet
	}
	return next
}

// Reconcile discovers at most 500 current visible inbox references. lookup
// must re-authorize against the authenticated person's current Principal;
// stored roles and stale sign-in claims are never accepted. Pending references
// are checked again at release, and every rendered delivery is checked again.
// A failed write returns nothing and leaves the in-memory projection unchanged.
func (s *Store) Reconcile(tenant, subject, cursor string, candidates []Candidate, p prefs.Personal, now, cut time.Time, lookup func(string) (Candidate, bool)) ([]Delivery, error) {
	if tenant == "" || subject == "" || len(subject) > 512 || len(cursor) > 1024 || len(candidates) > 500 || lookup == nil || now.IsZero() {
		return nil, ErrInvalid
	}
	for _, candidate := range candidates {
		if !valid(subject, candidate) {
			return nil, ErrInvalid
		}
	}
	var out []Delivery
	err := state.Retry(func() error {
		read, err := s.read(tenant, subject)
		if err != nil {
			return err
		}
		deliveries, ops := s.reconcile(tenant, subject, cursor, read, candidates, p, now, cut, lookup)
		if ops == nil {
			return ErrCapacity
		}
		if err := s.write(tenant, ops); err != nil {
			return err
		}
		out = deliveries
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// reconcile works out one Reconcile from what it read: the deliveries, and
// the ops that record them. Nil ops is a person past MaxEntries.
func (s *Store) reconcile(tenant, subject, cursor string, read person, candidates []Candidate, p prefs.Personal, now, cut time.Time, lookup func(string) (Candidate, bool)) ([]Delivery, []state.Op) {
	before := read.rows
	rows := make(map[string]Entry, len(before))
	for id, e := range before {
		rows[id] = e.Entry
	}
	ops := []state.Op{}
	for id, e := range rows {
		if e.Stamp.Before(cut) {
			delete(rows, id)
			ops = append(ops, state.DeleteAt(tenant, key(subject, id), before[id].version))
		}
	}
	grew := false
	for _, candidate := range candidates {
		if candidate.Stamp.Before(cut) {
			continue
		}
		if old, ok := rows[candidate.Message]; ok && old.Stamp.Equal(candidate.Stamp) {
			continue
		}
		mode, configured := p.Notifications[candidate.Kind]
		if !configured {
			continue
		}
		if _, ok := rows[candidate.Message]; !ok {
			if len(rows) >= MaxEntries {
				return nil, nil
			}
			if _, kept := before[candidate.Message]; !kept {
				grew = true
			}
		}
		e := Entry{Candidate: candidate, Subject: subject, Mode: mode, Due: due(p, candidate.Kind, now), Suppressed: mode == prefs.Off}
		if s.namespace == emailNamespace {
			e.PolicyMode = mode
			e.Mode = p.NotificationDelivery(candidate.Kind, now)
		}
		rows[candidate.Message] = e
	}
	pending := []Entry{}
	for id, e := range rows {
		if e.Suppressed || e.Acknowledged || !e.DeliveredAt.IsZero() {
			continue
		}
		current, visible := lookup(id)
		if !visible || !current.Stamp.Equal(e.Stamp) {
			e.Suppressed = true
			rows[id] = e
			continue
		}
		mode := p.NotificationDelivery(e.Kind, now)
		if s.namespace == emailNamespace && e.Mode == prefs.Digest && e.PolicyMode == prefs.Immediate {
			mode = prefs.Digest
		}
		if mode == prefs.Off {
			e.Suppressed = true
			rows[id] = e
			continue
		}
		if quiet := QuietUntil(p, now); !quiet.IsZero() {
			if e.Due.Before(quiet) {
				e.Due = quiet
				rows[id] = e
			}
			continue
		}
		if mode == prefs.Digest && now.Before(e.Due) {
			continue
		}
		e.Mode = mode
		pending = append(pending, e)
	}
	slices.SortFunc(pending, func(a, b Entry) int {
		if n := a.Due.Compare(b.Due); n != 0 {
			return n
		}
		return strings.Compare(a.Message, b.Message)
	})
	digestID := ""
	for i, e := range pending {
		if i == MaxDeliveryItems {
			break
		}
		e.Delivery = "ntf-" + rand.Text()
		if e.Mode == prefs.Digest {
			if digestID == "" {
				digestID = e.Delivery
			}
			e.Delivery = digestID
		}
		e.DeliveredAt = now.UTC()
		rows[e.Message] = e
	}
	groups := map[string]Delivery{}
	for id, e := range rows {
		if e.Suppressed || e.Acknowledged || e.Delivery == "" {
			continue
		}
		current, visible := lookup(id)
		if !visible || !current.Stamp.Equal(e.Stamp) {
			continue
		}
		group := groups[e.Delivery]
		group.ID, group.Mode, group.At = e.Delivery, e.Mode, e.DeliveredAt
		group.Messages = append(group.Messages, id)
		groups[group.ID] = group
	}
	deliveries := make([]Delivery, 0, len(groups))
	for _, group := range groups {
		slices.Sort(group.Messages)
		deliveries = append(deliveries, group)
	}
	slices.SortFunc(deliveries, func(a, b Delivery) int {
		if n := b.At.Compare(a.At); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	if len(deliveries) > MaxDeliveries {
		deliveries = deliveries[:MaxDeliveries]
	}
	for id, e := range rows {
		if old, ok := before[id]; !ok || old.Entry != e {
			ops = append(ops, entryOp(tenant, e, old.version))
		}
	}
	// A Reconcile that adds rows writes the meta record at the version it
	// read too, so two that grow one person's rows at once conflict and the
	// cap holds.
	if read.cursor != cursor || grew {
		ops = append(ops, metaOp(tenant, subject, cursor, read.version))
	}
	return deliveries, ops
}

// Ack marks every entry of one of a person's deliveries acknowledged.
func (s *Store) Ack(tenant, subject, id string, visible func(string) bool) error {
	return state.Retry(func() error {
		read, err := s.read(tenant, subject)
		if err != nil {
			return err
		}
		ops := []state.Op{}
		for _, r := range read.rows {
			if r.Delivery == id && !r.Suppressed && visible(r.Message) {
				e := r.Entry
				e.Acknowledged = true
				ops = append(ops, entryOp(tenant, e, r.version))
			}
		}
		if len(ops) == 0 {
			return ErrNotFound
		}
		return s.write(tenant, ops)
	})
}

// EachTenant calls fn for every tenant the store has written for, a page at
// a time, from the tenant directory.
func (s *Store) EachTenant(fn func(tenant string) error) error {
	return s.tenants.Each(fn)
}

// Retain applies the same cutoff as the workspace inbox, using bounded writes.
func (s *Store) Retain(tenant string, cut time.Time) error {
	if tenant == "" {
		return nil
	}
	after := ""
	for {
		var next string
		err := state.Retry(func() error {
			c, cancel := ctx()
			recs, n, err := s.st.List(c, s.namespace, tenant, state.Query{After: after, Limit: state.MaxList})
			cancel()
			if err != nil {
				return err
			}
			next = n
			ops := []state.Op{}
			for _, record := range recs {
				var r Record
				if err := json.Unmarshal(record.Value, &r); err != nil {
					return err
				}
				if !r.Meta && r.Stamp.Before(cut) {
					ops = append(ops, state.DeleteAt(tenant, record.Key, record.Version))
				}
			}
			if len(ops) == 0 {
				return nil
			}
			c, cancel = ctx()
			defer cancel()
			return s.st.Write(c, s.namespace, ops...)
		})
		if err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		after = next
	}
}
