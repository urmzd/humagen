package threads

import (
	"cmp"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/state"

	"github.com/urmzd/humagen/internal/legacy"
)

// namespace is where every tenant's threads and messages are kept, so a
// message and its thread's record are one state.Write.
const namespace = "threads"

// Keys within the namespace, all in the thread's tenant. A thread's id holds
// no ":".
//
//   - "t:<thread>" a thread's record, which is also the head of its messages:
//     how many were posted (head), so a post numbers its message from it.
//   - "m:<thread>:<seq>" its messages, appended and never moved.
//   - "p:<thread>:<seq>" where it was moved from and to.
//   - "u:<inverted updated nanos>:<thread>" the activity index: newest
//     first, moved in the write that moves the record's UpdatedAt.
//   - "s:<client>:<channel>:<ts>" the thread a surface's thread is.
//   - "x:<thread>:<surface id>" the message a surface's id was posted as.
//   - "w:<word>:<thread>" that a message of the thread says the word.
//   - "c:counts" how many threads the tenant and each project hold.
const (
	keyThread    = "t:"
	keyMessage   = "m:"
	keyPlacement = "p:"
	keyActivity  = "u:"
	keySurface   = "s:"
	keyPosted    = "x:"
	keyWord      = "w:"
	keyCounts    = "c:counts"
)

// converted marks, in the tenant "", that the records kept before the
// layout above have their head, their indexes and their counts.
const converted = "converted:v2"

// layout is the layout of a thread's record that holds its head.
const layout = 2

func threadKey(id string) string { return keyThread + id }

func messageKey(id string, n int) string { return keyMessage + id + ":" + state.Seq(int64(n)) }

func messagePrefix(id string) string { return keyMessage + id + ":" }

func surfaceKey(sf Surface) string {
	return keySurface + sf.Client + ":" + sf.Channel + ":" + sf.TS
}

func postedKey(id, surfaceID string) string { return keyPosted + id + ":" + surfaceID }

// stamp is a time as nanoseconds, within what an int64 holds.
func stamp(at time.Time) int64 {
	switch {
	case at.Before(time.Unix(0, 0)):
		return 0
	case at.After(time.Unix(0, math.MaxInt64)):
		return math.MaxInt64
	}
	return at.UnixNano()
}

// activityKey sorts the most recently active first, and the id breaks ties.
func activityKey(nanos int64, id string) string {
	return keyActivity + state.Seq(math.MaxInt64-max(nanos, 0)) + ":" + id
}

// Notice says something happened in a thread: a message was posted or
// settled, or its record changed. It carries no content: a reader fetches
// the thread, and is shown it only if they may read it.
type Notice struct {
	ID string `json:"id"`
	// Message is the message posted or settled, if one was.
	Message string    `json:"message,omitempty"`
	At      time.Time `json:"at"`
}

// head is a thread's record as it is kept: the record, with how many
// messages were posted to it and the start of the first.
type head struct {
	Thread
	Layout int `json:"layout,omitempty"`
	// Head is how many messages were posted, and so the number of the
	// latest. A purge sets it to none.
	Head int `json:"head,omitempty"`
	// First is the start of the first message, at most MaxTitle characters.
	First string `json:"first,omitempty"`
}

// counts is how many threads a tenant holds records of, and each project,
// "" the root.
type counts struct {
	Total    int            `json:"total"`
	Projects map[string]int `json:"projects"`
}

// activity is an entry of the activity index: where the thread is, so a list
// of one project's threads reads no other record.
type activity struct {
	Project string `json:"project"`
	Channel string `json:"channel"`
}

// Store keeps every tenant's threads. It reads and writes per request: each
// read is one record or one bounded page of a tenant's keys, and each change
// is written at the version it read, read again and retried when another
// process changed the same record first. It holds no record in memory, so
// any number of processes on one database agree.
//
// What it holds in memory is who is watching: a notice reaches the watchers
// of the process that made the change.
type Store struct {
	kept *state.Table[json.RawMessage]

	mu       sync.Mutex
	watchers map[string]map[int]chan Notice
	nextW    int
}

// NewStore returns an empty store in memory.
func NewStore() *Store {
	s, err := Open(nil)
	if err != nil {
		// An empty memory store has nothing to convert and cannot fail.
		panic(err)
	}
	return s
}

// Open returns a store that keeps its threads in st. A nil st keeps them in
// memory. The first time any process opens st, the records an earlier
// release kept are given their head, their indexes and their counts; one that
// is not a thread's is refused (ErrInvalid).
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	s := &Store{kept: state.NewTable[json.RawMessage](st, namespace), watchers: map[string]map[int]chan Notice{}}
	if err := s.convert(st); err != nil {
		return nil, fmt.Errorf("threads: load: %w", err)
	}
	return s, nil
}

func (s *Store) tell(tenant string, n Notice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.watchers[tenant] {
		// A watcher that is not keeping up misses a notice and never blocks
		// a post. It refetches on the next one.
		select {
		case ch <- n:
		default:
		}
	}
}

// Watch reports what happens in a tenant's threads, through this process,
// until cancel is called.
func (s *Store) Watch(tenantName string) (<-chan Notice, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watchers[tenantName] == nil {
		s.watchers[tenantName] = map[int]chan Notice{}
	}
	ch := make(chan Notice, 32)
	id := s.nextW
	s.nextW++
	s.watchers[tenantName][id] = ch
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.watchers[tenantName], id)
		if len(s.watchers[tenantName]) == 0 {
			delete(s.watchers, tenantName)
		}
	}
}

// writeErr wraps a failed write, keeping state.ErrConflict matchable so a
// change is retried.
func writeErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("threads: write: %w", err)
}

