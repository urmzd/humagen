package platform

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/event"
	flowturn "github.com/urmzd/orchagent/flow/turn"
)

func scheduledReplayFixture(t *testing.T, engine flowturn.Runtime) (*Platform, evals.Suite, string) {
	t.Helper()
	policy, err := auth.ParsePolicy("replay", `role operator { allow read on *
allow write on runs, evals }
grant operator to subject "creator"`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Workspace: t.TempDir(), Runtime: engine, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if _, err = p.Agents.Create(t.Context(), "acme", wire.AgentSpec{Name: "writer", Provider: "offline", Model: "offline", SystemPrompt: "Cite evidence"}, "test"); err != nil {
		t.Fatal(err)
	}
	th, err := p.Threads.Create("acme", threads.Thread{ID: "th-1234567890abcdef", Channel: "general", Visibility: threads.Private, CreatedBy: "owner", Participants: []threads.Participant{{Kind: threads.Person, Name: "creator"}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tag := wire.NewEvent(wire.Raise{ID: "source", Origin: runs.OriginAPI, Tenant: "acme", Agent: "writer", Actor: event.Actor{ID: "owner"}, At: time.Now(), Context: map[string]string{runs.MetaThread: th.ID}})
	if err = p.Runs.Record("source", tag, 1); err != nil {
		t.Fatal(err)
	}
	suite := evals.Suite{Name: "scheduled", Target: evals.Agent, Subject: "writer", CreatedBy: "creator", EverySeconds: 3600, Cases: []evals.Case{{ID: "c", Input: "question", Checks: []evals.Check{{Kind: evals.Contains, Value: "evidence"}}, Trace: &evals.Trace{Run: "source", Tools: []evals.ToolResult{}}}}}
	return p, suite, th.ID
}

func TestRecordedEvalAdmissionRequiresCurrentConfiguredPersonAuthority(t *testing.T) {
	var calls atomic.Int64
	engine := flowturn.Func(func(context.Context, flowturn.Pinned, flowturn.Input, func(context.Context, event.Body) error) (flowturn.Result, error) {
		calls.Add(1)
		return flowturn.Result{Text: "evidence"}, nil
	})
	p, suite, _ := scheduledReplayFixture(t, engine)
	for _, creator := range []string{"", "agent:" + BuiltinBuilder, "system:schedule", "ungranted-person"} {
		suite.CreatedBy = creator
		if err := p.Evals.Put("acme", suite, creator != ""); err != nil {
			t.Fatal(err)
		}
		if _, err := p.StartEval(t.Context(), "acme", suite.Name, "system:schedule"); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("creator %q acquired authority: %v", creator, err)
		}
	}
	suite.CreatedBy = "creator"
	if err := p.Evals.Put("acme", suite, true); err != nil {
		t.Fatal(err)
	}
	if _, err := p.StartEval(t.Context(), "acme", suite.Name, "ungranted-person"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("explicit replay borrowed creator's authority: %v", err)
	}
	if history, err := p.Evals.Runs("acme", suite.Name); err != nil || len(history) != 0 || calls.Load() != 0 {
		t.Fatalf("denied replay admitted/model called: %+v %d %v", history, calls.Load(), err)
	}
	plain := suite
	plain.Cases = []evals.Case{{ID: "manual", Input: "question"}}
	if err := p.checkEvalReplayAccess("acme", plain, "system:schedule"); err != nil {
		t.Fatal("plain manually authored case gained private replay dependency", err)
	}
}

func TestRecordedEvalChecksSourceRevocationBetweenCasesAndDropsRevokedOutput(t *testing.T) {
	var p *Platform
	var thread string
	var calls atomic.Int64
	engine := flowturn.Func(func(context.Context, flowturn.Pinned, flowturn.Input, func(context.Context, event.Body) error) (flowturn.Result, error) {
		calls.Add(1)
		th, err := p.Threads.Get("acme", thread)
		if err != nil {
			return flowturn.Result{}, err
		}
		if _, err = p.Threads.Participants("acme", thread, th.Version, nil, []string{"creator"}, nil, "owner", time.Now()); err != nil {
			return flowturn.Result{}, err
		}
		return flowturn.Result{Text: "PRIVATE evidence"}, nil
	})
	var suite evals.Suite
	p, suite, thread = scheduledReplayFixture(t, engine)
	second := suite.Cases[0]
	second.ID = "second"
	suite.Cases = append(suite.Cases, second)
	if err := p.Evals.Put("acme", suite, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.StartEval(t.Context(), "acme", suite.Name, "system:schedule"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		history, err := p.Evals.Runs("acme", suite.Name)
		if err != nil {
			t.Fatal(err)
		}
		if len(history) == 1 && history[0].Status != evals.Running {
			if calls.Load() != 1 || len(history[0].Results) != 2 {
				t.Fatalf("revocation did not stop next model call: %+v calls=%d", history, calls.Load())
			}
			for _, result := range history[0].Results {
				if result.Passed || result.Error == "" || result.Output != "" {
					t.Fatalf("revoked output escaped: %+v", result)
				}
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("recorded eval did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestScheduledReplayRequiresCurrentRetrievedSourceVisibility(t *testing.T) {
	p, suite, _ := scheduledReplayFixture(t, nil)
	if _, err := p.Knowledge.Create("acme", "manual", "", "creator", time.Now()); err != nil {
		t.Fatal(err)
	}
	snapshot := evals.Retrieval{Input: suite.Cases[0].Input, Sources: []string{"manual"}, Blocks: []evals.ContextBlock{}}.Seal()
	suite.Cases[0].Trace.Retrieval = &snapshot
	if err := p.checkEvalReplayAccess("acme", suite, "system:schedule"); err != nil {
		t.Fatal("authorized retained source refused", err)
	}
	if err := p.Knowledge.Delete("acme", "manual"); err != nil {
		t.Fatal(err)
	}
	if err := p.Evals.Put("acme", suite, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.StartEval(t.Context(), "acme", suite.Name, "system:schedule"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("deleted source replayed: %v", err)
	}
}
