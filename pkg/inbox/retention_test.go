package inbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// records is what the store keeps for acme: how many changes, and whether
// any dropped message is still being cleaned up.
func records(t *testing.T, st state.Store) (int, bool) {
	t.Helper()
	recs, err := st.Load(context.Background(), "inbox")
	if err != nil {
		t.Fatal(err)
	}
	changes, cleaning := 0, false
	for _, r := range recs {
		switch {
		case r.Tenant != acme:
		case strings.HasPrefix(r.Key, "g:"):
			cleaning = true
		case strings.HasPrefix(r.Key, "c:"):
			changes++
		}
	}
	return changes, cleaning
}

// writeLegacy writes changes as a release before the layout by message kept
// them: one record each, numbered in the order they were made, with the
// tenant's cut beside them when there is one. edit may change each record
// as it is written.
func writeLegacy(t *testing.T, st state.Store, tenantName string, changes []Change, c *cut, edit func(storedChange) []byte) {
	t.Helper()
	var ops []state.Op
	for i, ch := range changes {
		sc := storedOf(ch)
		raw, err := json.Marshal(sc)
		if err != nil {
			t.Fatal(err)
		}
		if edit != nil {
			raw = edit(sc)
		}
		ops = append(ops, state.Put(tenantName, state.Seq(int64(i)), raw))
	}
	if c != nil {
		raw, err := json.Marshal(storedChange{Cut: c})
		if err != nil {
			t.Fatal(err)
		}
		ops = append(ops, state.Put(tenantName, cutKey, raw))
	}
	if err := st.Write(context.Background(), "inbox", ops...); err != nil {
		t.Fatal(err)
	}
}

