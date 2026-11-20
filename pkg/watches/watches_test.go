package watches_test

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/watches"
)

var t0 = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

func open(t *testing.T) *watches.Store {
	t.Helper()
	s, err := watches.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func spec() watches.Spec {
	return watches.Spec{Owner: "okta|dana", Agent: "ops", Session: "s1", Project: "Field", Condition: "the deploy is green", How: "read the workflow status", Every: 10 * time.Minute, For: time.Hour}
}

func TestCreateValidates(t *testing.T) {
	s := open(t)
	for name, change := range map[string]func(*watches.Spec){
		"no condition":   func(sp *watches.Spec) { sp.Condition = "  " },
		"no owner":       func(sp *watches.Spec) { sp.Owner = "" },
		"no session":     func(sp *watches.Spec) { sp.Session = "" },
		"too often":      func(sp *watches.Spec) { sp.Every = time.Minute },
		"too rarely":     func(sp *watches.Spec) { sp.Every = 48 * time.Hour },
		"too long":       func(sp *watches.Spec) { sp.For = 8 * 24 * time.Hour },
		"shorter than 1": func(sp *watches.Spec) { sp.For = 5 * time.Minute },
		"long condition": func(sp *watches.Spec) { sp.Condition = string(make([]byte, watches.MaxCondition+1)) },
	} {
		sp := spec()
		change(&sp)
		if _, err := s.Create("acme", sp, t0); !errors.Is(err, watches.ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", name, err)
		}
	}
	sp := spec()
	sp.Every, sp.For = 0, 0
	w, err := s.Create("acme", sp, t0)
	if err != nil {
		t.Fatal(err)
	}
	if w.Every != watches.DefaultEvery || !w.Until.Equal(t0.Add(watches.DefaultFor)) || !w.Due(t0) || w.State != watches.Watching {
		t.Fatalf("defaults: %+v", w)
	}
}

func TestPersonLimit(t *testing.T) {
	s := open(t)
	for range watches.MaxPerPerson {
		if _, err := s.Create("acme", spec(), t0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Create("acme", spec(), t0); !errors.Is(err, watches.ErrLimit) {
		t.Fatalf("over the limit: %v", err)
	}
	other := spec()
	other.Owner = "okta|lee"
	if _, err := s.Create("acme", other, t0); err != nil {
		t.Fatalf("another person: %v", err)
	}
	if _, err := s.Create("globex", spec(), t0); err != nil {
		t.Fatalf("another tenant: %v", err)
	}
}

func TestCheckCycle(t *testing.T) {
	s := open(t)
	w, err := s.Create("acme", spec(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Begin("acme", w.ID, "r1", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Begin("acme", w.ID, "r2", t0); !errors.Is(err, watches.ErrBusy) {
		t.Fatalf("a second begin while one is in flight: %v", err)
	}
	if _, err := s.Settle("acme", w.ID, "other", watches.Outcome{}, t0); !errors.Is(err, watches.ErrBusy) {
		t.Fatalf("a run that is not the check: %v", err)
	}
	at := t0.Add(time.Minute)
	w, err = s.Settle("acme", w.ID, "r1", watches.Outcome{}, at)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != watches.Watching || w.Running != "" || !w.Next.Equal(at.Add(10*time.Minute)) || w.Due(at) {
		t.Fatalf("after a check that found nothing: %+v", w)
	}
	at = w.Next
	if _, err := s.Begin("acme", w.ID, "r2", at); err != nil {
		t.Fatal(err)
	}
	w, err = s.Settle("acme", w.ID, "r2", watches.Outcome{Met: true, Result: "Deploy is green"}, at)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != watches.Met || w.Result != "Deploy is green" || w.Checks != 2 {
		t.Fatalf("met: %+v", w)
	}
	if got, err := s.Get("acme", w.ID); err != nil || got.State != watches.Met {
		t.Fatalf("an ended watch is still read: %+v %v", got, err)
	}
	if active, _ := s.Active("acme"); len(active) != 0 {
		t.Fatalf("an ended watch is not active: %+v", active)
	}
	if _, err := s.Get("globex", w.ID); !errors.Is(err, watches.ErrNotFound) {
		t.Fatalf("another tenant reads it: %v", err)
	}
}

func TestExpiresAndFails(t *testing.T) {
	s := open(t)
	w, _ := s.Create("acme", spec(), t0)
	at := t0
	for w.State == watches.Watching {
		if _, err := s.Begin("acme", w.ID, "r", at); err != nil {
			t.Fatal(err)
		}
		var err error
		if w, err = s.Settle("acme", w.ID, "r", watches.Outcome{}, at); err != nil {
			t.Fatal(err)
		}
		at = w.Next
	}
	if w.State != watches.Expired || w.Checks != 6 {
		t.Fatalf("an hour at ten minutes: %+v", w)
	}

	w, _ = s.Create("acme", spec(), t0)
	for i := range watches.MaxFailures {
		if _, err := s.Begin("acme", w.ID, "r", w.Next); err != nil {
			t.Fatal(i, err)
		}
		w, _ = s.Abandon("acme", w.ID, "r", w.Next)
	}
	if w.State != watches.Failed {
		t.Fatalf("after %d failures: %+v", watches.MaxFailures, w)
	}
}

func TestStopAndPrune(t *testing.T) {
	s := open(t)
	w, _ := s.Create("acme", spec(), t0)
	if _, err := s.Begin("acme", w.ID, "r", t0); err != nil {
		t.Fatal(err)
	}
	w, err := s.Stop("acme", w.ID, t0)
	if err != nil || w.State != watches.Stopped {
		t.Fatalf("stop: %+v %v", w, err)
	}
	if _, err := s.Settle("acme", w.ID, "r", watches.Outcome{Met: true}, t0); !errors.Is(err, watches.ErrNotFound) {
		t.Fatalf("the check of a stopped watch: %v", err)
	}
	if n, err := s.Prune("acme", t0); err != nil || n != 0 {
		t.Fatalf("pruned too early: %d %v", n, err)
	}
	if n, err := s.Prune("acme", t0.Add(watches.Retention)); err != nil || n != 1 {
		t.Fatalf("prune: %d %v", n, err)
	}
	var tenants []string
	_ = s.Tenants(func(t string) error { tenants = append(tenants, t); return nil })
	if len(tenants) != 1 || tenants[0] != "acme" {
		t.Fatalf("tenants: %v", tenants)
	}
}
