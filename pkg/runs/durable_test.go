package runs

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"

	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

func TestRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		x, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		rootTag := envelope.Tag{ID: "root", Tenant: "acme", Agent: "lead", Origin: OriginAPI, Text: "go", At: t0,
			Meta: map[string]string{MetaThread: "th-1", MetaAsk: "what I typed"}}
		rootID := runtime.RunID("acme", rootTag.ID)
		if err := x.Record(rootID, rootTag, 2); err != nil {
			t.Fatal(err)
		}
		x.PinRequesters(rootID, []string{"ada", "slack:U1"})
		child, err := call.New(rootTag, call.Request{Key: "k1", Agent: "helper", Text: "help", Mode: call.Subagent, Lease: 500}, call.State{}, call.Limits{MaxDepth: 3, MaxFanOut: 3}, t0)
		if err != nil {
			t.Fatal(err)
		}
		childID := runtime.RunID("acme", child.ID)
		if err := x.Record(childID, child, 1); err != nil {
			t.Fatal(err)
		}
		x.Called(rootID, 500)
		x.SetTitle("acme", "th-1", "Release notes")
		record(t, x, "globex", "other", "lead", "api", time.Second, nil)
		end(t, x, "acme", rootID, time.Second, envelope.KindStarted, payload.Lifecycle{Attempt: 1})
		x.Spend(childID, 120)
		end(t, x, "acme", childID, 2*time.Second, envelope.KindCompleted, payload.Lifecycle{Text: "done"})
		c, _ := x.Get("acme", childID)
		x.SettleCall(c)

		// Before a flush only the records are written.
		early, _ := Open(st)
		if r, err := early.Get("acme", childID); err != nil || r.Status != Accepted {
			t.Fatalf("before a flush = %+v, %v", r, err)
		}
		if err := x.Flush(); err != nil {
			t.Fatal(err)
		}

		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{rootID, childID} {
			got, _ := again.Get("acme", id)
			want, _ := x.Get("acme", id)
			if !reflect.DeepEqual(got.Tag(), want.Tag()) || !reflect.DeepEqual(got.Requesters(), want.Requesters()) {
				t.Fatalf("%s: tag or requesters after restart = %+v %v, want %+v %v", id, got.Tag(), got.Requesters(), want.Tag(), want.Requesters())
			}
			got.tag, want.tag = envelope.Tag{}, envelope.Tag{}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s after restart = %+v\nwant %+v", id, got, want)
			}
		}
		r, _ := again.Get("acme", rootID)
		if r.Text != "what I typed" || r.Thread != "th-1" || r.Calls != 1 || r.Granted != 0 || r.Spent != 120 || r.Status != Running {
			t.Fatalf("root after restart = %+v", r)
		}
		if got := again.Active(); len(got) != 2 {
			t.Fatalf("active after restart = %v", got)
		}
		// What has not ended comes back with the tag it was accepted with,
		// which is where its routes are registered again from.
		if open := again.Unfinished(); len(open) != 2 || open[0].ID != rootID || !reflect.DeepEqual(open[0].Tag(), rootTag) {
			t.Fatalf("unfinished after restart = %+v", open)
		}
		if th := again.Threads("acme", 10); len(th) != 1 || th[0].Title != "Release notes" {
			t.Fatalf("threads after restart = %+v", th)
		}
		if l := again.List("acme", 10); len(l) != 2 || l[0].ID != childID {
			t.Fatalf("order after restart = %+v", l)
		}
		// A settled call is not settled twice after a restart.
		again.SettleCall(c)
		if r, _ := again.Get("acme", rootID); r.Spent != 120 {
			t.Fatalf("a call settled again after restart: spent %d", r.Spent)
		}
		if _, err := again.Get("acme", "other"); !errors.Is(err, ErrNotFound) {
			t.Fatal("another tenant's run is visible")
		}
	})
}

func TestFailedWrites(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	x, _ := Open(f)
	record(t, x, "acme", "r1", "lead", "api", 0, nil)
	f.Fail(statetest.ErrInjected)
	tag := envelope.Tag{ID: "r2", Tenant: "acme", Agent: "lead", Origin: "api", Text: "go", At: t0}
	if err := x.Record("r2", tag, 1); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Record = %v", err)
	}
	if _, err := x.Get("acme", "r2"); !errors.Is(err, ErrNotFound) {
		t.Fatal("a run that was not written was recorded")
	}
	// An event already happened: it is kept in memory and written later.
	end(t, x, "acme", "r1", time.Second, envelope.KindCompleted, payload.Lifecycle{Text: "ok"})
	if err := x.Flush(); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Flush = %v", err)
	}
	f.Fail(nil)
	if err := x.Flush(); err != nil {
		t.Fatal(err)
	}
	again, _ := Open(f)
	if r, _ := again.Get("acme", "r1"); r.Status != Completed || r.Answer != "ok" {
		t.Fatalf("after a failed flush and a good one = %+v", r)
	}
	if err := x.Record("r2", tag, 1); err != nil {
		t.Fatal(err)
	}
	if l := x.List("acme", 10); len(l) != 2 || l[0].ID != "r2" {
		t.Fatalf("after recording again = %+v", l)
	}
}
