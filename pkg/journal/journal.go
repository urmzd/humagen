// Package journal is the platform's record of what happened and when.
//
// Every event a run publishes is appended here, so three questions have one
// answer: what did this tenant's agents do (Query), what happened inside this
// run and the runs it called (Trace), and in what order.
//
// The journal is bounded per tenant: when a tenant holds its capacity, the
// oldest event it holds is dropped for each new one. It answers "what
// happened in the last while", not "what happened last quarter".
//
// It reads and writes the database per request (pkg/state, per request): an
// event is written as it is appended, a read is a bounded page of one
// tenant's keys, and it holds nothing in memory, so any number of processes
// on one database agree, and it does not need pkg/state's single-writer lock.
// An event is kept once by its run and sequence, so two processes that both
// fold the same event store it once.
package journal

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/state"
)

// DefaultCapacity is how many events a journal keeps for each tenant.
const DefaultCapacity = 100_000

// MaxScan bounds how many events one Query reads, newest first, before it
// answers with what matched so far. A query by run reads that run's events
// instead, which the capacity bounds.
const MaxScan = 10_000

// maxDetail bounds the text an entry carries, so a query result stays small
// however long an answer was.
const maxDetail = 240

// maxTrim bounds how many old events one append drops, so a journal opened
// with a smaller capacity than it was written with shrinks over a few
// appends rather than in one.
const maxTrim = 16

// namespace is where the journal is kept. The tenant is the shard key;
// within it:
//
//	e:<time>:<run>:<seq>  an event, by when it happened, for Query
//	r:<run>:<seq>         the same event, by its run, for Events and Trace
//	n                     how many events the tenant holds
//
// time is the event's time in Unix nanoseconds and seq its place in its
// run, both zero-padded, and run is hashed: a run id may hold a ":". An
// event with no run is kept under e:<time>:-:<hash of its id> alone.
//
// A release before this layout kept one ring for every tenant, under
// "slot/<n>" and "capacity" in the tenant "". It is moved into this layout
// the first time any process opens the journal, and converted marks that it
// was.
const namespace = "journal"

const (
	countKey    = "n"
	eventPrefix = "e:"
	runPrefix   = "r:"
	capacityKey = "capacity"
	slotPrefix  = "slot/"
	converted   = "converted:v2"
)

// Journal keeps the most recent events of every tenant. Reads are always
// confined to one tenant.
type Journal struct {
	kept     *state.Table[stored]
	capacity int
}

// held is one event and the origin of the run it belongs to, which the
// event does not carry.
type held struct {
	e      event.Envelope
	origin string
}

// stored is one record: an event, a tenant's count, or, from before this
// layout, an event in its place in the ring or the ring's capacity. An event
// written before the union has a kind and its origin inside it.
type stored struct {
	Seq      int64           `json:"seq,omitempty"`
	Event    json.RawMessage `json:"event,omitempty"`
	Origin   string          `json:"origin,omitempty"`
	Capacity int             `json:"capacity,omitempty"`
	Count    int64           `json:"count,omitempty"`
}

// decode reads a stored event, in either shape.
func (r stored) decode() (held, error) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(r.Event, &probe); err != nil {
		return held{}, err
	}
	if probe.Type == "" {
		e, origin, err := legacy.DecodeV1(r.Event)
		return held{e: e, origin: origin}, err
	}
	var e event.Envelope
	if err := json.Unmarshal(r.Event, &e); err != nil {
		return held{}, err
	}
	return held{e: e, origin: r.Origin}, nil
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

// timeKey writes a time so that keys sort as times do.
func timeKey(t time.Time) string {
	n := int64(0)
	if !t.IsZero() && t.Unix() > 0 {
		n = t.UnixNano()
	}
	return fmt.Sprintf("%020d", n)
}

func runKeys(run string) string { return runPrefix + hash(run) + ":" }

// keys returns where an event is kept: its key by time, and its key by run,
// "" for an event with no run. The key by run, or by time when there is
// none, is the one that says the event is already kept.
func keys(e event.Envelope, raw []byte) (byTime, byRun string) {
	id := ""
	if e.Run != "" {
		id = hash(e.Run) + ":" + state.Seq(int64(e.Seq))
		byRun = runPrefix + id
	} else {
		id = "-:" + hash(cmp.Or(e.ID, string(raw)))
	}
	return eventPrefix + timeKey(e.Time) + ":" + id, byRun
}

// twin returns the key by run of an event kept under a key by time, or "".
func twin(byTime string) string {
	id := strings.TrimPrefix(byTime, eventPrefix)
	if len(id) < 21 {
		return ""
	}
	id = id[21:]
	if strings.HasPrefix(id, "-:") {
		return ""
	}
	return runPrefix + id
}

