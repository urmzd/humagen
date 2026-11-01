package runs

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/internal/legacy"

	"github.com/urmzd/humagen/pkg/state"
)

// namespace is where the index keeps its runs. The tenant is the shard key,
// and every key in it starts with what a request reads by:
//
//	h                    the tenant's head: the last run's number, and how many conversations its runs opened
//	i:<run>              a run's number, written once
//	o:<seq>              a run, by its number in the tenant
//	a:<seq>              a run that has not ended
//	t:<root>:<seq>       a run under a root, its call tree
//	c:<thread>:<seq>     a turn of a conversation (a run no run called)
//	s:<thread>           a conversation at a glance, kept as its runs change
//	u:<activity>:<thread> the same, ordered by its latest activity, newest first
//	p:<project>          what a project holds: its conversations and runs in flight
//	y:<opened>:<thread>  a conversation a housekeeping run opened
//
// A thread, a root and a project are hashed: each may hold a ":".
//
// Runs kept by a release before this layout, one per key in the namespace
// "runs" with titles in "runs.title", are moved into it once.
const namespace = "runs.v2"

// The namespaces an older release kept runs and titles in, read once to move
// them.
const (
	oldRuns   = "runs"
	oldTitles = "runs.title"
)

// converted marks, in the tenant "", that the runs an older release kept have
// been moved.
const converted = "converted:v1"

// Bounds on what one read gathers, so no request reads a tenant's whole
// history.
const (
	// MaxScan is the most runs a read that pages a tenant's runs from the
	// newest (List, Recent, Summarize, Search and the like) looks at.
	MaxScan = 20000
	// MaxTree is the most runs one call tree, one conversation's turns or
	// one tenant's runs in flight hold for a read.
	MaxTree = 5000
	// MaxThreadScan is the most conversations one page of conversations
	// looks at to fill itself, and the most a count across them reads.
	MaxThreadScan = 20000
	// scanSlack is how far before a window's start a read from the newest
	// keeps looking: a run is numbered when it is recorded, and its start
	// is when its trigger says it happened, which may be a little earlier.
	scanSlack = time.Hour
	// lookups is how many records a read fetches at once.
	lookups = 16
)

const headKey = "h"

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

func ptrKey(id string) string                 { return "i:" + id }
func recKey(seq int64) string                 { return "o:" + state.Seq(seq) }
func activeKey(seq int64) string              { return "a:" + state.Seq(seq) }
func treePrefix(root string) string           { return "t:" + hash(root) + ":" }
func treeKey(root string, seq int64) string   { return treePrefix(root) + state.Seq(seq) }
func turnPrefix(thread string) string         { return "c:" + hash(thread) + ":" }
func turnKey(thread string, seq int64) string { return turnPrefix(thread) + state.Seq(seq) }
func threadKey(thread string) string          { return "s:" + hash(thread) }
func projectKey(project string) string        { return "p:" + hash(project) }
func systemKey(opened int64, thread string) string {
	return "y:" + state.Seq(opened) + ":" + hash(thread)
}

// activityKey orders conversations by their latest activity, newest first,
// and by id between two at the same moment, as a key read in order.
func activityKey(at time.Time, thread string) string {
	return "u:" + state.Seq(math.MaxInt64-nanos(at)) + ":" + thread
}

func nanos(at time.Time) int64 {
	if at.Before(time.Unix(0, 0)) {
		return 0
	}
	return at.UnixNano()
}

// seqOf reads the number at the end of a key.
func seqOf(key string) int64 {
	n, _ := strconv.ParseInt(key[strings.LastIndexByte(key, ':')+1:], 10, 64)
	return n
}

// head is a tenant's counters.
type head struct {
	Seq    int64 `json:"seq"`
	Opened int64 `json:"opened"`
}