func must[T any](t *testing.T) func(T, bool, error) T {
	return func(m T, _ bool, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
}

// Retention drops what was settled for everyone before the cut, and every
// change about it, from the inbox and from the store, and nothing else:
// what is open, snoozed, waiting on a person, hidden by one reader alone or
// settled since stays as it was. A replay rebuilds exactly what is kept, no
// id is issued again, a key posted once is never posted again, and receipts
// and keys still find what is kept.
func TestRetentionDropsOnlyWhatWasSettledBeforeTheCut(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		msg, ok := must[Message](t), func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		hour := func(n float64) time.Time { return t0.Add(time.Duration(n * float64(time.Hour))) }

		resolved := raise(s, "alert:resolved", t0, Part{Type: PartAlert})
		msg(s.Resolve(acme, resolved.ID, "p1", Outcome{Action: "acknowledge", By: "ada", At: hour(1)}))
		dismissed := raise(s, "alert:dismissed", t0, Part{Type: PartAlert})
		_, err = s.Read(acme, dismissed.ID, "bo", true, hour(0.5))
		ok(err)
		msg(s.Dismiss(acme, dismissed.ID, "ada", hour(1)))
		withdrawn := post(t, s, "Field", "Offsite", t0)
		ok(s.Deliver(acme, withdrawn.ID, "slack-acme", Delivery{Channel: "C1", TS: "1.0", At: t0}, nil))
		msg(s.Dismiss(acme, withdrawn.ID, "ada", hour(0.5)))
		msg(s.Withdraw(acme, withdrawn.ID, "dana", "Moved", hour(1)))
		release := msg(s.PostOnce(acme, "release:0.15.0", announcement("", "What's new"), t0, PostCaps{}))
		msg(s.Withdraw(acme, release.ID, "dana", "", hour(1)))
		first := raise(s, "alert:again", t0, Part{Type: PartAlert})
		msg(s.Resolve(acme, first.ID, "p1", Outcome{Action: "acknowledge", By: "ada", At: hour(0.5)}))

		waiting := raise(s, "question", t0, Part{Type: PartApproval, Question: &QuestionPart{Run: "r1", ID: "q1", Kind: "approval"}})
		msg(s.Dismiss(acme, waiting.ID, "ada", hour(1)))
		snoozed := raise(s, "alert:snoozed", t0, Part{Type: PartAlert})
		msg(s.Snooze(acme, snoozed.ID, "ada", hour(5), hour(1)))
		openAlert := raise(s, "alert:open", t0, Part{Type: PartAlert})
		news := post(t, s, "Field", "Renewal signed", t0)
		ok(s.Deliver(acme, news.ID, "slack-acme", Delivery{Channel: "C1", TS: "2.0", At: t0}, nil))
		msg(s.Dismiss(acme, news.ID, "ada", hour(1)))
		late := raise(s, "alert:late", t0, Part{Type: PartAlert})
		msg(s.Resolve(acme, late.ID, "p1", Outcome{Action: "acknowledge", By: "ada", At: hour(3)}))
		second := raise(s, "alert:again", hour(1), Part{Type: PartAlert})
		msg(s.Resolve(acme, second.ID, "p1", Outcome{Action: "acknowledge", By: "ada", At: hour(3)}))
		// The last id issued is one retention drops.
		tail := raise(s, "alert:tail", t0, Part{Type: PartAlert})
		msg(s.Resolve(acme, tail.ID, "p1", Outcome{Action: "acknowledge", By: "ada", At: hour(1)}))

		readers := []Query{readerIn("ada", "Field", ""), readerIn("bo", "Field"), readerIn("cy")}
		before := map[string]Counts{}
		for _, q := range readers {
			before[q.Reader] = count(s, acme, q)
		}

		n, err := s.Retain(acme, hour(2))
		if err != nil || n != 6 {
			t.Fatalf("Retain = %d, %v, want 6 dropped", n, err)
		}
		gone := []Message{resolved, dismissed, withdrawn, release, first, tail}
		kept := []Message{waiting, snoozed, openAlert, news, late, second}
		restarted, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		sameInbox(t, restarted, s)
		for name, store := range map[string]*Store{"live": s, "after a restart": restarted} {
			for _, m := range gone {
				if _, err := store.GetAs(acme, m.ID, Reader{Subject: "ada", Projects: []string{"", "Field"}}); !errors.Is(err, ErrNotFound) {
					t.Errorf("%s: %s %q is still there: %v", name, m.ID, m.Subject, err)
				}
				if h := store.History(acme, m.ID); len(h) != 0 {
					t.Errorf("%s: %s keeps a history: %+v", name, m.ID, h)
				}
			}
			for _, m := range kept {
				if _, err := store.GetAs(acme, m.ID, Reader{Subject: "ada", Projects: []string{"", "Field"}}); err != nil {
					t.Errorf("%s: %s %q was dropped: %v", name, m.ID, m.Subject, err)
				}
			}
			for _, q := range readers {
				if c := count(store, acme, q); c != before[q.Reader] {
					t.Errorf("%s: %s counts %+v, before %+v", name, q.Reader, c, before[q.Reader])
				}
			}
			if m, ok := store.ByKey(acme, "alert:again"); !ok || m.ID != second.ID {
				t.Errorf("%s: a key finds %+v", name, m)
			}
			if _, ok := store.ByKey(acme, "alert:resolved"); ok {
				t.Errorf("%s: a dropped key still finds a message", name)
			}
			if m, ok := store.ByDelivery(acme, "slack-acme", Delivery{Channel: "C1", TS: "2.0"}); !ok || m.ID != news.ID {
				t.Errorf("%s: a kept receipt finds %+v", name, m)
			}
			if _, ok := store.ByDelivery(acme, "slack-acme", Delivery{Channel: "C1", TS: "1.0"}); ok {
				t.Errorf("%s: a dropped receipt still finds a message", name)
			}
			// A key posted once is never posted again, whatever the day.
			if m, posted, err := store.PostOnce(acme, "release:0.15.0", announcement("", "What's new"), hour(48), PostCaps{}); err != nil || posted || m.ID != "" {
				t.Errorf("%s: posting a dropped release again = %+v, %v, %v", name, m, posted, err)
			}
		}
		// What the store keeps is the record of what is kept.
		if changes, cleaning := records(t, st); changes != len(s.Changes(acme)) || cleaning {
			t.Fatalf("the store keeps %d changes, cleaning %v, for a record of %d", changes, cleaning, len(s.Changes(acme)))
		}
		// No id is issued twice, the last dropped one's included.
		if next := raise(restarted, "alert:next", hour(4)); next.ID != "m-13" || tail.ID != "m-12" {
			t.Fatalf("after a restart the next id is %s, after %s", next.ID, tail.ID)
		}
		// Nothing more to drop, and a second pass changes nothing.
		if n, err := restarted.Retain(acme, hour(2)); n != 0 || err != nil {
			t.Fatalf("a second pass = %d, %v", n, err)
		}
		if n, err := restarted.Retain("nobody", hour(2)); n != 0 || err != nil {
			t.Fatalf("an empty inbox = %d, %v", n, err)
		}
	})
}

