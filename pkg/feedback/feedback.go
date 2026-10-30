// Package feedback keeps ratings and comments linked to their source runs.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds nothing in memory, so any number of processes on one database agree,
// and it does not need pkg/state's single-writer lock.
package feedback

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// Record is one person's latest feedback on a run. The run owns its content.
// Subject is the key the platform keeps the person by, which may hold an
// address, so it is never handed out: a route names the rater as the
// platform calls them. Seq orders a run's feedback and pages it, and is not a
// subject.
type Record struct {
	Run       string    `json:"run"`
	Subject   string    `json:"subject"`
	Seq       int64     `json:"seq,omitempty"`
	Rating    string    `json:"rating"`
	Reason    string    `json:"reason,omitempty"`
	Comment   string    `json:"comment,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MaxPage is the most records one page of a run's feedback holds.
const MaxPage = 200

// namespace is where feedback is kept. The tenant is the shard key; within
// it, every key starts with its run's hash, so a run's feedback is one
// prefix:
//
//	r:<run>:<seq>      a record, in the order it was first given
//	x:<run>:<subject>  the seq of a person's record on a run
//	h:<run>            the last seq a run gave
//
// A run and a subject are hashed: either may hold a ":" or an address.
const namespace = "feedback"

// converted marks, in the tenant "", that records kept before the layout
// above have been moved into it.
const converted = "converted:v2"

// maxTries bounds how often a save reads again after another process changed
// the same run between its read and its write. Each try waits a little
// longer, at random, so writers on one run spread out.
const maxTries = 64

func backoff(try int) {
	if try > 0 {
		time.Sleep(time.Duration(rand.Int64N(int64(min(try, 20)) * int64(time.Millisecond))))
	}
}

// ErrBusy reports a save that lost every try to other writers on its run.
var ErrBusy = errors.New("feedback: too many writers on one run")

// Store keeps feedback in a state.Store.
type Store struct {
	st state.Store
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

func runPrefix(run string) string          { return "r:" + hash(run) + ":" }
func recordKey(run string, n int64) string { return runPrefix(run) + state.Seq(n) }
func subjectKey(run, subject string) string {
	return "x:" + hash(run) + ":" + hash(subject)
}
func headKey(run string) string { return "h:" + hash(run) }

// Open returns a store on st, which may be shared with other processes. A nil
// st keeps feedback in this process's memory.
//
// Records kept by a release before this layout, under one key per run and
// person, are moved into it the first time any process opens the store; a
// process that opens it at the same time finds them moved.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	s := &Store{st: st}
	if err := s.convert(); err != nil {
		return nil, fmt.Errorf("feedback: convert: %w", err)
	}
	return s, nil
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), state.Timeout)
}

// lookup reads one record into v, and its version.
func (s *Store) lookup(tenant, key string, v any) (int64, error) {
	c, cancel := ctx()
	defer cancel()
	r, err := s.st.Lookup(c, namespace, tenant, key)
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(r.Value, v); err != nil {
		return 0, fmt.Errorf("feedback: %q: %w", key, err)
	}
	return r.Version, nil
}

// write is one write's ops, built as JSON.
type write struct {
	ops []state.Op
	err error
}

func (w *write) put(tenant, key string, v any, version int64) {
	raw, err := json.Marshal(v)
	if err != nil {
		w.err = cmp.Or(w.err, err)
		return
	}
	op := state.Put(tenant, key, raw)
	op.If = version
	w.ops = append(w.ops, op)
}

func (w *write) commit(st state.Store) error {
	if w.err != nil {
		return w.err
	}
	c, cancel := ctx()
	defer cancel()
	return st.Write(c, namespace, w.ops...)
}

// advance sets a run's head to at least n, at the version read: Missing when
// the run has none.
func (w *write) advance(tenant, run string, head, n, version int64) {
	if version == 0 {
		version = state.Missing
	}
	w.put(tenant, headKey(run), max(head, n), version)
}

// convert moves records kept one per run and person into the layout by run.
// A record kept before records were numbered is given the next number in its
// run when it is moved, so a cursor means the same record from then on.
func (s *Store) convert() error {
	var done int64
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
	type old struct {
		tenant, key string
		r           Record
	}
	var olds []old
	for _, rec := range recs {
		if rec.Tenant == "" || strings.Contains(rec.Key, ":") {
			continue
		}
		var r Record
		if err := json.Unmarshal(rec.Value, &r); err != nil {
			return fmt.Errorf("%q/%q: %w", rec.Tenant, rec.Key, err)
		}
		olds = append(olds, old{rec.Tenant, rec.Key, r})
	}
	// Numbered records keep their number and go first; the rest follow, in
	// the order they were last rated. Every process converting goes in this
	// order, so when one reaches an unnumbered record, every numbered one is
	// moved.
	slices.SortStableFunc(olds, func(a, b old) int {
		if c := strings.Compare(a.tenant, b.tenant); c != 0 {
			return c
		}
		if (a.r.Seq == 0) != (b.r.Seq == 0) {
			if a.r.Seq == 0 {
				return 1
			}
			return -1
		}
		return cmp.Or(a.r.UpdatedAt.Compare(b.r.UpdatedAt), strings.Compare(a.key, b.key))
	})
	for _, o := range olds {
		if err := s.move(o.tenant, o.key, o.r); err != nil {
			return err
		}
	}
	w := &write{}
	w.put("", converted, 2, state.Missing)
	if err := w.commit(s.st); err != nil && !errors.Is(err, state.ErrConflict) {
		return err
	}
	return nil
}

// move writes one old record under the new layout and deletes its old key,
// in one write conditional on the old key: when a process converting at the
// same time moved it first, the old key is gone and there is nothing to do.
func (s *Store) move(tenant, key string, r Record) error {
	for try := range maxTries {
		backoff(try)
		var was Record
		at, err := s.lookup(tenant, key, &was)
		if errors.Is(err, state.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var head int64
		hv, err := s.lookup(tenant, headKey(r.Run), &head)
		if err != nil && !errors.Is(err, state.ErrNotFound) {
			return err
		}
		r := r
		if r.Seq == 0 {
			r.Seq = head + 1
		}
		w := &write{}
		w.put(tenant, recordKey(r.Run, r.Seq), r, state.Missing)
		w.put(tenant, subjectKey(r.Run, r.Subject), r.Seq, state.Missing)
		w.advance(tenant, r.Run, head, r.Seq, hv)
		w.ops = append(w.ops, state.DeleteAt(tenant, key, at))
		if err := w.commit(s.st); !errors.Is(err, state.ErrConflict) {
			return err
		}
	}
	return ErrBusy
}

// Save replaces a rating. Repeating a rating preserves existing comments.
func (s *Store) Save(tenant string, r Record) (Record, error) {
	if tenant == "" || r.Run == "" || r.Subject == "" || (r.Rating != "up" && r.Rating != "down") || len(r.Comment) > 4000 || len(r.Reason) > 80 {
		return Record{}, errors.New("feedback: invalid record")
	}
	r.Comment, r.Reason = strings.TrimSpace(r.Comment), strings.TrimSpace(r.Reason)
	for try := range maxTries {
		backoff(try)
		out, err := s.save(tenant, r)
		if !errors.Is(err, state.ErrConflict) {
			return out, err
		}
	}
	return Record{}, ErrBusy
}

// save reads what the run holds and writes the rating at the versions read.
// It returns state.ErrConflict when another writer moved any of them.
func (s *Store) save(tenant string, r Record) (Record, error) {
	r.UpdatedAt = time.Now().UTC()
	var n int64
	_, err := s.lookup(tenant, subjectKey(r.Run, r.Subject), &n)
	switch {
	case err == nil:
		var old Record
		at, err := s.lookup(tenant, recordKey(r.Run, n), &old)
		if errors.Is(err, state.ErrNotFound) {
			// One write makes both keys, so this cannot stay; read again.
			return Record{}, state.ErrConflict
		}
		if err != nil {
			return Record{}, err
		}
		r.Seq = old.Seq
		if old.Rating == r.Rating {
			r.Comment = cmp.Or(r.Comment, old.Comment)
			r.Reason = cmp.Or(r.Reason, old.Reason)
		}
		w := &write{}
		w.put(tenant, recordKey(r.Run, n), r, at)
		return r, w.commit(s.st)
	case !errors.Is(err, state.ErrNotFound):
		return Record{}, err
	}
	var head int64
	hv, err := s.lookup(tenant, headKey(r.Run), &head)
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return Record{}, err
	}
	r.Seq = head + 1
	w := &write{}
	w.advance(tenant, r.Run, head, r.Seq, hv)
	w.put(tenant, subjectKey(r.Run, r.Subject), r.Seq, state.Missing)
	w.put(tenant, recordKey(r.Run, r.Seq), r, state.Missing)
	return r, w.commit(s.st)
}

// List returns at most MaxPage ratings for a run, in the order they were
// first given, after the one numbered after (0 for the first page).
func (s *Store) List(tenant, run string, after int64) ([]Record, error) {
	q := state.Query{Prefix: runPrefix(run), Limit: MaxPage}
	if after > 0 {
		q.After = recordKey(run, after)
	}
	c, cancel := ctx()
	defer cancel()
	recs, _, err := s.st.List(c, namespace, tenant, q)
	if err != nil {
		return nil, fmt.Errorf("feedback: list: %w", err)
	}
	out := make([]Record, 0, len(recs))
	for _, rec := range recs {
		var r Record
		if err := json.Unmarshal(rec.Value, &r); err != nil {
			return nil, fmt.Errorf("feedback: %q: %w", rec.Key, err)
		}
		out = append(out, r)
	}
	return out, nil
}
