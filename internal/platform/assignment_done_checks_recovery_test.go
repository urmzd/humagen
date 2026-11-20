package platform_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	anchorrun "github.com/urmzd/saige/durable/run"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// secondAcceptFault lets the first run into the ledger and refuses every one
// after it: the process ends, as far as the ledger knows, between a
// continuation's reservation and its admission.
type secondAcceptFault struct {
	platform.Ledger
	accepted atomic.Int32
}

func (f *secondAcceptFault) Accept(ctx context.Context, r anchorrun.Run) error {
	if f.accepted.Add(1) > 1 {
		return statetest.ErrInjected
	}
	return f.Ledger.Accept(ctx, r)
}

func TestAContinuationReservedBeforeARestartIsAdmittedAfterIt(t *testing.T) {
	eachBackend(t, func(t *testing.T, b platform.Backends, again func() platform.Backends) {
		b.Ledger = &secondAcceptFault{Ledger: b.Ledger}
		p, stop := running(t, b)
		if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "standing", Model: "offline", Provider: "offline", SystemPrompt: "Complete assigned work."}, "owner"); err != nil {
			t.Fatal(err)
		}
		who := auth.Principal{Tenant: tenant, Subject: "owner", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
		a, err := p.SetAssignment(t.Context(), who, operations.Assignment{ID: "report", Agent: "standing", Owner: "owner", Goal: "File the report", DoneWhen: "It is filed", State: operations.Requested,
			DoneChecks: []operations.Check{{Kind: operations.Contains, Value: "NEVER-SAID-7731"}}, MaxContinue: 1}, 0)
		if err != nil {
			t.Fatal(err)
		}
		first, err := p.RunAssignment(t.Context(), who, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		// The first run completes, its answer fails the check, and the
		// continuation is reserved. Its admission is refused.
		sourceAwait(t, func() bool {
			got, _, _ := p.Operations.Assignment(tenant, a.ID)
			return got.Continued == 1
		})
		reserved, _, _ := p.Operations.Assignment(tenant, a.ID)
		if reserved.State != operations.InProgress || reserved.RunID == first.ID {
			t.Fatalf("reservation = %+v", reserved)
		}
		if _, err := p.Runs.Get(tenant, reserved.RunID); err == nil {
			t.Fatal("the continuation was admitted before the restart")
		}
		stop()

		p2, stop2 := running(t, again())
		defer stop2()
		sourceAwait(t, func() bool { _, err := p2.Runs.Get(tenant, reserved.RunID); return err == nil })
		run, err := p2.Runs.Get(tenant, reserved.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(run.Text, `the answer contains "NEVER-SAID-7731"`) || run.Agent != "standing" {
			t.Fatalf("the recovered run asked %q of %s", run.Text, run.Agent)
		}
		// It is the same conversation, continued once, and the allowance is
		// then used up.
		sourceAwait(t, func() bool {
			got, _, _ := p2.Operations.Assignment(tenant, a.ID)
			return got.State == operations.Blocked
		})
		got, _, _ := p2.Operations.Assignment(tenant, a.ID)
		if got.Continued != 1 || !strings.Contains(got.Blocker, "continued 1 times") {
			t.Fatalf("assignment = %+v", got)
		}
		if turns, err := p2.Runs.Conversation(tenant, first.ID); err != nil || len(turns) != 2 || turns[1].ID != reserved.RunID {
			t.Fatalf("%d turns, %v", len(turns), err)
		}
	})
}