func readErr(err error) error { return fmt.Errorf("threads: read: %w", err) }

// decodeHead reads a thread's record, as an earlier release may have written
// it.
func decodeHead(raw []byte) (head, error) {
	var h head
	if err := json.Unmarshal(raw, &h); err != nil {
		return head{}, err
	}
	h.Default = legacy.Default(h.Default)
	if h.Mode == modeBestFit {
		h.Mode = ModeJoin
	}
	return h, nil
}

// lookup reads a thread's record and its version. A thread that is not
// there is ErrNotFound.
func (s *Store) lookup(tenantName, id string) (head, int64, error) {
	if !ValidID(id) {
		return head{}, 0, fmt.Errorf("%w: thread %q", ErrNotFound, id)
	}
	it, err := s.kept.Lookup(tenantName, threadKey(id))
	if errors.Is(err, state.ErrNotFound) {
		return head{}, 0, fmt.Errorf("%w: thread %q", ErrNotFound, id)
	}
	if err != nil {
		return head{}, 0, readErr(err)
	}
	h, err := decodeHead(it.Value)
	if err != nil {
		return head{}, 0, readErr(fmt.Errorf("%q: %w", it.Key, err))
	}
	return h, it.Version, nil
}

// writable reads a thread a write may change. Who may change it is the
// platform's to decide.
func (s *Store) writable(tenantName, id string) (head, int64, error) {
	h, v, err := s.lookup(tenantName, id)
	if err != nil {
		return head{}, 0, err
	}
	if h.Archive != nil && h.Archive.Deleted != nil {
		return head{}, 0, fmt.Errorf("%w: thread %q", ErrNotFound, id)
	}
	return h, v, nil
}

// readCounts reads a tenant's counts and their version, 0 when there are
// none yet.
func (s *Store) readCounts(tenantName string) (counts, int64, error) {
	it, err := s.kept.Lookup(tenantName, keyCounts)
	if errors.Is(err, state.ErrNotFound) {
		return counts{Projects: map[string]int{}}, 0, nil
	}
	if err != nil {
		return counts{}, 0, readErr(err)
	}
	var c counts
	if err := json.Unmarshal(it.Value, &c); err != nil {
		return counts{}, 0, readErr(err)
	}
	if c.Projects == nil {
		c.Projects = map[string]int{}
	}
	return c, it.Version, nil
}

// message reads one of a thread's messages and its version.
func (s *Store) message(tenantName, id string, n int) (Message, int64, error) {
	it, err := s.kept.Lookup(tenantName, messageKey(id, n))
	if errors.Is(err, state.ErrNotFound) {
		return Message{}, 0, fmt.Errorf("%w: message %s of %q", ErrNotFound, MessageID(n), id)
	}
	if err != nil {
		return Message{}, 0, readErr(err)
	}
	var m Message
	if err := json.Unmarshal(it.Value, &m); err != nil {
		return Message{}, 0, readErr(fmt.Errorf("%q: %w", it.Key, err))
	}
	return m, it.Version, nil
}

// seqOf is the number of a message id that is at most head, or 0.
func seqOf(mid string, head int) int {
	n, err := strconv.Atoi(strings.TrimPrefix(mid, "m-"))
	if err != nil || !strings.HasPrefix(mid, "m-") || n < 1 || n > head || MessageID(n) != mid {
		return 0
	}
	return n
}

// write collects one state.Write. The thread's record goes first, so every
// write that touches a thread's other keys waits on its record.
type write struct {
	ops []func(b *state.Batch[json.RawMessage])
	err error
}

func (s *Store) write() *write { return &write{} }

func (w *write) raw(key string, v any) json.RawMessage {
	if w.err != nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		w.err = fmt.Errorf("threads: %q: %w", key, err)
		return nil
	}
	return raw
}

// put writes v whatever is there.
func (w *write) put(tenantName, key string, v any) {
	if raw := w.raw(key, v); raw != nil {
		w.ops = append(w.ops, func(b *state.Batch[json.RawMessage]) { b.Put(tenantName, key, raw) })
	}
}

// at writes v at version, or creates it with version 0.
func (w *write) at(tenantName, key string, v any, version int64) {
	raw := w.raw(key, v)
	switch {
	case raw == nil:
	case version == 0:
		w.ops = append(w.ops, func(b *state.Batch[json.RawMessage]) { b.Create(tenantName, key, raw) })
	default:
		w.ops = append(w.ops, func(b *state.Batch[json.RawMessage]) { b.PutAt(tenantName, key, raw, version) })
	}
}

func (w *write) del(tenantName, key string) {
	w.ops = append(w.ops, func(b *state.Batch[json.RawMessage]) { b.Delete(tenantName, key) })
}

// then adds another write's changes after this one's.
func (w *write) then(other *write) {
	if w.err == nil {
		w.err = other.err
	}
	w.ops = append(w.ops, other.ops...)
}

func (w *write) commit(s *Store) error {
	if w.err != nil {
		return w.err
	}
	b := s.kept.Batch()
	for _, op := range w.ops {
		op(b)
	}
	return writeErr(b.Commit())
}

// moved writes the activity entry of a record that moved from was to now.
func (w *write) moved(tenantName string, was *head, now head) {
	if was != nil && stamp(was.UpdatedAt) == stamp(now.UpdatedAt) && was.Project == now.Project && was.Channel == now.Channel {
		return
	}
	if was != nil && stamp(was.UpdatedAt) != stamp(now.UpdatedAt) {
		w.del(tenantName, activityKey(stamp(was.UpdatedAt), now.ID))
	}
	w.put(tenantName, activityKey(stamp(now.UpdatedAt), now.ID), activity{Project: now.Project, Channel: now.Channel})
}

