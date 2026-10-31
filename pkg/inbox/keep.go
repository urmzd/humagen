package inbox

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// namespace is where the inbox is kept. The tenant is the shard key; within
// it, a message's records start with what a request reads them by:
//
//	n                        the number of the last message id issued
//	i:<id>                   when a message was raised, to find it by id
//	a:<at>:<id>              a message, every one, ordered by when it was raised
//	l:<at>:<id>              a copy of it while it is open or snoozed
//	e:<updated>:<id>         a message retention may drop, by when it was settled
//	g:<id>                   a dropped message whose history is still being deleted
//	k:<key>                  the latest message raised under a key
//	d:<post>                 the messages a client posted at a place, the last last
//	c:<id>:<n>               the n-th change to a message
//	u:<id>:<subject>         one reader's own marks on a message
//	b:<signature>            open messages alike in who reads them, counted
//	o:<subject>:<signature>  how many of those one reader has set aside
//	z:<id>                   a message snoozed for everyone, until when
//	y:<subject>:<id>         one reader's own snooze of an open broadcast
//	p:<day>                  the announcements posted on a day
//
// An id, a time and a change's number are written with state.Seq, so keys
// sort in their order; a key, a subject, a post and a signature are hashed,
// since each may hold a ":" or an address. Every change to a message writes
// its "a:" record at the version it read, so two writers on one message are
// ordered, and its copies, counters and marks change in the same write.
const namespace = "inbox"

// converted marks, in the tenant "", that changes kept by a release before
// this layout have been moved into it.
const converted = "converted:v2"

// Caps on what one request reads. A count reads every bucket of open
// messages and every message snoozed for everyone; a change of state reads
// every reader's marks on its message.
const (
	maxBuckets = 20000
	maxSnoozed = 20000
	maxMarks   = 20000
	// maxScan is the most index records one page of the inbox reads before
	// it returns what it found, with a cursor where it stopped.
	maxScan = 5000
	// maxOpen is the most open messages OpenOf returns.
	maxOpen = 5000
	// maxChanges is the most changes History reads of one message, and
	// Changes of a tenant.
	maxChanges = 100000
	// maxMessages is the most messages Snapshot reads.
	maxMessages = 100000
)

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

// num is the number in a message id, "m-<n>", or false.
func num(id string) (int64, bool) {
	n, ok := strings.CutPrefix(id, "m-")
	if !ok {
		return 0, false
	}
	i, err := strconv.ParseInt(n, 10, 64)
	if err != nil || i <= 0 || "m-"+strconv.FormatInt(i, 10) != id {
		return 0, false
	}
	return i, true
}

// stamp is a time as a key orders it. A time before 1970 sorts first.
func stamp(at time.Time) int64 {
	if at.Before(time.Unix(0, 0)) {
		return 0
	}
	return at.UnixNano()
}

const headKey = "n"

func seqOf(id string) string {
	n, _ := num(id)
	return state.Seq(n)
}

func ptrKey(id string) string            { return "i:" + seqOf(id) }
func allKey(at int64, id string) string  { return "a:" + state.Seq(at) + ":" + seqOf(id) }
func liveKey(at int64, id string) string { return "l:" + state.Seq(at) + ":" + seqOf(id) }
func retainKey(m *Message) string        { return "e:" + state.Seq(stamp(m.UpdatedAt)) + ":" + seqOf(m.ID) }
func garbageKey(id string) string        { return "g:" + seqOf(id) }
func keyKey(key string) string           { return "k:" + hash(key) }
func postRef(client string, d Delivery) string {
	return "d:" + hash(postKey(client, d))
}
func changePrefix(id string) string          { return "c:" + seqOf(id) + ":" }
func changeKey(id string, n int64) string    { return changePrefix(id) + state.Seq(n) }
func markPrefix(id string) string            { return "u:" + seqOf(id) + ":" }
func markKey(id, subject string) string      { return markPrefix(id) + hash(subject) }
func bucketKey(sig string) string            { return "b:" + hash(sig) }
func outPrefix(subject string) string        { return "o:" + hash(subject) + ":" }
func outKey(subject, sig string) string      { return outPrefix(subject) + hash(sig) }
func sleepKey(id string) string              { return "z:" + seqOf(id) }
func ownSnoozePrefix(subject string) string  { return "y:" + hash(subject) + ":" }
func ownSnoozeKey(subject, id string) string { return ownSnoozePrefix(subject) + seqOf(id) }
func dayKey(day time.Time) string            { return "p:" + day.Format("2006-01-02") }
func idOfKey(key string) string {
	return "m-" + strings.TrimLeft(key[strings.LastIndexByte(key, ':')+1:], "0")
}
func live(m *Message) bool    { return m.State == Open || m.State == Snoozed }
func settled(m *Message) bool { return m.State == Resolved || m.State == Dismissed }

// head is the "n" record.
type head struct {
	Next int64 `json:"next"`
}

// pointer is an "i:" record.
type pointer struct {
	At int64 `json:"at"`
}

