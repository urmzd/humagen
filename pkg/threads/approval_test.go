package threads_test

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

// A conversation's approval mode is set on its own, by a person, and kept;
// Update cannot change it, and its version does not move.
func TestApprovalModeIsSetOnItsOwnAndKept(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		th, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "hello"))
		if th.Approval.Auto() {
			t.Fatal("a new conversation approves on its own")
		}
		got, err := s.SetApproval("acme", th.ID, threads.ApprovalAuto, "dana", []string{"operator"}, t0)
		if err != nil || !got.Approval.Auto() || got.Approval.By != "dana" || got.Approval.Roles[0] != "operator" || got.Version != th.Version {
			t.Fatalf("auto = %+v, %v", got.Approval, err)
		}
		if _, err := s.Update("acme", th.ID, got.Version, t0, func(n *threads.Thread) error {
			n.Approval = nil
			return nil
		}); !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("approval cleared through Update = %v", err)
		}
		again, _ := threads.Open(st)
		if kept, _ := again.Get("acme", th.ID); !kept.Approval.Auto() {
			t.Fatalf("after restart = %+v", kept.Approval)
		}
		if got, _ := s.SetApproval("acme", th.ID, threads.ApprovalAsk, "dana", nil, t0); got.Approval != nil {
			t.Fatalf("ask = %+v", got.Approval)
		}
		for _, bad := range [][2]string{{"maybe", "dana"}, {threads.ApprovalAuto, ""}} {
			if _, err := s.SetApproval("acme", th.ID, bad[0], bad[1], nil, t0); !errors.Is(err, threads.ErrInvalid) {
				t.Fatalf("%v = %v", bad, err)
			}
		}
		if _, err := s.SetApproval("globex", th.ID, threads.ApprovalAuto, "dana", nil, t0); err == nil {
			t.Fatal("another tenant set the mode")
		}
	})
}
