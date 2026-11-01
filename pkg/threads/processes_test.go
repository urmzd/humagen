package threads_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

func messagesOf(t *testing.T, s *threads.Store, tenant, id string) []threads.Message {
	t.Helper()
	var all []threads.Message
	for after := ""; ; {
		page, next, err := s.Messages(tenant, id, after, 0)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, page...)
		if next == "" {
			return all
		}
		after = next
	}
}

// Two processes on one database, each with its own store, see each other's
// threads at once, and people posting to one conversation at the same time
// through either are each kept once, numbered without a gap or a repeat, in
// one line.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := threads.Open(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := threads.Open(b)
		if err != nil {
			t.Fatal(err)
		}
		in := shared("Globex", "renewals")
		in.Surface = &slackThread
		th, _ := start(t, one, "acme", in, msg("dana", "zephyr renewal"))
		if got, err := two.Get("acme", th.ID); err != nil || got.ID != th.ID {
			t.Fatalf("the other process reads %+v, %v", got, err)
		}
		if got, ok, err := two.BySurface("acme", slackThread); err != nil || !ok || got.ID != th.ID {
			t.Fatalf("the other process finds by surface %+v, %v, %v", got, ok, err)
		}
		if _, _, err := two.Start("acme", in, msg("dana", "again"), t0); !errors.Is(err, threads.ErrExists) {
			t.Fatalf("the surface's thread started again through the other process = %v", err)
		}
		if page, _, err := two.List("acme", threads.Query{Project: "Globex"}); err != nil || len(page) != 1 {
			t.Fatalf("the other process lists %+v, %v", page, err)
		}
		if hits, err := two.Search("acme", "zephyr", t0, 10, nil); err != nil || len(hits) != 1 {
			t.Fatalf("the other process searches %+v, %v", hits, err)
		}

		const posters = 16
		var wg sync.WaitGroup
		for i := range posters {
			s := []*threads.Store{one, two}[i%2]
			wg.Go(func() {
				m := msg(fmt.Sprintf("p%02d", i), fmt.Sprintf("word%02d", i))
				m.Via, m.SurfaceID = "acme-slack", fmt.Sprintf("1700.%02d", i)
				if _, created, err := s.Post("acme", th.ID, m, t0.Add(time.Duration(i)*time.Second)); err != nil || !created {
					t.Errorf("post %d: %v, %v", i, created, err)
				}
				// The same surface id again, through the other process, is
				// the same message.
				if got, created, err := []*threads.Store{two, one}[i%2].Post("acme", th.ID, m, t0); err != nil || created || got.SurfaceID != m.SurfaceID {
					t.Errorf("post %d again: %+v, %v, %v", i, got, created, err)
				}
			})
		}
		wg.Wait()
		for _, s := range []*threads.Store{one, two} {
			all := messagesOf(t, s, "acme", th.ID)
			if len(all) != posters+1 {
				t.Fatalf("%d messages, want %d", len(all), posters+1)
			}
			seen := map[string]bool{}
			for i, m := range all {
				want := ""
				if i > 0 {
					want = all[i-1].ID
				}
				if m.ID != threads.MessageID(i+1) || m.Prev != want || seen[m.By] {
					t.Fatalf("message %d = %+v", i, m)
				}
				seen[m.By] = true
			}
			got, err := s.Get("acme", th.ID)
			if err != nil || len(got.Participants) != posters+1 {
				t.Fatalf("%d participants, %v", len(got.Participants), err)
			}
		}
		if hits, err := one.Search("acme", "word07", t0, 10, nil); err != nil || len(hits) != 1 || hits[0].By != "p07" {
			t.Fatalf("search after the race = %+v, %v", hits, err)
		}
		sums, _, err := two.Summaries("acme", func(threads.Thread) bool { return true })
		if err != nil || len(sums) != 1 || sums[0].Messages != posters+1 {
			t.Fatalf("summaries = %+v, %v", sums, err)
		}

		// Edits through both at once each land: every agent joins, and
		// each title change reads the version before it.
		for i := range posters {
			s := []*threads.Store{one, two}[i%2]
			wg.Go(func() {
				if _, err := s.Join("acme", th.ID, []threads.Participant{{Kind: threads.Agent, Name: fmt.Sprintf("agent%02d", i)}}, t0); err != nil {
					t.Errorf("join %d: %v", i, err)
				}
			})
		}
		wg.Wait()
		got, err := one.Get("acme", th.ID)
		if err != nil || len(got.Participants) != 2*posters+1 {
			t.Fatalf("%d participants after the joins, %v", len(got.Participants), err)
		}

		// Threads started at once through both are counted once each.
		for i := range posters {
			s := []*threads.Store{one, two}[i%2]
			wg.Go(func() {
				if _, _, err := s.Start("acme", shared("Initech", "general"), msg("dana", "hi"), t0); err != nil {
					t.Errorf("start %d: %v", i, err)
				}
			})
		}
		wg.Wait()
		if page, _, err := one.List("acme", threads.Query{Project: "Initech"}); err != nil || len(page) != posters {
			t.Fatalf("Initech lists %d, %v", len(page), err)
		}
		if _, err := two.Get("other", th.ID); !errors.Is(err, threads.ErrNotFound) {
			t.Fatalf("another tenant reads %v", err)
		}
	})
}