// keyRef is a "k:" record: the latest message raised under a key, or, for a
// key posted once whose message retention dropped, Gone.
type keyRef struct {
	Key  string `json:"key"`
	ID   string `json:"id,omitempty"`
	Gone bool   `json:"gone,omitempty"`
}

// postRec is a "d:" record: the messages a client recorded at one post,
// the last one recorded last. The post finds the last.
type postRec struct {
	IDs []string `json:"ids"`
}

// keptMessage is a message as it is kept, with what its wire form leaves out.
type keptMessage struct {
	Message
	Key       string   `json:"key"`
	Audience  []string `json:"audience,omitempty"`
	Excluded  []string `json:"excluded,omitempty"`
	Broadcast bool     `json:"broadcast,omitempty"`
	Once      bool     `json:"once,omitempty"`
	Reminded  bool     `json:"reminded,omitempty"`
	// Changes is the number of the last change recorded about it.
	Changes int64 `json:"changes"`
}

func keep(m *Message) keptMessage {
	k := keptMessage{Message: *m, Key: m.Key, Audience: m.Audience, Excluded: m.Excluded, Broadcast: m.Broadcast, Once: m.Once, Reminded: m.reminded, Changes: m.changes}
	k.Unread, k.Hidden = false, false
	return k
}

func (k keptMessage) message(tenant string) *Message {
	m := k.Message
	m.Key, m.Audience, m.Excluded, m.Broadcast, m.Once, m.reminded, m.changes = k.Key, k.Audience, k.Excluded, k.Broadcast, k.Once, k.Reminded, k.Changes
	m.Tenant = tenant
	return &m
}

// mark is a "u:" record: one reader's own marks on a message.
type mark struct {
	Subject string `json:"subject"`
	Read    bool   `json:"read,omitempty"`
	// Hide is how they set a broadcast aside for themselves.
	Hide *hide `json:"hide,omitempty"`
}

func (k mark) empty() bool { return !k.Read && k.Hide == nil }

// hide is one reader's own state of a broadcast: dismissed, or snoozed
// until a time.
type hide struct {
	State State     `json:"state"`
	Until time.Time `json:"until,omitzero"`
}

// bucketRec is a "b:" record (see tally.go).
type bucketRec struct {
	Sig  string      `json:"sig"`
	Rep  keptMessage `json:"rep"`
	Open int         `json:"open"`
}

// outRec is an "o:" record.
type outRec struct {
	N int `json:"n"`
}

// sleepRec is a "z:" record.
type sleepRec struct {
	Until time.Time   `json:"until"`
	Sig   string      `json:"sig"`
	Rep   keptMessage `json:"rep"`
}

// ownSnooze is a "y:" record.
type ownSnooze struct {
	Until time.Time `json:"until"`
	Sig   string    `json:"sig"`
}

// postedRec is a "p:" record: the announcements raised on a day, in UTC.
type postedRec struct {
	Total int            `json:"total"`
	By    map[string]int `json:"by,omitempty"`
}