// said writes that a thread's message says each of its words.
func (w *write) said(tenantName, id, text string) {
	for _, word := range words(text) {
		w.put(tenantName, wordKey(word, id), struct{}{})
	}
}

// settled returns v as a read would: through what is kept.
func settled[T any](v T) T {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out T
	if json.Unmarshal(raw, &out) != nil {
		return v
	}
	return out
}

// newID makes a new thread's id.
func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "th-" + hex.EncodeToString(b[:])
}

// opening checks a new thread's record and fills in what the store sets: its
// id, version and times, and the person who starts it as its owner. A
// workspace holds at most MaxThreads, and a project MaxProjectThreads
// (ErrLimit).
func opening(t Thread, c counts, at time.Time) (Thread, error) {
	t = t.clone()
	if t.ID == "" {
		t.ID = newID()
	}
	if c.Total >= MaxThreads {
		return Thread{}, fmt.Errorf("%w: a workspace holds at most %d threads", ErrLimit, MaxThreads)
	}
	if c.Projects[t.Project] >= MaxProjectThreads {
		return Thread{}, fmt.Errorf("%w: a project holds at most %d threads", ErrLimit, MaxProjectThreads)
	}
	for i := range t.Participants {
		p := &t.Participants[i]
		p.Owner = false
		p.AddedBy = cmp.Or(p.AddedBy, t.CreatedBy)
		if p.At.IsZero() {
			p.At = at
		}
	}
	if t.CreatedBy != "" && !t.has(Person, t.CreatedBy) {
		t.Participants = append([]Participant{{Kind: Person, Name: t.CreatedBy, At: at}}, t.Participants...)
	}
	for i := range t.Participants {
		if p := &t.Participants[i]; p.Kind == Person && p.Name == t.CreatedBy {
			p.Owner, p.AddedBy = true, ""
		}
	}
	t.Version, t.CreatedAt, t.UpdatedAt = 1, at, at
	t.Default = legacy.Default(t.Default)
	if err := t.validate(); err != nil {
		return Thread{}, err
	}
	return t, nil
}

// open writes a new thread, and its first message when m is not nil, in one
// write with its counts, its surface and its activity.
func (s *Store) open(tenantName string, in Thread, m *Message, at time.Time) (Thread, Message, error) {
	var t Thread
	var first Message
	err := state.Retry(func() error {
		c, cv, err := s.readCounts(tenantName)
		if err != nil {
			return err
		}
		if in.Surface != nil {
			if it, err := s.kept.Lookup(tenantName, surfaceKey(*in.Surface)); err == nil {
				var id string
				_ = json.Unmarshal(it.Value, &id)
				return fmt.Errorf("%w: that surface's thread is %q", ErrExists, id)
			} else if !errors.Is(err, state.ErrNotFound) {
				return readErr(err)
			}
		}
		if t, err = opening(in, c, at); err != nil {
			return err
		}
		if in.ID != "" {
			if _, _, err := s.lookup(tenantName, t.ID); err == nil {
				return fmt.Errorf("%w: thread %q", ErrExists, t.ID)
			} else if !errors.Is(err, ErrNotFound) {
				return err
			}
		}
		h := head{Thread: t, Layout: layout}
		if m != nil {
			first = m.clone()
			first.ID, first.Thread = MessageID(1), t.ID
			h.Head, h.First = 1, clipText(first.Text, MaxTitle)
		}
		c.Total++
		c.Projects[t.Project]++
		w := s.write()
		w.at(tenantName, keyCounts, c, cv)
		w.at(tenantName, threadKey(t.ID), h, 0)
		w.moved(tenantName, nil, h)
		if t.Surface != nil {
			w.at(tenantName, surfaceKey(*t.Surface), t.ID, 0)
		}
		if m != nil {
			w.at(tenantName, messageKey(t.ID, 1), first, 0)
			if first.SurfaceID != "" {
				w.put(tenantName, postedKey(t.ID, first.SurfaceID), 1)
			}
			w.said(tenantName, t.ID, first.Text)
		}
		return w.commit(s)
	})
	if err != nil {
		return Thread{}, Message{}, err
	}
	n := Notice{ID: t.ID, At: at}
	if m != nil {
		n.Message = first.ID
	}
	s.tell(tenantName, n)
	return settled(t), settled(first), nil
}

// Create writes a thread's record with no messages: the record of a
// conversation from before threads, the first time anything about it changes,
// or a thread to post into later. A thread with no id is given a new one. The
// person who starts it takes part as its owner. Only the record of a
// conversation from before threads may name nobody: one no person asked in,
// changed first by someone who may not own it.
func (s *Store) Create(tenantName string, t Thread, at time.Time) (Thread, error) {
	th, _, err := s.open(tenantName, t, nil, at)
	return th, err
}

// edit reads a thread a write may change, lets fn change its record, and
// writes it at the version it read, with the activity entry it moves to and
// the writes fn adds, which go after the record. fn returning
// state.ErrUnchanged writes nothing and returns the record as it is. When
// another writer changed the thread first, it reads it again and calls fn
// again, so fn keeps nothing from a call that did not write.
func (s *Store) edit(tenantName, id string, fn func(h *head, w *write) error) (head, error) {
	var out head
	err := state.Retry(func() error {
		cur, v, err := s.writable(tenantName, id)
		if err != nil {
			return err
		}
		next := cur
		next.Thread = cur.clone()
		extra := s.write()
		if err := fn(&next, extra); errors.Is(err, state.ErrUnchanged) {
			out = cur
			return nil
		} else if err != nil {
			return err
		}
		next.movedBy = ""
		w := s.write()
		w.at(tenantName, threadKey(id), next, v)
		w.moved(tenantName, &cur, next)
		w.then(extra)
		if err := w.commit(s); err != nil {
			return err
		}
		out = settled(next)
		return nil
	})
	return out, err
}