// New returns a journal that keeps the last capacity events of each tenant,
// in this process's memory.
func New(capacity int) *Journal {
	j, err := Open(nil, capacity)
	if err != nil {
		// A journal in memory has nothing to convert.
		panic(err)
	}
	return j
}

// Open returns a journal that keeps the last capacity events of each tenant
// in st, which may be shared with other processes. A nil st keeps them in
// this process's memory. A tenant holding more than capacity events, from a
// journal opened with a larger one, drops its oldest as new ones arrive.
func Open(st state.Store, capacity int) (*Journal, error) {
	if st == nil {
		st = state.NewMemory()
	}
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	j := &Journal{kept: state.NewTable[stored](st, namespace), capacity: capacity}
	if err := j.convert(); err != nil {
		return nil, fmt.Errorf("journal: convert: %w", err)
	}
	return j, nil
}

// Append records an event of a run that came through origin. An event
// already kept, by its run and sequence, is not kept again. An event with no
// tenant is not kept: no read could return it.
func (j *Journal) Append(e event.Envelope, origin string) error {
	if e.Tenant == "" {
		return nil
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	byTime, byRun := keys(e, raw)
	if err := state.Retry(func() error { return j.append(e.Tenant, byTime, byRun, stored{Event: raw, Origin: origin}) }); err != nil {
		return fmt.Errorf("journal: write: %w", err)
	}
	return nil
}

// append writes one event, its count and the oldest events it drops, at the
// count it read. It returns state.ErrConflict when another writer moved the
// count, or dropped an event it meant to drop.
func (j *Journal) append(tenant, byTime, byRun string, r stored) error {
	return j.appendMany(tenant, []entryKeys{{byTime, byRun, r}})
}

// entryKeys is one event and where it is kept.
type entryKeys struct {
	byTime, byRun string
	r             stored
}

// appendMany writes events in one write, as append does one.
func (j *Journal) appendMany(tenant string, es []entryKeys) error {
	n, err := j.kept.Lookup(tenant, countKey)
	have := err == nil
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return err
	}
	count := n.Value.Count
	b := j.kept.Batch()
	for _, e := range es {
		b.Create(tenant, e.byTime, e.r)
		if e.byRun != "" {
			b.Create(tenant, e.byRun, e.r)
		}
	}
	if over := count + int64(len(es)) - int64(j.capacity); over > 0 {
		olds, _, err := j.kept.List(tenant, state.Query{Prefix: eventPrefix, Limit: int(min(over, int64(maxTrim*len(es))))})
		if err != nil {
			return err
		}
		for _, o := range olds {
			b.DeleteAt(tenant, o.Key, o.Version)
			if t := twin(o.Key); t != "" {
				b.Delete(tenant, t)
			}
			count--
		}
	}
	count += int64(len(es))
	if have {
		b.PutAt(tenant, countKey, stored{Count: count}, n.Version)
	} else {
		b.Create(tenant, countKey, stored{Count: count})
	}
	err = b.Commit()
	if !errors.Is(err, state.ErrConflict) || len(es) != 1 {
		return err
	}
	// The event may be kept already: another process folded it too.
	seen := cmp.Or(es[0].byRun, es[0].byTime)
	if _, lerr := j.kept.Lookup(tenant, seen); lerr == nil {
		return nil
	} else if !errors.Is(lerr, state.ErrNotFound) {
		return lerr
	}
	return err
}

