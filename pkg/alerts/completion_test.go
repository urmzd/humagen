package alerts

import (
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"testing"
	"time"
)

func TestCompletionReturnsOriginalAlertsAfterRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		one, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if err := one.AddRule("acme", Rule{Name: "failure", Kind: RunFailed, Severity: Critical}); err != nil {
			t.Fatal(err)
		}
		run := ended("lead", runs.Failed, time.Second)
		first, err := one.EvaluateOnce(run, nil)
		if err != nil || len(first) != 1 {
			t.Fatalf("first = %v %v", first, err)
		}
		if _, err := one.Ack("acme", first[0].ID, "ada", t0); err != nil {
			t.Fatal(err)
		}
		two, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		again, err := two.EvaluateOnce(run, nil)
		if err != nil || len(again) != 1 || again[0].ID != first[0].ID {
			t.Fatalf("retry = %v %v", again, err)
		}
		all, err := two.Alerts("acme")
		if err != nil || len(all) != 1 {
			t.Fatalf("alerts = %v %v", all, err)
		}
	})
}