// place records a move of a thread's record from cur to next, when it moves:
// one more entry of its history, and the counts of the projects it leaves
// and joins, refusing a project that holds MaxProjectThreads (ErrLimit).
func (s *Store) place(tenantName string, cur Thread, next *head, w *write, at time.Time) error {
	if next.Project == cur.Project && next.Channel == cur.Channel {
		return nil
	}
	if cur.Moves >= MaxMoves {
		return fmt.Errorf("%w: conversation placement history is full", ErrLimit)
	}
	if next.Project != cur.Project {
		c, cv, err := s.readCounts(tenantName)
		if err != nil {
			return err
		}
		if c.Projects[next.Project] >= MaxProjectThreads {
			return fmt.Errorf("%w: a project holds at most %d threads", ErrLimit, MaxProjectThreads)
		}
		c.Projects[cur.Project]--
		if c.Projects[cur.Project] <= 0 {
			delete(c.Projects, cur.Project)
		}
		c.Projects[next.Project]++
		w.at(tenantName, keyCounts, c, cv)
	}
	next.Moves = cur.Moves + 1
	w.at(tenantName, placementKey(next.ID, next.Moves), PlacementChange{By: next.movedBy, Thread: next.ID, Version: next.Moves, FromProject: cur.Project, FromChannel: cur.Channel, Project: next.Project, Channel: next.Channel, At: at}, 0)
	return nil
}

// Claim makes by the owner of a thread nobody started, the record of a
// conversation from before threads that no person asked in: they start it,
// and take part as its owner. A thread someone started is returned as it is.
func (s *Store) Claim(tenantName, id, by string, at time.Time) (Thread, error) {
	if strings.TrimSpace(by) == "" {
		return Thread{}, fmt.Errorf("%w: a thread is started by someone", ErrInvalid)
	}
	changed := false
	h, err := s.edit(tenantName, id, func(next *head, _ *write) error {
		if next.CreatedBy != "" {
			changed = false
			return state.ErrUnchanged
		}
		next.CreatedBy, changed = by, true
		if i := slices.IndexFunc(next.Participants, func(p Participant) bool { return p.Kind == Person && p.Name == by }); i >= 0 {
			next.Participants[i].Owner, next.Participants[i].AddedBy = true, ""
		} else {
			next.Participants = append([]Participant{{Kind: Person, Name: by, Owner: true, At: at}}, next.Participants...)
		}
		if len(next.Participants) > MaxHeld {
			return fmt.Errorf("%w: a thread holds at most %d participants", ErrInvalid, MaxHeld)
		}
		next.Version, next.UpdatedAt = next.Version+1, at
		next.Default = legacy.Default(next.Default)
		return next.validate()
	})
	if err != nil {
		return Thread{}, err
	}
	if changed {
		s.tell(tenantName, Notice{ID: id, At: at})
	}
	return h.Thread, nil
}

// Start writes a new thread and its first message in one write. The person
// who posts the message starts the thread.
func (s *Store) Start(tenantName string, t Thread, m Message, at time.Time) (Thread, Message, error) {
	if t.ReadOnly {
		return Thread{}, Message{}, fmt.Errorf("%w: an archive accepts no messages", ErrInvalid)
	}
	if t.CreatedBy == "" {
		t.CreatedBy = m.By
	}
	if t.CreatedBy != m.By {
		return Thread{}, Message{}, fmt.Errorf("%w: the person who starts a thread posts its first message", ErrInvalid)
	}
	m, err := posting(m, at)
	if err != nil {
		return Thread{}, Message{}, err
	}
	if m.Prev != "" && !validRun.MatchString(m.Prev) {
		return Thread{}, Message{}, fmt.Errorf("%w: a new thread's first message follows no message", ErrInvalid)
	}
	return s.open(tenantName, t, &m, at)
}

// posting checks a message as it is posted and fills in what is left out.
func posting(m Message, at time.Time) (Message, error) {
	m = m.clone()
	m.Via = cmp.Or(m.Via, ViaConsole)
	m.At = at
	if err := m.validate(); err != nil {
		return Message{}, err
	}
	return m, nil
}

