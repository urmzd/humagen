// Package journal is the platform's record of what happened and when.
//
// Every event a run publishes is appended here, so three questions have one
// answer: what did this tenant's agents do (Query), what happened inside this
// run and the runs it called (Trace), and in what order.
//
// The journal is a bounded ring in memory. When it is full the oldest events
// are dropped. It answers "what happened in the last while", not "what
// happened last quarter".
//
// Built with Open, the ring is kept in a state.Store too, one record per
// place in it, so it holds no more there than in memory and comes back after
// a restart. Events are written behind, by Flush, not as each one arrives: an
// event is something that already happened, which a failed write cannot
// undo, and a run streams many of them. What a crash loses is what arrived
// since the last Flush.
package journal

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/state"
)

// DefaultCapacity is how many events a journal keeps.
const DefaultCapacity = 100_000

// maxDetail bounds the text an entry carries, so a query result stays small
// however long an answer was.
const maxDetail = 240

// Journal holds the most recent events across every tenant. Reads are always
// confined to one tenant.
type Journal struct {
	mu     sync.RWMutex
	events []held
	head   int
	full   bool

	// seq counts every event ever appended. An event's place in the ring is
	// its seq modulo the capacity, and that place is its record's key.
	seq int64
	// pending are appended events not yet written. They are bounded by the
	// capacity: one the ring has already dropped need not be written.
	pending []stored
	// flushing makes one Flush at a time, so records are written in order.
	flushing sync.Mutex
	kept     *state.Table[stored]
}

// held is one event and the origin of the run it belongs to, which the
// event does not carry.
type held struct {
	e      event.Envelope
	origin string
}

