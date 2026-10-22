package inbox

import (
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

// scan is Count as it was before it kept counters: every message the reader
// may see, read one by one.
func scan(s *Store, tenantName string, q Query) Counts {
	q.State, q.Kind, q.Kinds, q.Projects, q.Shared, q.Alert, q.Agent, q.Unread = Open, "", nil, nil, false, "", "", false
	f := q.filter()
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.of(tenantName)
	var c Counts
	for _, m := range t.messages {
		if !t.match(m, f) {
			continue
		}
		unread := !t.read[m.ID][q.Reader]
		switch {
		case m.Broadcast:
			if unread {
				c.Updates++
			}
		default:
			c.Open++
			if unread {
				c.Unread++
			}
		}
	}
	return c
}

// world is a randomized inbox: who reads it, what it is about, and what
// happened to it so far.
type world struct {
	t   *testing.T
	rnd *rand.Rand
	st  state.Store
	s   *Store
	now time.Time
	// ids are every id the inbox ever issued, dropped ones included, and
	// issued says which.
	ids    []string
	issued map[string]bool
	// news are the broadcasts among them.
	news []string
	// dropped is how many messages retention dropped.
	dropped int
}

var (
	worldSubjects = []string{"ada", "bo", "cy", "dee", ""}
	worldRoles    = []string{"operator", "admin"}
	worldProjects = []string{"", "Field", "Globex", "Initech"}
	worldKinds    = []Kind{KindAlert, KindFailure, KindApproval, KindMention, KindMemory, KindAnnouncement}
)

func pick[T any](r *rand.Rand, from []T) T { return from[r.IntN(len(from))] }

func some[T any](r *rand.Rand, from []T) []T {
	var out []T
	for _, v := range from {
		if r.IntN(2) == 0 {
			out = append(out, v)
		}
	}
	return out
}

func (w *world) issue(m Message, raised bool, err error) {
	w.t.Helper()
	if err != nil || !raised {
		return
	}
	if w.issued[m.ID] {
		w.t.Fatalf("%s was issued again", m.ID)
	}
	w.issued[m.ID] = true
	w.ids = append(w.ids, m.ID)
	if m.Broadcast {
		w.news = append(w.news, m.ID)
	}
}

// raise raises a message that is not a broadcast, addressed and sourced at
// random, with a part to decide and sometimes a question.
func (w *world) raise() {
	r := w.rnd
	m := Message{
		Kind: pick(r, worldKinds), Subject: "something", From: From{Kind: "rule", Name: "watch"}, Severity: Warning,
		Source: Source{Project: pick(r, worldProjects), Thread: pick(r, []string{"", "t1", "t2"}), Run: pick(r, []string{"", "r1", "r2"}), Agent: pick(r, []string{"", "lead"})},
	}
	switch r.IntN(5) {
	case 0:
		m.To = pick(r, worldSubjects[:4])
	case 1:
		m.Audience = []string{"role:" + pick(r, worldRoles), pick(r, worldSubjects[:4])}
	case 2:
		m.Audience = []string{ProjectAudience(pick(r, worldProjects)), OwnersAudience(pick(r, worldProjects))}
	}
	if r.IntN(4) == 0 {
		m.Excluded = []string{pick(r, worldSubjects[:4])}
	}
	m.Parts = []Part{{Type: PartAlert}}
	switch r.IntN(3) {
	case 0:
		m.Parts = append(m.Parts, Part{Type: PartApproval, Question: &QuestionPart{Run: "r1", ID: "q", Kind: "approval"}})
	case 1:
		m.Parts = append(m.Parts, Part{Type: PartText, State: PartInfo, Text: "For context."})
	}
	w.issue(w.s.Raise(acme, "key-"+strconv.Itoa(r.IntN(12)), m, w.now))
}

// post posts a broadcast in a project, from a person or from warp, once
// in a while under a key it is posted once by.
func (w *world) post() {
	r := w.rnd
	m := announcement(pick(r, worldProjects), "News")
	if r.IntN(3) == 0 {
		m.From = From{Kind: "system", Name: "warp"}
	}
	if r.IntN(5) == 0 {
		w.issue(w.s.PostOnce(acme, "release:0."+strconv.Itoa(r.IntN(4)), m, w.now, PostCaps{PerProject: 1000, PerTenant: 1000}))
		return
	}
	out, err := w.s.Post(acme, m, w.now, PostCaps{PerProject: 1000, PerTenant: 1000})
	w.issue(out, err == nil, err)
}

// step does one thing at random.
func (w *world) step() {
	r := w.rnd
	if len(w.ids) == 0 || r.IntN(8) == 0 {
		w.raise()
		return
	}
	id, who := pick(r, w.ids), pick(r, worldSubjects)
	if len(w.news) > 0 && r.IntN(2) == 0 {
		// Half the time, something about the news, by a few readers.
		id, who = pick(r, w.news), pick(r, worldSubjects[:3])
	}
	at := w.now
	switch r.IntN(14) {
	case 0, 13:
		w.post()
	case 1, 2:
		_, _ = w.s.Read(acme, id, who, r.IntN(4) != 0, at)
	case 3:
		_, _, _ = w.s.Dismiss(acme, id, who, at)
	case 4:
		_, _, _ = w.s.Snooze(acme, id, who, at.Add(time.Duration(1+r.IntN(180))*time.Minute), at)
	case 5:
		_, _, _ = w.s.Unhide(acme, id, who, at)
	case 6:
		_, _, _ = w.s.Resolve(acme, id, pick(r, []string{"p1", "p2"}), Outcome{Action: "acknowledge", By: who, At: at})
	case 7:
		_, _, _ = w.s.Withdraw(acme, id, who, "", at)
	case 8:
		_ = w.s.Deliver(acme, id, "slack-"+strconv.Itoa(r.IntN(2)), Delivery{Channel: "C1", TS: strconv.Itoa(r.IntN(1000)), At: at}, nil)
	case 9:
		n, err := w.s.Retain(acme, at.Add(-time.Duration(r.IntN(240))*time.Minute))
		if err != nil {
			w.t.Fatal(err)
		}
		w.dropped += n
	case 10:
		w.now = w.now.Add(time.Duration(r.IntN(90)) * time.Minute)
	default:
		w.raise()
	}
}

// reader is someone reading at random: their roles, the projects whose news
// reaches them less the ones they muted, the projects they own, the kinds
// they may see, what they may read, and when.
func (w *world) reader() Query {
	r := w.rnd
	who := pick(r, worldSubjects)
	visible := some(r, worldKinds)
	salt := r.Uint64()
	sees := func(m Message) bool {
		h := fnv.New64a()
		_, _ = fmt.Fprint(h, salt, "|", m.Source.Project, "|", m.Source.Thread, "|", m.Source.Run, "|", m.Source.Agent)
		return h.Sum64()%4 != 0
	}
	// The platform's mute: a member's projects, less those they muted (the
	// root is never muted), are the projects whose news reaches them.
	member, muted := some(r, worldProjects), some(r, worldProjects)
	reaches := slices.DeleteFunc(slices.Clone(member), func(p string) bool { return p != "" && slices.Contains(muted, p) })
	q := Query{
		Reader: who, Roles: some(r, worldRoles), ReaderProjects: reaches, ReaderOwns: some(r, worldProjects),
		Visible: func(k Kind) bool { return slices.Contains(visible, k) },
		Now:     pick(r, []time.Time{w.now, w.now.Add(time.Hour), w.now.Add(4 * time.Hour), {}}),
	}
	switch r.IntN(4) {
	case 0:
		q.Visible = nil
	case 1:
		q.Sees = sees
	}
	return q
}

// check holds the counts to a scan for readers at random.
func (w *world) check(s *Store, step int) {
	w.t.Helper()
	for range 4 {
		q := w.reader()
		if got, want := s.Count(acme, q), scan(s, acme, q); got != want {
			w.t.Fatalf("step %d: %s counts %+v, a scan %+v", step, q.Reader, got, want)
		}
	}
}

// The counts a badge and What's new read are kept as changes apply, never
// added up from every message, and they always agree with a scan of every
// message, for every reader and every moment: across raises, reads, hides,
// unhides, snoozes, resolves, withdrawals, broadcasts to projects the reader
// is in and not in, mutes, retention, and restarts.
func TestCountsAgreeWithAScan(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		for seed := range 4 {
			t.Run(fmt.Sprint("seed ", seed), func(t *testing.T) {
				if seed%2 == 1 {
					// Retention in steps of a handful of records.
					was := maxCut
					maxCut = 7
					t.Cleanup(func() { maxCut = was })
				}
				st := open(t)
				s, err := OpenStore(st)
				if err != nil {
					t.Fatal(err)
				}
				w := &world{t: t, rnd: rand.New(rand.NewPCG(uint64(seed), 9)), st: st, s: s, now: t0, issued: map[string]bool{}}
				steps := 300
				for i := range steps {
					w.step()
					w.check(w.s, i)
					if i == steps/2 {
						// A restart halfway, and the inbox carries on.
						if w.s, err = OpenStore(st); err != nil {
							t.Fatal(err)
						}
						w.check(w.s, i)
					}
				}
				if _, err := w.s.Retain(acme, w.now.Add(-30*time.Minute)); err != nil {
					t.Fatal(err)
				}
				if w.dropped == 0 {
					t.Fatal("retention dropped nothing along the way")
				}
				again, err := OpenStore(st)
				if err != nil {
					t.Fatal(err)
				}
				for range 40 {
					q := w.reader()
					live, rebuilt, scanned := w.s.Count(acme, q), again.Count(acme, q), scan(again, acme, q)
					if live != rebuilt || rebuilt != scanned {
						t.Fatalf("%s counts %+v live, %+v rebuilt, a scan %+v", q.Reader, live, rebuilt, scanned)
					}
				}
				sameInbox(t, again, w.s)
				// What the store keeps is the record and the cut, nothing more.
				recs, err := st.Load(t.Context(), "inbox")
				if err != nil {
					t.Fatal(err)
				}
				if changes := len(w.s.Changes(acme)); len(recs) > changes+1 || len(recs) < changes {
					t.Fatalf("the store keeps %d records for %d changes", len(recs), changes)
				}
				// No id is issued twice, the dropped ones' included.
				w.s = again
				for range 20 {
					w.raise()
					w.post()
				}
				// The counters go on agreeing with a scan after a restart.
				for step := range 40 {
					w.step()
					w.check(w.s, step)
				}
			})
		}
	})
}

