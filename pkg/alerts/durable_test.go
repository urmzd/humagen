package alerts

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// must is a call's value, for tests whose engine writes nowhere that fails.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		e, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.AddRule("acme", Rule{Name: "fails", Kind: RunFailed, Severity: Critical, Notify: []string{"role:operator"}, CooldownSeconds: 3600}); err != nil {
			t.Fatal(err)
		}
		if err := e.AddRule("acme", Rule{Name: "slow", Kind: SlowRun, Severity: Warning, Threshold: 5}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.ReplaceRule("acme", Rule{Name: "slow", Kind: SlowRun, Severity: Warning, Threshold: 9}); err != nil {
			t.Fatal(err)
		}
		if err := e.AddRule("acme", Rule{Name: "gone", Kind: RunFailed, Severity: Warning}); err != nil {
			t.Fatal(err)
		}
		if err := e.RemoveRule("acme", "gone"); err != nil {
			t.Fatal(err)
		}
		raised, err := e.Evaluate(ended("lead", runs.Failed, time.Second), nil)
		if err != nil || len(raised) != 1 {
			t.Fatalf("Evaluate = %+v, %v", raised, err)
		}
		if _, err := e.Ack("acme", raised[0].ID, "ada", t0); err != nil {
			t.Fatal(err)
		}
		budget, ok, err := e.Raise("acme", "budget: x", Alert{Kind: Budget, Severity: Warning, Title: "near", At: t0})
		if err != nil || !ok {
			t.Fatalf("Raise = %v, %v", ok, err)
		}

		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(must(again.Rules("acme")), must(e.Rules("acme"))) || len(must(again.Rules("acme"))) != 2 {
			t.Fatalf("rules after restart = %+v", must(again.Rules("acme")))
		}
		if !reflect.DeepEqual(must(again.Alerts("acme")), must(e.Alerts("acme"))) {
			t.Fatalf("alerts after restart = %+v, want %+v", must(again.Alerts("acme")), must(e.Alerts("acme")))
		}
		if a := must(again.Alerts("acme"))[1]; a.AckedBy != "ada" || len(a.Notify) != 1 {
			t.Fatalf("the acknowledged alert after restart = %+v", a)
		}
		// The cooldown survives: the same failure an hour later is quiet.
		if got := must(again.Evaluate(ended("lead", runs.Failed, time.Second), nil)); len(got) != 0 {
			t.Fatalf("a rule cooling down raised %+v after a restart", got)
		}
		// A key still open is not raised again, and numbering carries on.
		if _, ok, _ := again.Raise("acme", "budget: x", Alert{Kind: Budget}); ok {
			t.Fatal("an open key was raised twice across a restart")
		}
		next, _, _ := again.Raise("acme", "budget: y", Alert{Kind: Budget, At: t0})
		if next.ID == budget.ID || next.ID == raised[0].ID {
			t.Fatalf("a restarted engine issued %q again", next.ID)
		}
		if len(must(again.Rules("globex"))) != 0 || len(must(again.Alerts("globex"))) != 0 {
			t.Fatal("another tenant sees acme's rules")
		}
	})
}

func TestRestartKeepsTheCap(t *testing.T) {
	st := state.NewMemory()
	e, _ := Open(st)
	for range maxAlerts + 5 {
		if _, _, err := e.Raise("acme", "k", Alert{Kind: Budget, At: t0}); err != nil {
			t.Fatal(err)
		}
		// Acknowledge it, so the next is raised.
		a := must(e.Alerts("acme"))[0]
		if _, err := e.Ack("acme", a.ID, "ada", t0); err != nil {
			t.Fatal(err)
		}
	}
	again, _ := Open(st)
	got := must(again.Alerts("acme"))
	if len(got) != maxAlerts || got[0].ID != must(e.Alerts("acme"))[0].ID || got[len(got)-1].ID != "al-6" {
		t.Fatalf("after restart: %d alerts, newest %s, oldest %s", len(got), got[0].ID, got[len(got)-1].ID)
	}
}

func TestFailedWriteRaisesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	e, _ := Open(f)
	if err := e.AddRule("acme", Rule{Name: "fails", Kind: RunFailed, Severity: Critical}); err != nil {
		t.Fatal(err)
	}
	f.Fail(statetest.ErrInjected)
	if got, err := e.Evaluate(ended("lead", runs.Failed, time.Second), nil); !errors.Is(err, statetest.ErrInjected) || len(got) != 0 {
		t.Fatalf("Evaluate = %+v, %v", got, err)
	}
	if _, _, err := e.Raise("acme", "k", Alert{Kind: Budget}); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Raise = %v", err)
	}
	if err := e.AddRule("acme", Rule{Name: "more", Kind: RunFailed, Severity: Warning}); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("AddRule = %v", err)
	}
	if err := e.RemoveRule("acme", "fails"); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("RemoveRule = %v", err)
	}
	if len(must(e.Alerts("acme"))) != 0 || len(must(e.Rules("acme"))) != 1 {
		t.Fatalf("after failed writes: %d alerts, %d rules", len(must(e.Alerts("acme"))), len(must(e.Rules("acme"))))
	}
	f.Fail(nil)
	// Nothing was raised, so nothing is cooling down and no id was used.
	got := must(e.Evaluate(ended("lead", runs.Failed, time.Second), nil))
	if len(got) != 1 || got[0].ID != "al-1" {
		t.Fatalf("after the store recovered = %+v", got)
	}
}
