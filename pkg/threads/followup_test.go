package threads_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

// A message posted while agents worked keeps its follow-up. One still to be
// carried out is pending, oldest first and across a restart; handling it
// keeps the decision, takes it off the index and is safe to repeat. A stop
// is posted handled, so it is never pending.
func TestAFollowupIsPendingUntilHandled(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		th, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "@ignite renew it"))
		queued := threads.Followup{Outcome: threads.FollowupQueue, Reason: "Adds a detail.", Waiting: []string{run}, Model: "anthropic/claude", Effort: "low"}
		q := post(t, s, "acme", th.ID, threads.Message{By: "dana", Text: "and the invoice", Followup: &queued}, t0.Add(time.Minute))
		stopped := threads.Followup{Outcome: threads.FollowupStop, Reason: "not needed", Waiting: []string{run}, Handled: true}
		post(t, s, "acme", th.ID, threads.Message{By: "ana", Text: "stop: not needed", Followup: &stopped}, t0.Add(2*time.Minute))
		replaced := threads.Followup{Outcome: threads.FollowupReplace, Reason: "A new task.", Waiting: []string{run}}
		r := post(t, s, "acme", th.ID, threads.Message{By: "dana", Text: "replace: the other one", Followup: &replaced}, t0.Add(3*time.Minute))
		post(t, s, "acme", th.ID, msg("dana", "plain"), t0.Add(4*time.Minute))

		again, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		pending, err := again.Pending("acme", th.ID, 0)
		if err != nil || len(pending) != 2 || pending[0].ID != q.ID || pending[1].ID != r.ID {
			t.Fatalf("pending = %+v, %v", pending, err)
		}
		if f := pending[0].Followup; f.Model != "anthropic/claude" || f.Effort != "low" || f.Waiting[0] != run {
			t.Fatalf("the follow-up read back = %+v", f)
		}
		handled, err := again.HandleFollowup("acme", th.ID, q.ID)
		if err != nil || !handled.Followup.Handled || handled.Followup.Outcome != threads.FollowupQueue || handled.Followup.Reason != "Adds a detail." {
			t.Fatalf("handled = %+v, %v", handled, err)
		}
		if twice, err := again.HandleFollowup("acme", th.ID, q.ID); err != nil || !twice.Followup.Handled {
			t.Fatalf("handled twice = %+v, %v", twice, err)
		}
		if pending, _ := again.Pending("acme", th.ID, 0); len(pending) != 1 || pending[0].ID != r.ID {
			t.Fatalf("pending after handling = %+v", pending)
		}
		if m, _ := again.Message("acme", th.ID, q.ID); m.Followup == nil || !m.Followup.Handled {
			t.Fatalf("the message after handling = %+v", m)
		}
		if _, err := again.HandleFollowup("acme", th.ID, "m-5"); !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("handling a message that is no follow-up = %v", err)
		}
		if _, err := again.HandleFollowup("acme", th.ID, "m-9"); !errors.Is(err, threads.ErrNotFound) {
			t.Fatalf("handling a message that is not there = %v", err)
		}
		if _, err := again.Pending("globex", th.ID, 0); !errors.Is(err, threads.ErrNotFound) {
			t.Fatalf("another tenant's pending follow-ups = %v", err)
		}
	})
}

// A follow-up is checked as it is posted.
func TestFollowupRules(t *testing.T) {
	s := threads.NewStore()
	th, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "x"))
	long := make([]string, threads.MaxFollowupWaiting+1)
	for i := range long {
		long[i] = run
	}
	for name, f := range map[string]threads.Followup{
		"an unknown outcome":     {Outcome: "pause", Reason: "x", Waiting: []string{run}},
		"waiting on nothing":     {Outcome: threads.FollowupQueue, Reason: "x"},
		"waiting on too many":    {Outcome: threads.FollowupQueue, Reason: "x", Waiting: long},
		"a reason of two lines":  {Outcome: threads.FollowupStop, Reason: "a\nb", Waiting: []string{run}},
		"a run id with a space":  {Outcome: threads.FollowupStop, Reason: "x", Waiting: []string{"r 1"}},
		"an empty run id":        {Outcome: threads.FollowupStop, Reason: "x", Waiting: []string{""}},
		"an effort that is long": {Outcome: threads.FollowupQueue, Reason: "x", Waiting: []string{run}, Effort: string(make([]byte, 40))},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := s.Post("acme", th.ID, threads.Message{By: "dana", Text: "x", Followup: &f}, t0); !errors.Is(err, threads.ErrInvalid) {
				t.Fatalf("post = %v, want ErrInvalid", err)
			}
		})
	}
	f := threads.Followup{Outcome: threads.FollowupQueue, Reason: "x", Waiting: []string{run}}
	if _, _, err := s.Post("acme", th.ID, threads.Message{By: "dana", Text: "x", Followup: &f, Answers: &threads.AnswerRef{Run: run, Question: "q1"}}, t0); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("an answer with a follow-up = %v", err)
	}
}

// Deleting an archived conversation takes its pending follow-ups with it.
func TestPurgeForgetsPendingFollowups(t *testing.T) {
	st := state.NewMemory()
	s, err := threads.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	th, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "x"))
	f := threads.Followup{Outcome: threads.FollowupQueue, Reason: "x", Waiting: []string{run}}
	post(t, s, "acme", th.ID, threads.Message{By: "dana", Text: "y", Followup: &f}, t0)
	indexed := func() int {
		recs, err := st.Load(t.Context(), "threads")
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, r := range recs {
			if strings.HasPrefix(r.Key, "f:") {
				n++
			}
		}
		return n
	}
	if indexed() != 1 {
		t.Fatal("a pending follow-up is not indexed")
	}
	if _, err := s.SetArchive("acme", th.ID, &threads.Archive{By: "dana", At: t0, DeleteAfter: t0, Requested: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Purge("acme", th.ID, "admin", t0); err != nil {
		t.Fatal(err)
	}
	if n := indexed(); n != 0 {
		t.Fatalf("%d follow-ups indexed after deletion", n)
	}
}
