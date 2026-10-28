package platform_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/event"
	turnrt "github.com/urmzd/humagen/fiber/pkg/turn"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
)

// A Runtime in the configuration runs every turn in place of the native one,
// and is handed the pinned definition of the agent the run names.
func TestAConfiguredRuntimeRunsTheTurn(t *testing.T) {
	var turns atomic.Int32
	var pinned atomic.Value
	rt := turnrt.Func(func(ctx context.Context, def turnrt.Pinned, in turnrt.Input, emit func(context.Context, event.Body) error) (turnrt.Result, error) {
		turns.Add(1)
		pinned.Store(def)
		if err := emit(ctx, event.Text{Text: "from my runtime"}); err != nil {
			return turnrt.Result{}, err
		}
		return turnrt.Result{Text: "from my runtime"}, nil
	})
	p := startWith(t, platform.Config{Limits: call.Limits{}, Runtime: rt})

	r, err := p.Invoke(t.Context(), tag("run-rt", "helper"))
	if err != nil {
		t.Fatal(err)
	}
	got := await(t, p, r.ID, runs.Completed)
	if got.Answer != "from my runtime" {
		t.Fatalf("answer %q, want the configured runtime's", got.Answer)
	}
	if turns.Load() == 0 {
		t.Fatal("the configured runtime was never asked")
	}
	if def, _ := pinned.Load().(turnrt.Pinned); def.Name != "helper" || def.Version == "" {
		t.Fatalf("pinned %+v, want helper at a version", def)
	}
}
