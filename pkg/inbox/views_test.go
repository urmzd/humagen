package inbox

import (
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// viewOf is the ids in one reader's view, newest first.
func viewOf(t *testing.T, s *Store, tenantName, who string, v View) []string {
	t.Helper()
	page, _, err := s.Page(tenantName, Query{Reader: who, View: v, Now: t0.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, m := range page {
		out = append(out, m.ID)
	}
	return out
}

func info(s *Store, tenantName, key string, at time.Time) Message {
	m, _, err := s.Raise(tenantName, key, Message{Kind: KindArtifact, Subject: key, From: From{Kind: "agent", Name: "lead"}, Severity: Info, Parts: []Part{{Type: PartText, Text: key, State: PartInfo}}}, at)
	if err != nil {
		panic(err)
	}
	return m
}

// A reader's views are drawn from their own marks: hiding, deleting and
// reading change only their own, and one reader's marks never reach
// another reader's views or counts.
func TestViewsArePerReader(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s, err := OpenStore(open(t))
		if err != nil {
			t.Fatal(err)
		}
		a := info(s, acme, "a", t0)
		b := info(s, acme, "b", t0.Add(time.Second))
		c := info(s, acme, "c", t0.Add(2*time.Second))
		done := info(s, acme, "done", t0.Add(3*time.Second))
		if _, _, err := s.Dismiss(acme, done.ID, "bo", t0); err != nil {
			t.Fatal(err)
		}

		if _, _, err := s.Hide(acme, a.ID, "ada", t0); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Delete(acme, b.ID, "ada", t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Read(acme, c.ID, "ada", true, t0); err != nil {
			t.Fatal(err)
		}

		want := map[View][]string{
			ViewInbox:  {c.ID},
			ViewUnread: {},
			ViewHidden: {a.ID},
			ViewDone:   {done.ID},
		}
		for v, ids := range want {
			if got := viewOf(t, s, acme, "ada", v); !slices.Equal(got, ids) {
				t.Errorf("ada's %s = %q, want %q", v, got, ids)
			}
		}
		// Bo set nothing aside: every open message is in his Inbox, unread.
		for v, ids := range map[View][]string{
			ViewInbox:  {c.ID, b.ID, a.ID},
			ViewUnread: {c.ID, b.ID, a.ID},
			ViewHidden: {},
			ViewDone:   {done.ID},
		} {
			if got := viewOf(t, s, acme, "bo", v); !slices.Equal(got, ids) {
				t.Errorf("bo's %s = %q, want %q", v, got, ids)
			}
		}
		now := t0.Add(time.Minute)
		if got := count(s, acme, Query{Reader: "ada", Now: now}); got != (Counts{Open: 1, Unread: 0, Inbox: 1, Hidden: 1}) {
			t.Errorf("ada counts %+v", got)
		}
		if got := count(s, acme, Query{Reader: "bo", Now: now}); got != (Counts{Open: 3, Unread: 3, Inbox: 3}) {
			t.Errorf("bo counts %+v", got)
		}
		// The open state Home and Slack read leaves out what she hid or deleted.
		if got := ids(t, s, Query{Reader: "ada", State: Open, Now: now}); !slices.Equal(got, []string{c.ID}) {
			t.Errorf("ada's open page = %q", got)
		}
		// A deleted message is still hers to read by its id, so an undo can bring it back.
		got, err := s.GetAs(acme, b.ID, Reader{Subject: "ada"})
		if err != nil || !got.Deleted || got.State != Open {
			t.Fatalf("ada reads her deleted message as %+v, %v", got, err)
		}
		if got, _ := s.GetAs(acme, b.ID, Reader{Subject: "bo"}); got.Deleted || got.Hidden {
			t.Fatalf("bo reads ada's deleted message as deleted %v, hidden %v", got.Deleted, got.Hidden)
		}

		// Undo: restore and unhide bring both back.
		if _, _, err := s.Restore(acme, b.ID, "ada", t0); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Unhide(acme, a.ID, "ada", t0); err != nil {
			t.Fatal(err)
		}
		if got := viewOf(t, s, acme, "ada", ViewUnread); !slices.Equal(got, []string{b.ID, a.ID}) {
			t.Errorf("ada's unread after the undo = %q", got)
		}
		if got := count(s, acme, Query{Reader: "ada", Now: now}); got != (Counts{Open: 3, Unread: 2, Inbox: 3}) {
			t.Errorf("ada counts after the undo %+v", got)
		}
	})
}

// Hiding a message settled for everyone moves it from Done to Hidden, and a
// hide or delete of one already so, or by nobody, records nothing.
func TestHidingIsIdempotentAndWorksInAnyState(t *testing.T) {
	s := NewStore()
	m := info(s, acme, "m", t0)
	if _, _, err := s.Dismiss(acme, m.ID, "bo", t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Hide(acme, m.ID, "ada", t0); err != nil {
		t.Fatal(err)
	}
	if got := viewOf(t, s, acme, "ada", ViewHidden); !slices.Equal(got, []string{m.ID}) {
		t.Fatalf("ada's hidden = %q", got)
	}
	if got := viewOf(t, s, acme, "ada", ViewDone); len(got) != 0 {
		t.Fatalf("ada's done = %q", got)
	}
	n := len(s.History(acme, m.ID))
	for _, f := range []func() error{
		func() error { _, _, err := s.Hide(acme, m.ID, "ada", t0); return err },
		func() error { _, _, err := s.Restore(acme, m.ID, "ada", t0); return err },
		func() error { _, _, err := s.Hide(acme, m.ID, "", t0); return err },
		func() error { _, _, err := s.Unhide(acme, m.ID, "cy", t0); return err },
	} {
		if err := f(); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.History(acme, m.ID)) != n {
		t.Fatal("a mark that changed nothing was recorded")
	}
	// Her marks are hers alone in the history others read.
	for _, c := range s.HistoryFor(acme, m.ID, "bo", nil) {
		if c.What == DidHide {
			t.Fatalf("bo is shown ada's hide: %+v", c)
		}
	}
	if _, _, err := s.Hide(acme, "m-999", "ada", t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hiding a message that is not there: %v", err)
	}
}

// A tenant's marks are its own: the same id in another tenant is untouched.
func TestMarksStayInTheirTenant(t *testing.T) {
	s := NewStore()
	a := info(s, acme, "a", t0)
	b := info(s, "globex", "a", t0)
	if a.ID != b.ID {
		t.Fatalf("ids %s and %s", a.ID, b.ID)
	}
	if _, _, err := s.Hide(acme, a.ID, "ada", t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Delete(acme, a.ID, "bo", t0); err != nil {
		t.Fatal(err)
	}
	for _, who := range []string{"ada", "bo"} {
		if got := viewOf(t, s, "globex", who, ViewInbox); !slices.Equal(got, []string{b.ID}) {
			t.Errorf("%s's inbox in globex = %q", who, got)
		}
	}
	if got := count(s, "globex", Query{Reader: "ada"}); got.Hidden != 0 || got.Inbox != 1 {
		t.Errorf("ada counts in globex %+v", got)
	}
}

// A page holds at most MaxPage, and the Hidden count stops at MaxHiddenCount.
func TestViewsAreBounded(t *testing.T) {
	s := NewStore()
	for i := range MaxHiddenCount + 5 {
		m := info(s, acme, "m"+strconv.Itoa(i), t0.Add(time.Duration(i)*time.Second))
		if _, _, err := s.Hide(acme, m.ID, "ada", t0); err != nil {
			t.Fatal(err)
		}
	}
	page, next, err := s.Page(acme, Query{Reader: "ada", View: ViewHidden, Limit: MaxPage * 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != MaxPage || next == "" {
		t.Fatalf("a page holds %d, next %q", len(page), next)
	}
	if c := count(s, acme, Query{Reader: "ada"}); c.Hidden != MaxHiddenCount || c.Inbox != 0 {
		t.Fatalf("counts %+v", c)
	}
}

// Rebuilding from the record gives every reader the same views.
func TestReplayKeepsHiddenAndDeleted(t *testing.T) {
	s := NewStore()
	a := info(s, acme, "a", t0)
	b := info(s, acme, "b", t0.Add(time.Second))
	_, _, _ = s.Hide(acme, a.ID, "ada", t0)
	_, _, _ = s.Delete(acme, b.ID, "ada", t0)
	r := Rebuild(s.Changes(acme))
	for _, v := range []View{ViewInbox, ViewHidden, ViewUnread, ViewDone} {
		if got, want := viewOf(t, r, acme, "ada", v), viewOf(t, s, acme, "ada", v); !slices.Equal(got, want) {
			t.Errorf("rebuilt %s = %q, want %q", v, got, want)
		}
	}
	if got, want := count(r, acme, Query{Reader: "ada"}), count(s, acme, Query{Reader: "ada"}); got != want {
		t.Errorf("rebuilt counts %+v, want %+v", got, want)
	}
}
