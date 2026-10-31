package inbox

import (
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database, each with its own store, see each other's
// changes at once. People answering one question through either at the
// same time decide it once, readers marking one broadcast through either
// are each kept, raises through either are each issued their own id, and
// the counts stay exact throughout.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := OpenStore(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := OpenStore(b)
		if err != nil {
			t.Fatal(err)
		}
		both := []*Store{one, two}
		q := raise(one, "question", t0, Part{Type: PartApproval, Question: &QuestionPart{Run: "r1", ID: "q1", Kind: "approval"}})
		if got, err := two.Get(acme, q.ID, "ada"); err != nil || got.Subject != "question" || got.State != Open {
			t.Fatalf("the other process reads %+v, %v", got, err)
		}
		if c := count(two, acme, Query{Reader: "ada"}); c != (Counts{Open: 1, Unread: 1}) {
			t.Fatalf("the other process counts %+v", c)
		}

		// Eight people answer at once, through either process: one decides.
		const answers = 8
		var decided atomic.Int32
		var wg sync.WaitGroup
		got := make([]Message, answers)
		for i := range answers {
			wg.Go(func() {
				m, changed, err := both[i%2].Resolve(acme, q.ID, "p1", Outcome{Action: "approve", By: "p" + strconv.Itoa(i), At: t0.Add(time.Minute)})
				if err != nil {
					t.Error(err)
				}
				if changed {
					decided.Add(1)
				}
				got[i] = m
			})
		}
		wg.Wait()
		if decided.Load() != 1 {
			t.Fatalf("%d answers decided the question, want 1", decided.Load())
		}
		winner := got[0].Parts[0].Outcome
		for i := range answers {
			if o := got[i].Parts[0].Outcome; got[i].State != Resolved || o == nil || o.By != winner.By {
				t.Fatalf("answer %d sees %s, %+v; want resolved by %s", i, got[i].State, o, winner.By)
			}
		}
		if h := two.History(acme, q.ID); len(h) != 2 {
			t.Fatalf("the record holds %d changes, want the raise and one answer", len(h))
		}

		// Readers mark one broadcast at once, through either process, and
		// raises through either are each issued their own id.
		news := post(t, one, "", "News", t0)
		const readers, raises = 12, 12
		ids := make([]string, raises)
		for i := range readers {
			wg.Go(func() {
				s := both[i%2]
				who := "r" + strconv.Itoa(i)
				if _, err := s.Read(acme, news.ID, who, true, t0); err != nil {
					t.Error(err)
				}
				if i%3 == 0 {
					if _, _, err := s.Dismiss(acme, news.ID, who, t0); err != nil {
						t.Error(err)
					}
				}
			})
		}
		for i := range raises {
			wg.Go(func() {
				m, raised, err := both[i%2].Raise(acme, fmt.Sprintf("alert:%d", i), Message{Kind: KindAlert, Subject: "alert", From: From{Kind: "rule", Name: "watch"}, Parts: []Part{{Type: PartAlert}}}, t0)
				if err != nil || !raised {
					t.Errorf("raise %d = %v, %v", i, raised, err)
				}
				ids[i] = m.ID
			})
		}
		wg.Wait()
		seen := map[string]bool{q.ID: true, news.ID: true}
		for _, id := range ids {
			if seen[id] {
				t.Fatalf("%s was issued twice", id)
			}
			seen[id] = true
		}
		for _, s := range both {
			for i := range readers {
				who := "r" + strconv.Itoa(i)
				m, err := s.GetAs(acme, news.ID, Reader{Subject: who, Projects: []string{""}})
				if err != nil || m.Unread || m.Hidden != (i%3 == 0) {
					t.Fatalf("%s reads the news as %+v, %v", who, m, err)
				}
				c := count(s, acme, Query{Reader: who, ReaderProjects: []string{""}})
				if c != (Counts{Open: raises, Unread: raises}) {
					t.Fatalf("%s counts %+v", who, c)
				}
			}
			if c := count(s, acme, Query{Reader: "nobody", ReaderProjects: []string{""}}); c != (Counts{Open: raises, Unread: raises, Updates: 1}) {
				t.Fatalf("someone who read nothing counts %+v", c)
			}
		}
		sameInbox(t, two, one)
	})
}