func dayOf(at time.Time) time.Time {
	at = at.UTC()
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// tx is one change to one tenant's inbox: what it read, at which versions,
// and what it writes, all in one write. A record it read is written at the
// version it read, so the write fails with state.ErrConflict when another
// writer moved it in between, and the change is read and made again.
type tx struct {
	st     state.Store
	tenant string
	// seen is the version each key was read at, state.Missing for none.
	seen map[string]int64
	// vals is each key's value as the change stands: as read, or as
	// written, nil for no record.
	vals  map[string][]byte
	order []string
	dirty map[string]bool
	// msgs are the messages read, by id, as they stand, and was each as it
	// was read (nil for one raised here).
	msgs   map[string]*Message
	was    map[string]*Message
	morder []string
	err    error
	// notices are told once the change is written.
	notices []Notice
}

func newTx(st state.Store, tenant string) *tx {
	return &tx{st: st, tenant: tenant, seen: map[string]int64{}, vals: map[string][]byte{}, dirty: map[string]bool{}, msgs: map[string]*Message{}, was: map[string]*Message{}}
}

// raw reads one record's value, nil for none.
func (x *tx) raw(key string) ([]byte, error) {
	if v, ok := x.vals[key]; ok {
		return v, nil
	}
	if _, ok := x.seen[key]; ok {
		return nil, nil
	}
	c, cancel := ctx()
	defer cancel()
	r, err := x.st.Lookup(c, namespace, x.tenant, key)
	if errors.Is(err, state.ErrNotFound) {
		x.seen[key] = state.Missing
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	x.seen[key], x.vals[key] = r.Version, r.Value
	return r.Value, nil
}

// get reads one record into v, and reports whether it has one.
func (x *tx) get(key string, v any) (bool, error) {
	raw, err := x.raw(key)
	if err != nil || raw == nil {
		return false, err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, fmt.Errorf("inbox: %s %q/%q: %w", namespace, x.tenant, key, err)
	}
	return true, nil
}

func (x *tx) put(key string, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		x.err = cmp.Or(x.err, err)
		return
	}
	x.set(key, raw)
}

func (x *tx) del(key string) { x.set(key, nil) }

func (x *tx) set(key string, raw []byte) {
	if !x.dirty[key] {
		x.dirty[key] = true
		x.order = append(x.order, key)
	}
	x.vals[key] = raw
}

// item is one record a list read.
type item struct {
	key string
	raw []byte
}

// list reads every record under a prefix, as the change stands, failing
// past most. Records it reads are written at the version read.
func (x *tx) list(prefix string, most int) ([]item, error) {
	recs, err := listAll(x.st, x.tenant, prefix, most)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	var out []item
	for _, r := range recs {
		have[r.Key] = true
		if _, ok := x.seen[r.Key]; !ok {
			x.seen[r.Key], x.vals[r.Key] = r.Version, r.Value
		}
		if v := x.vals[r.Key]; v != nil {
			out = append(out, item{r.Key, v})
		}
	}
	for _, k := range x.order {
		if strings.HasPrefix(k, prefix) && !have[k] && x.vals[k] != nil {
			out = append(out, item{k, x.vals[k]})
		}
	}
	slices.SortFunc(out, func(a, b item) int { return strings.Compare(a.key, b.key) })
	return out, nil
}

// listAll reads every record of a tenant under a prefix, page by page, and
// fails with state.ErrTooMany past most.
func listAll(st state.Store, tenant, prefix string, most int) ([]state.Record, error) {
	var out []state.Record
	after := ""
	for {
		c, cancel := ctx()
		page, next, err := st.List(c, namespace, tenant, state.Query{Prefix: prefix, After: after, Limit: min(most+1-len(out), state.MaxList)})
		cancel()
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(out) > most {
			return nil, fmt.Errorf("%w: inbox %q under %q holds more than %d", state.ErrTooMany, tenant, prefix, most)
		}
		if next == "" {
			return out, nil
		}
		after = next
	}
}

// message reads a message by id, nil when there is none.
func (x *tx) message(id string) (*Message, error) {
	if m, ok := x.msgs[id]; ok {
		return m, nil
	}
	if _, ok := num(id); !ok {
		return nil, nil
	}
	var p pointer
	if ok, err := x.get(ptrKey(id), &p); err != nil || !ok {
		return nil, err
	}
	var k keptMessage
	if ok, err := x.get(allKey(p.At, id), &k); err != nil || !ok {
		return nil, err
	}
	m := k.message(x.tenant)
	was := m.clone()
	x.msgs[id], x.was[id] = m, &was
	x.morder = append(x.morder, id)
	return m, nil
}

// raised adds a message raised in this change.
func (x *tx) raised(m *Message) {
	x.msgs[m.ID] = m
	x.morder = append(x.morder, m.ID)
	x.put(ptrKey(m.ID), pointer{At: stamp(m.At)})
}

// retainable reports whether retention may drop a message once it is old
// enough: settled for everyone, with no question in it a run still waits on.
func retainable(m *Message) bool {
	return settled(m) && !slices.ContainsFunc(m.Parts, func(p Part) bool { return p.Question != nil && p.State == PartOpen })
}

// flush writes every message the change read or raised that it changed,
// with its copies and its place in retention's index.
func (x *tx) flush() {
	for _, id := range x.morder {
		m, was := x.msgs[id], x.was[id]
		if m == nil {
			continue
		}
		if was != nil && was.changes == m.changes {
			continue
		}
		at := stamp(m.At)
		k := keep(m)
		x.put(allKey(at, id), k)
		if live(m) {
			x.put(liveKey(at, id), k)
		} else if was != nil && live(was) {
			x.del(liveKey(at, id))
		}
		if was != nil && retainable(was) && (!retainable(m) || retainKey(was) != retainKey(m)) {
			x.del(retainKey(was))
		}
		if retainable(m) {
			x.put(retainKey(m), struct{}{})
		}
	}
}

// commit writes the change, all or none.
func (x *tx) commit() error {
	x.flush()
	if x.err != nil {
		return x.err
	}
	ops := make([]state.Op, 0, len(x.order))
	for _, k := range x.order {
		v := x.vals[k]
		ver, read := x.seen[k]
		switch {
		case read && ver == state.Missing && v == nil:
			continue
		case read && ver == state.Missing:
			ops = append(ops, state.Create(x.tenant, k, v))
		case read && v == nil:
			ops = append(ops, state.DeleteAt(x.tenant, k, ver))
		case read:
			ops = append(ops, state.PutAt(x.tenant, k, v, ver))
		case v == nil:
			ops = append(ops, state.Delete(x.tenant, k))
		default:
			ops = append(ops, state.Put(x.tenant, k, v))
		}
	}
	if len(ops) == 0 {
		return nil
	}
	c, cancel := ctx()
	defer cancel()
	return x.st.Write(c, namespace, ops...)
}
