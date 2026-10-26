package audit

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestAttemptsKeepUnknownCompletionAndSurviveRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		a, err := s.Begin("acme", "ada", Change{Action: "memory.forget", Resource: "memory", ID: "m1", Project: "Field", Before: map[string]string{"text": "Before", "api_key": "secret-value"}}, now)
		if err != nil {
			t.Fatal(err)
		}
		replay, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := replay.List("acme", Query{From: now.Add(-time.Second), To: now.Add(time.Hour)}, nil)
		if err != nil || len(rows) != 1 || rows[0].Outcome != Pending || rows[0].Before["api_key"] != "[redacted]" {
			t.Fatalf("replay: %+v %v", rows, err)
		}
		if err := replay.Finish("acme", a.ID, nil, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := replay.Finish("acme", a.ID, errors.New("different"), now.Add(time.Second)); !errors.Is(err, ErrConflict) {
			t.Fatalf("outcome overwritten: %v", err)
		}
		rows, err = replay.List("acme", Query{From: now.Add(-time.Second), To: now.Add(time.Hour)}, nil)
		if err != nil || rows[0].Outcome != Succeeded {
			t.Fatalf("finished: %+v %v", rows, err)
		}
		hidden, _ := replay.List("acme", Query{From: now.Add(-time.Second), To: now.Add(time.Hour)}, func(Record) bool { return false })
		other, _ := replay.List("other", Query{From: now.Add(-time.Second), To: now.Add(time.Hour)}, nil)
		if len(hidden) != 0 || len(other) != 0 {
			t.Fatal("audit leaked across boundary")
		}
	})
}

func TestAuditWriteFailuresLeaveAttemptPending(t *testing.T) {
	st := statetest.NewFaulty(state.NewMemory())
	s, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	a, err := s.Begin("acme", "ada", Change{Action: "skill.publish", Resource: "skill", ID: "review"}, now)
	if err != nil {
		t.Fatal(err)
	}
	st.Fail(errors.New("disk full"))
	if err := s.Finish("acme", a.ID, nil, now.Add(time.Second)); err == nil {
		t.Fatal("completion accepted despite failed durability")
	}
	rows, _ := s.List("acme", Query{From: now.Add(-time.Second), To: now.Add(time.Hour)}, nil)
	if len(rows) != 1 || rows[0].Outcome != Pending {
		t.Fatalf("completion fabricated: %+v", rows)
	}
	if _, err := s.Begin("acme", "ada", Change{Action: "skill.publish", Resource: "skill", ID: "another"}, now); err == nil {
		t.Fatal("unrecorded attempt accepted")
	}
}

func TestRetentionKeepsUniqueIDsAndVisibleWindow(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		first, err := s.Begin("acme", "ada", Change{Action: "source.create", Resource: "source", ID: "visible"}, now)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Begin("acme", "ada", Change{Action: "source.create", Resource: "source", ID: "hidden"}, now.Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		q := Query{From: now.Add(-time.Second), To: now.Add(time.Hour), Limit: 1}
		rows, err := s.List("acme", q, func(r Record) bool { return r.Change.ID == "visible" })
		if err != nil || len(rows) != 1 || rows[0].ID != first.ID {
			t.Fatalf("visible limit: %+v %v", rows, err)
		}
		if err := s.Retain("acme", now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		s, err = Open(st)
		if err != nil {
			t.Fatal(err)
		}
		next, err := s.Begin("acme", "ada", Change{Action: "source.create", Resource: "source", ID: "next"}, now.Add(2*time.Second))
		if err != nil || next.ID == first.ID {
			t.Fatalf("reused audit id: %+v %v", next, err)
		}
		if _, err := s.List("acme", Query{From: now, To: now.Add(MaxWindow + time.Second)}, nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unbounded window: %v", err)
		}
	})
}