// stored is one record: an event in its place in the ring, or the capacity
// the ring was written with. An event written before the union has a kind
// and its origin inside it.
type stored struct {
	Seq      int64           `json:"seq,omitempty"`
	Event    json.RawMessage `json:"event,omitempty"`
	Origin   string          `json:"origin,omitempty"`
	Capacity int             `json:"capacity,omitempty"`
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

const capacityKey = "capacity"

func slotKey(seq int64, capacity int) string { return "slot/" + state.Seq(seq%int64(capacity)) }

// New returns a journal that keeps the last capacity events, in memory.
func New(capacity int) *Journal {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Journal{events: make([]held, capacity)}
}

// Open returns a journal that keeps the last capacity events in st too,
// holding the ones st already has. A nil st is New. A ring written with
// another capacity is rewritten for this one, keeping its newest events.
func Open(st state.Store, capacity int) (*Journal, error) {
	j := New(capacity)
	j.kept = state.NewTable[stored](st, "journal")
	var all []stored
	written := 0
	err := j.kept.Load(func(_, key string, r stored) error {
		switch {
		case key == capacityKey:
			written = r.Capacity
		case len(r.Event) > 0:
			all = append(all, r)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("journal: load: %w", err)
	}
	slices.SortFunc(all, func(a, b stored) int { return cmp.Compare(a.Seq, b.Seq) })
	if written != 0 && written != len(j.events) || len(all) > len(j.events) {
		b := j.kept.Batch()
		for _, r := range all {
			b.Delete("", slotKey(r.Seq, max(written, 1)))
		}
		if len(all) > len(j.events) {
			all = all[len(all)-len(j.events):]
		}
		for _, r := range all {
			b.Put("", slotKey(r.Seq, len(j.events)), r)
		}
		b.Put("", capacityKey, stored{Capacity: len(j.events)})
		if err := b.Commit(); err != nil {
			return nil, fmt.Errorf("journal: rewrite for capacity %d: %w", len(j.events), err)
		}
	} else if written == 0 && j.kept.Enabled() {
		if err := j.kept.Put("", capacityKey, stored{Capacity: len(j.events)}); err != nil {
			return nil, fmt.Errorf("journal: write: %w", err)
		}
	}
	for _, r := range all {
		h, err := r.decode()
		if err != nil {
			// An event this build cannot read is history it cannot show.
			continue
		}
		j.put(h)
	}
	if len(all) > 0 {
		j.seq = all[len(all)-1].Seq
	}
	return j, nil
}

// Append records an event of a run that came through origin. With a store,
// it is written at the next Flush.
func (j *Journal) Append(e event.Envelope, origin string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.put(held{e: e, origin: origin})
	if j.kept.Enabled() {
		raw, err := json.Marshal(e)
		if err != nil {
			return
		}
		j.seq++
		j.pending = append(j.pending, stored{Seq: j.seq, Event: raw, Origin: origin})
		if len(j.pending) > len(j.events) {
			j.pending = slices.Delete(j.pending, 0, len(j.pending)-len(j.events))
		}
	}
}

func (j *Journal) put(e held) {
	j.events[j.head] = e
	j.head = (j.head + 1) % len(j.events)
	if j.head == 0 {
		j.full = true
	}
}

// Flush writes the events appended since the last Flush. On a failed write
// they are kept, to be written by the next one. A journal without a store
// has nothing to flush.
func (j *Journal) Flush() error {
	if !j.kept.Enabled() {
		return nil
	}
	j.flushing.Lock()
	defer j.flushing.Unlock()
	j.mu.Lock()
	batch := j.pending
	j.pending = nil
	j.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	b := j.kept.Batch()
	for _, r := range batch {
		b.Put("", slotKey(r.Seq, len(j.events)), r)
	}
	if err := b.Commit(); err != nil {
		j.mu.Lock()
		j.pending = append(batch, j.pending...)
		if len(j.pending) > len(j.events) {
			j.pending = slices.Delete(j.pending, 0, len(j.pending)-len(j.events))
		}
		j.mu.Unlock()
		return fmt.Errorf("journal: write: %w", err)
	}
	return nil
}

// each visits events oldest first. The caller holds the read lock.
func (j *Journal) each(fn func(held)) {
	if j.full {
		for _, e := range j.events[j.head:] {
			fn(e)
		}
	}
	for _, e := range j.events[:j.head] {
		fn(e)
	}
}

// Events returns the events of the named runs in a tenant, oldest first, as
// they were published. It is what the journal still holds: a run whose first
// events have left the ring comes back without them.
func (j *Journal) Events(tenant string, runIDs []string) []event.Envelope {
	j.mu.RLock()
	defer j.mu.RUnlock()
	var out []event.Envelope
	j.each(func(h held) {
		if h.e.Tenant == tenant && slices.Contains(runIDs, h.e.Run) {
			out = append(out, h.e)
		}
	})
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
	Type   string
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

// Query returns matching entries, newest first.
func (j *Journal) Query(q Query) []Entry {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	needle := strings.ToLower(q.Contains)

	j.mu.RLock()
	defer j.mu.RUnlock()
	var out []Entry
	j.each(func(h held) {
		e := h.e
		switch {
		case q.Tenant == "" || e.Tenant != q.Tenant:
			return
		case q.Agent != "" && e.Agent != q.Agent:
			return
		case q.RunID != "" && e.Run != q.RunID:
			return
		case q.Origin != "" && h.origin != q.Origin:
			return
		case q.Type != "" && !MatchesType(e.Type, q.Type):
			return
		case !q.Since.IsZero() && e.Time.Before(q.Since):
			return
		case !q.Until.IsZero() && e.Time.After(q.Until):
			return
		}
		if q.Visible != nil && !q.Visible(e) {
			return
		}
		if q.ProjectEvent != nil {
			e = q.ProjectEvent(e)
		}
		entry := entryOf(e, h.origin)
		if needle != "" && !strings.Contains(strings.ToLower(entry.Detail), needle) {
			return
		}
		out = append(out, entry)
	})
	slices.Reverse(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
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
func (j *Journal) Trace(tenant string, runIDs []string) []Span {
	want := make(map[string]bool, len(runIDs))
	for _, id := range runIDs {
		want[id] = true
	}

	type open struct {
		run, attempt, output, parked *Span
		tools                        map[string]*Span
	}
	state := map[string]*open{}
	var spans []*Span
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

	j.mu.RLock()
	j.each(func(h held) {
		e := h.e
		if e.Tenant != tenant || !want[e.Run] {
			return
		}
		o := state[e.Run]
		if o == nil {
			o = &open{tools: map[string]*Span{}}
			state[e.Run] = o
		}
		if o.run == nil && e.Type != event.TypeAccepted {
			// The accepted event fell out of the journal. Open the run at
			// the first event still held, rather than drop the run.
			o.run = start(e, SpanRun, e.Agent)
		}
		switch e.Type {
		case event.TypeAccepted:
			if o.run == nil {
				o.run = start(e, SpanRun, e.Agent)
			}
		case event.TypeStarted, event.TypeResumed:
			end(&o.parked, e.Time, "")
			end(&o.attempt, e.Time, "")
			b, _ := wire.LifecycleOf(e)
			name := "attempt"
			if b.Attempt > 0 {
				name = "attempt " + itoa(b.Attempt)
			}
			o.attempt = start(e, SpanAttempt, name)
		case event.TypeParked:
			b, _ := wire.LifecycleOf(e)
			end(&o.output, e.Time, "")
			end(&o.attempt, e.Time, "")
			name := "waiting"
			if b.Reason != "" {
				name = "waiting: " + b.Reason
			}
			o.parked = start(e, SpanParked, name)
		case event.TypeText:
			if o.output == nil {
				o.output = start(e, SpanOutput, "answer")
			}
			o.output.End = e.Time
		case event.TypeToolCall:
			b, _ := e.Data.(event.ToolCall)
			if _, seen := o.tools[b.ID]; !seen && b.ID != "" {
				o.tools[b.ID] = start(e, SpanTool, b.Name)
			}
		case event.TypeToolDone:
			b, _ := e.Data.(event.ToolDone)
			if s := o.tools[b.ID]; s != nil {
				s.End, s.Error = e.Time, b.Error
				delete(o.tools, b.ID)
			}
		case event.TypeCompleted, event.TypeFailed, event.TypeStopped:
			b, _ := wire.LifecycleOf(e)
			o.output = nil
			end(&o.attempt, e.Time, b.Error)
			// A failed attempt that will be retried does not end the run. A
			// stopped one ends it, without an error: nothing went wrong.
			if e.Type != event.TypeFailed || b.Terminal {
				for id, s := range o.tools {
					s.End = e.Time
					delete(o.tools, id)
				}
				end(&o.run, e.Time, b.Error)
			}
		}
	})
	j.mu.RUnlock()

	out := make([]Span, len(spans))
	for i, s := range spans {
		out[i] = *s
	}
	slices.SortStableFunc(out, func(a, b Span) int { return a.Start.Compare(b.Start) })
	return out
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
