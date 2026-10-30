package rooms

import (
	"context"
	"sync"
	"time"
)

// Kinds of item a room carries.
const (
	// KindRun is a run's event.
	KindRun = "run"
	// KindThread is a notice that a message was posted or settled in the
	// conversation, or that its record changed.
	KindThread = "thread"
)

// Item is one thing that happened in a room, as its Source read it once for
// every viewer.
type Item struct {
	// Kind is KindRun or KindThread.
	Kind string
	// Involves are the subjects of the people it is for: who posted the
	// message or is named in it, who asked for the run, and whom its
	// question is put to. In a large room only they are sent it at once.
	Involves []string
	// Value is what the stream sends: the event or the notice. The hub
	// never reads it.
	Value any
}

// Page is a large room's tail: how much happened in it since the page
// before, and nothing about what. Every viewer of the room is sent the same
// page, so it carries nothing one of them may not read: the viewer fetches
// the conversation's newest turns to see it.
type Page struct {
	// Thread is the conversation.
	Thread string `json:"thread"`
	// Seq orders a room's pages on the server that sent them, from 1. A
	// viewer that missed one is sent the newest, whose counts are only its
	// own.
	Seq uint64 `json:"seq"`
	// Messages is how many notices of messages posted or settled, or of the
	// record changing, the page folds.
	Messages int `json:"messages"`
	// Events is how many run events the page folds.
	Events int `json:"events"`
	// Viewers is how many viewers the room had on this server when the page
	// was made.
	Viewers int `json:"viewers"`
	// From and Through bound when what it folds happened.
	From    time.Time `json:"from"`
	Through time.Time `json:"through"`
}

// Source reads one conversation's events for its room and hands each to
// publish, until ctx is done. It is started when a room gains its first
// viewer and its ctx is cancelled when the room loses its last. An error it
// returns ends every viewer of the room, who reconnect.
type Source func(ctx context.Context, tenant, thread string, publish func(Item)) error

type key struct{ tenant, thread string }

// Hub holds the rooms with viewers on this server.
type Hub struct {
	cfg    Config
	source Source
	now    func() time.Time

	mu     sync.Mutex
	rooms  map[key]*room
	closed bool

	stop chan struct{}
	done chan struct{}
}

// NewHub returns a hub reading each room's events from source. It runs the
// tail until Close.
func NewHub(cfg Config, source Source) *Hub {
	h := &Hub{
		cfg: cfg.Defaults(), source: source, now: time.Now,
		rooms: map[key]*room{}, stop: make(chan struct{}), done: make(chan struct{}),
	}
	go h.tail()
	return h
}

// Config is the hub's configuration, its defaults filled in.
func (h *Hub) Config() Config { return h.cfg }

// Close ends every viewer with ErrClosed and stops the tail.
func (h *Hub) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	rooms := h.rooms
	h.rooms = map[key]*room{}
	h.mu.Unlock()
	close(h.stop)
	<-h.done
	for _, r := range rooms {
		r.cancel()
		r.end(ErrClosed)
	}
	return nil
}

// Join adds a viewer, whose subject is who, to a conversation's room,
// opening the room if it had none. The viewer is the caller's until Leave.
func (h *Hub) Join(tenant, thread, who string) *Viewer {
	v := &Viewer{subject: who, items: make(chan Item, ViewerBuffer), pages: make(chan struct{}, 1), done: make(chan struct{})}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		v.end(ErrClosed)
		return v
	}
	k := key{tenant, thread}
	r, ok := h.rooms[k]
	if !ok {
		ctx, cancel := context.WithCancel(context.Background())
		r = &room{h: h, key: k, cancel: cancel, bySubject: map[string]map[*Viewer]struct{}{}, index: map[*Viewer]int{}}
		h.rooms[k] = r
		go func() {
			err := h.source(ctx, tenant, thread, func(it Item) { r.publish(it) })
			if ctx.Err() == nil {
				// The source ended on its own: the room cannot be
				// followed, so its viewers reconnect to a new one.
				h.drop(r)
				if err == nil {
					err = ErrClosed
				}
				r.end(err)
			}
		}()
	}
	v.room = r
	r.add(v)
	h.mu.Unlock()
	return v
}

// Publish hands an item to a conversation's room, if it has one here. A
// Source calls it through the function it is given; tests and benchmarks
// call it directly.
func (h *Hub) Publish(tenant, thread string, it Item) {
	h.mu.Lock()
	r := h.rooms[key{tenant, thread}]
	h.mu.Unlock()
	if r != nil {
		r.publish(it)
	}
}

// Viewers is how many viewers a conversation's room has on this server.
func (h *Hub) Viewers(tenant, thread string) int {
	h.mu.Lock()
	r := h.rooms[key{tenant, thread}]
	h.mu.Unlock()
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.viewers)
}

func (h *Hub) drop(r *room) {
	h.mu.Lock()
	if h.rooms[r.key] == r {
		delete(h.rooms, r.key)
	}
	h.mu.Unlock()
}

// leave removes v from its room, and closes the room when it was the last.
func (h *Hub) leave(v *Viewer) {
	r := v.room
	if r == nil {
		return
	}
	h.mu.Lock()
	empty := r.remove(v)
	if empty && h.rooms[r.key] == r {
		delete(h.rooms, r.key)
	}
	h.mu.Unlock()
	if empty {
		r.cancel()
	}
}

