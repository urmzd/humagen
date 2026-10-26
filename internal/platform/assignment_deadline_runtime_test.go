package platform_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func TestReviewingAnActiveDeadlineKeepsTheSameNativeRun(t *testing.T) {
	eachBackend(t, func(t *testing.T, b platform.Backends, _ func() platform.Backends) {
		base := time.Now().UTC()
		var clock atomic.Int64
		clock.Store(base.UnixNano())
		now := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
		c := &cast{says: map[string][][]saigetypes.Delta{"worker": {say("Recorded result.")}}, gates: map[string]chan struct{}{"worker": make(chan struct{})}}
		p := stage(t, c, platform.Config{Backends: b, Now: now})
		assignment, err := p.SetAssignment(t.Context(), admin, operations.Assignment{ID: "deadline-review", Agent: "worker", Owner: admin.Subject, Goal: "Review", DoneWhen: "Report", DueAt: base.Add(time.Minute), State: operations.Requested}, 0)
		if err != nil {
			t.Fatal(err)
		}
		run, err := p.RunAssignment(t.Context(), admin, assignment.ID)
		if err != nil {
			t.Fatal(err)
		}
		await(t, p, run.ID, runs.Running)
		clock.Store(base.Add(2 * time.Minute).UnixNano())
		if _, err := p.Operations.Due(tenant, now()); err != nil {
			t.Fatal(err)
		}
		current, err := p.Assignment(admin, assignment.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.State != operations.NeedsPerson || current.RunStatus != "running" {
			t.Fatalf("expired native state=%+v", current)
		}
		current.DueAt = base.Add(time.Hour)
		reviewed, err := p.SetAssignment(t.Context(), admin, current, current.Version)
		if err != nil {
			t.Fatal(err)
		}
		if reviewed.State != operations.InProgress || reviewed.RunID != run.ID || reviewed.DeadlineMissed {
			t.Fatalf("deadline review substituted work: %+v", reviewed)
		}
		repeated, err := p.RunAssignment(t.Context(), admin, assignment.ID)
		if err != nil || repeated.ID != run.ID {
			t.Fatalf("deadline review duplicated execution: %+v,%v", repeated, err)
		}
		c.release("worker")
		await(t, p, run.ID, runs.Completed)
		within(t, "native deadline assignment settled", func() bool {
			a, _ := p.Operations.Assignment(tenant, assignment.ID)
			return a.State == operations.Result && a.RunID == run.ID
		})
	})
}
