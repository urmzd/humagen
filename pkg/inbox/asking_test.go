package inbox

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// statements counts every read a store is asked for.
type statements struct {
	state.Store
	lists, lookups atomic.Int64
}

func (c *statements) Get(ctx context.Context, namespace, tenant, key string) ([]byte, error) {
	c.lookups.Add(1)
	return c.Store.Get(ctx, namespace, tenant, key)
}

func (c *statements) Lookup(ctx context.Context, namespace, tenant, key string) (state.Record, error) {
	c.lookups.Add(1)
	return c.Store.Lookup(ctx, namespace, tenant, key)
}

func (c *statements) LookupMany(ctx context.Context, namespace, tenant string, keys []string) ([]state.Record, error) {
	c.lookups.Add(1)
	return state.LookupMany(ctx, c.Store, namespace, tenant, keys)
}

func (c *statements) List(ctx context.Context, namespace, tenant string, q state.Query) ([]state.Record, string, error) {
	c.lists.Add(1)
	return c.Store.List(ctx, namespace, tenant, q)
}

// question raises an open question about a thread, for one person.
func question(t *testing.T, s *Store, key, thread, who string, kind Kind, at time.Time) Message {
	t.Helper()
	part := Part{Type: PartReply, State: PartOpen, Question: &QuestionPart{Run: "r-" + key, ID: "q", Kind: "reply", Prompt: "Which?"}}
	if kind == KindApproval {
		part.Type = PartApproval
	}
	m, _, err := s.Raise(acme, key, Message{
		Kind: kind, Subject: "A question", From: From{Kind: "agent", Name: "lead"}, Audience: []string{who},
		Source: Source{Thread: thread, Run: "r-" + key}, Parts: []Part{part},
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func askingIDs(t *testing.T, s *Store, threads ...string) ([]string, bool) {
	t.Helper()
	msgs, complete, err := s.Asking(acme, threads)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	return ids, complete
}

// Asking is the open questions about some threads, newest first, whoever
// each is for and whatever a reader did with their own copy: a reader's
// hide, snooze or delete changes their inbox, not what a run waits on. A
// question answered, a message settled for everyone, another thread's and a
// message of another kind are not in it. Another tenant's never are.
func TestAskingIsTheOpenQuestionsAboutSomeThreadsWhateverAReaderMarked(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s, err := OpenStore(open(t))
		if err != nil {
			t.Fatal(err)
		}
		one := question(t, s, "one", "s-1", "ada", KindClarification, t0)
		two := question(t, s, "two", "s-2", "bo", KindApproval, t0.Add(time.Second))
		hidden := question(t, s, "hidden", "s-1", "ada", KindClarification, t0.Add(2*time.Second))
		snoozed := question(t, s, "snoozed", "s-1", "ada", KindClarification, t0.Add(3*time.Second))
		deleted := question(t, s, "deleted", "s-1", "ada", KindApproval, t0.Add(4*time.Second))
		everyone := question(t, s, "everyone", "s-1", "ada", KindClarification, t0.Add(5*time.Second))
		answered := question(t, s, "answered", "s-1", "ada", KindClarification, t0.Add(6*time.Second))
		dismissed := question(t, s, "dismissed", "s-1", "ada", KindClarification, t0.Add(7*time.Second))
		question(t, s, "elsewhere", "s-3", "ada", KindClarification, t0.Add(8*time.Second))
		info(s, acme, "news", t0.Add(9*time.Second))
		if _, _, err := s.Raise("globex", "theirs", Message{Kind: KindClarification, Source: Source{Thread: "s-1"}, Parts: []Part{{Type: PartReply, State: PartOpen, Question: &QuestionPart{Run: "r", ID: "q"}}}}, t0); err != nil {
			t.Fatal(err)
		}
		at := t0.Add(time.Minute)
		if _, _, err := s.Hide(acme, hidden.ID, "ada", at); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Snooze(acme, snoozed.ID, "ada", at.Add(time.Hour), at); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Delete(acme, deleted.ID, "ada", at); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Resolve(acme, answered.ID, answered.Parts[0].ID, Outcome{Action: "reply", By: "ada", At: at}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Dismiss(acme, dismissed.ID, "ada", at); err != nil {
			t.Fatal(err)
		}
		got, complete := askingIDs(t, s, "s-1", "s-2")
		want := []string{everyone.ID, deleted.ID, snoozed.ID, hidden.ID, two.ID, one.ID}
		if !complete || !slices.Equal(got, want) {
			t.Fatalf("asking about s-1 and s-2 = %v (complete %v), want %v", got, complete, want)
		}
		if got, _ := askingIDs(t, s, "s-2"); !slices.Equal(got, []string{two.ID}) {
			t.Fatalf("asking about s-2 = %v", got)
		}
		if got, complete := askingIDs(t, s); len(got) != 0 || !complete {
			t.Fatalf("asking about no thread = %v, complete %v", got, complete)
		}
		// Whom each is for is kept, for the caller to decide.
		msgs, _, _ := s.Asking(acme, []string{"s-2"})
		if !msgs[0].For(Reader{Subject: "bo"}) || msgs[0].For(Reader{Subject: "ada"}) {
			t.Fatalf("the message is for %v", msgs[0].Audience)
		}
	})
}

// Asking costs one statement whatever the inbox holds: hundreds of other
// people's alerts and of the reader's own questions cost what an empty inbox
// does, and nothing is looked up for any message. Past MaxAsking live
// messages it still costs one, and says it did not read them all.
func TestAskingIsOneStatementWhateverTheInboxHolds(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		counted := &statements{Store: open(t)}
		s, err := OpenStore(counted)
		if err != nil {
			t.Fatal(err)
		}
		old := question(t, s, "old", "s-1", "ada", KindClarification, t0)
		raise := func(from, n int) {
			for i := from; i < from+n; i++ {
				at := t0.Add(time.Duration(i+1) * time.Second)
				if i%3 == 0 {
					question(t, s, fmt.Sprintf("q-%d", i), fmt.Sprintf("other-%d", i), "ada", KindClarification, at)
					continue
				}
				if _, _, err := s.Raise(acme, fmt.Sprintf("alert-%d", i), Message{Kind: KindAlert, To: "bo", Subject: "Alert", Parts: []Part{{Type: PartAlert, State: PartOpen}}}, at); err != nil {
					t.Fatal(err)
				}
			}
		}
		read := func() ([]string, bool, int64, int64) {
			lists, lookups := counted.lists.Load(), counted.lookups.Load()
			got, complete := askingIDs(t, s, "s-1")
			return got, complete, counted.lists.Load() - lists, counted.lookups.Load() - lookups
		}
		raise(0, 750)
		got, complete, lists, lookups := read()
		if lists != 1 || lookups != 0 {
			t.Fatalf("asking read %d lists and %d lookups of an inbox of 751 live messages, 250 of them the reader's questions; want one list", lists, lookups)
		}
		if !complete || !slices.Equal(got, []string{old.ID}) {
			t.Fatalf("asking = %v, complete %v", got, complete)
		}
		// One more live message than is read: the oldest is not seen, and
		// the read says so.
		raise(750, MaxAsking-750)
		got, complete, lists, lookups = read()
		if lists != 1 || lookups != 0 {
			t.Fatalf("asking read %d lists and %d lookups past its cap; want one list", lists, lookups)
		}
		if complete || len(got) != 0 {
			t.Fatalf("past %d live messages asking = %v, complete %v; want none found and not complete", MaxAsking, got, complete)
		}
	})
}
