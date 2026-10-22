package threads_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

func everything(t *testing.T, s *threads.Store, tenant, id string) (threads.Thread, []threads.Message) {
	t.Helper()
	th, err := s.Get(tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	msgs, _, err := s.Messages(tenant, id, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	return th, msgs
}

func TestRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		tags := []threads.Mention{{Kind: threads.Agent, Name: "ignite"}, {Kind: threads.SME, Name: "billing"}}
		th, m := start(t, s, "acme", shared("Globex", "renewals"), threads.Message{By: "dana", Name: "Dana", Text: "@ignite @sme:billing", Mentions: tags})
		if _, err := s.Settle("acme", th.ID, m.ID, []threads.Mention{{Kind: threads.Agent, Name: "ignite", Run: run}, {Kind: threads.SME, Name: "billing"}}); err != nil {
			t.Fatal(err)
		}
		post(t, s, "acme", th.ID, threads.Message{By: "ana", Text: "Renew it.", Answers: &threads.AnswerRef{Run: run, Question: "q1"}, Via: "acme-slack", SurfaceID: "1700.1"}, t0.Add(time.Minute))
		post(t, s, "acme", th.ID, threads.Message{By: "dana", Text: "Thanks", Prev: "m-1"}, t0.Add(2*time.Minute))
		if _, err := s.Update("acme", th.ID, 1, t0, func(t *threads.Thread) error { t.Title = "Renewal"; return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Participants("acme", th.ID, 2, []threads.Participant{{Kind: threads.Agent, Name: "ignite"}}, nil, nil, "dana", t0); err != nil {
			t.Fatal(err)
		}
		legacy, err := s.Create("acme", threads.Thread{ID: run, Project: "", Channel: "general", Visibility: threads.Shared, CreatedBy: "eve"}, t0)
		if err != nil {
			t.Fatal(err)
		}
		other, _ := start(t, s, "globex", shared("", "general"), msg("zed", "hello"))

		again, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct{ tenant, id string }{{"acme", th.ID}, {"acme", legacy.ID}, {"globex", other.ID}} {
			wantT, wantM := everything(t, s, c.tenant, c.id)
			gotT, gotM := everything(t, again, c.tenant, c.id)
			if !reflect.DeepEqual(gotT, wantT) || !reflect.DeepEqual(gotM, wantM) {
				t.Fatalf("%s after restart = %+v %+v, want %+v %+v", c.id, gotT, gotM, wantT, wantM)
			}
		}
		if _, err := again.Get("globex", th.ID); !errors.Is(err, threads.ErrNotFound) {
			t.Fatalf("another tenant's thread after restart = %v", err)
		}
		// What a restart reads carries on: the next message follows the
		// latest, a surface id is still known, and numbering goes on.
		if _, created, _ := again.Post("acme", th.ID, threads.Message{By: "ana", Text: "again", Via: "acme-slack", SurfaceID: "1700.1"}, t0); created {
			t.Fatal("a surface id was forgotten across a restart")
		}
		if m := post(t, again, "acme", th.ID, msg("dana", "four"), t0); m.ID != "m-4" || m.Prev != "m-3" {
			t.Fatalf("after restart = %+v", m)
		}
	})
}

// The record of a conversation from before threads that no person asked in
// may name nobody as its owner, and is read back so across a restart. The
// first who claims it owns it, taking part as its owner, and a claim after
// that changes nothing. A thread started with a message always has one.
func TestARecordNobodyStartedIsKeptSoUntilClaimed(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		agentOnly := threads.Thread{ID: run, Project: "Globex", Channel: "general", Visibility: threads.Shared, Participants: []threads.Participant{{Kind: threads.Agent, Name: "lead"}}}
		rec, err := s.Create("acme", agentOnly, t0)
		if err != nil || rec.CreatedBy != "" || len(rec.Participants) != 1 || rec.Participants[0].Owner {
			t.Fatalf("a record nobody started = %+v, %v", rec, err)
		}
		if _, _, err := s.Start("acme", shared("Globex", "general"), threads.Message{Text: "x"}, t0); !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("a thread started by nobody = %v", err)
		}
		again, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := again.Get("acme", run); err != nil || !reflect.DeepEqual(got, rec) {
			t.Fatalf("after a restart = %+v, %v, want %+v", got, err, rec)
		}
		claimed, err := again.Claim("acme", run, "cy", t0.Add(time.Minute))
		if err != nil || claimed.CreatedBy != "cy" || claimed.Version != rec.Version+1 || len(claimed.Participants) != 2 || !claimed.Participants[0].Owner || claimed.Participants[0].Name != "cy" {
			t.Fatalf("claimed = %+v, %v", claimed, err)
		}
		if got, err := again.Claim("acme", run, "dee", t0.Add(2*time.Minute)); err != nil || !reflect.DeepEqual(got, claimed) {
			t.Fatalf("a second claim = %+v, %v", got, err)
		}
		if _, err := again.Claim("acme", run, " ", t0); !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("a claim by nobody = %v", err)
		}
	})
}

func TestAFailedWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, err := threads.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	th, m := start(t, s, "acme", shared("Globex", "renewals"), threads.Message{By: "dana", Text: "@ignite", Mentions: []threads.Mention{{Kind: threads.Agent, Name: "ignite"}}})
	beforeT, beforeM := everything(t, s, "acme", th.ID)
	f.Fail(statetest.ErrInjected)
	if _, _, err := s.Start("acme", shared("Globex", "renewals"), msg("dana", "x"), t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Start = %v", err)
	}
	if _, err := s.Create("acme", threads.Thread{Project: "Globex", Channel: "renewals", Visibility: threads.Shared, CreatedBy: "dana"}, t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Create = %v", err)
	}
	if _, _, err := s.Post("acme", th.ID, msg("ana", "x"), t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Post = %v", err)
	}
	if _, err := s.Settle("acme", th.ID, m.ID, []threads.Mention{{Kind: threads.Agent, Name: "ignite", Run: run}}); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Settle = %v", err)
	}
	if _, err := s.Update("acme", th.ID, 1, t0, func(t *threads.Thread) error { t.Title = "x"; return nil }); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Update = %v", err)
	}
	if _, err := s.Participants("acme", th.ID, 1, []threads.Participant{{Kind: threads.Agent, Name: "ignite"}}, nil, nil, "dana", t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Participants = %v", err)
	}
	if _, err := s.Join("acme", th.ID, []threads.Participant{{Kind: threads.Agent, Name: "ignite"}}, t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Join = %v", err)
	}
	afterT, afterM := everything(t, s, "acme", th.ID)
	if !reflect.DeepEqual(afterT, beforeT) || !reflect.DeepEqual(afterM, beforeM) {
		t.Fatalf("a failed write changed the thread: %+v %+v", afterT, afterM)
	}
	if page, _, _ := s.List("acme", threads.Query{Project: "Globex"}); len(page) != 1 {
		t.Fatalf("a failed write left a thread: %+v", page)
	}
	f.Fail(nil)
	if m := post(t, s, "acme", th.ID, msg("ana", "x"), t0); m.ID != "m-2" {
		t.Fatalf("the post after a failure = %+v", m)
	}
	again, err := threads.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, msgs := everything(t, again, "acme", th.ID); len(msgs) != 2 {
		t.Fatalf("after restart %d messages, want 2", len(msgs))
	}
}

func TestOpenRefusesRecordsThatAreNotThreads(t *testing.T) {
	put := func(st state.Store, key string, v any) {
		t.Helper()
		raw, _ := json.Marshal(v)
		if err := st.Write(context.Background(), "threads", state.Put("acme", key, raw)); err != nil {
			t.Fatal(err)
		}
	}
	id := "th-0123456789abcdef"
	rec := threads.Thread{ID: id, Project: "Globex", Channel: "general", Visibility: threads.Shared, Version: 1, CreatedBy: "dana",
		Participants: []threads.Participant{{Kind: threads.Person, Name: "dana", Owner: true}}}
	m := func(n int, prev string) threads.Message {
		return threads.Message{ID: threads.MessageID(n), Thread: id, Prev: prev, By: "dana", Text: "x", Via: threads.ViaConsole, Mentions: []threads.Mention{}}
	}
	key := func(n int) string { return "m:" + id + ":" + state.Seq(int64(n)) }
	for _, c := range []struct {
		name  string
		write func(st state.Store)
	}{
		{"a message of a missing thread", func(st state.Store) { put(st, key(1), m(1, "")) }},
		{"a gap", func(st state.Store) {
			put(st, "t:"+id, rec)
			put(st, key(1), m(1, ""))
			put(st, key(3), m(3, "m-1"))
		}},
		{"a message that follows a later one", func(st state.Store) {
			put(st, "t:"+id, rec)
			put(st, key(1), m(1, "m-2"))
			put(st, key(2), m(2, "m-1"))
		}},
		{"a message under another key", func(st state.Store) {
			put(st, "t:"+id, rec)
			put(st, key(1), m(2, ""))
		}},
		{"an agent's words", func(st state.Store) {
			put(st, "t:"+id, rec)
			put(st, key(1), threads.Message{ID: "m-1", Thread: id, Text: "I am an agent", Via: threads.ViaConsole})
		}},
		{"a thread under another key", func(st state.Store) { put(st, "t:th-ffffffffffffffff", rec) }},
		{"an unknown key", func(st state.Store) { put(st, "q:x", map[string]string{}) }},
		{"a thread with no visibility", func(st state.Store) {
			bad := rec
			bad.Visibility = ""
			put(st, "t:"+id, bad)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			st := state.NewMemory()
			c.write(st)
			if _, err := threads.Open(st); !errors.Is(err, threads.ErrInvalid) {
				t.Fatalf("Open = %v, want ErrInvalid", err)
			}
		})
	}
}

