package platform_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/runschema"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// slowFirst is a model whose first call is held: until open closes, or, when
// open is nil, until the call is cancelled. Every call answers with answer.
type slowFirst struct {
	answer string
	open   chan struct{}
	called chan struct{}
	calls  atomic.Int32
}

func (m *slowFirst) Stream(ctx context.Context, _ saigetypes.Request) (<-chan saigetypes.Delta, error) {
	first := m.calls.Add(1) == 1
	if first {
		close(m.called)
	}
	out := make(chan saigetypes.Delta)
	go func() {
		defer close(out)
		if first {
			select {
			case <-m.open: // a nil channel never opens
			case <-ctx.Done():
				return
			}
		}
		for _, d := range agenttest.TextResponse(m.answer) {
			select {
			case out <- d:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// A lease can lapse under an attempt that is still at work: the database's
// clock steps forward past it (a virtual machine's clock set right after it
// fell behind does this), or a heartbeat comes too late. The queue then
// delivers the run again, to the process still executing it. That process
// cannot execute one run twice, so every further delivery failed at once and
// the run spent all its attempts in about four seconds, ending failed while
// its first attempt was still answering. The second delivery is handed back
// uncharged instead, and the run ends completed with its answer.
func TestARunDeliveredAgainWhileItsFirstAttemptWorksCompletes(t *testing.T) {
	for _, c := range []struct {
		name string
		// outlasts says the first attempt's model call is still held when its
		// next heartbeat learns the lease is gone, which cancels the attempt.
		outlasts bool
		calls    int32
	}{
		// The first attempt answers before its next heartbeat: its answer is
		// the run's, and the model is called once.
		{name: "the first attempt answers", calls: 1},
		// The first attempt is cancelled by its heartbeat, at most 10 seconds
		// after the lapse, and the delivery that follows answers.
		{name: "the first attempt is cancelled", outlasts: true, calls: 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := t.Context()
			pool := statetest.Pool(t)
			b, release, err := platform.OpenPostgres(ctx, pool)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = release() })
			const answer = "Answered once."
			m := &slowFirst{answer: answer, called: make(chan struct{})}
			if !c.outlasts {
				m.open = make(chan struct{})
			}
			p, _ := runningWith(t, platform.Config{Backends: b, AgentOptions: []native.Option{
				native.WithProvider(native.ProviderOffline, func(native.Spec) (saigetypes.Provider, error) { return m, nil }),
			}})
			if _, err := p.Agents.Create(ctx, tenant, wire.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead."}, "test"); err != nil {
				t.Fatal(err)
			}
			r, err := p.Invoke(ctx, tag("delivered-again", "lead"))
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-m.called:
			case <-time.After(10 * time.Second):
				t.Fatal("the run never called its model")
			}
			queued := func() (attempt, failures int) {
				t.Helper()
				if err := pool.QueryRow(ctx, "SELECT attempt, failures FROM "+runschema.QueueTable+" WHERE run_id = $1", r.ID).Scan(&attempt, &failures); err != nil {
					t.Fatal(err)
				}
				return attempt, failures
			}
			// What a step of the database's clock past the lease does to it.
			if _, err := pool.Exec(ctx, "UPDATE "+runschema.QueueTable+" SET lease_until = now() - interval '1 second' WHERE run_id = $1", r.ID); err != nil {
				t.Fatal(err)
			}
			eventually(t, "the queue delivered the run again", func() bool {
				attempt, _ := queued()
				return attempt > 1
			})
			if !c.outlasts {
				// The first attempt is still in its model call, and the run has
				// been delivered again more than once before it answers.
				eventually(t, "the run came back a second time", func() bool {
					attempt, _ := queued()
					return attempt > 2
				})
				if got, err := p.Runs.Get(tenant, r.ID); err != nil || got.Status != runs.Running {
					t.Fatalf("while its first attempt works the run is %q (%v, error %q), want %q", got.Status, err, got.Error, runs.Running)
				}
				close(m.open)
			}
			got := await(t, p, r.ID, runs.Completed)
			if got.Answer != answer {
				t.Errorf("the run answered %q, want %q", got.Answer, answer)
			}
			if n := m.calls.Load(); n != c.calls {
				t.Errorf("the model was called %d times, want %d", n, c.calls)
			}
			if _, failures := queued(); failures != 1 {
				t.Errorf("the run was charged %d failures, want 1: the lapsed lease and nothing for being delivered again", failures)
			}
			eventually(t, "the platform holds no run", func() bool { return p.InFlight() == 0 })
		})
	}
}
