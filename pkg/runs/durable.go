package runs

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/internal/legacy"

	"github.com/urmzd/humagen/pkg/state"
)

// persisted is how an index keeps its runs in a state.Store.
//
// Recording a run is written through: Record fails, and the run is not
// recorded, when its record cannot be written. Everything after that is an
// observation of something that already happened and is kept elsewhere too:
// lifecycle events on the bus and in the run ledger, spend in the usage
// ledger. A failed write cannot undo them, so those changes are written
// behind: each marks its run, and Flush writes every marked run, keeping the
// marks of any it could not write for the next Flush. What a crash loses is
// what changed since the last Flush, and the ledger reconciler settles a run
// whose ending was among it.
type persisted struct {
	runs   *state.Table[storedRun]
	titles *state.Table[string]
	// dirty are runs changed since they were last written, and dirtyTitles
	// conversations renamed since then, by tenant and thread.
	dirty       map[string]bool
	dirtyTitles map[[2]string]bool
	seq         int64
	seqs        map[string]int64
	// flushing makes one Flush at a time, so a newer record is never
	// overwritten by an older one.
	flushing sync.Mutex
}

// storedRun is a run with what its wire form leaves out. A run's usage
// tally is not kept: a run still working when the process stopped tallies
// afresh from its next call, until the runtime's own figure replaces it.
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

func (p *persisted) record(r *Run) storedRun {
	out := storedRun{PrivateContext: r.privateContext, Run: *r, Seq: p.seqs[r.ID], Requesters: r.requesters, Final: r.final, Closed: r.closed, SettledUp: r.settledUp}
	if r.tag.Type != "" {
		t := r.tag
		out.Trigger = &t
	}
	return out
}

// Open returns an index that keeps its runs and conversation titles in st,
// holding what st already has. A nil st is NewIndex.
func Open(st state.Store) (*Index, error) {
	x := NewIndex()
	if st == nil {
		return x, nil
	}
	p := &persisted{
		runs: state.NewTable[storedRun](st, "runs"), titles: state.NewTable[string](st, "runs.title"),
		dirty: map[string]bool{}, dirtyTitles: map[[2]string]bool{}, seqs: map[string]int64{},
	}
	var loaded []storedRun
	if err := p.runs.Load(func(_, _ string, r storedRun) error {
		loaded = append(loaded, r)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("runs: load: %w", err)
	}
	slices.SortFunc(loaded, func(a, b storedRun) int { return cmp.Compare(a.Seq, b.Seq) })
	for _, s := range loaded {
		r := s.Run
		if r.Citations == nil {
			r.Citations = []Citation{}
		}
		r.privateContext = s.PrivateContext
		trigger, err := s.trigger()
		if err != nil {
			return nil, fmt.Errorf("runs: load %s: %w", r.ID, err)
		}
		r.tag, r.requesters, r.final, r.closed, r.settledUp = trigger, s.Requesters, s.Final, s.Closed, s.SettledUp
		x.runs[r.ID] = &r
		x.order = append(x.order, r.ID)
		x.count(&r)
		p.seqs[r.ID] = s.Seq
		p.seq = max(p.seq, s.Seq)
	}
	if err := p.titles.Load(func(tenant, thread, title string) error {
		x.titles[tenant+"\x00"+thread] = title
		return nil
	}); err != nil {
		return nil, fmt.Errorf("runs: load titles: %w", err)
	}
	x.kept = p
	return x, nil
}

// touch marks runs changed, to be written by the next Flush. The caller holds
// the lock.
func (x *Index) touch(ids ...string) {
	if x.kept == nil {
		return
	}
	for _, id := range ids {
		x.kept.dirty[id] = true
	}
}

// Flush writes every run and title changed since it was last written. On a
// failed write they stay marked, to be written by the next Flush. An index
// without a store has nothing to flush.
func (x *Index) Flush() error {
	p := x.kept
	if p == nil {
		return nil
	}
	p.flushing.Lock()
	defer p.flushing.Unlock()

	x.mu.Lock()
	runs := p.runs.Batch()
	titles := p.titles.Batch()
	ids := make([]string, 0, len(p.dirty))
	for id := range p.dirty {
		if r, ok := x.runs[id]; ok {
			runs.Put(r.Tenant, r.ID, p.record(r))
			ids = append(ids, id)
		}
	}
	names := make([][2]string, 0, len(p.dirtyTitles))
	for k := range p.dirtyTitles {
		titles.Put(k[0], k[1], x.titles[k[0]+"\x00"+k[1]])
		names = append(names, k)
	}
	clear(p.dirty)
	clear(p.dirtyTitles)
	x.mu.Unlock()

	err := runs.Commit()
	if err == nil {
		err = titles.Commit()
	}
	if err != nil {
		x.mu.Lock()
		for _, id := range ids {
			p.dirty[id] = true
		}
		for _, k := range names {
			p.dirtyTitles[k] = true
		}
		x.mu.Unlock()
		return fmt.Errorf("runs: write: %w", err)
	}
	return nil
}

// recorded writes a run that was just recorded. The caller holds the lock,
// and on an error forgets the run.
func (x *Index) recorded(r *Run) error {
	p := x.kept
	if p == nil {
		return nil
	}
	p.seqs[r.ID] = p.seq + 1
	if err := p.runs.Put(r.Tenant, r.ID, p.record(r)); err != nil {
		delete(p.seqs, r.ID)
		return fmt.Errorf("runs: write: %w", err)
	}
	p.seq++
	return nil
}

// titled marks a conversation's title changed. The caller holds the lock.
func (x *Index) titled(key string) {
	if x.kept == nil {
		return
	}
	tenant, thread, _ := strings.Cut(key, "\x00")
	x.kept.dirtyTitles[[2]string{tenant, thread}] = true
}