// Records kept by an earlier release, a thread's record and its messages
// with no head or index, are converted once, though two processes open the
// store at the same time, and read the same after.
func TestTwoProcessesConvertOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		put := func(tenant, key string, v any) {
			t.Helper()
			raw, _ := json.Marshal(v)
			if err := a.Write(t.Context(), "threads", state.Put(tenant, key, raw)); err != nil {
				t.Fatal(err)
			}
		}
		var ids []string
		for i := range 6 {
			id := fmt.Sprintf("th-%016x", i+1)
			ids = append(ids, id)
			rec := threads.Thread{ID: id, Project: "Globex", Channel: "general", Visibility: threads.Shared, Version: 1, CreatedBy: "dana",
				Participants: []threads.Participant{{Kind: threads.Person, Name: "dana", Owner: true, At: t0}}, CreatedAt: t0, UpdatedAt: t0.Add(time.Duration(i) * time.Minute)}
			if i == 0 {
				rec.Surface = &slackThread
			}
			put("acme", "t:"+id, rec)
			for n := 1; n <= 3; n++ {
				m := threads.Message{ID: threads.MessageID(n), Thread: id, By: "dana", Text: fmt.Sprintf("old note %d of %d", n, i), Via: threads.ViaConsole, Mentions: []threads.Mention{}, At: t0}
				if n > 1 {
					m.Prev = threads.MessageID(n - 1)
				}
				if n == 2 {
					m.Via, m.SurfaceID = "acme-slack", "1699.1"
				}
				put("acme", "m:"+id+":"+state.Seq(int64(n)), m)
			}
		}
		put("globex", "t:"+ids[0], threads.Thread{ID: ids[0], Project: "", Channel: "general", Visibility: threads.Shared, Version: 1, CreatedBy: "zed",
			Participants: []threads.Participant{{Kind: threads.Person, Name: "zed", Owner: true, At: t0}}, CreatedAt: t0, UpdatedAt: t0})

		var wg sync.WaitGroup
		stores := make([]*threads.Store, 4)
		for i := range stores {
			wg.Go(func() {
				s, err := threads.Open([]state.Store{a, b}[i%2])
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
		page, _, err := stores[1].List("acme", threads.Query{Project: "Globex"})
		if err != nil || len(page) != len(ids) {
			t.Fatalf("Globex lists %d, %v", len(page), err)
		}
		for i, th := range page {
			if th.ID != ids[len(ids)-1-i] {
				t.Fatalf("list order: %d is %s", i, th.ID)
			}
		}
		if got, ok, err := stores[2].BySurface("acme", slackThread); err != nil || !ok || got.ID != ids[0] {
			t.Fatalf("by surface after converting = %+v, %v, %v", got, ok, err)
		}
		sums, _, err := stores[3].Summaries("acme", func(threads.Thread) bool { return true })
		if err != nil || len(sums) != len(ids) || sums[0].Messages != 3 || sums[0].First == "" {
			t.Fatalf("summaries after converting = %+v, %v", sums, err)
		}
		if hits, err := stores[0].Search("acme", "note 2 of 4", t0, 10, nil); err != nil || len(hits) != 1 || hits[0].Thread != ids[4] || hits[0].ID != "m-2" {
			t.Fatalf("search after converting = %+v, %v", hits, err)
		}
		if _, created, err := stores[1].Post("acme", ids[3], threads.Message{By: "ana", Text: "again", Via: "acme-slack", SurfaceID: "1699.1"}, t0); err != nil || created {
			t.Fatalf("a surface id kept before converting was posted again: %v, %v", created, err)
		}
		if m := post(t, stores[2], "acme", ids[3], msg("ana", "a new note"), t0.Add(time.Hour)); m.ID != "m-4" || m.Prev != "m-3" {
			t.Fatalf("the first post after converting = %+v", m)
		}
		if got, err := stores[0].Get("globex", ids[0]); err != nil || got.CreatedBy != "zed" {
			t.Fatalf("globex's thread = %+v, %v", got, err)
		}
		// Converting counted each thread once: the project holds as many as
		// were kept, and one more is counted on top.
		if _, _, err := stores[3].Start("acme", shared("Globex", "general"), msg("dana", "new"), t0); err != nil {
			t.Fatal(err)
		}
		rec, err := b.Lookup(t.Context(), "threads", "acme", "c:counts")
		if err != nil {
			t.Fatal(err)
		}
		var c struct {
			Total    int            `json:"total"`
			Projects map[string]int `json:"projects"`
		}
		if err := json.Unmarshal(rec.Value, &c); err != nil || c.Total != len(ids)+1 || c.Projects["Globex"] != len(ids)+1 {
			t.Fatalf("counts after converting = %+v, %v", c, err)
		}
		again, err := threads.Open(b)
		if err != nil {
			t.Fatal(err)
		}
		if page, _, err := again.List("acme", threads.Query{Project: "Globex"}); err != nil || len(page) != len(ids)+1 {
			t.Fatalf("after reopening Globex lists %d, %v", len(page), err)
		}
	})
}

