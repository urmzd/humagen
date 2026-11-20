package alerts

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

func TestRetentionPreservesUnresolvedAndSnoozedAlerts(t *testing.T) {
	db := state.NewMemory()
	e, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	open, err := e.CreateSession("acme", Alert{Session: "s", Title: "Still needs answer", Priority: Normal, At: now})
	if err != nil {
		t.Fatal(err)
	}
	snoozed, err := e.CreateSession("acme", Alert{Session: "s", Title: "Later", Priority: Normal, At: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.ChangeState("acme", snoozed.ID, "ana", Snoozed, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	for range maxAlerts + 5 {
		a, err := e.CreateSession("acme", Alert{Session: "s", Title: "Resolved history", Priority: Low, At: now})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.Ack("acme", a.ID, "ana", now); err != nil {
			t.Fatal(err)
		}
	}
	again, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{open.ID, snoozed.ID} {
		a, err := again.Alert("acme", id)
		if err != nil || !a.Open() {
			t.Fatalf("unresolved attention evicted: %+v %v", a, err)
		}
	}
	all, err := again.Alerts("acme")
	if err != nil || len(all) != maxAlerts {
		t.Fatalf("retained count = %d, %v", len(all), err)
	}
}

func TestFullUnresolvedCapacityRefusesWithoutEviction(t *testing.T) {
	e := NewEngine()
	w, err := e.raising("acme")
	if err != nil {
		t.Fatal(err)
	}
	for range maxAlerts {
		if err := w.add(Alert{Session: "s", Title: "Needs answer", State: OpenState}, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.commit(); err != nil {
		t.Fatal(err)
	}
	_, err = e.CreateSession("acme", Alert{Session: "s", Title: "Another", Priority: Normal, At: time.Now()})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("full capacity = %v", err)
	}
	if a, err := e.Alert("acme", "al-1"); err != nil || !a.Open() {
		t.Fatalf("old attention evicted: %+v %v", a, err)
	}
	if _, err := e.Ack("acme", "al-1", "ana", time.Now()); err != nil {
		t.Fatal(err)
	}
	a, err := e.CreateSession("acme", Alert{Session: "s", Title: "Another", Priority: Normal, At: time.Now()})
	if err != nil || a.ID != "al-1001" {
		t.Fatalf("capacity was not reclaimed: %+v %v", a, err)
	}
}

func TestSessionAlertLifecycleSurvivesReopen(t *testing.T) {
	db := state.NewMemory()
	e, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	a, err := e.CreateSession("acme", Alert{Session: "session-1", Title: "Answer needed", Priority: Normal, Who: []string{"ana"}, At: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Alert("other", a.ID); err == nil {
		t.Fatal("cross-tenant alert exposed")
	}
	until := now.Add(time.Hour)
	a, err = e.ChangeState("acme", a.ID, "ana", Snoozed, until, now)
	if err != nil {
		t.Fatal(err)
	}
	e, err = Open(db)
	if err != nil {
		t.Fatal(err)
	}
	a, err = e.Alert("acme", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.View(now).State != Snoozed || a.View(until).State != OpenState {
		t.Fatal("snooze did not survive reopening or expire")
	}
	a, err = e.ChangeState("acme", a.ID, "ana", Resolved, time.Time{}, until)
	if err != nil {
		t.Fatal(err)
	}
	if a.Open() || a.ResolvedBy != "ana" {
		t.Fatal("not resolved")
	}
	a, err = e.ChangeState("acme", a.ID, "ana", OpenState, time.Time{}, until)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Open() || a.AckedBy != "" || !a.ResolvedAt.IsZero() {
		t.Fatal("undo retained resolution")
	}
}

func TestSessionAlertValidationAndLegacyDefaults(t *testing.T) {
	e := NewEngine()
	now := time.Now().UTC()
	for _, a := range []Alert{
		{Title: "missing session", Priority: Normal, At: now},
		{Session: "s", Title: "bad priority", Priority: "critical", At: now},
		{Session: "s", Title: "bad action", Priority: Normal, Action: "delete", At: now},
	} {
		if _, err := e.CreateSession("acme", a); err == nil {
			t.Fatalf("accepted %+v", a)
		}
	}
	a, err := e.CreateSession("acme", Alert{Session: "s", Title: "valid", Priority: Low, At: now})
	if err != nil {
		t.Fatal(err)
	}
	for _, until := range []time.Time{now, now.Add(-time.Minute), now.Add(31 * 24 * time.Hour)} {
		if _, err = e.ChangeState("acme", a.ID, "ana", Snoozed, until, now); err == nil {
			t.Fatal("accepted invalid snooze")
		}
	}
	if got := (Alert{Severity: Critical}).View(now); got.Priority != Urgent || got.State != OpenState {
		t.Fatalf("legacy defaults %+v", got)
	}
	if got := (Alert{AckedBy: "ana", AckedAt: now}).View(now); got.State != Resolved || got.ResolvedBy != "ana" {
		t.Fatal("legacy resolution missing")
	}
}

func TestSessionAlertsConcurrentProcesses(t *testing.T) {
	db := state.NewMemory()
	one, _ := Open(db)
	two, _ := Open(db)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			e := one
			if i%2 == 0 {
				e = two
			}
			if _, err := e.CreateSession("acme", Alert{Session: "s", Title: "Question", Priority: Normal, At: time.Now()}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	all, err := one.Alerts("acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 20 {
		t.Fatalf("lost alert: %d", len(all))
	}
}

func TestProjectedSessionAlertReplayDoesNotReopenResolved(t *testing.T) {
	e := NewEngine()
	now := time.Now().UTC()
	in := Alert{Session: "s", Title: "Approval needed", Priority: Normal, At: now}
	a, err := e.ProjectSession("acme", "question-1", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.ChangeState("acme", a.ID, "ana", Resolved, time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	replay, err := e.ProjectSession("acme", "question-1", in)
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != a.ID || replay.Open() {
		t.Fatalf("completion replay raised attention again: %+v", replay)
	}
	all, err := e.Alerts("acme")
	if err != nil || len(all) != 1 {
		t.Fatalf("replay duplicated alert: %v %v", all, err)
	}
}