func TestPublicAuditIDsDoNotExposeHiddenRecordSequence(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		for _, subject := range []string{"reader", "hidden-owner", "reader"} {
			_, err := s.Begin("acme", subject, Change{Action: "memory.forget", Resource: "memory", ID: "note", Subject: subject}, now)
			if err != nil {
				t.Fatal(err)
			}
		}
		rows, err := s.List("acme", Query{From: now.Add(-time.Second), To: now.Add(time.Hour)}, func(r Record) bool { return r.Subject == "reader" })
		if err != nil || len(rows) != 2 {
			t.Fatalf("visible rows: %d %v", len(rows), err)
		}
		opaque := regexp.MustCompile(`^a_[0-9a-f]{32}$`)
		for _, row := range rows {
			if !opaque.MatchString(row.ID) {
				t.Fatalf("public ID exposes an audit sequence: %q", row.ID)
			}
		}
		if rows[0].ID == rows[1].ID {
			t.Fatal("audit IDs reused")
		}
		replay, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		again, err := replay.List("acme", Query{From: now.Add(-time.Second), To: now.Add(time.Hour)}, func(r Record) bool { return r.Subject == "reader" })
		if err != nil || len(again) != 2 || again[0].ID != rows[0].ID || again[1].ID != rows[1].ID {
			t.Fatal("opaque identity or ordering changed after restart")
		}
	})
}

func TestLegacyAuditRecordsRemainReadableWithOpaqueNewIDs(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		now := time.Now().UTC()
		legacy := Record{ID: "a00000000000000000007", Actor: "ada", At: now, Outcome: Pending, Change: Change{Action: "source.create", Resource: "source", ID: "legacy"}}
		raw, err := json.Marshal(legacy)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Write(context.Background(), namespace, state.Put("acme", legacy.ID, raw)); err != nil {
			t.Fatal(err)
		}
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Finish("acme", legacy.ID, nil, now); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Begin("acme", "ada", Change{Action: "source.create", Resource: "source", ID: "new"}, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		replay, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := replay.List("acme", Query{From: now.Add(-time.Second), To: now.Add(time.Hour)}, nil)
		if err != nil || len(rows) != 2 || rows[1].ID != legacy.ID || rows[1].Outcome != Succeeded {
			t.Fatalf("legacy replay: %+v %v", rows, err)
		}
	})
}

func TestAuditSanitizesSnapshotsAndDoesNotRetainCallerMaps(t *testing.T) {
	s, _ := Open(nil)
	now := time.Now()
	fields := map[string]string{"url": "https://person:password@example.com?token=credential", "authorization": "Bearer credential", "name": "Before"}
	_, err := s.Begin("acme", "ada", Change{Action: "source.update", Resource: "source", ID: "one", After: fields}, now)
	if err != nil {
		t.Fatal(err)
	}
	fields["name"] = "Changed"
	q := Query{From: now.Add(-time.Second), To: now.Add(time.Hour)}
	rows, _ := s.List("acme", q, nil)
	if rows[0].After["url"] != "https[redacted]example.com[redacted]" || rows[0].After["authorization"] != "[redacted]" || rows[0].After["name"] != "Before" {
		t.Fatalf("unsafe snapshot: %+v", rows)
	}
	rows[0].After["name"] = "Mutated"
	rows, _ = s.List("acme", q, nil)
	if rows[0].After["name"] != "Before" {
		t.Fatal("returned map mutated stored snapshot")
	}
	if _, err := s.Begin("acme", string([]byte{255}), Change{Action: "source.update", Resource: "source", ID: "one"}, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid identity accepted: %v", err)
	}
}

func TestActorAndDomainFiltersRunBeforeVisibleLimit(t *testing.T) {
	s, _ := Open(nil)
	now := time.Now()
	if err := s.Record("acme", "ada", Change{Action: "memory.edit", Resource: "memory", ID: "one"}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Record("acme", "ben", Change{Action: "skill.publish", Resource: "skill", ID: "two"}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	rows, err := s.List("acme", Query{From: now.Add(-time.Second), To: now.Add(time.Hour), Actor: "ada", Resource: "memory", Limit: 1}, nil)
	if err != nil || len(rows) != 1 || rows[0].Actor != "ada" {
		t.Fatalf("filtered limit: %+v %v", rows, err)
	}
}
