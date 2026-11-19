package continuation

import (
	"context"
	"testing"

	"github.com/urmzd/saige/flow/isolation"
)

func turnOf(tenant, run string) context.Context {
	return isolation.WithRun(context.Background(), isolation.Unisolated(isolation.Request{Tenant: tenant, Run: run}))
}

// A turn is a continuation's only when the platform marked its own run, read
// by the claim: another run's mark, another tenant's, a context with no
// claim, and a mark released are none.
func TestATurnIsToldOnlyItsOwnRunsRule(t *testing.T) {
	Mark("acme", "r1", Rule{Thread: "th-0000000000000001", First: true})
	t.Cleanup(func() { Release("acme", "r1") })
	if got, ok := Of(turnOf("acme", "r1")); !ok || got != (Rule{Thread: "th-0000000000000001", First: true}) {
		t.Fatalf("own run: %+v %v", got, ok)
	}
	for name, ctx := range map[string]context.Context{
		"another run": turnOf("acme", "r2"), "another tenant": turnOf("globex", "r1"),
		"no run": turnOf("acme", ""), "no claim": context.Background(), "a nil claim": isolation.WithRun(context.Background(), nil),
	} {
		if got, ok := Of(ctx); ok {
			t.Errorf("%s: told %+v", name, got)
		}
	}
	Mark("acme", "", Rule{Thread: "th-0000000000000001"})
	Mark("acme", "r3", Rule{})
	if _, ok := Of(turnOf("acme", "")); ok {
		t.Fatal("a mark with no run was kept")
	}
	if _, ok := Of(turnOf("acme", "r3")); ok {
		t.Fatal("a mark with no thread was kept")
	}
	Release("acme", "r1")
	if _, ok := Of(turnOf("acme", "r1")); ok {
		t.Fatal("a released mark is still read")
	}
}