// convert moves the ring kept by a release before this layout, one ring for
// every tenant, into each tenant's events, and deletes it. Each tenant's
// events go in a few writes; a write that conflicts, because a process
// converting at the same time wrote some of them first, is done again one
// event at a time, each kept once.
func (j *Journal) convert() error {
	if _, err := j.kept.Lookup("", converted); err == nil {
		return nil
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	var olds []stored
	var stale []string
	err := j.kept.Load(func(tenant, key string, r stored) error {
		if tenant != "" {
			return nil
		}
		switch {
		case key == capacityKey:
			stale = append(stale, key)
		case strings.HasPrefix(key, slotPrefix):
			stale = append(stale, key)
			if len(r.Event) > 0 {
				olds = append(olds, r)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	slices.SortFunc(olds, func(a, b stored) int { return cmp.Compare(a.Seq, b.Seq) })
	byTenant := map[string][]entryKeys{}
	var order []string
	for _, r := range olds {
		h, err := r.decode()
		if err != nil || h.e.Tenant == "" {
			// An event this build cannot read is history it cannot show.
			continue
		}
		raw, err := json.Marshal(h.e)
		if err != nil {
			continue
		}
		byTime, byRun := keys(h.e, raw)
		if _, ok := byTenant[h.e.Tenant]; !ok {
			order = append(order, h.e.Tenant)
		}
		byTenant[h.e.Tenant] = append(byTenant[h.e.Tenant], entryKeys{byTime, byRun, stored{Event: raw, Origin: h.origin}})
	}
	const chunk = 200
	for _, tenant := range order {
		es := byTenant[tenant]
		// The same event twice in the ring is kept once.
		seen := map[string]bool{}
		es = slices.DeleteFunc(es, func(e entryKeys) bool {
			k := cmp.Or(e.byRun, e.byTime)
			if seen[k] {
				return true
			}
			seen[k] = true
			return false
		})
		if len(es) > j.capacity {
			es = es[len(es)-j.capacity:]
		}
		for len(es) > 0 {
			part := es[:min(chunk, len(es))]
			es = es[len(part):]
			if err := j.appendMany(tenant, part); err == nil {
				continue
			} else if !errors.Is(err, state.ErrConflict) {
				return err
			}
			for _, e := range part {
				if err := state.Retry(func() error { return j.append(tenant, e.byTime, e.byRun, e.r) }); err != nil {
					return err
				}
			}
		}
	}
	for len(stale) > 0 {
		part := stale[:min(state.MaxList, len(stale))]
		stale = stale[len(part):]
		b := j.kept.Batch()
		for _, k := range part {
			b.Delete("", k)
		}
		if err := b.Commit(); err != nil {
			return err
		}
	}
	b := j.kept.Batch()
	b.Create("", converted, stored{Count: 2})
	if err := b.Commit(); err != nil && !errors.Is(err, state.ErrConflict) {
		return err
	}
	return nil
}

// runEvents reads one run's events in a tenant, in the order the run
// published them, at most the capacity.
func (j *Journal) runEvents(tenant, run string) ([]held, error) {
	q := state.Query{Prefix: runKeys(run)}
	var out []held
	for read := 0; read < j.capacity; {
		q.Limit = min(state.MaxList, j.capacity-read)
		page, next, err := j.kept.List(tenant, q)
		if err != nil {
			return nil, fmt.Errorf("journal: read: %w", err)
		}
		read += len(page)
		for _, it := range page {
			h, err := it.Value.decode()
			if err != nil {
				// An event this build cannot read is history it cannot show.
				continue
			}
			out = append(out, h)
		}
		if next == "" {
			break
		}
		q.After = next
	}
	return out, nil
}

// Events returns the events of the named runs in a tenant, oldest first, as
// they were published. It is what the journal still holds: a run whose first
// events have been dropped comes back without them.
func (j *Journal) Events(tenant string, runIDs []string) ([]event.Envelope, error) {
	var out []event.Envelope
	for _, id := range dedupe(runIDs) {
		hs, err := j.runEvents(tenant, id)
		if err != nil {
			return nil, err
		}
		for _, h := range hs {
			out = append(out, h.e)
		}
	}
	if len(runIDs) > 1 {
		slices.SortStableFunc(out, func(a, b event.Envelope) int { return a.Time.Compare(b.Time) })
	}
	return out, nil
}

func dedupe(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// Query selects events. Tenant is required. Every other field narrows, and an
// empty field matches everything.
type Query struct {
	// Visible confines events before text matching and the result bound.
	Visible func(event.Envelope) bool
	// ProjectEvent supplies an operational event view before text matching.
	ProjectEvent func(event.Envelope) event.Envelope
	Tenant       string
	Agent        string
	RunID        string
	// Type is a type prefix ("run.delta"), or a kind from before the union,
	// exact ("delta.tool.call") or a family ("lifecycle").
	Type string
	// Types are more type prefixes. An entry matches Type or any of them.
	Types  []string
	Origin string
	// Contains matches a substring of the entry's detail, ignoring case.
	Contains string
	Since    time.Time
	Until    time.Time
	// Limit caps the result. Zero means DefaultLimit.
	Limit int
}

// DefaultLimit and MaxLimit bound a query's result.
const (
	DefaultLimit = 200
	MaxLimit     = 2000
)

// Entry is one event as a person reads it: the envelope's fields plus a short
// plain-text account of its body.
type Entry struct {
	Seq    uint64    `json:"seq"`
	RunID  string    `json:"run_id"`
	Agent  string    `json:"agent"`
	Rev    int       `json:"rev"`
	Origin string    `json:"origin"`
	Type   string    `json:"type"`
	At     time.Time `json:"at"`
	Detail string    `json:"detail,omitempty"`
}

// Query returns matching entries, newest first. It reads the tenant's
// events newest first and stops at the limit, at the start of the window, or
// after MaxScan events; a query by run reads that run's events.
func (j *Journal) Query(q Query) ([]Entry, error) {
	if q.Tenant == "" {
		return nil, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	needle := strings.ToLower(q.Contains)
	var out []Entry
	// take adds an event when it matches, and reports whether the result
	// is full.
	take := func(h held) bool {
		e := h.e
		switch {
		case e.Tenant != q.Tenant:
			return false
		case q.Agent != "" && e.Agent != q.Agent:
			return false
		case q.RunID != "" && e.Run != q.RunID:
			return false
		case q.Origin != "" && h.origin != q.Origin:
			return false
		case !q.matchesType(e.Type):
			return false
		case !q.Since.IsZero() && e.Time.Before(q.Since):
			return false
		case !q.Until.IsZero() && e.Time.After(q.Until):
			return false
		}
		if q.Visible != nil && !q.Visible(e) {
			return false
		}
		if q.ProjectEvent != nil {
			e = q.ProjectEvent(e)
		}
		entry := entryOf(e, h.origin)
		if needle != "" && !strings.Contains(strings.ToLower(entry.Detail), needle) {
			return false
		}
		out = append(out, entry)
		return len(out) >= limit
	}

	if q.RunID != "" {
		hs, err := j.runEvents(q.Tenant, q.RunID)
		if err != nil {
			return nil, err
		}
		for i := len(hs) - 1; i >= 0; i-- {
			if take(hs[i]) {
				break
			}
		}
		return out, nil
	}

	page := state.Query{Prefix: eventPrefix, Desc: true}
	if !q.Until.IsZero() {
		page.After = eventPrefix + timeKey(q.Until.Add(time.Nanosecond))
	}
	since := ""
	if !q.Since.IsZero() {
		since = eventPrefix + timeKey(q.Since)
	}
	for scanned := 0; scanned < MaxScan; {
		page.Limit = min(state.MaxList, MaxScan-scanned)
		items, next, err := j.kept.List(q.Tenant, page)
		if err != nil {
			return nil, fmt.Errorf("journal: read: %w", err)
		}
		scanned += len(items)
		for _, it := range items {
			if since != "" && it.Key < since {
				return out, nil
			}
			h, err := it.Value.decode()
			if err != nil {
				continue
			}
			if take(h) {
				return out, nil
			}
		}
		if next == "" {
			break
		}
		page.After = next
	}
	return out, nil
}

// matchesType reports whether a type is chosen by Type or Types. No selector
// chooses every type.
func (q Query) matchesType(typ string) bool {
	if q.Type == "" && len(q.Types) == 0 {
		return true
	}
	if q.Type != "" && MatchesType(typ, q.Type) {
		return true
	}
	return slices.ContainsFunc(q.Types, func(sel string) bool { return MatchesType(typ, sel) })
}

// MatchesType reports whether a type is what a selector chose: a type
// prefix, or a kind from before the union.
func MatchesType(typ, sel string) bool {
	return event.Selects(sel, typ) || legacy.TypeMatchesKind(typ, sel)
}

func entryOf(e event.Envelope, origin string) Entry {
	return Entry{
		Seq: e.Seq, RunID: e.Run, Agent: e.Agent, Rev: legacy.RevOf(e.Version), Origin: origin,
		Type: e.Type, At: e.Time, Detail: clip(detail(e)),
	}
}

// detail says what an event's body means, in a line.
func detail(e event.Envelope) string {
	switch b := e.Data.(type) {
	case event.Text:
		return b.Text
	case event.Thinking:
		return b.Text
	case event.ToolCall:
		return b.Name + " " + b.Phase
	case event.ToolDone:
		if b.Error != "" {
			return b.Name + " failed: " + b.Error
		}
		return b.Name + " done"
	case event.Citation:
		return b.Source.DocumentTitle
	case event.Action:
		if b.Error != "" {
			return b.Name + " failed: " + b.Error
		}
		return b.Name + " " + b.Summary
	}
	if wire.IsLifecycle(e.Type) {
		b, _ := wire.LifecycleOf(e)
		switch {
		case b.Error != "":
			return Cause(b.Error)
		case b.Reason != "":
			return b.Reason
		case b.Text != "":
			return b.Text
		}
	}
	return ""
}

// Cause returns the last line of an error. Runtime errors nest one wrapper per
// line, so the first line names the run and the last one says what went wrong.
func Cause(err string) string {
	lines := strings.Split(strings.TrimSpace(err), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxDetail {
		return s
	}
	// Cut on a rune boundary.
	cut := maxDetail
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

// SpanKind says what a span measures.
type SpanKind string

const (
	// SpanRun is a whole run, from accepted to its last event.
	SpanRun SpanKind = "run"
	// SpanAttempt is one execution attempt inside a run.
	SpanAttempt SpanKind = "attempt"
	// SpanTool is one tool call, from requested to done.
	SpanTool SpanKind = "tool"
	// SpanOutput is the window in which the answer streamed.
	SpanOutput SpanKind = "output"
	// SpanParked is time the run spent waiting on something outside itself.
	SpanParked SpanKind = "parked"
)

// Span is an interval inside a run. A span with no End is still open.
type Span struct {
	RunID string    `json:"run_id"`
	Agent string    `json:"agent"`
	Kind  SpanKind  `json:"kind"`
	Name  string    `json:"name"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end,omitzero"`
	// Error is set when the span ended in a failure.
	Error string `json:"error,omitempty"`
}

// Trace rebuilds the spans of the given runs from their events, ordered by
// start. Runs whose events have left the journal contribute nothing.
func (j *Journal) Trace(tenant string, runIDs []string) ([]Span, error) {
	var spans []*Span
	for _, id := range dedupe(runIDs) {
		hs, err := j.runEvents(tenant, id)
		if err != nil {
			return nil, err
		}
		spans = append(spans, trace(hs)...)
	}
	out := make([]Span, len(spans))
	for i, s := range spans {
		out[i] = *s
	}
	slices.SortStableFunc(out, func(a, b Span) int { return a.Start.Compare(b.Start) })
	return out, nil
}

// trace rebuilds one run's spans from its events, in the order it published
// them.
func trace(hs []held) []*Span {
	var (
		run, attempt, output, parked *Span
		tools                        = map[string]*Span{}
		spans                        []*Span
	)
	start := func(e event.Envelope, kind SpanKind, name string) *Span {
		s := &Span{RunID: e.Run, Agent: e.Agent, Kind: kind, Name: name, Start: e.Time}
		spans = append(spans, s)
		return s
	}
	end := func(s **Span, at time.Time, err string) {
		if *s != nil {
			(*s).End, (*s).Error = at, err
			*s = nil
		}
	}
	for _, h := range hs {
		e := h.e
		if run == nil && e.Type != event.TypeAccepted {
			// The accepted event fell out of the journal. Open the run at
			// the first event still held, rather than drop the run.
			run = start(e, SpanRun, e.Agent)
		}
		switch e.Type {
		case event.TypeAccepted:
			if run == nil {
				run = start(e, SpanRun, e.Agent)
			}
		case event.TypeStarted, event.TypeResumed:
			end(&parked, e.Time, "")
			end(&attempt, e.Time, "")
			b, _ := wire.LifecycleOf(e)
			name := "attempt"
			if b.Attempt > 0 {
				name = "attempt " + itoa(b.Attempt)
			}
			attempt = start(e, SpanAttempt, name)
		case event.TypeParked:
			b, _ := wire.LifecycleOf(e)
			end(&output, e.Time, "")
			end(&attempt, e.Time, "")
			name := "waiting"
			if b.Reason != "" {
				name = "waiting: " + b.Reason
			}
			parked = start(e, SpanParked, name)
		case event.TypeText:
			if output == nil {
				output = start(e, SpanOutput, "answer")
			}
			output.End = e.Time
		case event.TypeToolCall:
			b, _ := e.Data.(event.ToolCall)
			if _, seen := tools[b.ID]; !seen && b.ID != "" {
				tools[b.ID] = start(e, SpanTool, b.Name)
			}
		case event.TypeToolDone:
			b, _ := e.Data.(event.ToolDone)
			if s := tools[b.ID]; s != nil {
				s.End, s.Error = e.Time, b.Error
				delete(tools, b.ID)
			}
		case event.TypeCompleted, event.TypeFailed, event.TypeStopped:
			b, _ := wire.LifecycleOf(e)
			output = nil
			end(&attempt, e.Time, b.Error)
			// A failed attempt that will be retried does not end the run. A
			// stopped one ends it, without an error: nothing went wrong.
			if e.Type != event.TypeFailed || b.Terminal {
				for id, s := range tools {
					s.End = e.Time
					delete(tools, id)
				}
				end(&run, e.Time, b.Error)
			}
		}
	}
	return spans
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