// Post adds a person's message to a thread, and reports whether it is new.
//
// A message with no Prev follows the thread's latest message, read from the
// thread's record in the write that appends it, so messages posted at once,
// through any process, form one line rather than branches, each with its own
// number. A Prev that names a message must name one of this thread; one that
// names a run is the platform's to have checked. A message whose SurfaceID
// the thread already holds is not posted again: the first is returned. A
// thread holds at most MaxMessages (ErrFull). The poster joins the thread,
// while there is room, and the thread's version is unchanged.
func (s *Store) Post(tenantName, id string, m Message, at time.Time) (Message, bool, error) {
	m, err := posting(m, at)
	if err != nil {
		return Message{}, false, err
	}
	var out Message
	created := false
	err = state.Retry(func() error {
		cur, v, err := s.writable(tenantName, id)
		if err != nil {
			return err
		}
		if cur.ReadOnly {
			return fmt.Errorf("%w: an archive accepts no messages", ErrInvalid)
		}
		if m.SurfaceID != "" {
			if it, err := s.kept.Lookup(tenantName, postedKey(id, m.SurfaceID)); err == nil {
				var n int
				if err := json.Unmarshal(it.Value, &n); err != nil {
					return readErr(err)
				}
				out, _, err = s.message(tenantName, id, n)
				created = false
				return err
			} else if !errors.Is(err, state.ErrNotFound) {
				return readErr(err)
			}
		}
		if cur.Head >= MaxMessages {
			return fmt.Errorf("%w: it holds %d messages", ErrFull, MaxMessages)
		}
		next := m.clone()
		switch {
		case next.Prev == "" && cur.Head > 0:
			next.Prev = MessageID(cur.Head)
		case strings.HasPrefix(next.Prev, "m-") && seqOf(next.Prev, cur.Head) == 0:
			return fmt.Errorf("%w: %q has no message %s", ErrInvalid, id, next.Prev)
		}
		n := cur.Head + 1
		next.ID, next.Thread = MessageID(n), id
		rec := cur
		rec.Thread = cur.clone()
		rec.UpdatedAt, rec.Head = at, n
		if n == 1 {
			rec.First = clipText(next.Text, MaxTitle)
		}
		if !rec.has(Person, next.By) && len(rec.Participants) < MaxHeld {
			rec.Participants = append(rec.Participants, Participant{Kind: Person, Name: next.By, At: at})
		}
		w := s.write()
		w.at(tenantName, threadKey(id), rec, v)
		w.moved(tenantName, &cur, rec)
		w.at(tenantName, messageKey(id, n), next, 0)
		if next.SurfaceID != "" {
			w.at(tenantName, postedKey(id, next.SurfaceID), n, 0)
		}
		w.said(tenantName, id, next.Text)
		if err := w.commit(s); err != nil {
			return err
		}
		out, created = next, true
		return nil
	})
	if err != nil {
		return Message{}, false, err
	}
	if created {
		s.tell(tenantName, Notice{ID: id, Message: out.ID, At: at})
	}
	return settled(out), created, nil
}

// Settle records what a message's tags came to: the run each agent's tag
// started, and why a tag started or notified nothing. The tags themselves
// cannot change: mentions must name the same kinds and names, in the same
// order, as the message holds.
func (s *Store) Settle(tenantName, id, mid string, mentions []Mention) (Message, error) {
	var out Message
	changed := false
	err := state.Retry(func() error {
		cur, _, err := s.writable(tenantName, id)
		if err != nil {
			return err
		}
		n := seqOf(mid, cur.Head)
		if n == 0 {
			return fmt.Errorf("%w: message %s of %q", ErrNotFound, mid, id)
		}
		was, v, err := s.message(tenantName, id, n)
		if err != nil {
			return err
		}
		m := was.clone()
		if !slices.EqualFunc(m.Mentions, mentions, func(a, b Mention) bool { return a.Kind == b.Kind && a.Name == b.Name }) {
			return fmt.Errorf("%w: settling a message keeps its tags", ErrInvalid)
		}
		m.Mentions = cloneMentions(mentions)
		if err := m.validate(); err != nil {
			return err
		}
		if slices.Equal(m.Mentions, was.Mentions) {
			out, changed = m, false
			return nil
		}
		w := s.write()
		w.at(tenantName, messageKey(id, n), m, v)
		if err := w.commit(s); err != nil {
			return err
		}
		out, changed = m, true
		return nil
	})
	if err != nil {
		return Message{}, err
	}
	if changed {
		s.tell(tenantName, Notice{ID: id, Message: mid, At: out.At})
	}
	return settled(out), nil
}

// Join adds people or agents who take part by acting in the thread: an agent
// that answered, a person who replied. Those already there are skipped, and
// those that do not fit are left out. The thread's version is unchanged.
func (s *Store) Join(tenantName, id string, ps []Participant, at time.Time) (Thread, error) {
	for _, p := range ps {
		p.Owner, p.At = false, at
		if err := p.validate(); err != nil {
			return Thread{}, err
		}
	}
	changed := false
	h, err := s.edit(tenantName, id, func(next *head, _ *write) error {
		before := len(next.Participants)
		for _, p := range ps {
			p.Owner, p.At = false, at
			if !next.has(p.Kind, p.Name) && len(next.Participants) < MaxHeld {
				next.Participants = append(next.Participants, p)
			}
		}
		if len(next.Participants) == before {
			changed = false
			return state.ErrUnchanged
		}
		next.UpdatedAt, changed = at, true
		return nil
	})
	if err != nil {
		return Thread{}, err
	}
	if changed {
		s.tell(tenantName, Notice{ID: id, At: at})
	}
	return h.Thread, nil
}

// Update changes a thread's title, visibility, default or routing. fn edits a copy;
// the id, place, participants and record fields cannot change, and version
// must be the current one (ErrVersion). A change that changes nothing writes
// nothing.
func (s *Store) Update(tenantName, id string, version int, at time.Time, fn func(*Thread) error) (Thread, error) {
	return s.update(tenantName, id, version, at, fn, false)
}

// UpdateMoving is Update that may also move a private thread to another
// project or channel: a shared thread, and one on a surface or started from
// an announcement, stays where it is (ErrInvalid), and a project it moves to
// that holds MaxProjectThreads already refuses it (ErrLimit).
func (s *Store) UpdateMoving(tenantName, id string, version int, at time.Time, fn func(*Thread) error) (Thread, error) {
	return s.update(tenantName, id, version, at, fn, true)
}

