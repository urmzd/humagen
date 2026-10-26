// Package notifications keeps an opaque native-console delivery projection of
// the inbox. It never stores a subject line, body, project name or principal.
package notifications

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
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
type Store struct {
	namespace string
	mu        sync.Mutex
	st        state.Store
	rows      map[[2]string]map[string]Entry
	cursors   map[[2]string]string
}

func key(subject, message string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(subject)) + "/" + base64.RawURLEncoding.EncodeToString([]byte(message))
}
func valid(subject string, c Candidate) bool {
	return subject != "" && len(subject) <= 512 && c.Message != "" && len(c.Message) <= 256 && len(c.Kind) <= 64 && !c.Stamp.IsZero() && !strings.ContainsRune(subject+c.Message+c.Kind, 0)
}
func Open(st state.Store) (*Store, error) { return open(st, namespace) }

// OpenEmail keeps independent delivery and acknowledgment state for email.
func OpenEmail(st state.Store) (*Store, error) { return open(st, emailNamespace) }

func open(st state.Store, space string) (*Store, error) {
	s := &Store{namespace: space, st: st, rows: map[[2]string]map[string]Entry{}, cursors: map[[2]string]string{}}
	if st == nil {
		return s, nil
	}
	records, err := st.Load(context.Background(), space)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		var r Record
		if err := json.Unmarshal(record.Value, &r); err != nil {
			return nil, err
		}
		owner := [2]string{record.Tenant, r.Subject}
		if r.Meta {
			if record.Key != key(r.Subject, "") || r.Subject == "" || len(r.Cursor) > 1024 {
				return nil, ErrInvalid
			}
			s.cursors[owner] = r.Cursor
			continue
		}
		if !valid(r.Subject, r.Candidate) || record.Key != key(r.Subject, r.Message) || r.Mode != prefs.Immediate && r.Mode != prefs.Digest && r.Mode != prefs.Off || r.PolicyMode != "" && r.PolicyMode != prefs.Immediate && r.PolicyMode != prefs.Digest && r.PolicyMode != prefs.Off {
			return nil, ErrInvalid
		}
		if s.rows[owner] == nil {
			s.rows[owner] = map[string]Entry{}
		}
		if len(s.rows[owner]) >= MaxEntries {
			return nil, ErrCapacity
		}
		s.rows[owner][r.Message] = r.Entry
	}
	return s, nil
}
func (s *Store) Cursor(tenant, subject string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursors[[2]string{tenant, subject}]
}
func (s *Store) write(ops []state.Op) error {
	if s.st == nil || len(ops) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), state.Timeout)
	defer cancel()
	return s.st.Write(ctx, s.namespace, ops...)
}
func entryOp(tenant string, e Entry) state.Op {
	raw, _ := json.Marshal(Record{Entry: e})
	return state.Put(tenant, key(e.Subject, e.Message), raw)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	owner := [2]string{tenant, subject}
	before := s.rows[owner]
	rows := make(map[string]Entry, len(before))
	for id, e := range before {
		rows[id] = e
	}
	ops := []state.Op{}
	for id, e := range rows {
		if e.Stamp.Before(cut) {
			delete(rows, id)
			ops = append(ops, state.Delete(tenant, key(subject, id)))
		}
	}
	for _, candidate := range candidates {
		if !valid(subject, candidate) {
			return nil, ErrInvalid
		}
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
		if _, ok := rows[candidate.Message]; !ok && len(rows) >= MaxEntries {
			return nil, ErrCapacity
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
		if old, ok := before[id]; !ok || old != e {
			ops = append(ops, entryOp(tenant, e))
		}
	}
	if s.cursors[owner] != cursor {
		raw, _ := json.Marshal(Record{Entry: Entry{Subject: subject}, Meta: true, Cursor: cursor})
		ops = append(ops, state.Put(tenant, key(subject, ""), raw))
	}
	if err := s.write(ops); err != nil {
		return nil, err
	}
	if len(rows) > 0 || cursor != "" {
		s.rows[owner] = rows
		s.cursors[owner] = cursor
	}
	return deliveries, nil
}
func (s *Store) Ack(tenant, subject, id string, visible func(string) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	owner := [2]string{tenant, subject}
	changed := []Entry{}
	for _, e := range s.rows[owner] {
		if e.Delivery == id && !e.Suppressed && visible(e.Message) {
			e.Acknowledged = true
			changed = append(changed, e)
		}
	}
	if len(changed) == 0 {
		return ErrNotFound
	}
	ops := make([]state.Op, 0, len(changed))
	for _, e := range changed {
		ops = append(ops, entryOp(tenant, e))
	}
	if err := s.write(ops); err != nil {
		return err
	}
	for _, e := range changed {
		s.rows[owner][e.Message] = e
	}
	return nil
}

func (s *Store) Tenants() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for owner := range s.rows {
		if !slices.Contains(out, owner[0]) {
			out = append(out, owner[0])
		}
	}
	slices.Sort(out)
	return out
}

// Retain applies the same cutoff as the workspace inbox, using bounded writes.
func (s *Store) Retain(tenant string, cut time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		type removed struct {
			owner   [2]string
			message string
		}
		drop := []removed{}
		ops := []state.Op{}
	outer:
		for owner, rows := range s.rows {
			if owner[0] != tenant {
				continue
			}
			for id, e := range rows {
				if e.Stamp.Before(cut) {
					drop = append(drop, removed{owner, id})
					ops = append(ops, state.Delete(tenant, key(owner[1], id)))
					if len(drop) == 5000 {
						break outer
					}
				}
			}
		}
		if len(drop) == 0 {
			return nil
		}
		if err := s.write(ops); err != nil {
			return err
		}
		for _, row := range drop {
			delete(s.rows[row.owner], row.message)
		}
	}
}