// tail sends each large room's page every TailInterval, when anything was
// folded into it.
func (h *Hub) tail() {
	defer close(h.done)
	tick := time.NewTicker(h.cfg.TailInterval)
	defer tick.Stop()
	for {
		select {
		case <-h.stop:
			return
		case <-tick.C:
		}
		h.mu.Lock()
		rooms := make([]*room, 0, len(h.rooms))
		for _, r := range h.rooms {
			rooms = append(rooms, r)
		}
		h.mu.Unlock()
		for _, r := range rooms {
			r.flush(h.now())
		}
	}
}

type room struct {
	h      *Hub
	key    key
	cancel context.CancelFunc

	mu sync.Mutex
	// viewers is a slice so a page is sent with one pass and no map walk;
	// index places each viewer in it.
	viewers   []*Viewer
	index     map[*Viewer]int
	bySubject map[string]map[*Viewer]struct{}
	// pending is the page being folded, and latest the one last sent.
	pending Page
	dirty   bool
	latest  Page
}

func (r *room) add(v *Viewer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.index[v] = len(r.viewers)
	r.viewers = append(r.viewers, v)
	set := r.bySubject[v.subject]
	if set == nil {
		set = map[*Viewer]struct{}{}
		r.bySubject[v.subject] = set
	}
	set[v] = struct{}{}
}

// remove reports whether the room is empty after it.
func (r *room) remove(v *Viewer) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, ok := r.index[v]
	if ok {
		last := len(r.viewers) - 1
		r.viewers[i] = r.viewers[last]
		r.index[r.viewers[i]] = i
		r.viewers[last] = nil
		r.viewers = r.viewers[:last]
		delete(r.index, v)
		if set := r.bySubject[v.subject]; set != nil {
			delete(set, v)
			if len(set) == 0 {
				delete(r.bySubject, v.subject)
			}
		}
	}
	return len(r.viewers) == 0
}

func (r *room) large() bool { return len(r.viewers) > r.h.cfg.CoalesceAbove }

func (r *room) publish(it Item) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.large() {
		for _, v := range r.viewers {
			v.send(it)
		}
		return
	}
	// The people it involves are sent it at once, each once however many
	// of their names it lists.
	for i, who := range it.Involves {
		if who == "" || indexOf(it.Involves[:i], who) {
			continue
		}
		for v := range r.bySubject[who] {
			v.send(it)
		}
	}
	now := r.h.now()
	if !r.dirty {
		r.pending = Page{Thread: r.key.thread, From: now}
		r.dirty = true
	}
	r.pending.Through = now
	if it.Kind == KindThread {
		r.pending.Messages++
	} else {
		r.pending.Events++
	}
}

func indexOf(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// flush makes the folded page the latest and tells every viewer, without
// waiting on any of them: a viewer already told reads the newest page.
func (r *room) flush(now time.Time) {
	r.mu.Lock()
	if !r.dirty {
		r.mu.Unlock()
		return
	}
	page := r.pending
	page.Seq = r.latest.Seq + 1
	page.Viewers = len(r.viewers)
	if page.Through.IsZero() {
		page.Through = now
	}
	r.latest, r.pending, r.dirty = page, Page{}, false
	viewers := append([]*Viewer(nil), r.viewers...)
	r.mu.Unlock()
	for _, v := range viewers {
		select {
		case v.pages <- struct{}{}:
		default:
		}
	}
}

func (r *room) page() Page {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latest
}

func (r *room) end(err error) {
	r.mu.Lock()
	viewers := append([]*Viewer(nil), r.viewers...)
	r.mu.Unlock()
	for _, v := range viewers {
		v.end(err)
	}
}

// Viewer is one stream's place in a room.
type Viewer struct {
	room    *room
	subject string
	items   chan Item
	pages   chan struct{}
	seen    uint64

	once sync.Once
	done chan struct{}
	err  error
}

// Items are what the viewer is sent at once: everything in a small room,
// and in a large one what involves them.
func (v *Viewer) Items() <-chan Item { return v.items }

// Pages says a newer page may be ready; Page reads it.
func (v *Viewer) Pages() <-chan struct{} { return v.pages }

// Page returns the room's newest page, and false when the viewer was
// already sent it.
func (v *Viewer) Page() (Page, bool) {
	if v.room == nil {
		return Page{}, false
	}
	p := v.room.page()
	if p.Seq == 0 || p.Seq <= v.seen {
		return Page{}, false
	}
	v.seen = p.Seq
	return p, true
}

// Done is closed when the viewer is ended; Err says why.
func (v *Viewer) Done() <-chan struct{} { return v.done }

// Err is why the viewer was ended: ErrSlow, ErrClosed, or its source's.
func (v *Viewer) Err() error {
	select {
	case <-v.done:
		return v.err
	default:
		return nil
	}
}

// Leave takes the viewer out of its room. It is safe to call more than once.
func (v *Viewer) Leave() {
	if v.room != nil {
		v.room.h.leave(v)
	}
	v.end(ErrClosed)
}

// send hands v an item without waiting: a viewer that is ViewerBuffer
// behind is ended rather than slow the room.
func (v *Viewer) send(it Item) {
	select {
	case <-v.done:
		return
	default:
	}
	select {
	case v.items <- it:
	default:
		v.end(ErrSlow)
	}
}

func (v *Viewer) end(err error) {
	v.once.Do(func() {
		v.err = err
		close(v.done)
	})
}
