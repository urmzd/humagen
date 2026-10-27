package threads

import (
	"cmp"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/internal/textindex"
	"github.com/urmzd/humagen/pkg/state"
)

// namespace is where every tenant's threads and messages are kept, so a
// message and its thread's record are one state.Write.
const namespace = "threads"

// Key prefixes within the namespace: "t:<thread>" a thread's record and
// "m:<thread>:<seq>" its messages. A thread's id holds no ":".
const (
	keyThread    = "t:"
	keyMessage   = "m:"
	keyPlacement = "p:"
)

func threadKey(id string) string { return keyThread + id }

func messageKey(id string, n int) string { return keyMessage + id + ":" + state.Seq(int64(n)) }

// Notice says something happened in a thread: a message was posted or
// settled, or its record changed. It carries no content: a reader fetches
// the thread, and is shown it only if they may read it.
type Notice struct {
	ID string `json:"id"`
	// Message is the message posted or settled, if one was.
	Message string    `json:"message,omitempty"`
	At      time.Time `json:"at"`
}

type thread struct {
	rec  Thread
	msgs []Message
	// surfaces finds a message by its surface id.
	surfaces map[string]int
}

type tenant struct {
	search  textindex.Index
	threads map[string]*thread
	// inProject counts the threads of each project, "" the root.
	inProject map[string]int
	// surfaces finds a thread by its surface, built from the records.
	surfaces map[string]string
	watchers map[int]chan Notice
	nextW    int
}

func newTenant() *tenant {
	return &tenant{threads: map[string]*thread{}, inProject: map[string]int{}, surfaces: map[string]string{}, watchers: map[int]chan Notice{}}
}

// hold adds a thread to a tenant's records, with its surface found.
func (t *tenant) hold(th *thread) {
	t.threads[th.rec.ID] = th
	t.inProject[th.rec.Project]++
	if th.rec.Surface != nil {
		t.surfaces[th.rec.Surface.key()] = th.rec.ID
	}
}

func (t *tenant) tell(n Notice) {
	for _, ch := range t.watchers {
		// A watcher that is not keeping up misses a notice and never blocks
		// a post. It refetches on the next one.
		select {
		case ch <- n:
		default:
		}
	}
}

// Store keeps every tenant's threads. It is in memory, and built with Open it
// writes every change through a state.Store and reads them back when it is
// built again. It is one process's: it reads the store once and then trusts
// its memory. A write that cannot be kept changes nothing.
type Store struct {
	mu      sync.RWMutex
	tenants map[string]*tenant
	kept    *state.Table[json.RawMessage]
}

// NewStore returns an empty store in memory.
func NewStore() *Store { return &Store{tenants: map[string]*tenant{}} }

// loaded is a message read back, before it joins its thread.
type loaded struct {
	tenant, thread string
	n              int
	m              Message
}

