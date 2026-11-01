package runs

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"

	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		x, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		rootTag := wire.NewEvent(wire.Raise{ID: "root", Tenant: "acme", Agent: "lead", Origin: OriginAPI, Text: "go", At: t0, Context: map[string]string{MetaThread: "th-1", MetaAsk: "what I typed", MetaRouted: "best_fit"}})
		rootID := runtime.RunID("acme", rootTag.ID)
		if err := x.Record(rootID, rootTag, 2); err != nil {
			t.Fatal(err)
		}
		x.PinRequesters("acme", rootID, []string{"ada", "slack:U1"})
		child, err := call.New(rootTag, call.Request{Key: "k1", Agent: "helper", Text: "help", Mode: call.Subagent, Lease: 500}, call.State{}, call.Limits{MaxDepth: 3, MaxFanOut: 3}, t0)
		if err != nil {
			t.Fatal(err)
		}
		childID := runtime.RunID("acme", child.ID)
		if err := x.Record(childID, child, 1); err != nil {
			t.Fatal(err)
		}
		x.Called("acme", rootID, 500)
		x.SetTitle("acme", "th-1", "Release notes")
		record(t, x, "globex", "other", "lead", "api", time.Second, nil)
		end(t, x, "acme", rootID, time.Second, event.TypeStarted, wire.Lifecycle{Attempt: 1})
		x.Spend("acme", childID, 120)
		end(t, x, "acme", childID, 2*time.Second, event.TypeCompleted, wire.Lifecycle{Text: "done"})
		c, _ := x.Get("acme", childID)
		x.SettleCall(c)

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
			got.tag, want.tag = event.Envelope{}, event.Envelope{}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s after restart = %+v\nwant %+v", id, got, want)
			}
		}
		r, _ := again.Get("acme", rootID)
		if r.Text != "what I typed" || r.Thread != "th-1" || r.Routed != "best_fit" || r.Calls != 1 || r.Granted != 0 || r.Spent != 120 || r.Status != Running {
			t.Fatalf("root after restart = %+v", r)
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
	tag := wire.NewEvent(wire.Raise{ID: "r2", Tenant: "acme", Agent: "lead", Origin: "api", Text: "go", At: t0})
	if err := x.Record("r2", tag, 1); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Record = %v", err)
	}
	if _, err := x.Get("acme", "r2"); !errors.Is(err, ErrNotFound) {
		t.Fatal("a run that was not written was recorded")
	}
	// An event that cannot be written is not folded: the run is as it was,
	// and the event is folded when it is delivered again.
	if _, ended := end(t, x, "acme", "r1", time.Second, event.TypeCompleted, wire.Lifecycle{Text: "ok"}); ended {
		t.Fatal("an ending that was not written was reported")
	}
	if r, _ := x.Get("acme", "r1"); r.Status != Accepted {
		t.Fatalf("after a failed write = %+v", r)
	}
	f.Fail(nil)
	if _, ended := end(t, x, "acme", "r1", time.Second, event.TypeCompleted, wire.Lifecycle{Text: "ok"}); !ended {
		t.Fatal("the event delivered again did not end the run")
	}
	again, _ := Open(f)
	if r, _ := again.Get("acme", "r1"); r.Status != Completed || r.Answer != "ok" {
		t.Fatalf("after a failed write and a good one = %+v", r)
	}
	if err := x.Record("r2", tag, 1); err != nil {
		t.Fatal(err)
	}
	if l := x.List("acme", 10); len(l) != 2 || l[0].ID != "r2" {
		t.Fatalf("after recording again = %+v", l)
	}
}