// Sharing a thread moves it and makes it shared in one write, which is kept:
// read back, it is where it was shared, with whom it added, and its old
// project lists it no more. A thread on a surface stays where it was started.
func TestShareIsKept(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		private := shared("Globex", "general")
		private.Visibility = threads.Private
		th, _ := start(t, s, "acme", private, msg("dana", "a draft"))
		if _, err := s.Share("acme", th.ID, th.Version+1, "Field", "renewals", "", nil, "", nil, nil, "dana", t0); !errors.Is(err, threads.ErrVersion) {
			t.Fatalf("a share at a stale version = %v", err)
		}
		got, err := s.Share("acme", th.ID, th.Version, "Field", "renewals", "", nil, "", []threads.Participant{{Kind: threads.Person, Name: "ana"}}, nil, "dana", t0.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if got.Project != "Field" || got.Channel != "renewals" || got.Visibility != threads.Shared || got.Version != th.Version+1 || len(got.Participants) != 2 || got.Participants[1].AddedBy != "dana" {
			t.Fatalf("shared, the thread is %+v", got)
		}
		same, err := s.Share("acme", th.ID, got.Version, "Field", "renewals", "", nil, "", nil, nil, "dana", t0.Add(2*time.Minute))
		if err != nil || same.Version != got.Version {
			t.Fatalf("sharing it again where it is = %+v, %v", same, err)
		}
		again, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if kept, _ := everything(t, again, "acme", th.ID); !reflect.DeepEqual(kept, got) {
			t.Fatalf("read back, the thread is %+v, not %+v", kept, got)
		}
		for _, c := range []struct {
			project string
			n       int
		}{{"Field", 1}, {"Globex", 0}} {
			if list, _, err := again.List("acme", threads.Query{Project: c.project}); err != nil || len(list) != c.n {
				t.Fatalf("%s lists %d threads, %v", c.project, len(list), err)
			}
		}
		onSlack := shared("Globex", "general")
		onSlack.Surface = &threads.Surface{Client: "app", Channel: "C01", TS: "1700.1"}
		sl, _ := start(t, again, "acme", onSlack, msg("dana", "from slack"))
		if _, err := again.Share("acme", sl.ID, sl.Version, "Field", "general", "", nil, "", nil, nil, "dana", t0); !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("a surface's thread moved: %v", err)
		}
		dm := shared("Globex", "general")
		dm.Visibility, dm.Surface = threads.Private, &threads.Surface{Client: "app", Channel: "D01", TS: "1700.2"}
		d, _ := start(t, again, "acme", dm, msg("dana", "a direct message"))
		if _, err := again.Share("acme", d.ID, d.Version, "Globex", "general", "", nil, "", nil, nil, "dana", t0); !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("a surface's private thread was made shared: %v", err)
		}
	})
}

// A thread's selected agents and mode are kept, and a record that holds an
// invalid selection is refused.
func TestRoutingIsKept(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		rec := shared("Globex", "general")
		rec.Agents, rec.Mode = []string{"scribe", "ignite"}, threads.ModeAll
		th, _ := start(t, s, "acme", rec, msg("dana", "both of you"))
		got, err := s.Update("acme", th.ID, th.Version, t0, func(t *threads.Thread) error {
			t.Agents, t.Mode = []string{"scribe", "ignite", "lead"}, threads.ModeBestFit
			return nil
		})
		if err != nil || got.Version != th.Version+1 {
			t.Fatalf("changing the selection = %+v, %v", got, err)
		}
		again, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if kept, _ := everything(t, again, "acme", th.ID); !reflect.DeepEqual(kept.Agents, []string{"scribe", "ignite", "lead"}) || kept.Mode != threads.ModeBestFit {
			t.Fatalf("read back, the selection is %v %q", kept.Agents, kept.Mode)
		}
		if _, err := again.Update("acme", th.ID, got.Version, t0, func(t *threads.Thread) error {
			t.Agents, t.Mode = []string{"a", "b", "c", "d"}, threads.ModeAll
			return nil
		}); !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("four agents all at once were kept: %v", err)
		}
	})
}
