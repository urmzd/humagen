package humagen

import (
	"context"
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/turn"
	"github.com/urmzd/humagen/internal/app"
	"github.com/urmzd/humagen/internal/platform"
)

func TestWithRuntimeSetsThePlatformsRuntime(t *testing.T) {
	rt := turn.Func(func(context.Context, turn.Pinned, turn.Input, func(context.Context, event.Body) error) (turn.Result, error) {
		return turn.Result{}, nil
	})
	var o app.Options
	WithRuntime(rt)(&o)
	var cfg platform.Config
	if err := o.Platform(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Runtime == nil {
		t.Fatal("WithRuntime did not reach the platform's configuration")
	}
	if WithRuntime(nil) != nil {
		t.Fatal("WithRuntime(nil) should add nothing")
	}
}
