package platform_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
)

// policyFaults is a store whose policy reads fail once failing is set.
type policyFaults struct {
	state.Store
	failing atomic.Bool
}

var errPolicyRead = errors.New("policy read failed")

func (f *policyFaults) Lookup(ctx context.Context, namespace, tenant, key string) (state.Record, error) {
	if namespace == "policy" && f.failing.Load() {
		return state.Record{}, errPolicyRead
	}
	return f.Store.Lookup(ctx, namespace, tenant, key)
}

func (f *policyFaults) List(ctx context.Context, namespace, tenant string, q state.Query) ([]state.Record, string, error) {
	if namespace == "policy" && f.failing.Load() {
		return nil, "", errPolicyRead
	}
	return f.Store.List(ctx, namespace, tenant, q)
}

// failAfterFirst makes the policy store fail once the run's first model
// call has been made, after the run was admitted.
type failAfterFirst struct {
	saigetypes.Provider
	faults *policyFaults
}

func (f failAfterFirst) Stream(ctx context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
	f.faults.failing.Store(true)
	return f.Provider.Stream(ctx, req)
}

// A run whose limits cannot be read after it spends is stopped, as at a
// limit it reached: an unreadable limit never lets spend go on. The stop is
// asynchronous, made by the observer once it counts the call, so the run
// holds its next call open until then rather than racing it to the end.
func TestSpendStopsWhenItsLimitsCannotBeRead(t *testing.T) {
	faults := &policyFaults{Store: state.NewMemory()}
	p := metered(t, platform.Config{Backends: platform.Backends{Shared: faults}}, map[string]agentModel{
		"lead": {tools: probes, model: func(int) saigetypes.Provider {
			return priced{Provider: failAfterFirst{Provider: &heldAfter{ScriptedProvider: &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{
				spends(probe("p1"), 1000, 100),
			}}, n: 1}, faults: faults}, pricing: card}
		}},
	})
	if _, err := p.Policies.Put(tenant, policy.Policy{
		Scope: policy.Scope{Kind: policy.Tenant}, Period: policy.Day, SpendLimit: 1_000_000_000, Action: policy.Block,
	}, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	r, err := p.Invoke(t.Context(), tag("evt-1", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	stopped := await(t, p, r.ID, runs.Stopped)
	if !strings.Contains(stopped.Cause, "limits could not be read") {
		t.Fatalf("the run was stopped for %q", stopped.Cause)
	}
}
