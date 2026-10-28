package platform_test

import (
	"context"
	"errors"
	"maps"
	"sync/atomic"
	"testing"
	"time"

	anchorrun "github.com/urmzd/anchor/pkg/run"
	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

type assignmentAcceptanceFault struct {
	platform.Ledger
	fail atomic.Bool
}

func (f *assignmentAcceptanceFault) Accept(ctx context.Context, r anchorrun.Run) error {
	if f.fail.Load() {
		return statetest.ErrInjected
	}
	return f.Ledger.Accept(ctx, r)
}

func TestCronAssignmentRecoversExactReservedInvocationAfterRestart(t *testing.T) {
	eachBackend(t, func(t *testing.T, b platform.Backends, again func() platform.Backends) {
		fault := &assignmentAcceptanceFault{Ledger: b.Ledger}
		b.Ledger = fault
		p, stop := running(t, b)
		if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "standing", Model: "offline", Provider: "offline", SystemPrompt: "Complete assigned work."}, "owner"); err != nil {
			t.Fatal(err)
		}
		who := auth.Principal{Tenant: tenant, Subject: "owner", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
		a, err := p.SetAssignment(t.Context(), who, operations.Assignment{ID: "nightly-review", Agent: "standing", Owner: "owner", Goal: "Review the scheduled occurrence", DoneWhen: "Report saved", State: operations.Requested, Cron: "0 0 1 1 *"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		a.State = operations.Watching
		a, err = p.SetAssignment(t.Context(), who, a, a.Version)
		if err != nil {
			t.Fatal(err)
		}
		sc, _, err := p.Schedules.Get(tenant, a.Schedule)
		if err != nil {
			t.Fatal(err)
		}
		meta := maps.Clone(sc.Context)
		meta["cron_schedule"] = sc.Name
		meta["cron_expression"] = sc.Expression
		meta["cron_occurrence"] = "2025-01-01T00:00:00Z"
		meta["occurrence_context"] = "exact-reservation-proof"
		tag := wire.NewEvent(wire.Raise{ID: sc.Name + "@1735689600", Tenant: tenant, Agent: sc.Agent, Origin: "cron", Actor: event.Actor{ID: "cron:" + sc.Name, Bot: true}, Text: sc.Text, Context: meta, At: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)})
		fault.fail.Store(true)
		if _, err := p.Invoke(t.Context(), tag); !errors.Is(err, statetest.ErrInjected) {
			t.Fatalf("expected acceptance failure after reservation: %v", err)
		}
		reserved, _ := p.Operations.Assignment(tenant, a.ID)
		if reserved.State != operations.InProgress {
			t.Fatalf("reservation was not kept: %+v", reserved)
		}
		stop()
		p2, stop2 := running(t, again())
		defer stop2()
		control, err := p2.Operations.SetControl(tenant, a.Agent, operations.Control{Paused: true}, 0, "owner", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p2.RunAssignment(t.Context(), who, a.ID); !errors.Is(err, operations.ErrPaused) {
			t.Fatalf("recovery bypassed current pause: %v", err)
		}
		if _, err := p2.Operations.SetControl(tenant, a.Agent, operations.Control{}, control.Version, "owner", time.Now()); err != nil {
			t.Fatal(err)
		}
		sourceAwait(t, func() bool { _, err := p2.Runs.Get(tenant, reserved.RunID); return err == nil })
		got, err := p2.Runs.Get(tenant, reserved.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != runtime.RunID(tenant, tag.ID) || got.Origin != "cron" || wire.Meta(got.Tag())["occurrence_context"] != "exact-reservation-proof" || wire.Meta(got.Tag())["cron_occurrence"] != wire.Meta(tag)["cron_occurrence"] || !got.Tag().Time.Equal(tag.Time) {
			t.Fatalf("recovery changed exact occurrence: %+v", got)
		}
		sourceAwait(t, func() bool {
			a, _ := p2.Operations.Assignment(tenant, a.ID)
			return a.State == operations.Watching && a.RunID == got.ID
		})
		settled, _ := p2.Operations.Assignment(tenant, a.ID)
		if _, err := p2.Operations.Reserve(tenant, a.ID, "legacy-clock-reservation", settled.Version, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := p2.RunAssignment(t.Context(), who, a.ID); !errors.Is(err, operations.ErrConflict) {
			t.Fatalf("legacy occurrence was guessed: %v", err)
		}
		legacy, _ := p2.Operations.Assignment(tenant, a.ID)
		if legacy.State != operations.Blocked || legacy.RunID != "legacy-clock-reservation" {
			t.Fatalf("uncertain legacy occurrence did not remain for explicit review: %+v", legacy)
		}

	})
}
