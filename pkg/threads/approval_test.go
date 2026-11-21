package threads_test

import (
	"context"
	"errors"
	"strings"
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
		// decide, Run and Answer's old name, is taken as auto.
		got, err = s.SetApproval("acme", th.ID, "decide", "dana", []string{"operator"}, t0)
		if err != nil || !got.Approval.Auto() || got.Approval.Mode != threads.ApprovalAuto || got.Approval.ModeOf() != threads.ApprovalAuto {
			t.Fatalf("decide = %+v, %v", got.Approval, err)
		}
		if got, _ := s.SetApproval("acme", th.ID, threads.ApprovalAsk, "dana", nil, t0); got.Approval.ModeOf() != threads.ApprovalAsk || got.Approval.Auto() {
			t.Fatalf("ask after decide = %+v", got.Approval)
		}
		for _, bad := range [][2]string{{"maybe", "dana"}, {threads.ApprovalAuto, ""}, {"decide", ""}} {
			if _, err := s.SetApproval("acme", th.ID, bad[0], bad[1], nil, t0); !errors.Is(err, threads.ErrInvalid) {
				t.Fatalf("%v = %v", bad, err)
			}
		}
		if _, err := s.SetApproval("globex", th.ID, threads.ApprovalAuto, "dana", nil, t0); err == nil {
			t.Fatal("another tenant set the mode")
		}
	})
}

// A conversation stored as Run (auto) or as Run and Answer (decide), the
// two modes that became Auto, reads as Auto after a restart, and a write
// keeps it Auto. The stored names are pinned: a change here strands stored
// conversations.
func TestStoredApprovalModesReadAsAuto(t *testing.T) {
	if threads.ApprovalAsk != "ask" || threads.ApprovalAuto != "auto" {
		t.Fatalf("stored names are %q and %q", threads.ApprovalAsk, threads.ApprovalAuto)
	}
	for _, stored := range []string{"auto", "decide"} {
		t.Run(stored, func(t *testing.T) {
			statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
				st := open(t)
				s, err := threads.Open(st)
				if err != nil {
					t.Fatal(err)
				}
				th, _ := start(t, s, "acme", shared("Globex", "renewals"), msg("dana", "hello"))
				if _, err := s.SetApproval("acme", th.ID, threads.ApprovalAuto, "dana", []string{"operator"}, t0); err != nil {
					t.Fatal(err)
				}
				// Rewrite the record as an earlier release kept it.
				ctx := context.Background()
				rec, err := st.Lookup(ctx, "threads", "acme", "t:"+th.ID)
				if err != nil {
					t.Fatal(err)
				}
				raw := strings.Replace(string(rec.Value), `"mode":"auto"`, `"mode":"`+stored+`"`, 1)
				if !strings.Contains(raw, `"mode":"`+stored+`"`) {
					t.Fatalf("no approval mode in the record: %s", rec.Value)
				}
				if err := st.Write(ctx, "threads", state.Put("acme", "t:"+th.ID, []byte(raw))); err != nil {
					t.Fatal(err)
				}
				again, err := threads.Open(st)
				if err != nil {
					t.Fatal(err)
				}
				got, err := again.Get("acme", th.ID)
				if err != nil || !got.Approval.Auto() || got.Approval.Mode != threads.ApprovalAuto || got.Approval.ModeOf() != threads.ApprovalAuto || got.Approval.By != "dana" {
					t.Fatalf("stored %s reads as %+v, %v", stored, got.Approval, err)
				}
				// A change to the thread writes it back in its current spelling.
				if _, err := again.Update("acme", th.ID, got.Version, t0, func(n *threads.Thread) error {
					n.Title = "renamed"
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				rec, _ = st.Lookup(ctx, "threads", "acme", "t:"+th.ID)
				if !strings.Contains(string(rec.Value), `"mode":"auto"`) {
					t.Fatalf("written back as %s", rec.Value)
				}
			})
		})
	}
}
