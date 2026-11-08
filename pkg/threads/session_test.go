package threads_test

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestSessionIdentitySurvivesRestartAndCannotBeReassigned(t *testing.T) {
	db := state.NewMemory()
	s, err := threads.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	const id = "s_00000000000000000000000000000001"
	th, err := s.Create("acme", threads.Thread{ID: id, Channel: "general", Visibility: threads.Private, CreatedBy: "ana", Default: "lead", Agents: []string{"lead"}, Session: &threads.SessionIdentity{Agent: "lead", Parent: "console://ana"}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*threads.Thread) error{
		func(th *threads.Thread) error { th.Session = nil; return nil },
		func(th *threads.Thread) error {
			th.Session.Agent = "other"
			th.Default = "other"
			th.Agents = []string{"other"}
			return nil
		},
		func(th *threads.Thread) error { th.Agents = append(th.Agents, "other"); return nil },
	} {
		if _, err := s.Update("acme", id, th.Version, t0, change); !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("session reassignment = %v", err)
		}
	}
	th.Session.Label = "changed outside store"
	reopened, err := threads.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get("acme", id)
	if err != nil || got.Session.Agent != "lead" || got.Session.Label != "" {
		t.Fatalf("stored identity changed: %+v, %v", got, err)
	}
	if _, err := reopened.Get("other", id); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("cross tenant read = %v", err)
	}
}