// storedRun is a run with what its wire form leaves out.
type storedRun struct {
	PrivateContext bool `json:"private_context,omitempty"`
	Run
	Seq int64 `json:"seq"`
	// Trigger is the event the run was accepted with.
	Trigger *event.Envelope `json:"trigger,omitempty"`
	// Tag is what a run accepted before the event union was accepted with.
	// It is read through legacy.DecodeTag and never written.
	Tag        json.RawMessage `json:"tag,omitempty"`
	Requesters []string        `json:"requesters,omitempty"`
	Final      bool            `json:"final,omitempty"`
	Closed     bool            `json:"closed,omitempty"`
	SettledUp  bool            `json:"settled_up,omitempty"`
	// Tally is the usage counted from the run's reports while it works, with
	// the reports it has counted, so a report two processes both fold is
	// counted once.
	Tally *event.Tally `json:"tally,omitempty"`
	// ObservedBy is the index that recorded the run. Only that index, which
	// has folded every event since, may say the run needed no person.
	ObservedBy string `json:"observed_by,omitempty"`
	// LastSeq is the sequence of the last lifecycle event folded in, so an
	// event two processes both fold changes the run once.
	LastSeq uint64 `json:"last_seq,omitempty"`
	// OpenAttached is how many runs attached to this one have not ended.
	OpenAttached int `json:"open_attached,omitempty"`
}

// trigger is the event the run was accepted with, read from an old tag for a
// run accepted before the event union.
func (s storedRun) trigger() (event.Envelope, error) {
	switch {
	case s.Trigger != nil:
		return *s.Trigger, nil
	case len(s.Tag) > 0 && string(s.Tag) != "null":
		return legacy.DecodeTag(s.Tag)
	}
	return event.Envelope{}, nil
}

func (s storedRun) run() (*Run, error) {
	r := s.Run
	if r.Citations == nil {
		r.Citations = []Citation{}
	}
	trigger, err := s.trigger()
	if err != nil {
		return nil, fmt.Errorf("runs: read %s: %w", r.ID, err)
	}
	r.privateContext = s.PrivateContext
	r.tag, r.requesters, r.final, r.closed, r.settledUp = trigger, s.Requesters, s.Final, s.Closed, s.SettledUp
	r.seq, r.tally, r.observedBy, r.lastSeq, r.openAttached = s.Seq, s.Tally, s.ObservedBy, s.LastSeq, s.OpenAttached
	return &r, nil
}

func stored(r *Run) storedRun {
	out := storedRun{
		PrivateContext: r.privateContext, Run: *r, Seq: r.seq, Requesters: r.requesters, Final: r.final, Closed: r.closed, SettledUp: r.settledUp,
		Tally: r.tally, ObservedBy: r.observedBy, LastSeq: r.lastSeq, OpenAttached: r.openAttached,
	}
	if r.tag.Type != "" {
		t := r.tag
		out.Trigger = &t
	}
	return out
}

// storedThread is a conversation at a glance with what its summary is kept
// from.
type storedThread struct {
	ThreadSummary
	// Name is the title a person or the platform gave it, "" for none.
	Name string `json:"name,omitempty"`
	// Answers counts the turns at each point of the conversation, by the
	// turn they follow, to count its branches.
	Answers map[string]int `json:"answers,omitempty"`
	// Opened is its place among the conversations the tenant's runs opened.
	Opened int64 `json:"opened,omitempty"`
	// Runs is how many runs it holds. A conversation named before any run
	// is in it holds none and is not listed.
	Runs int `json:"runs"`
}

func (s storedThread) summary() ThreadSummary {
	out := s.ThreadSummary
	out.Agents = slices.Clone(s.Agents)
	out.People = slices.Clone(s.People)
	if out.People == nil {
		out.People = []string{}
	}
	if s.Name != "" {
		out.Title, out.Named = s.Name, true
	}
	return out
}

// Index is every run a workspace has accepted, read and written per request
// in a state.Store, so every process on one database answers the same.
type Index struct {
	st      state.Store
	tenants *state.Tenants
	// instance names this index, for ObservedBy.
	instance string
	// seqs caches each run's number by tenant and id. A run's number never
	// changes, so the cache may be dropped at any time.
	seqs sync.Map
	log  *slog.Logger
}

// NewIndex returns an empty index in this process's memory.
func NewIndex() *Index {
	x, err := Open(nil)
	if err != nil {
		panic(err) // memory does not fail
	}
	return x
}