func (s *Store) update(tenantName, id string, version int, at time.Time, fn func(*Thread) error, moving bool) (Thread, error) {
	changed := false
	h, err := s.edit(tenantName, id, func(next *head, w *write) error {
		cur := next.clone()
		if version != cur.Version {
			return fmt.Errorf("%w: %q is at version %d, not %d", ErrVersion, id, cur.Version, version)
		}
		if err := fn(&next.Thread); err != nil {
			return err
		}
		moves := next.Project != cur.Project || next.Channel != cur.Channel
		switch {
		case next.ReadOnly != cur.ReadOnly:
			return fmt.Errorf("%w: archive status is immutable", ErrInvalid)
		case !sameArchive(next.Archive, cur.Archive):
			return fmt.Errorf("%w: archive and deletion are changed on their own", ErrInvalid)
		case next.Moves != cur.Moves:
			return fmt.Errorf("%w: placement history is server managed", ErrInvalid)
		case next.ID != cur.ID || !sameSurface(next.Surface, cur.Surface) || next.Announcement != cur.Announcement || !sameFork(next.Fork, cur.Fork):
			return fmt.Errorf("%w: a thread stays where it was started", ErrInvalid)
		case moves && (!moving || cur.Visibility != Private || next.Visibility != Private || cur.Surface != nil || cur.Announcement != ""):
			return fmt.Errorf("%w: a thread stays where it was started; a private conversation moves, and Share moves one as it shares it", ErrInvalid)
		case !slices.Equal(next.Participants, cur.Participants):
			return fmt.Errorf("%w: participants are changed on their own", ErrInvalid)
		}
		next.Version, next.CreatedBy, next.CreatedAt, next.UpdatedAt = cur.Version, cur.CreatedBy, cur.CreatedAt, cur.UpdatedAt
		next.Default = legacy.Default(next.Default)
		if err := next.validate(); err != nil {
			return err
		}
		if !moves && next.Title == cur.Title && next.Visibility == cur.Visibility && next.Default == cur.Default && slices.Equal(next.Agents, cur.Agents) && next.Mode == cur.Mode {
			changed = false
			return state.ErrUnchanged
		}
		if err := s.place(tenantName, cur, next, w, at); err != nil {
			return err
		}
		next.Version, next.UpdatedAt, changed = cur.Version+1, at, true
		return nil
	})
	if err != nil {
		return Thread{}, err
	}
	if changed {
		s.tell(tenantName, Notice{ID: id, At: at})
	}
	return h.Thread, nil
}

// Participants adds and removes people and agents. add and remove hold at
// most MaxParticipants between them, and a change that grows the ones
// counted reports true for (every one, when it is nil) leaves at most
// MaxParticipants of them,
// and at most MaxHeld in all; version must be the current one (ErrVersion).
// counted is who the writer is shown, so one they were not shown counts
// toward no limit they are told of. The person who started the thread stays
// in it. Adding someone already there, or removing someone who is not,
// changes nothing.
func (s *Store) Participants(tenantName, id string, version int, add []Participant, remove []string, counted func(Participant) bool, by string, at time.Time) (Thread, error) {
	if len(add)+len(remove) > MaxParticipants {
		return Thread{}, fmt.Errorf("%w: at most %d people and agents are added or removed at once", ErrInvalid, MaxParticipants)
	}
	changed := false
	h, err := s.edit(tenantName, id, func(next *head, _ *write) error {
		before := slices.Clone(next.Participants)
		if version != next.Version {
			return fmt.Errorf("%w: %q is at version %d, not %d", ErrVersion, id, next.Version, version)
		}
		var err error
		for _, name := range remove {
			next.Participants = slices.DeleteFunc(next.Participants, func(p Participant) bool {
				if p.Name == name && p.Owner {
					err = fmt.Errorf("%w: the person who started a thread stays in it", ErrInvalid)
				}
				return p.Name == name && !p.Owner
			})
		}
		if err != nil {
			return err
		}
		for _, p := range add {
			p.Owner, p.AddedBy, p.At = false, by, at
			if err := p.validate(); err != nil {
				return err
			}
			if !next.has(p.Kind, p.Name) {
				next.Participants = append(next.Participants, p)
			}
		}
		// People who post and agents who answer join past MaxParticipants, so a
		// change is refused only when it grows what the writer counts past it.
		shown := shownOf(next.Participants, counted)
		switch {
		case shown > MaxParticipants && shown > shownOf(before, counted):
			return fmt.Errorf("%w: a thread has at most %d participants", ErrInvalid, MaxParticipants)
		case len(next.Participants) > MaxHeld:
			return fmt.Errorf("%w: a thread holds at most %d participants", ErrInvalid, MaxHeld)
		}
		if slices.Equal(next.Participants, before) {
			changed = false
			return state.ErrUnchanged
		}
		next.Version, next.UpdatedAt, changed = next.Version+1, at, true
		return nil
	})
	if err != nil {
		return Thread{}, err
	}
	if changed {
		s.tell(tenantName, Notice{ID: id, At: at})
	}
	return h.Thread, nil
}

// shownOf counts the participants counted reports true for, every one when
// it is nil.
func shownOf(ps []Participant, counted func(Participant) bool) int {
	if counted == nil {
		return len(ps)
	}
	n := 0
	for _, p := range ps {
		if counted(p) {
			n++
		}
	}
	return n
}