// Two processes retaining one tenant at once drop each message once.
func TestTwoProcessesRetainOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		was := maxDrop
		maxDrop = 3
		t.Cleanup(func() { maxDrop = was })
		one, err := OpenStore(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := OpenStore(b)
		if err != nil {
			t.Fatal(err)
		}
		const settled = 20
		for i := range settled {
			m := raise(one, "alert:"+strconv.Itoa(i), t0, Part{Type: PartAlert})
			if _, err := two.Read(acme, m.ID, "bo", true, t0); err != nil {
				t.Fatal(err)
			}
			if _, _, err := two.Resolve(acme, m.ID, "p1", Outcome{Action: "acknowledge", By: "ada", At: t0.Add(time.Minute)}); err != nil {
				t.Fatal(err)
			}
		}
		keep := raise(two, "alert:open", t0, Part{Type: PartAlert})
		var dropped atomic.Int32
		var wg sync.WaitGroup
		for i := range 4 {
			wg.Go(func() {
				n, err := []*Store{one, two}[i%2].Retain(acme, t0.Add(time.Hour))
				if err != nil {
					t.Error(err)
				}
				dropped.Add(int32(n))
			})
		}
		wg.Wait()
		if dropped.Load() != settled {
			t.Fatalf("%d messages dropped, want %d", dropped.Load(), settled)
		}
		msgs, _ := one.Snapshot(acme)
		if len(msgs) != 1 || msgs[0].ID != keep.ID {
			t.Fatalf("after retention the inbox holds %+v", msgs)
		}
		if changes, cleaning := records(t, a); changes != 1 || cleaning {
			t.Fatalf("the store keeps %d changes, cleaning %v", changes, cleaning)
		}
	})
}

// Changes kept one record each by an older release are moved once, though
// two processes open the store at the same time, and what the inbox holds
// is what replaying them held.
func TestTwoProcessesConvertOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		older := NewStore()
		for _, tenantName := range []string{acme, "globex"} {
			for i := range 30 {
				m, _, err := older.Raise(tenantName, "alert:"+strconv.Itoa(i%20), Message{Kind: KindAlert, Subject: "alert", From: From{Kind: "rule", Name: "watch"}, Parts: []Part{{Type: PartAlert}}}, t0.Add(time.Duration(i)*time.Minute))
				if err != nil {
					t.Fatal(err)
				}
				switch i % 4 {
				case 0:
					_, _, err = older.Resolve(tenantName, m.ID, "p1", Outcome{Action: "acknowledge", By: "ada", At: t0.Add(time.Hour)})
				case 1:
					_, err = older.Read(tenantName, m.ID, "bo", true, t0.Add(time.Hour))
				case 2:
					_, _, err = older.Snooze(tenantName, m.ID, "ada", t0.Add(2*time.Hour), t0.Add(time.Hour))
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := older.Post(tenantName, announcement("Field", "News"), t0, PostCaps{}); err != nil {
				t.Fatal(err)
			}
			writeLegacy(t, a, tenantName, older.Changes(tenantName), &cut{Next: 40}, nil)
		}
		var wg sync.WaitGroup
		stores := make([]*Store, 4)
		for i := range stores {
			wg.Go(func() {
				s, err := OpenStore([]state.Store{a, b}[i%2])
				if err != nil {
					t.Error(err)
				}
				stores[i] = s
			})
		}
		wg.Wait()
		if t.Failed() {
			return
		}
		for _, tenantName := range []string{acme, "globex"} {
			wantMsgs, wantRead := older.Snapshot(tenantName)
			for i, s := range stores {
				gotMsgs, gotRead := s.Snapshot(tenantName)
				if !reflect.DeepEqual(gotMsgs, wantMsgs) || !reflect.DeepEqual(gotRead, wantRead) {
					t.Fatalf("store %d holds %d messages %v, want %d %v", i, len(gotMsgs), gotRead, len(wantMsgs), wantRead)
				}
				if got, want := tenantTally(t, s, tenantName), tenantTally(t, older, tenantName); !reflect.DeepEqual(got, want) {
					t.Fatalf("store %d counters = %v, want %v", i, got, want)
				}
			}
			old, err := legacyOf(a, tenantName)
			if err != nil || len(old) != 0 {
				t.Fatalf("%s keeps %d records of the older layout: %v", tenantName, len(old), err)
			}
			// Ids carry on from the cut.
			if m := raiseIn(t, stores[1], tenantName, "alert:next"); m.ID != "m-41" {
				t.Fatalf("%s: the next id is %s", tenantName, m.ID)
			}
		}
		tenants, err := stores[0].Tenants()
		if err != nil || !reflect.DeepEqual(tenants, []string{acme, "globex"}) {
			t.Fatalf("Tenants = %v, %v", tenants, err)
		}
	})
}

func raiseIn(t *testing.T, s *Store, tenantName, key string) Message {
	t.Helper()
	m, _, err := s.Raise(tenantName, key, Message{Kind: KindAlert, Subject: key, From: From{Kind: "rule", Name: "watch"}, Parts: []Part{{Type: PartAlert}}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// tenantTally is a tenant's counters, as plain values.
func tenantTally(t *testing.T, s *Store, tenantName string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, prefix := range []string{"b:", "o:", "z:", "y:", "p:", "k:", "d:", "l:", "e:"} {
		recs, err := listAll(s.st, tenantName, prefix, maxBuckets)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range recs {
			out[r.Key] = string(r.Value)
		}
	}
	return out
}
