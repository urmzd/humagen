package callpolicy_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/callpolicy"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestRestart(t *testing.T) {
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := callpolicy.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		p := callpolicy.Policy{Agent: "h", Callers: callpolicy.Listed, Allow: []string{"a"}, Approval: callpolicy.Unlisted,
			Trusted: []string{"a"}, Approvers: []string{"olu", "role:manager"}, SelfApproval: true, ExpiresSeconds: 3600}
		if _, err := s.Put("acme", p, known("a"), "olu", at); err != nil {
			t.Fatal(err)
		}
		want, err := s.Put("acme", p, known("a"), "olu", at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Put("acme", callpolicy.Policy{Agent: "gone", Callers: callpolicy.Nobody, Approval: callpolicy.Never}, known(), "olu", at); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete("acme", "gone"); err != nil {
			t.Fatal(err)
		}

		again, err := callpolicy.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := again.Get("acme", "h")
		if !ok || !reflect.DeepEqual(got, want) || got.Version != 2 {
			t.Fatalf("after restart = %+v, want %+v", got, want)
		}
		if _, ok := again.Get("acme", "gone"); ok {
			t.Fatal("a deleted policy came back")
		}
		if _, ok := again.Get("globex", "h"); ok {
			t.Fatal("another tenant sees the policy")
		}
	})
}

func TestFailedWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, _ := callpolicy.Open(f)
	p := callpolicy.Policy{Agent: "h", Callers: callpolicy.Nobody, Approval: callpolicy.Never}
	if _, err := s.Put("acme", p, known(), "olu", time.Now()); err != nil {
		t.Fatal(err)
	}
	f.Fail(statetest.ErrInjected)
	if _, err := s.Put("acme", p, known(), "olu", time.Now()); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Put = %v", err)
	}
	if err := s.Delete("acme", "h"); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Delete = %v", err)
	}
	if got, ok := s.Get("acme", "h"); !ok || got.Version != 1 {
		t.Fatalf("after failed writes = %+v, %v", got, ok)
	}
}