// failAfter is a store whose writes fail once it has taken n.
type failAfter struct {
	state.Store
	n      int
	maxOps int
}

func (f *failAfter) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	f.maxOps = max(f.maxOps, len(ops))
	if f.n == 0 {
		return statetest.ErrInjected
	}
	f.n--
	return f.Store.Write(ctx, namespace, ops...)
}

// Releases posted before retention existed have no Once field. Their keys
// must still outlive the messages, including another restart after a cut.
func TestRetentionKeepsLegacyPostOnceKeys(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		older := NewStore()
		m := must[Message](t)(older.PostOnce(acme, "release:old", announcement("", "News"), t0, PostCaps{}))
		must[Message](t)(older.Withdraw(acme, m.ID, "ada", "", t0))
		// Write the exact shape the old writer kept: no once field.
		writeLegacy(t, st, acme, older.Changes(acme), nil, func(sc storedChange) []byte {
			if sc.Raised != nil {
				sc.Raised.Once = nil
			}
			raw, err := json.Marshal(sc)
			if err != nil {
				t.Fatal(err)
			}
			return raw
		})
		s, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := s.Retain(acme, t0.Add(time.Hour)); n != 1 || err != nil {
			t.Fatalf("retain = %d, %v", n, err)
		}
		s, err = OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		if _, posted, err := s.PostOnce(acme, "release:old", announcement("", "News"), t0.Add(24*time.Hour), PostCaps{}); posted || err != nil {
			t.Fatalf("legacy key posted again = %v, %v", posted, err)
		}
	})
}

// A single message's history can exceed the write limit. A cut makes it
// absent at once, then clears its records in bounded steps. Restart and a
// failed ordinary write must preserve that unfinished cleanup.
func TestRetentionBoundsALongHistoryAcrossRestarts(t *testing.T) {
	was := maxCut
	maxCut = 5
	t.Cleanup(func() { maxCut = was })
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := &failAfter{Store: open(t), n: -1}
		s, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		m := must[Message](t)(s.PostOnce(acme, "release:long", announcement("", "News"), t0, PostCaps{}))
		for i := range 17 {
			if _, err := s.Read(acme, m.ID, "ada", i%2 == 0, t0); err != nil {
				t.Fatal(err)
			}
		}
		must[Message](t)(s.Withdraw(acme, m.ID, "ada", "", t0))
		keep := raise(s, "keep", t0)
		// The step that drops it is written, and the first cleanup is not.
		st.n = 1
		if n, err := s.Retain(acme, t0.Add(time.Hour)); n != 1 || !errors.Is(err, statetest.ErrInjected) {
			t.Fatalf("interrupted retain = %d, %v", n, err)
		}
		for range 2 {
			if _, ok := s.ByID(acme, m.ID); ok {
				t.Fatal("cut message survived")
			}
			if _, _, err := s.Dismiss(acme, keep.ID, "ada", t0); !errors.Is(err, statetest.ErrInjected) {
				t.Fatalf("failed ordinary write = %v", err)
			}
			var err error
			s, err = OpenStore(st)
			if err != nil {
				t.Fatal(err)
			}
		}
		st.n = -1
		added := raise(s, "added during cleanup", t0)
		s, err = OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		st.maxOps = 0
		if n, err := s.Retain(acme, t0.Add(time.Hour)); n != 0 || err != nil {
			t.Fatalf("remaining cleanup = %d, %v", n, err)
		}
		if st.maxOps > maxCut {
			t.Fatalf("write used %d operations, limit %d", st.maxOps, maxCut)
		}
		if changes, _ := records(t, st); changes != 2 {
			t.Fatalf("kept %d changes, want only the two open messages", changes)
		}
		s, err = OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := s.ByID(acme, added.ID); !ok {
			t.Fatal("cleanup deleted a message raised after the cut")
		}
		if _, posted, err := s.PostOnce(acme, "release:long", announcement("", "News"), t0.Add(24*time.Hour), PostCaps{}); posted || err != nil {
			t.Fatalf("cut key posted again = %v, %v", posted, err)
		}
	})
}

