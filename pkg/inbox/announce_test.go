package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func announcement(project, subject string) Message {
	return Message{
		Subject: subject, From: From{Kind: "person", Name: "dana"}, Source: Source{Project: project},
		Parts: []Part{{Type: PartText, Text: "The renewal is signed."}},
	}
}

func post(t *testing.T, s *Store, project, subject string, at time.Time) Message {
	t.Helper()
	m, err := s.Post(acme, announcement(project, subject), at, PostCaps{})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// readerIn reads as a member of the projects given, as the platform computes
// them from the tree.
func readerIn(subject string, projects ...string) Query {
	return Query{Reader: subject, ReaderProjects: projects, Now: t0.Add(time.Minute)}
}

func ids(t *testing.T, s *Store, q Query) []string {
	t.Helper()
	page, _, err := s.Page(acme, q)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, m := range page {
		out = append(out, m.ID)
	}
	return out
}

func TestPostRaisesABroadcastInTheProject(t *testing.T) {
	s := NewStore()
	m := post(t, s, "Globex", "Renewal signed", t0)
	if m.Kind != KindAnnouncement || !m.Broadcast || m.Key != "announce:Globex:1" || !slices.Equal(m.Audience, []string{"project:Globex"}) {
		t.Fatalf("posted = %+v", m)
	}
	if m.State != Open || m.Severity != Info || m.Parts[0].State != PartInfo {
		t.Fatalf("an announcement only to be read = %+v", m)
	}
	root := post(t, s, "", "All hands", t0)
	if root.Key != "announce::2" || !slices.Equal(root.Audience, []string{"project:"}) {
		t.Fatalf("the root's = %+v", root)
	}
	for name, edit := range map[string]func(*Message){
		"no subject":       func(m *Message) { m.Subject = "" },
		"a long subject":   func(m *Message) { m.Subject = strings.Repeat("s", MaxSubject+1) },
		"a subject in two": func(m *Message) { m.Subject = "a\nb" },
		"no text":          func(m *Message) { m.Parts = nil },
		"empty text":       func(m *Message) { m.Parts[0].Text = "" },
		"long text":        func(m *Message) { m.Parts[0].Text = strings.Repeat("x", MaxText+1) },
		"a part not text":  func(m *Message) { m.Parts = []Part{{Type: PartAlert, Alert: &AlertPart{ID: "a"}}} },
		"critical":         func(m *Message) { m.Severity = Critical },
		"from nobody":      func(m *Message) { m.From = From{} },
	} {
		a := announcement("Globex", "x")
		edit(&a)
		if _, err := s.Post(acme, a, t0, PostCaps{}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
}

func TestAProjectAudienceReachesItsReaders(t *testing.T) {
	s := NewStore()
	globex := post(t, s, "Globex", "Renewal signed", t0)
	field := post(t, s, "Field", "Field offsite", t0)
	// A member of Globex belongs to Globex, Field and the root.
	if got := ids(t, s, readerIn("cy", "", "Field", "Globex")); !slices.Equal(got, []string{field.ID, globex.ID}) {
		t.Fatalf("a member of Globex sees %q", got)
	}
	// A member of Field alone is not sent what was posted below it.
	if got := ids(t, s, readerIn("dee", "", "Field")); !slices.Equal(got, []string{field.ID}) {
		t.Fatalf("a member of Field sees %q", got)
	}
	if got := ids(t, s, readerIn("bo", "")); len(got) != 0 {
		t.Fatalf("someone in no project sees %q", got)
	}
	if _, err := s.GetAs(acme, globex.ID, Reader{Subject: "bo"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an announcement for others = %v", err)
	}
	// A reader with no subject belongs nowhere.
	if (Message{Audience: []string{"project:Globex"}}).For(Reader{Projects: []string{"Globex"}}) {
		t.Fatal("a reader with no subject was reached")
	}
	// Narrowed to a project, a reader's own inbox reaches no further.
	q := readerIn("bo")
	q.Projects = []string{"Field", "Globex"}
	if got := ids(t, s, q); len(got) != 0 {
		t.Fatalf("someone in no project, narrowed to Field, sees %q", got)
	}
	// In the project's own inbox, anyone who reads it is reached.
	q.Shared = true
	if got := ids(t, s, q); !slices.Equal(got, []string{field.ID, globex.ID}) {
		t.Fatalf("Field's inbox with its sub-projects = %q", got)
	}
	q.Projects = []string{"Globex"}
	if got := ids(t, s, q); !slices.Equal(got, []string{globex.ID}) {
		t.Fatalf("Globex's inbox = %q", got)
	}
}

func TestTheKindsAndProjectsFiltersNarrowAPage(t *testing.T) {
	s := NewStore()
	alert, _ := two(s.Raise(acme, "alert:1", Message{Kind: KindAlert, Source: Source{Project: "Globex"}, Parts: []Part{{Type: PartAlert}}}, t0))
	failed, _ := two(s.Raise(acme, "failure:1", Message{Kind: KindFailure, Source: Source{Project: "Initech"}, Parts: []Part{{Type: PartFailure}}}, t0))
	root, _ := two(s.Raise(acme, "result:1", Message{Kind: KindResult, Parts: []Part{{Type: PartText, State: PartInfo}}}, t0))
	news := post(t, s, "Globex", "Renewal signed", t0)
	q := readerIn("ada", "Globex")
	q.Kinds = []Kind{KindAlert, KindAnnouncement}
	if got := ids(t, s, q); !slices.Equal(got, []string{news.ID, alert.ID}) {
		t.Fatalf("alerts and announcements = %q", got)
	}
	q = readerIn("ada")
	q.Projects, q.Shared = []string{"Globex", "Initech"}, true
	if got := ids(t, s, q); !slices.Equal(got, []string{news.ID, failed.ID, alert.ID}) {
		t.Fatalf("two projects = %q", got)
	}
	// The root's inbox is what is filed in no project.
	q.Projects = []string{""}
	if got := ids(t, s, q); !slices.Equal(got, []string{root.ID}) {
		t.Fatalf("the root's inbox = %q", got)
	}
}

func TestABroadcastHiddenByOneStaysForOthers(t *testing.T) {
	s := NewStore()
	m := post(t, s, "Field", "Field offsite", t0)
	if got, _, err := s.Dismiss(acme, m.ID, "ada", t0); err != nil || got.State != Dismissed {
		t.Fatalf("ada's dismiss = %+v, %v", got, err)
	}
	if _, _, err := s.Snooze(acme, m.ID, "bo", t0.Add(time.Hour), t0); err != nil {
		t.Fatal(err)
	}
	for who, want := range map[string]State{"ada": Dismissed, "bo": Snoozed, "cy": Open} {
		got, err := s.GetAs(acme, m.ID, Reader{Subject: who, Projects: []string{"Field"}})
		if err != nil || got.State != want {
			t.Errorf("%s sees %s, %v, want %s", who, got.State, err, want)
		}
	}
	during, after := readerIn("bo", "Field"), readerIn("bo", "Field")
	during.Now, after.Now = t0.Add(30*time.Minute), t0.Add(2*time.Hour)
	if got := ids(t, s, during); len(got) != 0 {
		t.Fatalf("bo's open page while snoozed = %q", got)
	}
	if got := ids(t, s, after); !slices.Equal(got, []string{m.ID}) {
		t.Fatalf("bo's open page once the snooze ran out = %q", got)
	}
	if got := ids(t, s, readerIn("ada", "Field")); len(got) != 0 {
		t.Fatalf("ada's open page = %q", got)
	}
	if got := ids(t, s, readerIn("cy", "Field")); !slices.Equal(got, []string{m.ID}) {
		t.Fatalf("cy's open page = %q", got)
	}
	// Dismissed is final for the reader: a later snooze does not bring it back.
	if got, _, _ := s.Snooze(acme, m.ID, "ada", t0.Add(time.Hour), t0); got.State != Dismissed {
		t.Fatalf("ada snoozing a dismissed broadcast = %s", got.State)
	}
	// The message itself is still open for everyone.
	msgs, _ := s.Snapshot(acme)
	if msgs[0].State != Open {
		t.Fatalf("the shared state = %s", msgs[0].State)
	}
	// Withdrawing closes it for everyone.
	if _, _, err := s.Withdraw(acme, m.ID, "dana", "Posted to the wrong project", t0); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetAs(acme, m.ID, Reader{Subject: "cy", Projects: []string{"Field"}}); got.State != Dismissed {
		t.Fatalf("cy after the withdrawal = %s", got.State)
	}
}

func TestReplayRebuildsPerReaderState(t *testing.T) {
	s := NewStore()
	a := post(t, s, "Field", "One", t0)
	b := post(t, s, "Field", "Two", t0.Add(time.Second))
	_, _, _ = s.Dismiss(acme, a.ID, "ada", t0)
	_, _, _ = s.Snooze(acme, b.ID, "bo", t0.Add(time.Hour), t0)
	_, _ = s.Read(acme, b.ID, "cy", true, t0)
	_, _, _ = s.Withdraw(acme, b.ID, "dana", "", t0.Add(time.Minute))
	rebuilt := Rebuild(s.Changes(acme))
	for _, who := range []string{"ada", "bo", "cy", "dee"} {
		for _, id := range []string{a.ID, b.ID} {
			r := Reader{Subject: who, Projects: []string{"Field"}}
			live, _ := s.GetAs(acme, id, r)
			got, _ := rebuilt.GetAs(acme, id, r)
			if !reflect.DeepEqual(live, got) {
				t.Errorf("%s reads %s as %+v live and %+v rebuilt", who, id, live, got)
			}
		}
		q := readerIn(who, "Field")
		if live, got := count(s, acme, q), count(rebuilt, acme, q); live != got {
			t.Errorf("%s counts %+v live and %+v rebuilt", who, live, got)
		}
	}
	// The day's posts are part of what is rebuilt.
	if _, err := rebuilt.Post(acme, announcement("Field", "Three"), t0, PostCaps{PerProject: 2}); !errors.Is(err, ErrLimit) {
		t.Fatalf("a rebuilt inbox forgot the day's posts: %v", err)
	}
}

func TestBroadcastsCountOnlyInUpdates(t *testing.T) {
	s := NewStore()
	raise(s, "alert:1", t0, Part{Type: PartAlert})
	a := post(t, s, "Field", "One", t0)
	b := post(t, s, "", "Two", t0)
	q := readerIn("ada", "", "Field")
	if c := count(s, acme, q); c != (Counts{Open: 1, Unread: 1, Updates: 2}) {
		t.Fatalf("counts = %+v", c)
	}
	_, _ = s.Read(acme, a.ID, "ada", true, t0)
	_, _, _ = s.Dismiss(acme, b.ID, "ada", t0)
	if c := count(s, acme, q); c != (Counts{Open: 1, Unread: 1, Updates: 0}) {
		t.Fatalf("counts after reading one and hiding one = %+v", c)
	}
	// Someone else still has both to read.
	if c := count(s, acme, readerIn("bo", "", "Field")); c.Updates != 2 || c.Open != 1 {
		t.Fatalf("bo's counts = %+v", c)
	}
	// The count is the whole inbox, whatever a page narrows to.
	narrowed := q
	narrowed.Kinds, narrowed.Projects = []Kind{KindAlert}, []string{"Nowhere"}
	if c := count(s, acme, narrowed); c != count(s, acme, q) {
		t.Fatalf("a narrowed count = %+v", c)
	}
}

// Needs pages what may need a reader, what Open counts: posted announcements
// are left out before the page is cut, however many are newer, and an
// agent's draft for the project's owners stays. News pages the posts alone.
func TestNeedsLeavesOutBroadcastsBeforeThePageIsCut(t *testing.T) {
	s := NewStore()
	alert := raise(s, "alert:1", t0, Part{Type: PartAlert})
	draft, _ := two(s.Raise(acme, "draft:r1", Message{
		Kind: KindAnnouncement, Subject: "This week", From: From{Kind: "agent", Name: "recap"},
		Audience: []string{OwnersAudience("Field")}, Source: Source{Project: "Field"},
		Parts: []Part{{Type: PartDraft, Draft: &DraftPart{Project: "Field", Agent: "recap", Subject: "This week", Text: "Two fixes."}}},
	}, t0.Add(time.Second)))
	var news Message
	for i := range 3 {
		news = post(t, s, "Field", fmt.Sprint("News ", i), t0.Add(time.Duration(2+i)*time.Second))
	}
	q := readerIn("ada", "Field")
	q.ReaderOwns, q.Limit = []string{"Field"}, 2
	if got := ids(t, s, q); len(got) != 2 || slices.Contains(got, draft.ID) || slices.Contains(got, alert.ID) {
		t.Fatalf("the newest two = %q, want two announcements", got)
	}
	q.Needs = true
	if got := ids(t, s, q); !slices.Equal(got, []string{draft.ID, alert.ID}) {
		t.Fatalf("what needs ada = %q, want the draft and the alert", got)
	}
	if c := count(s, acme, q); c != (Counts{Open: 2, Unread: 2, Updates: 3}) {
		t.Fatalf("counts = %+v", c)
	}
	// News is the other side: the posted announcements alone, the draft left
	// out however new it is.
	q.Needs, q.News, q.Limit = false, true, 0
	if got := ids(t, s, q); len(got) != 3 || slices.Contains(got, draft.ID) || slices.Contains(got, alert.ID) {
		t.Fatalf("ada's news = %q, want the three posts", got)
	}
	q.News, q.Needs, q.Limit = false, true, 2
	// An announcement she hid is still news, not something that needs her.
	if _, _, err := s.Dismiss(acme, news.ID, "ada", t0); err != nil {
		t.Fatal(err)
	}
	q.State = Dismissed
	if got := ids(t, s, q); len(got) != 0 {
		t.Fatalf("what needs ada, hidden = %q", got)
	}
	q.Needs = false
	if got := ids(t, s, q); !slices.Equal(got, []string{news.ID}) {
		t.Fatalf("what ada hid = %q", got)
	}
}

func TestRacingPostsStopAtTheDailyCaps(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	var posted, limited atomic.Int64
	for i := range 60 {
		wg.Go(func() {
			project := []string{"Field", "Globex", "Initech"}[i%3]
			_, err := s.Post(acme, announcement(project, fmt.Sprint("post ", i)), t0.Add(time.Duration(i)*time.Second), PostCaps{PerProject: 5, PerTenant: 12})
			switch {
			case err == nil:
				posted.Add(1)
			case errors.Is(err, ErrLimit):
				limited.Add(1)
			default:
				t.Errorf("post: %v", err)
			}
		})
	}
	wg.Wait()
	if posted.Load() != 12 || limited.Load() != 48 {
		t.Fatalf("%d posted and %d refused, want 12 and 48", posted.Load(), limited.Load())
	}
	per := map[string]int{}
	msgs, _ := s.Snapshot(acme)
	for _, m := range msgs {
		per[m.Source.Project]++
	}
	for project, n := range per {
		if n > 5 {
			t.Errorf("%s posted %d, more than 5", project, n)
		}
	}
	// The default is 20 a project a day, and a new day starts again.
	d := NewStore()
	for i := range 20 {
		post(t, d, "Field", fmt.Sprint("post ", i), t0)
	}
	if _, err := d.Post(acme, announcement("Field", "one more"), t0, PostCaps{}); !errors.Is(err, ErrLimit) {
		t.Fatalf("the 21st post of the day = %v", err)
	}
	post(t, d, "Globex", "another project", t0)
	post(t, d, "Field", "tomorrow", t0.Add(24*time.Hour))
	// Another tenant counts its own.
	if _, err := d.Post("globex", announcement("Field", "elsewhere"), t0, PostCaps{}); err != nil {
		t.Fatalf("another tenant: %v", err)
	}
}

func TestWithdrawRecordsWho(t *testing.T) {
	s := NewStore()
	m := post(t, s, "Field", "Field offsite", t0)
	if _, _, err := s.Withdraw(acme, m.ID, "dana", "Moved to Friday", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Raised, read by dana, who posted it, and withdrawn.
	h := s.History(acme, m.ID)
	if len(h) != 3 || h[2].What != DidWithdraw || h[2].By != "dana" || h[2].Note != "Moved to Friday" {
		t.Fatalf("history = %+v", h)
	}
	other := raise(s, "alert:1", t0, Part{Type: PartAlert})
	if _, _, err := s.Withdraw(acme, other.ID, "", "Gone", t0); err != nil {
		t.Fatal(err)
	}
	if h := s.History(acme, other.ID); h[1].By != "system" {
		t.Fatalf("the platform's withdrawal = %+v", h[1])
	}
}

// Withdrawals written before the inbox recorded who did them read as the
// system's, and the record replays unchanged.
func TestOldWithdrawalsReadAsSystem(t *testing.T) {
	st := state.NewMemory()
	put := func(seq int64, v any) {
		t.Helper()
		raw, _ := json.Marshal(v)
		if err := st.Write(context.Background(), "inbox", state.Put(acme, state.Seq(seq), raw)); err != nil {
			t.Fatal(err)
		}
	}
	put(0, map[string]any{"message": "", "at": t0, "by": "rule", "what": "raised",
		"raised": map[string]any{"id": "", "key": "artifact:a1", "kind": "artifact", "subject": "Report", "from": map[string]any{"kind": "system", "name": "artifacts"},
			"at": t0, "updated_at": t0, "severity": "info", "state": "open", "unread": false, "source": map[string]any{}, "parts": []any{map[string]any{"type": "artifact", "state": "info", "actions": nil}}}})
	put(1, map[string]any{"message": "m-1", "at": t0.Add(time.Minute), "what": "withdrawn", "note": "Replaced"})
	s, err := OpenStore(st)
	if err != nil {
		t.Fatal(err)
	}
	h := s.History(acme, "m-1")
	if len(h) != 2 || h[1].By != "system" || h[1].Note != "Replaced" {
		t.Fatalf("an old withdrawal = %+v", h)
	}
	if m, _ := s.Get(acme, "m-1", "ada"); m.State != Dismissed || m.Broadcast {
		t.Fatalf("an old message = %+v", m)
	}
}

func TestRestartKeepsBroadcastsAndWhoHidThem(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		a := post(t, s, "Field", "One", t0)
		b := post(t, s, "Field", "Two", t0)
		if _, _, err := s.Dismiss(acme, a.ID, "ada", t0); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Snooze(acme, b.ID, "bo", t0.Add(time.Hour), t0); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Withdraw(acme, b.ID, "dana", "", t0); err != nil {
			t.Fatal(err)
		}
		again, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(again.Changes(acme), s.Changes(acme)) {
			t.Fatalf("record after restart = %+v, want %+v", again.Changes(acme), s.Changes(acme))
		}
		got, err := again.GetAs(acme, a.ID, Reader{Subject: "ada", Projects: []string{"Field"}})
		if err != nil || got.State != Dismissed || !got.Broadcast || got.Key != "announce:Field:1" {
			t.Fatalf("ada's copy after restart = %+v, %v", got, err)
		}
		if got, _ := again.GetAs(acme, a.ID, Reader{Subject: "cy", Projects: []string{"Field"}}); got.State != Open {
			t.Fatalf("cy's copy after restart = %s", got.State)
		}
		if _, err := again.GetAs(acme, a.ID, Reader{Subject: "cy"}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("who it reaches did not survive: %v", err)
		}
		if h := again.History(acme, b.ID); h[len(h)-1].By != "dana" {
			t.Fatalf("who withdrew did not survive: %+v", h)
		}
	})
}

func TestAFailedPostChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, _ := OpenStore(f)
	m := post(t, s, "Field", "One", t0)
	f.Fail(statetest.ErrInjected)
	if _, err := s.Post(acme, announcement("Field", "Two"), t0, PostCaps{PerProject: 2}); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Post = %v", err)
	}
	if _, _, err := s.Dismiss(acme, m.ID, "ada", t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Dismiss = %v", err)
	}
	if got, _ := s.GetAs(acme, m.ID, Reader{Subject: "ada", Projects: []string{"Field"}}); got.State != Open {
		t.Fatalf("a failed hide hid it: %s", got.State)
	}
	f.Fail(nil)
	// The failed post did not use up the day.
	if _, err := s.Post(acme, announcement("Field", "Two"), t0, PostCaps{PerProject: 2}); err != nil {
		t.Fatalf("a post after the failure: %v", err)
	}
}

// A message addressed to a project's owners reaches whoever owns it when it
// is read, and nobody without a subject.
func TestAnOwnersAudienceReachesTheOwnersWhenRead(t *testing.T) {
	m := Message{Audience: []string{OwnersAudience("Globex")}}
	switch {
	case !m.For(Reader{Subject: "ada", Owns: []string{"Field", "Globex"}}):
		t.Fatal("an owner of Globex was not reached")
	case m.For(Reader{Subject: "cy", Owns: []string{"Field"}}):
		t.Fatal("an owner of Field alone was reached")
	case m.For(Reader{Subject: "cy", Projects: []string{"Globex"}}):
		t.Fatal("a member was reached as an owner")
	case m.For(Reader{Owns: []string{"Globex"}}):
		t.Fatal("a reader with no subject was reached")
	}
	s := NewStore()
	raised, _, err := s.Raise(acme, "memory:note:1", Message{Kind: KindMemory, Audience: []string{OwnersAudience("Globex")}, Source: Source{Project: "Globex"}, Parts: []Part{{Type: PartMemory}}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	q := readerIn("ada")
	q.ReaderOwns = []string{"Globex"}
	if got := ids(t, s, q); !slices.Equal(got, []string{raised.ID}) {
		t.Fatalf("the owner's inbox = %q", got)
	}
	// In the project's own inbox it is still the owners'.
	q = readerIn("bo")
	q.Projects, q.Shared = []string{"Globex"}, true
	if got := ids(t, s, q); len(got) != 0 {
		t.Fatalf("a viewer of Globex's inbox sees %q", got)
	}
}

// A reader's own marks on a message are theirs alone: another reader is
// shown what changed the message for everyone, never who read it, or who
// hid or snoozed their own copy of an announcement. The record keeps every
// mark, and a snooze to the time already set is not one. The history a
// reader is shown is bounded, the newest kept.
func TestAReadersOwnMarksStayOutOfAnotherReadersHistory(t *testing.T) {
	s := NewStore()
	m := post(t, s, "Globex", "Renewal signed", t0)
	until := t0.Add(time.Hour)
	for range 3 {
		if _, _, err := s.Snooze(acme, m.ID, "dee", until, t0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Read(acme, m.ID, "dee", true, t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Dismiss(acme, m.ID, "dee", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(acme, m.ID, "eve", true, t0); err != nil {
		t.Fatal(err)
	}
	whats := func(cs []Change) []string {
		var out []string
		for _, c := range cs {
			out = append(out, string(c.What)+" "+c.By)
		}
		return out
	}
	if got := whats(s.HistoryFor(acme, m.ID, "bo", nil)); !slices.Equal(got, []string{"raised dana"}) {
		t.Fatalf("another reader is shown %q", got)
	}
	if got := whats(s.HistoryFor(acme, m.ID, "dee", nil)); !slices.Equal(got, []string{"raised dana", "snoozed dee", "read dee", "dismissed dee"}) {
		t.Fatalf("dee is shown %q", got)
	}
	// dana's own read of what she posted is a mark like any other.
	if n := len(s.History(acme, m.ID)); n != 6 {
		t.Fatalf("the record holds %d changes, want every mark once", n)
	}
	if got := whats(s.HistoryFor(acme, m.ID, "dana", nil)); !slices.Equal(got, []string{"raised dana", "read dana"}) {
		t.Fatalf("dana is shown %q", got)
	}

	// Snoozing to a new time each time is recorded each time, and shown
	// bounded.
	other := post(t, s, "Globex", "Kickoff", t0)
	for i := range MaxHistory + 20 {
		if _, _, err := s.Snooze(acme, other.ID, "dee", t0.Add(time.Duration(i+1)*time.Minute), t0); err != nil {
			t.Fatal(err)
		}
	}
	got := s.HistoryFor(acme, other.ID, "dee", nil)
	if len(got) != MaxHistory || !got[len(got)-1].Until.Equal(t0.Add(time.Duration(MaxHistory+20)*time.Minute)) {
		t.Fatalf("dee is shown %d changes, the last %+v", len(got), got[len(got)-1])
	}
	if got := s.HistoryFor(acme, other.ID, "bo", nil); len(got) != 1 {
		t.Fatalf("another reader is shown %d changes", len(got))
	}
}

// What is posted once under a key is never posted again for as long as the
// inbox is kept: not while it is open, not once it is withdrawn, not after
// a restart, and not past the day's caps, which a repeat never meets.
func TestPostOnceNeverPostsAKeyTwice(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		caps := PostCaps{PerProject: 1}
		first, posted, err := s.PostOnce(acme, "release:0.15.0", announcement("", "What's new"), t0, caps)
		if err != nil || !posted || first.Key != "release:0.15.0" || !first.Broadcast || !slices.Equal(first.Audience, []string{"project:"}) {
			t.Fatalf("first = %+v, %v, %v", first, posted, err)
		}
		// The day's one post is used, so only a repeat gets an answer.
		if again, posted, err := s.PostOnce(acme, "release:0.15.0", announcement("", "What's new"), t0, caps); err != nil || posted || again.ID != first.ID {
			t.Fatalf("again = %+v, %v, %v", again, posted, err)
		}
		if _, _, err := s.PostOnce(acme, "release:0.16.0", announcement("", "What's new"), t0, caps); !errors.Is(err, ErrLimit) {
			t.Fatalf("another key past the cap = %v", err)
		}
		if _, _, err := s.Withdraw(acme, first.ID, "dana", "", t0); err != nil {
			t.Fatal(err)
		}
		restarted, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		for name, store := range map[string]*Store{"withdrawn": s, "after a restart": restarted} {
			if again, posted, err := store.PostOnce(acme, "release:0.15.0", announcement("", "What's new"), t0.Add(48*time.Hour), PostCaps{}); err != nil || posted || again.ID != first.ID {
				t.Errorf("%s: %+v, %v, %v", name, again, posted, err)
			}
		}
		if got := ids(t, restarted, Query{Reader: "ada", ReaderProjects: []string{""}, Kinds: []Kind{KindAnnouncement}, State: "all", Now: t0}); !slices.Equal(got, []string{first.ID}) {
			t.Errorf("the inbox after a restart = %q", got)
		}
		if _, _, err := s.PostOnce(acme, "", announcement("", "What's new"), t0, PostCaps{}); !errors.Is(err, ErrInvalid) {
			t.Errorf("no key = %v", err)
		}
	})
}

// Whoever posts an announcement has read it: a person who wrote it, or who
// published an agent's draft. Everyone else still has it to read, and the
// mark is written with the post, so a rebuilt inbox has it too.
func TestThePosterHasReadWhatTheyPosted(t *testing.T) {
	s := NewStore()
	m := post(t, s, "Field", "Field offsite", t0)
	if m.Unread {
		t.Fatal("Post answers the author with their own post unread")
	}
	for who, unread := range map[string]bool{"dana": false, "ada": true} {
		got, err := s.GetAs(acme, m.ID, Reader{Subject: who, Projects: []string{"Field"}})
		if err != nil || got.Unread != unread {
			t.Errorf("%s reads it unread=%v, %v, want %v", who, got.Unread, err, unread)
		}
	}
	if c := count(s, acme, readerIn("dana", "Field")); c.Updates != 0 {
		t.Fatalf("the author has %d updates to read", c.Updates)
	}
	published := announcement("Field", "Weekly recap")
	published.From = From{Kind: "agent", Name: "recap-writer", ApprovedBy: "olu"}
	if _, err := s.Post(acme, published, t0, PostCaps{}); err != nil {
		t.Fatal(err)
	}
	agent := announcement("Field", "Nightly recap")
	agent.From = From{Kind: "agent", Name: "recap-writer"}
	if _, err := s.Post(acme, agent, t0, PostCaps{}); err != nil {
		t.Fatal(err)
	}
	_, read := s.Snapshot(acme)
	if !slices.Equal(read["m-1"], []string{"dana"}) || !slices.Equal(read["m-2"], []string{"olu"}) || len(read["m-3"]) != 0 {
		t.Fatalf("who has read what = %v", read)
	}
	_, rebuilt := Rebuild(s.Changes(acme)).Snapshot(acme)
	if !reflect.DeepEqual(rebuilt, read) {
		t.Fatalf("rebuilt, who has read what = %v, want %v", rebuilt, read)
	}
}

// A reader brings back an announcement they hid or snoozed for themselves,
// and is told which of their copies are hidden that way. Nobody else's copy
// changes, an unhide that would change nothing is not recorded, a
// withdrawal is not undone, and a message that is not a broadcast is not
// reopened.
func TestAReaderBringsBackWhatTheyHid(t *testing.T) {
	s := NewStore()
	m := post(t, s, "Field", "Field offsite", t0)
	as := func(who string) Message {
		t.Helper()
		got, err := s.GetAs(acme, m.ID, Reader{Subject: who, Projects: []string{"Field"}})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if _, _, err := s.Dismiss(acme, m.ID, "ada", t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Snooze(acme, m.ID, "bo", t0.Add(time.Hour), t0); err != nil {
		t.Fatal(err)
	}
	if got := as("ada"); got.State != Dismissed || !got.Hidden {
		t.Fatalf("ada's hidden copy = %s, hidden %v", got.State, got.Hidden)
	}
	if got := as("bo"); got.State != Snoozed || !got.Hidden {
		t.Fatalf("bo's snoozed copy = %s, hidden %v", got.State, got.Hidden)
	}
	if got := as("cy"); got.State != Open || got.Hidden {
		t.Fatalf("cy's copy = %s, hidden %v", got.State, got.Hidden)
	}
	if got, _, err := s.Unhide(acme, m.ID, "ada", t0.Add(time.Minute)); err != nil || got.State != Open || got.Hidden {
		t.Fatalf("ada's unhide = %s, hidden %v, %v", got.State, got.Hidden, err)
	}
	if got := ids(t, s, readerIn("ada", "Field")); !slices.Equal(got, []string{m.ID}) {
		t.Fatalf("ada's open page after the unhide = %q", got)
	}
	if got := as("bo"); got.State != Snoozed || !got.Hidden {
		t.Fatalf("bo's copy after ada's unhide = %s", got.State)
	}
	if _, _, err := s.Unhide(acme, m.ID, "bo", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := as("bo"); got.State != Open || got.Hidden {
		t.Fatalf("bo's copy after bo's unhide = %s, hidden %v", got.State, got.Hidden)
	}
	// Hiding it again works as the first time did.
	if _, _, err := s.Dismiss(acme, m.ID, "ada", t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := as("ada"); got.State != Dismissed || !got.Hidden {
		t.Fatalf("ada's copy hidden again = %s", got.State)
	}
	n := len(s.History(acme, m.ID))
	if _, _, err := s.Unhide(acme, m.ID, "cy", t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Unhide(acme, m.ID, "", t0); err != nil {
		t.Fatal(err)
	}
	if len(s.History(acme, m.ID)) != n {
		t.Fatal("an unhide of a copy never set aside was recorded")
	}
	whats := func(cs []Change) []string {
		var out []string
		for _, c := range cs {
			out = append(out, string(c.What)+" "+c.By)
		}
		return out
	}
	if got := whats(s.HistoryFor(acme, m.ID, "cy", nil)); !slices.Equal(got, []string{"raised dana"}) {
		t.Fatalf("cy is shown %q", got)
	}
	if got := whats(s.HistoryFor(acme, m.ID, "ada", nil)); !slices.Equal(got, []string{"raised dana", "dismissed ada", "unhidden ada", "dismissed ada"}) {
		t.Fatalf("ada is shown %q", got)
	}
	rebuilt := Rebuild(s.Changes(acme))
	for _, who := range []string{"ada", "bo", "cy"} {
		r := Reader{Subject: who, Projects: []string{"Field"}}
		live, _ := s.GetAs(acme, m.ID, r)
		got, _ := rebuilt.GetAs(acme, m.ID, r)
		if !reflect.DeepEqual(live, got) {
			t.Errorf("%s reads %+v live and %+v rebuilt", who, live, got)
		}
	}
	// A withdrawal is everyone's: unhiding changes nothing.
	if _, _, err := s.Withdraw(acme, m.ID, "dana", "", t0.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Unhide(acme, m.ID, "ada", t0.Add(4*time.Minute)); got.State != Dismissed || got.Hidden {
		t.Fatalf("ada unhiding a withdrawn announcement = %s, hidden %v", got.State, got.Hidden)
	}
	if got := as("cy"); got.State != Dismissed || got.Hidden {
		t.Fatalf("cy's withdrawn copy = %s, hidden %v", got.State, got.Hidden)
	}
	// A message that is not a broadcast is everyone's to set aside, and
	// not a reader's to bring back.
	alert := raise(s, "alert:1", t0, Part{Type: PartAlert})
	if _, _, err := s.Dismiss(acme, alert.ID, "ada", t0); err != nil {
		t.Fatal(err)
	}
	if got, changed, _ := s.Unhide(acme, alert.ID, "ada", t0); changed || got.State != Dismissed || got.Hidden {
		t.Fatalf("an unhidden alert = %s, changed %v, hidden %v", got.State, changed, got.Hidden)
	}
	if _, _, err := s.Unhide(acme, "m-404", "ada", t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unhiding a message that is not there = %v", err)
	}
}