// Share makes a thread shared in a project's channel and adds people to
// it, as one change: the thread moves to that place when it is another, with
// def as its default and agents and mode as its selection, and a project it moves to that holds MaxProjectThreads
// already refuses it (ErrLimit). A thread on a surface, or started from an
// announcement, stays where and as it was started (ErrInvalid). add holds at
// most MaxParticipants, and grows the ones counted reports true for to at
// most MaxParticipants, as Participants does. version must be the current one
// (ErrVersion). A share that changes nothing writes nothing.
func (s *Store) Share(tenantName, id string, version int, project, channel, def string, agents []string, mode string, add []Participant, counted func(Participant) bool, by string, at time.Time) (Thread, error) {
	if len(add) > MaxParticipants {
		return Thread{}, fmt.Errorf("%w: at most %d people are added at once", ErrInvalid, MaxParticipants)
	}
	changed := false
	h, err := s.edit(tenantName, id, func(next *head, w *write) error {
		cur := next.clone()
		if version != cur.Version {
			return fmt.Errorf("%w: %q is at version %d, not %d", ErrVersion, id, cur.Version, version)
		}
		moves := project != cur.Project || channel != cur.Channel
		if (moves || cur.Visibility != Shared) && (cur.Surface != nil || cur.Announcement != "") {
			return fmt.Errorf("%w: a thread on a surface stays where and as it was started", ErrInvalid)
		}
		next.Project, next.Channel, next.Visibility = project, channel, Shared
		if moves {
			next.Default, next.Agents, next.Mode = def, slices.Clone(agents), mode
		}
		for _, p := range add {
			p.Owner, p.AddedBy, p.At = false, by, at
			if err := p.validate(); err != nil {
				return err
			}
			if !next.has(p.Kind, p.Name) {
				next.Participants = append(next.Participants, p)
			}
		}
		shown := shownOf(next.Participants, counted)
		switch {
		case shown > MaxParticipants && shown > shownOf(cur.Participants, counted):
			return fmt.Errorf("%w: a thread has at most %d participants", ErrInvalid, MaxParticipants)
		case len(next.Participants) > MaxHeld:
			return fmt.Errorf("%w: a thread holds at most %d participants", ErrInvalid, MaxHeld)
		}
		next.Default = legacy.Default(next.Default)
		if err := next.validate(); err != nil {
			return err
		}
		if !moves && next.Visibility == cur.Visibility && slices.Equal(next.Participants, cur.Participants) {
			changed = false
			return state.ErrUnchanged
		}
		next.movedBy = by
		if err := s.place(tenantName, cur, next, w, at); err != nil {
			return err
		}
		next.Version, next.UpdatedAt, changed = cur.Version+1, at, true
		return nil
	})
	if err != nil {
		return Thread{}, err
	}
	if changed {
		s.tell(tenantName, Notice{ID: id, At: at})
	}
	return h.Thread, nil
}

func sameSurface(a, b *Surface) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

// BySurface returns the thread a surface's thread is, and false when none
// was started.
func (s *Store) BySurface(tenantName string, sf Surface) (Thread, bool, error) {
	it, err := s.kept.Lookup(tenantName, surfaceKey(sf))
	if errors.Is(err, state.ErrNotFound) {
		return Thread{}, false, nil
	}
	if err != nil {
		return Thread{}, false, readErr(err)
	}
	var id string
	if err := json.Unmarshal(it.Value, &id); err != nil {
		return Thread{}, false, readErr(err)
	}
	h, _, err := s.lookup(tenantName, id)
	if errors.Is(err, ErrNotFound) {
		return Thread{}, false, nil
	}
	if err != nil {
		return Thread{}, false, err
	}
	return h.Thread, true, nil
}

// Get returns a thread's record.
func (s *Store) Get(tenantName, id string) (Thread, error) {
	h, _, err := s.lookup(tenantName, id)
	if err != nil {
		return Thread{}, err
	}
	return h.Thread, nil
}

// Message returns one of a thread's messages.
func (s *Store) Message(tenantName, id, mid string) (Message, error) {
	h, _, err := s.lookup(tenantName, id)
	if err != nil {
		return Message{}, err
	}
	n := seqOf(mid, h.Head)
	if n == 0 {
		return Message{}, fmt.Errorf("%w: message %s of %q", ErrNotFound, mid, id)
	}
	m, _, err := s.message(tenantName, id, n)
	return m, err
}

// Last returns a thread's latest message, ErrNotFound when it has none.
func (s *Store) Last(tenantName, id string) (Message, error) {
	h, _, err := s.lookup(tenantName, id)
	if err != nil {
		return Message{}, err
	}
	if h.Head == 0 {
		return Message{}, fmt.Errorf("%w: %q has no messages", ErrNotFound, id)
	}
	m, _, err := s.message(tenantName, id, h.Head)
	return m, err
}

// page reads one page of a thread's messages after the nth, at most limit.
func (s *Store) page(tenantName, id string, after, limit int) ([]Message, bool, error) {
	q := state.Query{Prefix: messagePrefix(id), Limit: limit}
	if after > 0 {
		q.After = messageKey(id, after)
	}
	items, next, err := s.kept.List(tenantName, q)
	if err != nil {
		return nil, false, readErr(err)
	}
	out := make([]Message, 0, len(items))
	for _, it := range items {
		var m Message
		if err := json.Unmarshal(it.Value, &m); err != nil {
			return nil, false, readErr(fmt.Errorf("%q: %w", it.Key, err))
		}
		out = append(out, m)
	}
	return out, next != "", nil
}

// Messages returns a thread's messages in order, after the message after (""
// from the first): at most limit, MaxPage when zero or more. next is what to
// pass as after for the page that follows, or "" when there is none.
func (s *Store) Messages(tenantName, id, after string, limit int) (msgs []Message, next string, err error) {
	if limit <= 0 || limit > MaxPage {
		limit = MaxPage
	}
	h, _, err := s.lookup(tenantName, id)
	if err != nil {
		return nil, "", err
	}
	from := 0
	if after != "" {
		if from = seqOf(after, h.Head); from == 0 {
			return nil, "", fmt.Errorf("%w: %q", ErrCursor, after)
		}
	}
	msgs, more, err := s.page(tenantName, id, from, limit)
	if err != nil {
		return nil, "", err
	}
	if more && len(msgs) > 0 {
		next = msgs[len(msgs)-1].ID
	}
	return msgs, next, nil
}