// A workspace whose last message was cut still needs housekeeping until
// the last records are deleted, including after a restart.
func TestRetentionFinishesAnEmptyInbox(t *testing.T) {
	was := maxCut
	maxCut = 5
	t.Cleanup(func() { maxCut = was })
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := &failAfter{Store: open(t), n: -1}
		s, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		m := post(t, s, "Field", "News", t0)
		for i := range 8 {
			if _, err := s.Read(acme, m.ID, "ada", i%2 == 0, t0); err != nil {
				t.Fatal(err)
			}
		}
		must[Message](t)(s.Withdraw(acme, m.ID, "ada", "", t0))
		st.n = 1
		if _, err := s.Retain(acme, t0.Add(time.Hour)); !errors.Is(err, statetest.ErrInjected) {
			t.Fatal(err)
		}
		for range 2 {
			if tenants, err := s.Tenants(); err != nil || !slices.Contains(tenants, acme) {
				t.Fatalf("workspace with unfinished cleanup was forgotten: %v, %v", tenants, err)
			}
			s, err = OpenStore(st)
			if err != nil {
				t.Fatal(err)
			}
		}
		st.n = -1
		if _, err := s.Retain(acme, t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if changes, _ := records(t, st); changes != 0 {
			t.Fatalf("empty inbox kept %d changes", changes)
		}
		if keys, err := listAll(s.st, acme, "k:", 10); err != nil || len(keys) != 0 {
			t.Fatalf("ordinary posts do not need permanent tombstones: %v, %v", keys, err)
		}
	})
}

// Receipt lookups name the last recorded kept message, even when a client
// reused a receipt. Delivery order can differ from message creation order.
func TestRetentionRebuildsKeptReceiptLookups(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		first := post(t, s, "Field", "First", t0)
		second := post(t, s, "Field", "Second", t0)
		gone := post(t, s, "Field", "Gone", t0)
		receipt := Delivery{Channel: "C1", TS: "1.0", At: t0}
		for _, m := range []Message{second, first, gone} {
			if err := s.Deliver(acme, m.ID, "client", receipt, nil); err != nil {
				t.Fatal(err)
			}
		}
		must[Message](t)(s.Withdraw(acme, gone.ID, "ada", "", t0))
		if n, err := s.Retain(acme, t0.Add(time.Hour)); n != 1 || err != nil {
			t.Fatalf("retain = %d, %v", n, err)
		}
		for range 2 {
			if m, ok := s.ByDelivery(acme, "client", receipt); !ok || m.ID != first.ID {
				t.Fatalf("receipt finds %s, %v; want %s", m.ID, ok, first.ID)
			}
			s, err = OpenStore(st)
			if err != nil {
				t.Fatal(err)
			}
		}
	})
}

