package background_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/background"
)

var t0 = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

func open(t *testing.T) *background.Store {
	t.Helper()
	s, err := background.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func watch() background.Spec {
	return background.Spec{Kind: background.Watch, Owner: "okta|dana", Agent: "ops", Session: "s1", Project: "Field", Text: "the deploy is green", How: "read the workflow status", Every: 10 * time.Minute, For: time.Hour}
}

func goal() background.Spec {
	return background.Spec{Kind: background.Goal, Owner: "okta|dana", Agent: "ops", Session: "s1", Project: "Field", Text: "every test passes", Turns: 3, CreatedBy: "r0", Thread: "s1"}
}

func task() background.Spec {
	return background.Spec{Kind: background.Task, Owner: "okta|dana", Agent: "ops", Session: "s1", Project: "Field", Text: "find every caller of Open"}
}

func TestCreateValidates(t *testing.T) {
	s := open(t)
	for name, sp := range map[string]background.Spec{
		"no kind":          func() background.Spec { sp := watch(); sp.Kind = ""; return sp }(),
		"no text":          func() background.Spec { sp := watch(); sp.Text = " "; return sp }(),
		"no owner":         func() background.Spec { sp := watch(); sp.Owner = ""; return sp }(),
		"watch too often":  func() background.Spec { sp := watch(); sp.Every = time.Minute; return sp }(),
		"watch too long":   func() background.Spec { sp := watch(); sp.For = 8 * 24 * time.Hour; return sp }(),
		"goal interval":    func() background.Spec { sp := goal(); sp.Every = time.Hour; return sp }(),
		"goal too many":    func() background.Spec { sp := goal(); sp.Turns = background.MaxTurns + 1; return sp }(),
		"goal no run":      func() background.Spec { sp := goal(); sp.CreatedBy = ""; return sp }(),
		"task with a span": func() background.Spec { sp := task(); sp.For = time.Hour; return sp }(),
		"long text":        func() background.Spec { sp := task(); sp.Text = strings.Repeat("x", background.MaxText+1); return sp }(),
	} {
		if _, err := s.Create("acme", sp, t0); !errors.Is(err, background.ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", name, err)
		}
	}
	sp := watch()
	sp.Every, sp.For = 0, 0
	w, err := s.Create("acme", sp, t0)
	if err != nil {
		t.Fatal(err)
	}
	if w.Every != background.DefaultEvery || !w.Until.Equal(t0.Add(background.DefaultWatch)) || !w.Due(t0) || w.State != background.Active {
		t.Fatalf("watch defaults: %+v", w)
	}
}

func TestPersonLimit(t *testing.T) {
	s := open(t)
	for range background.MaxPerPerson {
		if _, err := s.Create("acme", task(), t0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Create("acme", watch(), t0); !errors.Is(err, background.ErrLimit) {
		t.Fatalf("over the limit: %v", err)
	}
	other := watch()
	other.Owner = "okta|lee"
	if _, err := s.Create("acme", other, t0); err != nil {
		t.Fatalf("another person: %v", err)
	}
	if _, err := s.Create("globex", watch(), t0); err != nil {
		t.Fatalf("another tenant: %v", err)
	}
}

func TestWatchCycle(t *testing.T) {
	s := open(t)
	w, err := s.Create("acme", watch(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Begin("acme", w.ID, "r1", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Begin("acme", w.ID, "r2", t0); !errors.Is(err, background.ErrBusy) {
		t.Fatalf("a second begin while one is in flight: %v", err)
	}
	if got, err := s.ByRun("acme", "r1"); err != nil || got.ID != w.ID {
		t.Fatalf("by run: %+v %v", got, err)
	}
	if _, err := s.Settle("acme", w.ID, "other", background.Outcome{}, t0); !errors.Is(err, background.ErrBusy) {
		t.Fatalf("a run that is not the one in flight: %v", err)
	}
	at := t0.Add(time.Minute)
	w, err = s.Settle("acme", w.ID, "r1", background.Outcome{}, at)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != background.Active || w.Running != "" || w.Last != "r1" || !w.Next.Equal(at.Add(10*time.Minute)) || w.Due(at) {
		t.Fatalf("after a check that found nothing: %+v", w)
	}
	if _, err := s.ByRun("acme", "r1"); !errors.Is(err, background.ErrNotFound) {
		t.Fatalf("a settled run is still indexed: %v", err)
	}
	at = w.Next
	if _, err := s.Begin("acme", w.ID, "r2", at); err != nil {
		t.Fatal(err)
	}
	w, err = s.Settle("acme", w.ID, "r2", background.Outcome{Done: true, Result: "Deploy is green"}, at)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != background.Done || w.Result != "Deploy is green" || w.Runs != 2 {
		t.Fatalf("done: %+v", w)
	}
	if got, err := s.Get("acme", w.ID); err != nil || got.State != background.Done {
		t.Fatalf("an ended task is still read: %+v %v", got, err)
	}
	if act, _ := s.Active("acme"); len(act) != 0 {
		t.Fatalf("an ended task is not active: %+v", act)
	}
	if ended, _ := s.Ended("acme"); len(ended) != 1 {
		t.Fatalf("ended: %+v", ended)
	}
	if _, err := s.Get("globex", w.ID); !errors.Is(err, background.ErrNotFound) {
		t.Fatalf("another tenant reads it: %v", err)
	}
}

// A goal's first turn is the run that set it; each later turn is due as the
// last ends, until it is met, blocked, or out of turns.
func TestGoalTurns(t *testing.T) {
	s := open(t)
	g, err := s.Create("acme", goal(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if g.Running != "r0" || g.Runs != 1 || g.Thread != "s1" || g.Due(t0) {
		t.Fatalf("a new goal waits on the run that set it: %+v", g)
	}
	if got, err := s.ByRun("acme", "r0"); err != nil || got.ID != g.ID {
		t.Fatalf("the setting run is indexed: %+v %v", got, err)
	}
	g, _ = s.Settle("acme", g.ID, "r0", background.Outcome{Result: "Fixed two of five"}, t0)
	if !g.Due(t0) || g.Result != "Fixed two of five" {
		t.Fatalf("the next turn is due at once: %+v", g)
	}
	for _, run := range []string{"r1", "r2"} {
		if _, err := s.Begin("acme", g.ID, run, t0); err != nil {
			t.Fatal(run, err)
		}
		g, _ = s.Settle("acme", g.ID, run, background.Outcome{}, t0)
	}
	if g.State != background.Expired || g.Runs != 3 {
		t.Fatalf("three turns: %+v", g)
	}

	g, _ = s.Create("acme", goal(), t0)
	g, _ = s.Settle("acme", g.ID, "r0", background.Outcome{Blocked: true, Result: "No access to the repo"}, t0)
	if g.State != background.Failed || g.Result != "No access to the repo" {
		t.Fatalf("blocked: %+v", g)
	}
}

func TestTaskRunsOnce(t *testing.T) {
	s := open(t)
	k, _ := s.Create("acme", task(), t0)
	if _, err := s.Begin("acme", k.ID, "r1", t0); err != nil {
		t.Fatal(err)
	}
	k, _ = s.Abandon("acme", k.ID, "r1", t0)
	if k.State != background.Failed {
		t.Fatalf("a task's failed run ends it: %+v", k)
	}
	k, _ = s.Create("acme", task(), t0)
	_, _ = s.Begin("acme", k.ID, "r2", t0)
	k, _ = s.Settle("acme", k.ID, "r2", background.Outcome{Done: true, Result: "12 callers"}, t0)
	if k.State != background.Done || k.Result != "12 callers" {
		t.Fatalf("a task's run finishing: %+v", k)
	}
}

func TestExpiresAndFails(t *testing.T) {
	s := open(t)
	w, _ := s.Create("acme", watch(), t0)
	at := t0
	for w.State == background.Active {
		if _, err := s.Begin("acme", w.ID, "r", at); err != nil {
			t.Fatal(err)
		}
		var err error
		if w, err = s.Settle("acme", w.ID, "r", background.Outcome{}, at); err != nil {
			t.Fatal(err)
		}
		at = w.Next
	}
	if w.State != background.Expired || w.Runs != 6 {
		t.Fatalf("an hour at ten minutes: %+v", w)
	}

	w, _ = s.Create("acme", watch(), t0)
	for i := range background.MaxFailures {
		if _, err := s.Begin("acme", w.ID, "r", w.Next); err != nil {
			t.Fatal(i, err)
		}
		w, _ = s.Abandon("acme", w.ID, "r", w.Next)
	}
	if w.State != background.Failed {
		t.Fatalf("after %d failures: %+v", background.MaxFailures, w)
	}
}

func TestStopAndPrune(t *testing.T) {
	s := open(t)
	w, _ := s.Create("acme", watch(), t0)
	if _, err := s.Begin("acme", w.ID, "r", t0); err != nil {
		t.Fatal(err)
	}
	w, err := s.Stop("acme", w.ID, t0)
	if err != nil || w.State != background.Stopped || w.Running != "" {
		t.Fatalf("stop: %+v %v", w, err)
	}
	if _, err := s.ByRun("acme", "r"); !errors.Is(err, background.ErrNotFound) {
		t.Fatalf("a stopped task's run is still indexed: %v", err)
	}
	if _, err := s.Settle("acme", w.ID, "r", background.Outcome{Done: true}, t0); !errors.Is(err, background.ErrNotFound) {
		t.Fatalf("the run of a stopped task: %v", err)
	}
	if n, err := s.Prune("acme", t0); err != nil || n != 0 {
		t.Fatalf("pruned too early: %d %v", n, err)
	}
	if n, err := s.Prune("acme", t0.Add(background.Retention)); err != nil || n != 1 {
		t.Fatalf("prune: %d %v", n, err)
	}
	var tenants []string
	_ = s.Tenants(func(t string) error { tenants = append(tenants, t); return nil })
	if len(tenants) != 1 || tenants[0] != "acme" {
		t.Fatalf("tenants: %v", tenants)
	}
}