// Path returns the messages from the start of a thread down to one, oldest
// first: the one branch it sits on. The first may follow a run, of a
// conversation from before threads. It reads the thread's messages up to
// that one, at most MaxMessages.
func (s *Store) Path(tenantName, id, mid string) ([]Message, error) {
	h, _, err := s.lookup(tenantName, id)
	if err != nil {
		return nil, err
	}
	end := seqOf(mid, h.Head)
	if end == 0 {
		return nil, fmt.Errorf("%w: message %s of %q", ErrNotFound, mid, id)
	}
	msgs := make([]Message, 0, end)
	for len(msgs) < end {
		page, more, err := s.page(tenantName, id, len(msgs), min(end-len(msgs), state.MaxList))
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, page...)
		if !more || len(page) == 0 {
			break
		}
	}
	var out []Message
	for i := end - 1; i >= 0 && i < len(msgs); i = seqOf(msgs[i].Prev, len(msgs)) - 1 {
		out = append(out, msgs[i])
		// A message only ever follows an earlier one, so this ends.
		if len(out) > len(msgs) {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: message %s of %q", ErrNotFound, mid, id)
	}
	slices.Reverse(out)
	return out, nil
}

// Query selects and pages a project's threads.
type Query struct {
	// Project is the project, "" the root.
	Project string
	// Channel keeps one of its channels. "" keeps them all.
	Channel string
	// Limit is the page size: DefaultPage when zero, at most MaxPage.
	Limit int
	// Before is the cursor a previous page returned. "" starts at the top.
	Before string
}

func encodeCursor(nanos int64, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(nanos, 10) + ":" + id))
}

func decodeCursor(c string) (int64, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return 0, "", ErrCursor
	}
	stamp, id, ok := strings.Cut(string(raw), ":")
	at, err := strconv.ParseInt(stamp, 10, 64)
	if !ok || id == "" || err != nil || strings.Contains(id, ":") {
		return 0, "", ErrCursor
	}
	return at, id, nil
}

// fromActivity reads the time and the thread of an activity entry's key.
func fromActivity(key string) (int64, string, bool) {
	inv, id, ok := strings.Cut(strings.TrimPrefix(key, keyActivity), ":")
	n, err := strconv.ParseInt(inv, 10, 64)
	if !ok || err != nil {
		return 0, "", false
	}
	return math.MaxInt64 - n, id, true
}

// List returns one page of a project's threads, most recently active first,
// with the id breaking ties, and the cursor for the page after it ("" when
// there is nothing older). It pages the tenant's activity index, which holds
// one entry a thread, so it reads at most MaxThreads entries.
func (s *Store) List(tenantName string, q Query) ([]Thread, string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultPage
	}
	limit = min(limit, MaxPage)
	after := ""
	if q.Before != "" {
		at, id, err := decodeCursor(q.Before)
		if err != nil {
			return nil, "", err
		}
		after = activityKey(at, id)
	}
	page := []Thread{}
	lastAt, lastID := int64(0), ""
	for read := 0; read < MaxThreads; {
		items, next, err := s.kept.List(tenantName, state.Query{Prefix: keyActivity, After: after, Limit: state.MaxList})
		if err != nil {
			return nil, "", readErr(err)
		}
		read += len(items)
		for _, it := range items {
			var a activity
			if err := json.Unmarshal(it.Value, &a); err != nil {
				return nil, "", readErr(err)
			}
			at, id, ok := fromActivity(it.Key)
			if !ok || a.Project != q.Project || (q.Channel != "" && a.Channel != q.Channel) {
				continue
			}
			if len(page) == limit {
				return page, encodeCursor(lastAt, lastID), nil
			}
			h, _, err := s.lookup(tenantName, id)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, "", err
			}
			page = append(page, h.Thread)
			lastAt, lastID = at, id
		}
		if next == "" {
			break
		}
		after = next
	}
	return page, "", nil
}

// Summary is a thread's record with what a list shows of its messages.
type Summary struct {
	Thread
	// Messages is how many messages it holds.
	Messages int `json:"messages"`
	// First is the start of what its first message says, at most MaxTitle
	// characters, "" when it has none.
	First string `json:"first,omitempty"`
}

// heads reads every record of a tenant's threads, which hold at most
// MaxThreads.
func (s *Store) heads(tenantName string, fn func(h head)) error {
	items, err := s.kept.All(tenantName, keyThread, MaxThreads)
	if err != nil {
		return readErr(err)
	}
	for _, it := range items {
		h, err := decodeHead(it.Value)
		if err != nil {
			return readErr(fmt.Errorf("%q: %w", it.Key, err))
		}
		fn(h)
	}
	return nil
}

// Summaries returns, in no order, the threads a tenant has a record of that
// keep holds. Elsewhere is the ids of the rest. It is what a list of
// conversations merges with the run index's: a record says where its
// conversation is, so one kept elsewhere is left out of the list too.
func (s *Store) Summaries(tenantName string, keep func(Thread) bool) (out []Summary, elsewhere map[string]bool, err error) {
	out, elsewhere = []Summary{}, map[string]bool{}
	err = s.heads(tenantName, func(h head) {
		if !keep(h.Thread) {
			elsewhere[h.ID] = true
			return
		}
		out = append(out, Summary{Thread: h.Thread, Messages: h.Head, First: h.First})
	})
	if err != nil {
		return nil, nil, err
	}
	return out, elsewhere, nil
}

// Records returns the records of a tenant's threads that keep holds, in no
// order.
func (s *Store) Records(tenantName string, keep func(Thread) bool) ([]Thread, error) {
	out := []Thread{}
	err := s.heads(tenantName, func(h head) {
		if keep(h.Thread) {
			out = append(out, h.Thread)
		}
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// clipText is the start of a text, at most n characters.
func clipText(text string, n int) string {
	if utf8.RuneCountInString(text) <= n {
		return text
	}
	return string([]rune(text)[:n])
}