// sameInbox fails unless two stores hold the same messages, read marks,
// record and counters.
func sameInbox(t *testing.T, got, want *Store) {
	t.Helper()
	gotMsgs, gotRead := got.Snapshot(acme)
	wantMsgs, wantRead := want.Snapshot(acme)
	if !reflect.DeepEqual(gotMsgs, wantMsgs) || !reflect.DeepEqual(gotRead, wantRead) {
		t.Fatalf("messages = %+v %v, want %+v %v", gotMsgs, gotRead, wantMsgs, wantRead)
	}
	if !reflect.DeepEqual(got.Changes(acme), want.Changes(acme)) {
		t.Fatalf("record = %+v, want %+v", got.Changes(acme), want.Changes(acme))
	}
	got.mu.Lock()
	defer got.mu.Unlock()
	want.mu.Lock()
	defer want.mu.Unlock()
	if g, w := tallied(got.of(acme)), tallied(want.of(acme)); !reflect.DeepEqual(g, w) {
		t.Fatalf("counters = %v, want %v", g, w)
	}
}

// tallied is a tenant's counters, as plain values.
func tallied(t *tenant) map[string]any {
	buckets := map[string]string{}
	for sig, b := range t.tally.buckets {
		buckets[sig] = fmt.Sprint(b.open, b.out)
	}
	in := map[string]string{}
	for id, b := range t.tally.in {
		in[id] = b.sig
	}
	snoozed := []string{}
	for id := range t.tally.snoozed {
		snoozed = append(snoozed, id)
	}
	slices.Sort(snoozed)
	asleep := map[string][]string{}
	for sig, g := range t.tally.asleep {
		for id := range g.msgs {
			asleep[sig] = append(asleep[sig], id)
		}
		slices.Sort(asleep[sig])
	}
	return map[string]any{"buckets": buckets, "in": in, "snoozed": snoozed, "asleep": asleep, "snoozedBy": fmt.Sprint(t.tally.snoozedBy)}
}

// A count asks who may see a bucket once for all of it, so what it asks
// about is only what the bucket's messages share.
func TestACountAsksOnceForMessagesAlike(t *testing.T) {
	s := NewStore()
	for i := range 50 {
		raise(s, "alert:"+strconv.Itoa(i), t0)
		if _, err := s.Post(acme, announcement("Field", fmt.Sprint("News ", i)), t0, PostCaps{PerProject: 50, PerTenant: 50}); err != nil {
			t.Fatal(err)
		}
	}
	// Messages snoozed for everyone, whose time came, are asked about once
	// for all of them too.
	for i := range 20 {
		m := raise(s, "alert:snoozed:"+strconv.Itoa(i), t0)
		if _, _, err := s.Snooze(acme, m.ID, "ada", t0.Add(time.Hour), t0); err != nil {
			t.Fatal(err)
		}
	}
	asked := 0
	q := readerIn("ada", "Field")
	q.Now = t0.Add(2 * time.Hour)
	q.Sees = func(Message) bool { asked++; return true }
	if c := s.Count(acme, q); c != (Counts{Open: 70, Unread: 70, Updates: 50}) || asked != 3 {
		t.Fatalf("counts %+v, asking %d times", c, asked)
	}
}