// Open returns the index kept in st, which may be shared with other
// processes. A nil st keeps it in this process's memory.
//
// Runs and titles an older release kept are moved into the index the first
// time any process opens it; a process that opens it at the same time finds
// them moved.
func Open(st state.Store) (*Index, error) {
	if st == nil {
		st = state.NewMemory()
	}
	var id [8]byte
	_, _ = rand.Read(id[:])
	x := &Index{st: st, instance: hex.EncodeToString(id[:]), log: slog.Default()}
	tenants, err := state.OpenTenants(st, namespace)
	if err != nil {
		return nil, fmt.Errorf("runs: tenants: %w", err)
	}
	x.tenants = tenants
	if err := x.convert(); err != nil {
		return nil, fmt.Errorf("runs: convert: %w", err)
	}
	return x, nil
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// lookup reads one record into v, and its version: 0 when there is none.
func (x *Index) lookup(tenant, key string, v any) (int64, error) {
	c, cancel := ctx()
	defer cancel()
	r, err := x.st.Lookup(c, namespace, tenant, key)
	if errors.Is(err, state.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(r.Value, v); err != nil {
		return 0, fmt.Errorf("runs: %q: %w", key, err)
	}
	return r.Version, nil
}

// keys reads the keys of a tenant under a prefix, in order, at most most of
// them.
func (x *Index) keys(tenant, prefix string, most int) ([]string, error) {
	var out []string
	after := ""
	for len(out) < most {
		c, cancel := ctx()
		recs, next, err := x.st.List(c, namespace, tenant, state.Query{Prefix: prefix, After: after, Limit: min(most-len(out), state.MaxList)})
		cancel()
		if err != nil {
			return nil, err
		}
		for _, r := range recs {
			out = append(out, r.Key)
		}
		if next == "" {
			break
		}
		after = next
	}
	return out, nil
}

// page reads one page of records under a prefix.
func (x *Index) page(tenant string, q state.Query) ([]state.Record, string, error) {
	c, cancel := ctx()
	defer cancel()
	return x.st.List(c, namespace, tenant, q)
}

// seq reads a run's number, or 0 when the tenant has no such run.
func (x *Index) seq(tenant, id string) (int64, error) {
	if id == "" {
		return 0, nil
	}
	ck := tenant + "\x00" + id
	if n, ok := x.seqs.Load(ck); ok {
		return n.(int64), nil
	}
	var n int64
	if _, err := x.lookup(tenant, ptrKey(id), &n); err != nil || n == 0 {
		return 0, err
	}
	x.seqs.Store(ck, n)
	return n, nil
}

// at reads the run numbered seq, with its version.
func (x *Index) at(tenant string, seq int64) (*Run, int64, error) {
	var s storedRun
	v, err := x.lookup(tenant, recKey(seq), &s)
	if err != nil || v == 0 {
		return nil, 0, err
	}
	r, err := s.run()
	return r, v, err
}

// get reads one run, or ErrNotFound.
func (x *Index) get(tenant, id string) (*Run, int64, error) {
	n, err := x.seq(tenant, id)
	if err != nil {
		return nil, 0, err
	}
	if n == 0 {
		return nil, 0, ErrNotFound
	}
	r, v, err := x.at(tenant, n)
	if err == nil && r == nil {
		return nil, 0, ErrNotFound
	}
	return r, v, err
}

// all reads the runs numbered seqs, in that order, a few at a time. A number
// with no run is left out.
func (x *Index) all(tenant string, seqs []int64) ([]*Run, error) {
	out := make([]*Run, len(seqs))
	errs := make([]error, len(seqs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, lookups)
	for i, n := range seqs {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			out[i], _, errs[i] = x.at(tenant, n)
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(out, func(r *Run) bool { return r == nil }), nil
}

// under reads the runs whose index keys are under a prefix, oldest first.
func (x *Index) under(tenant, prefix string, most int) ([]*Run, error) {
	keys, err := x.keys(tenant, prefix, most)
	if err != nil {
		return nil, err
	}
	seqs := make([]int64, len(keys))
	for i, k := range keys {
		seqs[i] = seqOf(k)
	}
	return x.all(tenant, seqs)
}

// scan calls fn for a tenant's runs from the newest, until fn returns false
// or MaxScan runs have been read.
func (x *Index) scan(tenant string, fn func(r *Run) bool) error {
	after, seen := "", 0
	for seen < MaxScan {
		recs, next, err := x.page(tenant, state.Query{Prefix: "o:", After: after, Limit: min(state.MaxList, MaxScan-seen), Desc: true})
		if err != nil {
			return err
		}
		for _, rec := range recs {
			seen++
			var s storedRun
			if err := json.Unmarshal(rec.Value, &s); err != nil {
				return fmt.Errorf("runs: %q: %w", rec.Key, err)
			}
			r, err := s.run()
			if err != nil {
				return err
			}
			if !fn(r) {
				return nil
			}
		}
		if next == "" {
			return nil
		}
		after = next
	}
	return nil
}

// fail logs a change that could not be written. The change did not happen;
// the event that asked for it is folded again when it is delivered again.
func (x *Index) fail(what, tenant, id string, err error) {
	x.log.Error("runs: could not write", "change", what, "tenant", tenant, "run", id, "error", err)
}

// txn is one change to a tenant's runs: what it read, at the versions it read
// them, and what it changed, written together or not at all.
type txn struct {
	x        *Index
	tenant   string
	runs     map[string]*held
	order    []*held
	threads  map[string]*heldThread
	projects map[string]*heldCount
	head     *heldHead
	ops      []state.Op
	err      error
}

type held struct {
	version int64
	before  Run
	run     *Run
	changed bool
	created bool
}

type heldThread struct {
	version int64
	before  storedThread
	s       storedThread
	changed bool
}

type heldCount struct {
	version int64
	n       storedCount
	changed bool
}

// storedCount is a project's count with the project's name.
type storedCount struct {
	Name string `json:"name"`
	ProjectCount
}

type heldHead struct {
	version int64
	h       head
	changed bool
}

// do runs fn in a txn on a tenant and writes what it changed, again from a
// fresh read when another process wrote what it read in between.
func (x *Index) do(tenant string, fn func(t *txn) error) error {
	return state.Retry(func() error {
		t := &txn{x: x, tenant: tenant, runs: map[string]*held{}, threads: map[string]*heldThread{}, projects: map[string]*heldCount{}}
		if err := fn(t); err != nil {
			if errors.Is(err, errSkip) {
				return nil
			}
			return err
		}
		return t.commit()
	})
}

// errSkip ends a txn without writing.
var errSkip = errors.New("runs: nothing to write")

// run reads a run into the txn, or returns nil for none.
func (t *txn) run(id string) (*Run, error) {
	if h, ok := t.runs[id]; ok {
		return h.run, nil
	}
	r, v, err := t.x.get(t.tenant, id)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	h := &held{version: v, before: *r, run: r}
	t.runs[id] = h
	t.order = append(t.order, h)
	return r, nil
}

// touch marks a run read into the txn changed.
func (t *txn) touch(r *Run) {
	if h, ok := t.runs[r.ID]; ok {
		h.changed = true
	}
}

func (t *txn) thread(id string) (*storedThread, error) {
	if h, ok := t.threads[id]; ok {
		return &h.s, nil
	}
	h := &heldThread{}
	v, err := t.x.lookup(t.tenant, threadKey(id), &h.s)
	if err != nil {
		return nil, err
	}
	if v == 0 {
		h.s = storedThread{ThreadSummary: ThreadSummary{ID: id, People: []string{}}}
	}
	h.version, h.before = v, h.s
	h.before.Agents, h.before.People = slices.Clone(h.s.Agents), slices.Clone(h.s.People)
	t.threads[id] = h
	return &h.s, nil
}

func (t *txn) threadChanged(id string) { t.threads[id].changed = true }

func (t *txn) project(name string) (*ProjectCount, error) {
	if h, ok := t.projects[name]; ok {
		return &h.n.ProjectCount, nil
	}
	h := &heldCount{}
	v, err := t.x.lookup(t.tenant, projectKey(name), &h.n)
	if err != nil {
		return nil, err
	}
	h.version, h.n.Name = v, name
	t.projects[name] = h
	return &h.n.ProjectCount, nil
}

func (t *txn) projectChanged(name string) { t.projects[name].changed = true }

func (t *txn) headOf() (*head, error) {
	if t.head == nil {
		t.head = &heldHead{}
		v, err := t.x.lookup(t.tenant, headKey, &t.head.h)
		if err != nil {
			t.head = nil
			return nil, err
		}
		t.head.version = v
	}
	return &t.head.h, nil
}

// put adds an op that writes v at version: created when version is 0, and
// unconditional when version is -2.
func (t *txn) put(key string, v any, version int64) {
	raw, err := json.Marshal(v)
	if err != nil {
		t.err = cmp.Or(t.err, err)
		return
	}
	op := state.Put(t.tenant, key, raw)
	switch {
	case version == 0:
		op.If = state.Missing
	case version > 0:
		op.If = version
	}
	t.ops = append(t.ops, op)
}

const always = -2

func (t *txn) del(key string) { t.ops = append(t.ops, state.Delete(t.tenant, key)) }

// place adds a run that was just recorded, or moved from an older release,
// to the tenant: its number, its record, its indexes, its conversation and
// its project's count.
func (t *txn) place(r *Run) error {
	h, err := t.headOf()
	if err != nil {
		return err
	}
	h.Seq++
	t.head.changed = true
	r.seq = h.Seq
	t.put(ptrKey(r.ID), r.seq, 0)
	t.put(treeKey(r.Root, r.seq), "", always)
	if r.Parent == "" {
		t.put(turnKey(r.Thread, r.seq), "", always)
	}
	if !r.Status.Terminal() {
		t.put(activeKey(r.seq), "", always)
	}
	hr := &held{run: r, created: true, changed: true}
	t.runs[r.ID] = hr
	t.order = append(t.order, hr)

	s, err := t.thread(r.Thread)
	if err != nil {
		return err
	}
	t.threadChanged(r.Thread)
	opened := s.Runs == 0
	if opened {
		h.Opened++
		s.Opened = h.Opened
		s.Title, s.Origin, s.Project, s.Channel, s.CreatedAt = r.Text, r.Origin, r.Project, cmp.Or(r.Channel, DefaultChannel), r.CreatedAt
		if r.Origin == OriginSystem {
			t.put(systemKey(s.Opened, r.Thread), r.Thread, always)
		}
	}
	s.Runs++
	if !r.Status.Terminal() {
		s.Active++
	}
	if r.Status == Failed {
		s.Failed++
	}
	if r.UpdatedAt.After(s.UpdatedAt) {
		s.UpdatedAt = r.UpdatedAt
	}
	if r.Parent == "" {
		s.Turns++
		if !slices.Contains(s.Agents, r.Agent) {
			s.Agents = append(s.Agents, r.Agent)
		}
		if who := AskedBy(*r); who != "" && !slices.Contains(s.People, who) {
			s.People = append(s.People, who)
		}
		// A second answer at the same point makes that point a branch.
		// Runs answering one person's message answer it side by side
		// instead.
		if s.Answers == nil {
			s.Answers = map[string]int{}
		}
		if s.Answers[r.Prev]++; s.Answers[r.Prev] == 2 && r.Message == "" {
			s.Branches++
		}
	}
	n, err := t.project(s.Project)
	if err != nil {
		return err
	}
	t.projectChanged(s.Project)
	if opened {
		n.Threads++
	}
	if !r.Status.Terminal() {
		n.Active++
	}
	if r.UpdatedAt.After(n.ActiveAt) {
		n.ActiveAt = r.UpdatedAt
	}
	return t.x.tenants.Mark(t.tenant)
}

// commit writes what the txn changed: each run at the version it read, and
// what follows from how it changed: whether it is in flight, its attached
// count on its caller, its conversation and its project.
func (t *txn) commit() error {
	if t.err != nil {
		return t.err
	}
	// A run that ends may change its caller, which joins the list.
	for i := 0; i < len(t.order); i++ {
		h := t.order[i]
		if !h.changed {
			continue
		}
		r := h.run
		if h.created {
			t.put(recKey(r.seq), stored(r), 0)
			continue
		}
		t.put(recKey(r.seq), stored(r), h.version)
		was, is := !h.before.Status.Terminal(), !r.Status.Terminal()
		if was == is && h.before.Status != Failed && r.Status != Failed && !r.UpdatedAt.After(h.before.UpdatedAt) {
			continue
		}
		if was && !is {
			t.del(activeKey(r.seq))
			if r.Lifecycle == Attached && r.Parent != "" {
				p, err := t.run(r.Parent)
				if err != nil {
					return err
				}
				if p != nil && p.openAttached > 0 {
					p.openAttached--
					t.touch(p)
				}
			}
		}
		s, err := t.thread(r.Thread)
		if err != nil {
			return err
		}
		active := b2i(is) - b2i(was)
		failed := b2i(r.Status == Failed) - b2i(h.before.Status == Failed)
		later := r.UpdatedAt.After(s.UpdatedAt)
		if active != 0 || failed != 0 || later {
			s.Active += active
			s.Failed += failed
			if later {
				s.UpdatedAt = r.UpdatedAt
			}
			t.threadChanged(r.Thread)
		}
		n, err := t.project(s.Project)
		if err != nil {
			return err
		}
		if active != 0 || r.UpdatedAt.After(n.ActiveAt) {
			n.Active += active
			if r.UpdatedAt.After(n.ActiveAt) {
				n.ActiveAt = r.UpdatedAt
			}
			t.projectChanged(s.Project)
		}
	}
	for id, h := range t.threads {
		if !h.changed {
			continue
		}
		t.put(threadKey(id), h.s, h.version)
		if h.before.Runs > 0 {
			t.del(activityKey(h.before.UpdatedAt, id))
		}
		if h.s.Runs > 0 {
			t.put(activityKey(h.s.UpdatedAt, id), h.s, always)
		}
	}
	for name, h := range t.projects {
		if h.changed {
			t.put(projectKey(name), h.n, h.version)
		}
	}
	if t.head != nil && t.head.changed {
		t.put(headKey, t.head.h, t.head.version)
	}
	if t.err != nil {
		return t.err
	}
	if len(t.ops) == 0 {
		return nil
	}
	c, cancel := ctx()
	defer cancel()
	if err := t.x.st.Write(c, namespace, t.ops...); err != nil {
		return err
	}
	for _, h := range t.order {
		if h.created {
			t.x.seqs.Store(t.tenant+"\x00"+h.run.ID, h.run.seq)
		}
	}
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// convert moves the runs and titles an older release kept, one record per
// run, into the layout above, in the order they were recorded. Each run is
// placed once, however many processes convert at the same time: its number
// is written only if it has none.
func (x *Index) convert() error {
	var done bool
	if v, err := x.lookup("", converted, &done); err != nil || v > 0 {
		return err
	}
	c, cancel := ctx()
	recs, err := x.st.Load(c, oldRuns)
	cancel()
	if err != nil {
		return err
	}
	olds := make([]storedRun, 0, len(recs))
	for _, rec := range recs {
		if rec.Tenant == "" {
			continue
		}
		var s storedRun
		if err := json.Unmarshal(rec.Value, &s); err != nil {
			return fmt.Errorf("%q/%q: %w", rec.Tenant, rec.Key, err)
		}
		if s.Tenant == "" {
			s.Tenant = rec.Tenant
		}
		olds = append(olds, s)
	}
	slices.SortStableFunc(olds, func(a, b storedRun) int { return cmp.Compare(a.Seq, b.Seq) })
	// An older release counted a run's open attached calls from memory.
	open := map[[2]string]int{}
	for _, s := range olds {
		if s.Parent != "" && s.Lifecycle == Attached && !s.Status.Terminal() {
			open[[2]string{s.Tenant, s.Parent}]++
		}
	}
	for _, s := range olds {
		r, err := s.run()
		if err != nil {
			return err
		}
		r.tally, r.observedBy, r.lastSeq = nil, "", 0
		r.openAttached = open[[2]string{r.Tenant, r.ID}]
		err = x.do(r.Tenant, func(t *txn) error {
			if n, err := x.seq(r.Tenant, r.ID); err != nil || n != 0 {
				return cmp.Or(err, errSkip)
			}
			return t.place(r)
		})
		if err != nil && !errors.Is(err, errSkip) {
			return err
		}
	}
	c, cancel = ctx()
	titles, err := x.st.Load(c, oldTitles)
	cancel()
	if err != nil {
		return err
	}
	for _, rec := range titles {
		var title string
		if err := json.Unmarshal(rec.Value, &title); err != nil {
			return fmt.Errorf("title %q/%q: %w", rec.Tenant, rec.Key, err)
		}
		if err := x.setTitle(rec.Tenant, rec.Key, title, true); err != nil {
			return err
		}
	}
	c, cancel = ctx()
	defer cancel()
	return x.st.Write(c, namespace, state.Put("", converted, []byte("true")))
}