// Open returns a store that keeps its threads in st, holding what st already
// has. A nil st is NewStore. It refuses records that do not make threads: a
// message of a thread that is not there, a gap in a thread's messages, or a
// record kept under another key.
func Open(st state.Store) (*Store, error) {
	s := NewStore()
	s.kept = state.NewTable[json.RawMessage](st, namespace)
	var msgs []loaded
	var moves []loadedPlacement
	err := s.kept.Load(func(tenantName, key string, raw json.RawMessage) error {
		switch {
		case strings.HasPrefix(key, keyThread):
			var rec Thread
			if err := json.Unmarshal(raw, &rec); err != nil {
				return fmt.Errorf("thread %q: %w", key, err)
			}
			if threadKey(rec.ID) != key {
				return fmt.Errorf("%w: the thread %q is kept under %q", ErrInvalid, rec.ID, key)
			}
			if err := rec.validate(); err != nil {
				return fmt.Errorf("thread %q: %w", key, err)
			}
			tn := s.of(tenantName)
			if rec.Surface != nil && tn.surfaces[rec.Surface.key()] != "" {
				return fmt.Errorf("%w: %q and %q are one surface's thread", ErrInvalid, tn.surfaces[rec.Surface.key()], rec.ID)
			}
			tn.hold(&thread{rec: rec, surfaces: map[string]int{}})
		case strings.HasPrefix(key, keyPlacement):
			var change PlacementChange
			if err := json.Unmarshal(raw, &change); err != nil {
				return err
			}
			if key != placementKey(change.Thread, change.Version) || !ValidID(change.Thread) || change.Version < 1 || change.Version > MaxMoves {
				return fmt.Errorf("%w: invalid placement history", ErrInvalid)
			}
			moves = append(moves, loadedPlacement{tenantName, change})
		case strings.HasPrefix(key, keyMessage):
			var m Message
			if err := json.Unmarshal(raw, &m); err != nil {
				return fmt.Errorf("message %q: %w", key, err)
			}
			id, seq, ok := strings.Cut(strings.TrimPrefix(key, keyMessage), ":")
			n, err := strconv.Atoi(seq)
			if !ok || err != nil || n < 1 || messageKey(id, n) != key || m.Thread != id || m.ID != MessageID(n) {
				return fmt.Errorf("%w: the message %q of %q is kept under %q", ErrInvalid, m.ID, m.Thread, key)
			}
			if err := m.validate(); err != nil {
				return fmt.Errorf("message %q: %w", key, err)
			}
			msgs = append(msgs, loaded{tenant: tenantName, thread: id, n: n, m: m})
		default:
			return fmt.Errorf("%w: nothing is kept under %q", ErrInvalid, key)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("threads: load: %w", err)
	}
	histories := map[string]map[int]PlacementChange{}
	for _, move := range moves {
		th := s.of(move.tenant).threads[move.change.Thread]
		if th == nil || move.change.Version > th.rec.Moves {
			return nil, fmt.Errorf("%w: orphan placement history", ErrInvalid)
		}
		key := move.tenant + "\x00" + move.change.Thread
		if histories[key] == nil {
			histories[key] = map[int]PlacementChange{}
		}
		histories[key][move.change.Version] = move.change
	}
	for tenantName, tn := range s.tenants {
		for id, th := range tn.threads {
			history := histories[tenantName+"\x00"+id]
			if th.rec.Moves < 0 || th.rec.Moves > MaxMoves || len(history) != th.rec.Moves {
				return nil, fmt.Errorf("%w: incomplete placement history", ErrInvalid)
			}
			var last PlacementChange
			for n := 1; n <= th.rec.Moves; n++ {
				next, ok := history[n]
				if !ok || n > 1 && (next.FromProject != last.Project || next.FromChannel != last.Channel) {
					return nil, fmt.Errorf("%w: broken placement chain", ErrInvalid)
				}
				last = next
			}
			if th.rec.Moves > 0 && (last.Project != th.rec.Project || last.Channel != th.rec.Channel) {
				return nil, fmt.Errorf("%w: placement does not match history", ErrInvalid)
			}
		}
	}

	// A message's key sorts after its thread's others, so each thread's
	// messages come in order; a gap is a lost message, and is refused.
	for _, l := range msgs {
		th := s.of(l.tenant).threads[l.thread]
		switch {
		case th == nil:
			return nil, fmt.Errorf("threads: load: %w: the message %s belongs to %q, which is missing", ErrInvalid, l.m.ID, l.thread)
		case l.n != len(th.msgs)+1:
			return nil, fmt.Errorf("threads: load: %w: %q has no message %d before %s", ErrInvalid, l.thread, len(th.msgs)+1, l.m.ID)
		case strings.HasPrefix(l.m.Prev, "m-") && th.message(l.m.Prev) < 0:
			return nil, fmt.Errorf("threads: load: %w: %s of %q follows %s, which is not before it", ErrInvalid, l.m.ID, l.thread, l.m.Prev)
		}
		th.add(l.m)
		s.of(l.tenant).search.Put(l.m.Thread+"\x00"+l.m.ID, l.m.Text)
	}
	return s, nil
}

// add appends a message to a thread in memory.
func (th *thread) add(m Message) {
	th.msgs = append(th.msgs, m)
	if m.SurfaceID != "" {
		th.surfaces[m.SurfaceID] = len(th.msgs) - 1
	}
}

// of returns a tenant's records, making them on first use. The caller holds
// the write lock, or is Open.
func (s *Store) of(name string) *tenant {
	t, ok := s.tenants[name]
	if !ok {
		t = newTenant()
		s.tenants[name] = t
	}
	return t
}

// find returns a thread for reading. The caller holds the lock.
func (s *Store) find(tenantName, id string) (*thread, error) {
	if t, ok := s.tenants[tenantName]; ok {
		if th, ok := t.threads[id]; ok {
			return th, nil
		}
	}
	return nil, fmt.Errorf("%w: thread %q", ErrNotFound, id)
}

// writable returns a thread a write may change. Who may change it is the
// platform's to decide.
func (s *Store) writable(tenantName, id string) (*thread, error) {
	th, err := s.find(tenantName, id)
	if err != nil {
		return nil, err
	}
	if th.rec.Archive != nil && th.rec.Archive.Deleted != nil {
		return nil, fmt.Errorf("%w: thread %q", ErrNotFound, id)
	}
	return th, nil
}

// batch collects one write.
type batch struct {
	b   *state.Batch[json.RawMessage]
	err error
}

func (s *Store) batch() *batch { return &batch{b: s.kept.Batch()} }

func (b *batch) put(tenantName, key string, v any) {
	if b.err != nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		b.err = fmt.Errorf("threads: %q: %w", key, err)
		return
	}
	b.b.Put(tenantName, key, raw)
}

func (b *batch) commit() error {
	if b.err != nil {
		return b.err
	}
	if err := b.b.Commit(); err != nil {
		return fmt.Errorf("threads: write: %w", err)
	}
	return nil
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
// (ErrLimit). The caller holds the lock.
func (s *Store) opening(tenantName string, t Thread, at time.Time) (Thread, error) {
	t = t.clone()
	if t.Surface != nil && s.of(tenantName).surfaces[t.Surface.key()] != "" {
		return Thread{}, fmt.Errorf("%w: that surface's thread is %q", ErrExists, s.of(tenantName).surfaces[t.Surface.key()])
	}
	tn := s.of(tenantName)
	have := tn.threads
	if t.ID == "" {
		for t.ID = newID(); have[t.ID] != nil; t.ID = newID() {
		}
	} else if have[t.ID] != nil {
		return Thread{}, fmt.Errorf("%w: thread %q", ErrExists, t.ID)
	}
	if len(have) >= MaxThreads {
		return Thread{}, fmt.Errorf("%w: a workspace holds at most %d threads", ErrLimit, MaxThreads)
	}
	if tn.inProject[t.Project] >= MaxProjectThreads {
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
	if err := t.validate(); err != nil {
		return Thread{}, err
	}
	return t, nil
}

// Create writes a thread's record with no messages: the record of a
// conversation from before threads, the first time anything about it changes,
// or a thread to post into later. A thread with no id is given a new one. The
// person who starts it takes part as its owner. Only the record of a
// conversation from before threads may name nobody: one no person asked in,
// changed first by someone who may not own it.
func (s *Store) Create(tenantName string, t Thread, at time.Time) (Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.opening(tenantName, t, at)
	if err != nil {
		return Thread{}, err
	}
	b := s.batch()
	b.put(tenantName, threadKey(t.ID), t)
	if err := b.commit(); err != nil {
		return Thread{}, err
	}
	tn := s.of(tenantName)
	tn.hold(&thread{rec: t, surfaces: map[string]int{}})
	tn.tell(Notice{ID: t.ID, At: at})
	return t.clone(), nil
}

// Claim makes by the owner of a thread nobody started, the record of a
// conversation from before threads that no person asked in: they start it,
// and take part as its owner. A thread someone started is returned as it is.
func (s *Store) Claim(tenantName, id, by string, at time.Time) (Thread, error) {
	if strings.TrimSpace(by) == "" {
		return Thread{}, fmt.Errorf("%w: a thread is started by someone", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	th, err := s.writable(tenantName, id)
	if err != nil {
		return Thread{}, err
	}
	if th.rec.CreatedBy != "" {
		return th.rec.clone(), nil
	}
	next := th.rec.clone()
	next.CreatedBy = by
	if i := slices.IndexFunc(next.Participants, func(p Participant) bool { return p.Kind == Person && p.Name == by }); i >= 0 {
		next.Participants[i].Owner, next.Participants[i].AddedBy = true, ""
	} else {
		next.Participants = append([]Participant{{Kind: Person, Name: by, Owner: true, At: at}}, next.Participants...)
	}
	if len(next.Participants) > MaxHeld {
		return Thread{}, fmt.Errorf("%w: a thread holds at most %d participants", ErrInvalid, MaxHeld)
	}
	next.Version, next.UpdatedAt = th.rec.Version+1, at
	if err := next.validate(); err != nil {
		return Thread{}, err
	}
	return s.rewrite(tenantName, th, next, at)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err = s.opening(tenantName, t, at)
	if err != nil {
		return Thread{}, Message{}, err
	}
	m.ID, m.Thread = MessageID(1), t.ID
	b := s.batch()
	b.put(tenantName, threadKey(t.ID), t)
	b.put(tenantName, messageKey(t.ID, 1), m)
	if err := b.commit(); err != nil {
		return Thread{}, Message{}, err
	}
	th := &thread{rec: t, surfaces: map[string]int{}}
	th.add(m)
	tn := s.of(tenantName)
	tn.hold(th)
	tn.search.Put(m.Thread+"\x00"+m.ID, m.Text)
	tn.tell(Notice{ID: t.ID, Message: m.ID, At: at})
	return t.clone(), m.clone(), nil
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
// A message with no Prev follows the thread's latest message, chosen under
// the store's lock, so messages posted at once form one line rather than
// branches. A Prev that names a message must name one of this thread; one
// that names a run is the platform's to have checked. A message whose
// SurfaceID the thread already holds is not posted again: the first is
// returned. A thread holds at most MaxMessages (ErrFull). The poster joins
// the thread, while there is room, and the thread's version is unchanged.
func (s *Store) Post(tenantName, id string, m Message, at time.Time) (Message, bool, error) {
	m, err := posting(m, at)
	if err != nil {
		return Message{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	th, err := s.writable(tenantName, id)
	if err != nil {
		return Message{}, false, err
	}
	if th.rec.ReadOnly {
		return Message{}, false, fmt.Errorf("%w: an archive accepts no messages", ErrInvalid)
	}
	if i, ok := th.surfaces[m.SurfaceID]; ok && m.SurfaceID != "" {
		return th.msgs[i].clone(), false, nil
	}
	if len(th.msgs) >= MaxMessages {
		return Message{}, false, fmt.Errorf("%w: it holds %d messages", ErrFull, MaxMessages)
	}
	switch {
	case m.Prev == "" && len(th.msgs) > 0:
		m.Prev = th.msgs[len(th.msgs)-1].ID
	case strings.HasPrefix(m.Prev, "m-") && th.message(m.Prev) < 0:
		return Message{}, false, fmt.Errorf("%w: %q has no message %s", ErrInvalid, id, m.Prev)
	}
	n := len(th.msgs) + 1
	m.ID, m.Thread = MessageID(n), id
	rec := th.rec.clone()
	rec.UpdatedAt = at
	if !rec.has(Person, m.By) && len(rec.Participants) < MaxHeld {
		rec.Participants = append(rec.Participants, Participant{Kind: Person, Name: m.By, At: at})
	}
	b := s.batch()
	b.put(tenantName, messageKey(id, n), m)
	b.put(tenantName, threadKey(id), rec)
	if err := b.commit(); err != nil {
		return Message{}, false, err
	}
	th.rec = rec
	th.add(m)
	s.tenants[tenantName].search.Put(m.Thread+"\x00"+m.ID, m.Text)
	s.tenants[tenantName].tell(Notice{ID: id, Message: m.ID, At: at})
	return m.clone(), true, nil
}

// message returns the place of a message in the thread, or -1.
func (th *thread) message(mid string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(mid, "m-"))
	if err != nil || !strings.HasPrefix(mid, "m-") || n < 1 || n > len(th.msgs) || MessageID(n) != mid {
		return -1
	}
	return n - 1
}

// Settle records what a message's tags came to: the run each agent's tag
// started, and why a tag started or notified nothing. The tags themselves
// cannot change: mentions must name the same kinds and names, in the same
// order, as the message holds.
func (s *Store) Settle(tenantName, id, mid string, mentions []Mention) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	th, err := s.writable(tenantName, id)
	if err != nil {
		return Message{}, err
	}
	i := th.message(mid)
	if i < 0 {
		return Message{}, fmt.Errorf("%w: message %s of %q", ErrNotFound, mid, id)
	}
	m := th.msgs[i].clone()
	if !slices.EqualFunc(m.Mentions, mentions, func(a, b Mention) bool { return a.Kind == b.Kind && a.Name == b.Name }) {
		return Message{}, fmt.Errorf("%w: settling a message keeps its tags", ErrInvalid)
	}
	m.Mentions = cloneMentions(mentions)
	if err := m.validate(); err != nil {
		return Message{}, err
	}
	if slices.Equal(m.Mentions, th.msgs[i].Mentions) {
		return m, nil
	}
	b := s.batch()
	b.put(tenantName, messageKey(id, i+1), m)
	if err := b.commit(); err != nil {
		return Message{}, err
	}
	th.msgs[i] = m
	s.tenants[tenantName].tell(Notice{ID: id, Message: mid, At: m.At})
	return m.clone(), nil
}

// Join adds people or agents who take part by acting in the thread: an agent
// that answered, a person who replied. Those already there are skipped, and
// those that do not fit are left out. The thread's version is unchanged.
func (s *Store) Join(tenantName, id string, ps []Participant, at time.Time) (Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	th, err := s.writable(tenantName, id)
	if err != nil {
		return Thread{}, err
	}
	rec := th.rec.clone()
	for _, p := range ps {
		p.Owner, p.At = false, at
		if err := p.validate(); err != nil {
			return Thread{}, err
		}
		if !rec.has(p.Kind, p.Name) && len(rec.Participants) < MaxHeld {
			rec.Participants = append(rec.Participants, p)
		}
	}
	if len(rec.Participants) == len(th.rec.Participants) {
		return rec, nil
	}
	rec.UpdatedAt = at
	return s.rewrite(tenantName, th, rec, at)
}

// rewrite writes a thread's new record. The caller holds the lock.
func (s *Store) rewrite(tenantName string, th *thread, rec Thread, at time.Time) (Thread, error) {
	b := s.batch()
	if rec.Project != th.rec.Project || rec.Channel != th.rec.Channel {
		if th.rec.Moves >= MaxMoves {
			return Thread{}, fmt.Errorf("%w: conversation placement history is full", ErrLimit)
		}
		rec.Moves = th.rec.Moves + 1
		b.put(tenantName, placementKey(rec.ID, rec.Moves), PlacementChange{By: rec.movedBy, Thread: rec.ID, Version: rec.Moves, FromProject: th.rec.Project, FromChannel: th.rec.Channel, Project: rec.Project, Channel: rec.Channel, At: at})
	}
	rec.movedBy = ""
	b.put(tenantName, threadKey(rec.ID), rec)
	if err := b.commit(); err != nil {
		return Thread{}, err
	}
	th.rec = rec
	s.tenants[tenantName].tell(Notice{ID: rec.ID, At: at})
	return rec.clone(), nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	th, err := s.writable(tenantName, id)
	if err != nil {
		return Thread{}, err
	}
	cur := th.rec
	if version != cur.Version {
		return Thread{}, fmt.Errorf("%w: %q is at version %d, not %d", ErrVersion, id, cur.Version, version)
	}
	next := cur.clone()
	if err := fn(&next); err != nil {
		return Thread{}, err
	}
	moves := next.Project != cur.Project || next.Channel != cur.Channel
	tn := s.of(tenantName)
	switch {
	case next.ReadOnly != cur.ReadOnly:
		return Thread{}, fmt.Errorf("%w: archive status is immutable", ErrInvalid)
	case !sameArchive(next.Archive, cur.Archive):
		return Thread{}, fmt.Errorf("%w: archive and deletion are changed on their own", ErrInvalid)
	case next.Moves != cur.Moves:
		return Thread{}, fmt.Errorf("%w: placement history is server managed", ErrInvalid)
	case next.ID != cur.ID || !sameSurface(next.Surface, cur.Surface) || next.Announcement != cur.Announcement || !sameFork(next.Fork, cur.Fork):
		return Thread{}, fmt.Errorf("%w: a thread stays where it was started", ErrInvalid)
	case moves && (!moving || cur.Visibility != Private || next.Visibility != Private || cur.Surface != nil || cur.Announcement != ""):
		return Thread{}, fmt.Errorf("%w: a thread stays where it was started; a private conversation moves, and Share moves one as it shares it", ErrInvalid)
	case next.Project != cur.Project && tn.inProject[next.Project] >= MaxProjectThreads:
		return Thread{}, fmt.Errorf("%w: a project holds at most %d threads", ErrLimit, MaxProjectThreads)
	case !slices.Equal(next.Participants, cur.Participants):
		return Thread{}, fmt.Errorf("%w: participants are changed on their own", ErrInvalid)
	}
	next.Version, next.CreatedBy, next.CreatedAt, next.UpdatedAt = cur.Version, cur.CreatedBy, cur.CreatedAt, cur.UpdatedAt
	if err := next.validate(); err != nil {
		return Thread{}, err
	}
	if !moves && next.Title == cur.Title && next.Visibility == cur.Visibility && next.Default == cur.Default && slices.Equal(next.Agents, cur.Agents) && next.Mode == cur.Mode {
		return cur.clone(), nil
	}
	next.Version, next.UpdatedAt = cur.Version+1, at
	stored, err := s.rewrite(tenantName, th, next, at)
	if err == nil && next.Project != cur.Project {
		tn.inProject[cur.Project]--
		tn.inProject[next.Project]++
	}
	return stored, err
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
	s.mu.Lock()
	defer s.mu.Unlock()
	th, err := s.writable(tenantName, id)
	if err != nil {
		return Thread{}, err
	}
	if version != th.rec.Version {
		return Thread{}, fmt.Errorf("%w: %q is at version %d, not %d", ErrVersion, id, th.rec.Version, version)
	}
	next := th.rec.clone()
	for _, name := range remove {
		next.Participants = slices.DeleteFunc(next.Participants, func(p Participant) bool {
			if p.Name == name && p.Owner {
				err = fmt.Errorf("%w: the person who started a thread stays in it", ErrInvalid)
			}
			return p.Name == name && !p.Owner
		})
	}
	if err != nil {
		return Thread{}, err
	}
	for _, p := range add {
		p.Owner, p.AddedBy, p.At = false, by, at
		if err := p.validate(); err != nil {
			return Thread{}, err
		}
		if !next.has(p.Kind, p.Name) {
			next.Participants = append(next.Participants, p)
		}
	}
	shownOf := func(ps []Participant) int {
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
	// People who post and agents who answer join past MaxParticipants, so a
	// change is refused only when it grows what the writer counts past it.
	shown := shownOf(next.Participants)
	switch {
	case shown > MaxParticipants && shown > shownOf(th.rec.Participants):
		return Thread{}, fmt.Errorf("%w: a thread has at most %d participants", ErrInvalid, MaxParticipants)
	case len(next.Participants) > MaxHeld:
		return Thread{}, fmt.Errorf("%w: a thread holds at most %d participants", ErrInvalid, MaxHeld)
	}
	if slices.Equal(next.Participants, th.rec.Participants) {
		return next, nil
	}
	next.Version, next.UpdatedAt = th.rec.Version+1, at
	return s.rewrite(tenantName, th, next, at)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	th, err := s.writable(tenantName, id)
	if err != nil {
		return Thread{}, err
	}
	cur := th.rec
	if version != cur.Version {
		return Thread{}, fmt.Errorf("%w: %q is at version %d, not %d", ErrVersion, id, cur.Version, version)
	}
	next := cur.clone()
	moves := project != cur.Project || channel != cur.Channel
	if (moves || cur.Visibility != Shared) && (cur.Surface != nil || cur.Announcement != "") {
		return Thread{}, fmt.Errorf("%w: a thread on a surface stays where and as it was started", ErrInvalid)
	}
	tn := s.of(tenantName)
	if project != cur.Project && tn.inProject[project] >= MaxProjectThreads {
		return Thread{}, fmt.Errorf("%w: a project holds at most %d threads", ErrLimit, MaxProjectThreads)
	}
	next.Project, next.Channel, next.Visibility = project, channel, Shared
	if moves {
		next.Default, next.Agents, next.Mode = def, slices.Clone(agents), mode
	}
	for _, p := range add {
		p.Owner, p.AddedBy, p.At = false, by, at
		if err := p.validate(); err != nil {
			return Thread{}, err
		}
		if !next.has(p.Kind, p.Name) {
			next.Participants = append(next.Participants, p)
		}
	}
	shownOf := func(ps []Participant) int {
		n := 0
		for _, p := range ps {
			if counted == nil || counted(p) {
				n++
			}
		}
		return n
	}
	shown := shownOf(next.Participants)
	switch {
	case shown > MaxParticipants && shown > shownOf(cur.Participants):
		return Thread{}, fmt.Errorf("%w: a thread has at most %d participants", ErrInvalid, MaxParticipants)
	case len(next.Participants) > MaxHeld:
		return Thread{}, fmt.Errorf("%w: a thread holds at most %d participants", ErrInvalid, MaxHeld)
	}
	if err := next.validate(); err != nil {
		return Thread{}, err
	}
	if !moves && next.Visibility == cur.Visibility && slices.Equal(next.Participants, cur.Participants) {
		return cur.clone(), nil
	}
	next.Version, next.UpdatedAt = cur.Version+1, at
	next.movedBy = by
	stored, err := s.rewrite(tenantName, th, next, at)
	if err != nil {
		return Thread{}, err
	}
	if project != cur.Project {
		tn.inProject[cur.Project]--
		tn.inProject[project]++
	}
	return stored, nil
}

func sameSurface(a, b *Surface) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

// BySurface returns the thread a surface's thread is, if one was started.
func (s *Store) BySurface(tenantName string, sf Surface) (Thread, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tn, ok := s.tenants[tenantName]
	if !ok {
		return Thread{}, false
	}
	th := tn.threads[tn.surfaces[sf.key()]]
	if th == nil {
		return Thread{}, false
	}
	return th.rec.clone(), true
}

// Get returns a thread's record.
func (s *Store) Get(tenantName, id string) (Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	th, err := s.find(tenantName, id)
	if err != nil {
		return Thread{}, err
	}
	return th.rec.clone(), nil
}

// Message returns one of a thread's messages.
func (s *Store) Message(tenantName, id, mid string) (Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	th, err := s.find(tenantName, id)
	if err != nil {
		return Message{}, err
	}
	i := th.message(mid)
	if i < 0 {
		return Message{}, fmt.Errorf("%w: message %s of %q", ErrNotFound, mid, id)
	}
	return th.msgs[i].clone(), nil
}

// Last returns a thread's latest message, ErrNotFound when it has none.
func (s *Store) Last(tenantName, id string) (Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	th, err := s.find(tenantName, id)
	if err != nil {
		return Message{}, err
	}
	if len(th.msgs) == 0 {
		return Message{}, fmt.Errorf("%w: %q has no messages", ErrNotFound, id)
	}
	return th.msgs[len(th.msgs)-1].clone(), nil
}

// Messages returns a thread's messages in order, after the message after (""
// from the first): at most limit, MaxPage when zero or more. next is what to
// pass as after for the page that follows, or "" when there is none.
func (s *Store) Messages(tenantName, id, after string, limit int) (msgs []Message, next string, err error) {
	if limit <= 0 || limit > MaxPage {
		limit = MaxPage
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	th, err := s.find(tenantName, id)
	if err != nil {
		return nil, "", err
	}
	from := 0
	if after != "" {
		i := th.message(after)
		if i < 0 {
			return nil, "", fmt.Errorf("%w: %q", ErrCursor, after)
		}
		from = i + 1
	}
	end := min(from+limit, len(th.msgs))
	msgs = make([]Message, 0, end-from)
	for _, m := range th.msgs[from:end] {
		msgs = append(msgs, m.clone())
	}
	if end < len(th.msgs) {
		next = th.msgs[end-1].ID
	}
	return msgs, next, nil
}

// Path returns the messages from the start of a thread down to one, oldest
// first: the one branch it sits on. The first may follow a run, of a
// conversation from before threads.
func (s *Store) Path(tenantName, id, mid string) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	th, err := s.find(tenantName, id)
	if err != nil {
		return nil, err
	}
	var out []Message
	for i := th.message(mid); i >= 0; i = th.message(th.msgs[i].Prev) {
		out = append(out, th.msgs[i].clone())
		// A message only ever follows an earlier one, so this ends.
		if len(out) > len(th.msgs) {
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

func encodeCursor(t Thread) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(t.UpdatedAt.UnixNano(), 10) + ":" + t.ID))
}

func decodeCursor(c string) (int64, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return 0, "", ErrCursor
	}
	stamp, id, ok := strings.Cut(string(raw), ":")
	at, err := strconv.ParseInt(stamp, 10, 64)
	if !ok || id == "" || err != nil {
		return 0, "", ErrCursor
	}
	return at, id, nil
}

// List returns one page of a project's threads, most recently active first,
// with the id breaking ties, and the cursor for the page after it ("" when
// there is nothing older).
func (s *Store) List(tenantName string, q Query) ([]Thread, string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultPage
	}
	limit = min(limit, MaxPage)
	var afterAt int64
	var afterID string
	if q.Before != "" {
		var err error
		if afterAt, afterID, err = decodeCursor(q.Before); err != nil {
			return nil, "", err
		}
	}
	s.mu.RLock()
	var all []Thread
	if t, ok := s.tenants[tenantName]; ok {
		for _, th := range t.threads {
			if th.rec.Project == q.Project && (q.Channel == "" || th.rec.Channel == q.Channel) {
				all = append(all, th.rec.clone())
			}
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(all, func(a, b Thread) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), strings.Compare(a.ID, b.ID))
	})
	page := []Thread{}
	for _, t := range all {
		if q.Before != "" {
			at := t.UpdatedAt.UnixNano()
			if at > afterAt || (at == afterAt && t.ID <= afterID) {
				continue
			}
		}
		if len(page) == limit {
			return page, encodeCursor(page[len(page)-1]), nil
		}
		page = append(page, t)
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

// Summaries returns, in no order, the threads a tenant has a record of that
// keep, called under the lock with each record as it stands, holds; only
// those are copied. Elsewhere is the ids of the rest. It is what a list of
// conversations merges with the run index's: a record says where its
// conversation is, so one kept elsewhere is left out of the list too.
func (s *Store) Summaries(tenantName string, keep func(Thread) bool) (out []Summary, elsewhere map[string]bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out, elsewhere = []Summary{}, map[string]bool{}
	t, ok := s.tenants[tenantName]
	if !ok {
		return out, elsewhere
	}
	for id, th := range t.threads {
		if !keep(th.rec) {
			elsewhere[id] = true
			continue
		}
		sum := Summary{Thread: th.rec.clone(), Messages: len(th.msgs)}
		if len(th.msgs) > 0 {
			sum.First = clipText(th.msgs[0].Text, MaxTitle)
		}
		out = append(out, sum)
	}
	return out, elsewhere
}

// Records returns the records of a tenant's threads that keep, called under
// the lock with each record as it stands, holds, in no order. Only those are
// copied.
func (s *Store) Records(tenantName string, keep func(Thread) bool) []Thread {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Thread{}
	t, ok := s.tenants[tenantName]
	if !ok {
		return out
	}
	for _, th := range t.threads {
		if keep(th.rec) {
			out = append(out, th.rec.clone())
		}
	}
	return out
}

// clipText is the start of a text, at most n characters.
func clipText(text string, n int) string {
	if utf8.RuneCountInString(text) <= n {
		return text
	}
	return string([]rune(text)[:n])
}

// Watch reports what happens in a tenant's threads until cancel is called.
func (s *Store) Watch(tenantName string) (<-chan Notice, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.of(tenantName)
	ch := make(chan Notice, 32)
	id := t.nextW
	t.nextW++
	t.watchers[id] = ch
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(t.watchers, id)
	}
}
