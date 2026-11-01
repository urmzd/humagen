package alerts

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database, each with its own engine, read each
// other's rules and alerts at once; rules added through either at the same
// time are each kept; alerts raised through either are numbered without a
// gap or a repeat; and a key both raise at once, as two processes that both
// see a limit do, is raised once.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := Open(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := Open(b)
		if err != nil {
			t.Fatal(err)
		}
		engines := []*Engine{one, two}
		if err := one.AddRule("acme", Rule{Name: "fails", Kind: RunFailed, Severity: Critical}); err != nil {
			t.Fatal(err)
		}
		if got := must(two.Rules("acme")); len(got) != 1 || got[0].Name != "fails" {
			t.Fatalf("the other process reads %+v", got)
		}

		const n = 16
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() {
				if err := engines[i%2].AddRule("acme", Rule{Name: fmt.Sprintf("slow-%02d", i), Kind: SlowRun, Severity: Warning, Threshold: 1}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if got := must(one.Rules("acme")); len(got) != n+1 {
			t.Fatalf("%d rules, want %d", len(got), n+1)
		}

		var raised atomic.Int32
		for i := range n {
			wg.Go(func() {
				_, ok, err := engines[i%2].Raise("acme", "budget: tenant, spend, refused", Alert{Kind: Budget, Severity: Critical, Title: "refused", At: t0})
				if err != nil {
					t.Error(err)
				}
				if ok {
					raised.Add(1)
				}
			})
			wg.Go(func() {
				if _, _, err := engines[i%2].Raise("acme", fmt.Sprintf("storage: %02d", i), Alert{Kind: Budget, Severity: Critical, At: t0}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if raised.Load() != 1 {
			t.Fatalf("one key was raised %d times", raised.Load())
		}
		all := must(two.Alerts("acme"))
		if len(all) != n+1 {
			t.Fatalf("%d alerts, want %d", len(all), n+1)
		}
		for i, al := range all {
			if want := fmt.Sprintf("al-%d", n+1-i); al.ID != want {
				t.Fatalf("alert %d is %s, want %s", i, al.ID, want)
			}
		}

		// Acknowledged through one, the key is raised again through the
		// other.
		var refused Alert
		for _, al := range all {
			if strings.HasPrefix(al.Rule, "budget:") {
				refused = al
			}
		}
		if _, err := one.Ack("acme", refused.ID, "ada", t0); err != nil {
			t.Fatal(err)
		}
		if got := must(two.Alert("acme", refused.ID)); got.AckedBy != "ada" {
			t.Fatalf("the other process reads %+v", got)
		}
		if _, ok, err := two.Raise("acme", "budget: tenant, spend, refused", Alert{Kind: Budget, At: t0}); err != nil || !ok {
			t.Fatalf("Raise after an ack = %v, %v", ok, err)
		}

		// A failure rate both processes evaluate at once raises once.
		if err := two.AddRule("acme", Rule{Name: "rate", Kind: FailureRate, Severity: Warning, Threshold: 50, WindowSeconds: 60, MinRuns: 1}); err != nil {
			t.Fatal(err)
		}
		recent := func(string, string, string, time.Time) runs.Window { return runs.Window{Total: 2, Failed: 2} }
		var rated atomic.Int32
		for i := range n {
			wg.Go(func() {
				got, err := engines[i%2].Evaluate(ended("lead", runs.Failed, 0), recent)
				if err != nil {
					t.Error(err)
				}
				for _, al := range got {
					if al.Rule == "rate" {
						rated.Add(1)
					}
				}
			})
		}
		wg.Wait()
		if rated.Load() != 1 {
			t.Fatalf("a failure rate was raised %d times while open", rated.Load())
		}
		if len(must(one.Alerts("globex"))) != 0 || len(must(one.Rules("globex"))) != 0 {
			t.Fatal("another tenant sees acme's")
		}
	})
}

// Open alerts kept before open keys get them once, though two processes open
// the engine at the same time, and then hold their key quiet.
func TestTwoProcessesConvertOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		old := state.NewTable[stored](a, namespace)
		bt := old.Batch()
		bt.Put("acme", rulesKey, stored{Next: 4, Rules: []Rule{
			{Name: "spend", Kind: Spend, Severity: Warning, ThresholdMicros: 1, WindowSeconds: 60},
			{Name: "fails", Kind: RunFailed, Severity: Critical},
		}})
		for i, al := range []Alert{
			{Rule: "budget: x", Kind: Budget},
			{Rule: "spend", Kind: Spend, Agent: "lead"},
			{Rule: "fails", Kind: RunFailed},
			{Rule: "budget: y", Kind: Budget, AckedBy: "ada"},
		} {
			al.ID, al.At = fmt.Sprintf("al-%d", i+1), t0
			bt.Put("acme", alertKey(al.ID), stored{Alert: &storedAlert{Alert: al}})
		}
		if err := bt.Commit(); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		engines := make([]*Engine, 4)
		for i := range engines {
			wg.Go(func() {
				e, err := Open([]state.Store{a, b}[i%2])
				if err != nil {
					t.Error(err)
				}
				engines[i] = e
			})
		}
		wg.Wait()
		if t.Failed() {
			return
		}
		recs, err := a.Load(t.Context(), namespace)
		if err != nil {
			t.Fatal(err)
		}
		opens := 0
		for _, r := range recs {
			if strings.HasPrefix(r.Key, openPrefix) {
				opens++
			}
		}
		// The open budget key and the open spend rule; not the run failure
		// nor the acknowledged key.
		if opens != 2 {
			t.Fatalf("%d open keys after converting, want 2", opens)
		}
		if _, ok, _ := engines[1].Raise("acme", "budget: x", Alert{Kind: Budget}); ok {
			t.Fatal("a key open before converting was raised again")
		}
		if _, ok, _ := engines[2].Raise("acme", "budget: y", Alert{Kind: Budget}); !ok {
			t.Fatal("an acknowledged key was not raised again")
		}
		spent := func(string, string, string, time.Time) runs.Window { return runs.Window{Total: 1, Spent: 5} }
		if got := must(engines[3].Evaluate(ended("lead", runs.Completed, 0), spent)); len(got) != 0 {
			t.Fatalf("a spend rule with an open alert raised %+v", got)
		}
		if _, err := engines[0].Ack("acme", "al-1", "ada", t0); err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := engines[1].Raise("acme", "budget: x", Alert{Kind: Budget}); !ok {
			t.Fatal("a key acknowledged after converting was not raised again")
		}
	})
}