// Retention writes in steps. A step that cannot be written changes nothing,
// the steps before it stand, and a replay rebuilds what the inbox holds
// after each.
func TestRetentionWritesInStepsAndAFailedStepChangesNothing(t *testing.T) {
	was := maxDrop
	maxDrop = 1
	t.Cleanup(func() { maxDrop = was })
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := &failAfter{Store: open(t), n: -1}
		s, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		var settled []Message
		for i := range 12 {
			m := raise(s, "alert:"+strconv.Itoa(i), t0, Part{Type: PartAlert})
			_, _ = s.Read(acme, m.ID, "bo", true, t0)
			if _, _, err := s.Resolve(acme, m.ID, "p1", Outcome{Action: "acknowledge", By: "ada", At: t0.Add(time.Minute)}); err != nil {
				t.Fatal(err)
			}
			settled = append(settled, m)
		}
		keep := raise(s, "alert:open", t0, Part{Type: PartAlert})

		// One message a step, each dropped and then cleaned up: two are
		// written, and the third drop fails.
		st.n = 4
		n, err := s.Retain(acme, t0.Add(time.Hour))
		if !errors.Is(err, statetest.ErrInjected) || n != 2 {
			t.Fatalf("Retain = %d, %v", n, err)
		}
		if msgs, _ := s.Snapshot(acme); len(msgs) != 11 {
			t.Fatalf("after two steps the inbox holds %d messages", len(msgs))
		}
		reopened, err := OpenStore(st.Store)
		if err != nil {
			t.Fatal(err)
		}
		sameInbox(t, reopened, s)

		st.n = -1
		if n, err := s.Retain(acme, t0.Add(time.Hour)); err != nil || n != 10 {
			t.Fatalf("the rest = %d, %v", n, err)
		}
		if msgs, _ := s.Snapshot(acme); len(msgs) != 1 || msgs[0].ID != keep.ID {
			t.Fatalf("after retention the inbox holds %+v", msgs)
		}
		reopened, err = OpenStore(st.Store)
		if err != nil {
			t.Fatal(err)
		}
		sameInbox(t, reopened, s)
		if changes, _ := records(t, st.Store); changes != 1 {
			t.Fatalf("the store keeps %d changes", changes)
		}
		for _, m := range settled {
			if _, ok := reopened.ByID(acme, m.ID); ok {
				t.Fatalf("%s survived", m.ID)
			}
		}
	})
}

// A release posted before announcements recorded that they were posted once
// is still never posted again once retention drops it: a broadcast
// announcement raised under a key its poster named is one posted once.
func TestARecordedReleaseIsPostedOnceAfterRetention(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s := NewStore()
		release, _, err := s.PostOnce(acme, "release:0.14.0", announcement("", "What's new"), t0, PostCaps{})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Withdraw(acme, release.ID, "dana", "", t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		// Keep the records as an older build kept them, with no mark.
		marked := false
		writeLegacy(t, st, "older", s.Changes(acme), nil, func(sc storedChange) []byte {
			raw, err := json.Marshal(sc)
			if err != nil {
				t.Fatal(err)
			}
			old := bytes.Replace(raw, []byte(`,"once":true`), nil, 1)
			marked = marked || !bytes.Equal(old, raw)
			return old
		})
		if !marked {
			t.Fatal("no record marks the release as posted once")
		}
		older, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := older.Retain("older", t0.Add(2*time.Hour)); n != 1 || err != nil {
			t.Fatalf("Retain = %d, %v", n, err)
		}
		restarted, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		for name, store := range map[string]*Store{"live": older, "after a restart": restarted} {
			if m, posted, err := store.PostOnce("older", "release:0.14.0", announcement("", "What's new"), t0.Add(48*time.Hour), PostCaps{}); err != nil || posted {
				t.Errorf("%s: an older release was posted again: %+v, %v", name, m, err)
			}
		}
		// Post's own keys are never one posted once.
		if _, _, err := restarted.PostOnce(acme, "announce::9", announcement("", "News"), t0.Add(48*time.Hour), PostCaps{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("posting once under an announce: key = %v", err)
		}
	})
}

// A raise kept without its message is skipped on replay, not a panic.
func TestARaiseKeptWithoutItsMessageIsSkipped(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		if err := st.Write(context.Background(), "inbox", state.Put(acme, state.Seq(0), []byte(`{"what":"`+string(Raised)+`"}`))); err != nil {
			t.Fatal(err)
		}
		s, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		if m := raise(s, "alert:after", t0); m.ID != "m-1" {
			t.Fatalf("the next message is %s", m.ID)
		}
	})
}

// Retention finishes however small a step is.
func TestRetentionFinishesInTheSmallestSteps(t *testing.T) {
	was := maxCut
	maxCut = 1
	defer func() { maxCut = was }()
	s := NewStore()
	m := post(t, s, "Field", "Offsite", t0)
	if _, err := s.Read(acme, m.ID, "bo", true, t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Withdraw(acme, m.ID, "dana", "", t0); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		n, _ := s.Retain(acme, t0.Add(time.Hour))
		done <- n
	}()
	select {
	case n := <-done:
		if n != 1 {
			t.Fatalf("Retain dropped %d", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Retain did not finish")
	}
}