// What a run's ending writes to its thread, the runs its message's tags
// started and the agents that answered, is written once: folding the same
// run again, through the other process, writes nothing.
func TestARefoldThroughTheOtherProcessWritesNothing(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := threads.Open(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := threads.Open(b)
		if err != nil {
			t.Fatal(err)
		}
		th, m := start(t, one, "acme", shared("Globex", "general"), threads.Message{By: "dana", Text: "@ignite", Mentions: []threads.Mention{{Kind: threads.Agent, Name: "ignite"}}})
		settled := []threads.Mention{{Kind: threads.Agent, Name: "ignite", Run: run}}
		answered := []threads.Participant{{Kind: threads.Agent, Name: "ignite"}}
		fold := func(s *threads.Store) {
			if _, err := s.Settle("acme", th.ID, m.ID, settled); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Join("acme", th.ID, answered, t0.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
		versions := func() [2]int64 {
			var out [2]int64
			for i, key := range []string{"t:" + th.ID, "m:" + th.ID + ":" + state.Seq(1)} {
				r, err := b.Lookup(t.Context(), "threads", "acme", key)
				if err != nil {
					t.Fatal(err)
				}
				out[i] = r.Version
			}
			return out
		}
		fold(one)
		before := versions()
		fold(two)
		if after := versions(); after != before {
			t.Fatalf("a refold wrote again: versions %v, then %v", before, after)
		}
	})
}
