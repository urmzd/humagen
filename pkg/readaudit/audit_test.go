package readaudit_test

import (
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/readaudit"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func list(t *testing.T, s *readaudit.Store, tenant string, from, to time.Time) []readaudit.Record {
	t.Helper()
	got, err := s.List(tenant, from, to, 100)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestReadsSurviveRestartAndRetention(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := readaudit.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		want := readaudit.Record{Subject: "governor", Project: "account", Thread: "private", Route: "GET /api/threads/{id}", At: at}
		if err := s.Add("acme", want); err != nil {
			t.Fatal(err)
		}
		s, err = readaudit.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		got := list(t, s, "acme", at.Add(-time.Hour), at.Add(time.Hour))
		if len(got) != 1 || got[0].Subject != want.Subject || got[0].Route != want.Route {
			t.Fatalf("restart = %#v", got)
		}
		if len(list(t, s, "other", at.Add(-time.Hour), at.Add(time.Hour))) != 0 {
			t.Fatal("cross tenant read")
		}
		if err := s.Retain("acme", at.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		s, err = readaudit.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if len(list(t, s, "acme", at.Add(-time.Hour), at.Add(time.Hour))) != 0 {
			t.Fatal("retention lost on replay")
		}
	})
}

func TestFailedWritesDoNotPermitOrLoseAnAudit(t *testing.T) {
	st := statetest.NewFaulty(state.NewMemory())
	s, err := readaudit.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	r := readaudit.Record{Subject: "ana", Project: "account", Route: "GET /api/projects/{name}", At: at}
	st.Fail(statetest.ErrInjected)
	if s.Add("acme", r) == nil {
		t.Fatal("unrecorded read permitted")
	}
	if len(list(t, s, "acme", at.Add(-time.Hour), at.Add(time.Hour))) != 0 {
		t.Fatal("failed write changed memory")
	}
	st.Fail(nil)
	if err := s.Add("acme", r); err != nil {
		t.Fatal(err)
	}
	st.Fail(statetest.ErrInjected)
	if s.Retain("acme", at.Add(time.Hour)) == nil {
		t.Fatal("failed delete accepted")
	}
	if len(list(t, s, "acme", at.Add(-time.Hour), at.Add(time.Hour))) != 1 {
		t.Fatal("failed retention lost record")
	}
	again, err := readaudit.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(list(t, again, "acme", at.Add(-time.Hour), at.Add(time.Hour))) != 1 {
		t.Fatal("record lost on replay")
	}
	if len(list(t, again, "acme", at.Add(-32*24*time.Hour), at.Add(time.Hour))) != 0 {
		t.Fatal("unbounded window accepted")
	}
}
