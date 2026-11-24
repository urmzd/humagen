package platform_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/spec"
)

// A provider an administrator set up runs a real turn: the runtime humagen
// builds finds it through the resolver, with the workspace's own key, and
// nothing about it was declared at start.
func TestATurnOnAWorkspaceProviderAnswers(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	p := startWith(t, platform.Config{
		ConfiguredModelsOnly: true,
		ModelKinds: platform.ModelKinds{Build: func(kind, name string, _ spec.ModelBlock, key platform.KeyFunc) native.ProviderFunc {
			return func(s native.Spec) (saigetypes.Provider, error) {
				k, err := key(context.Background())
				if err != nil {
					return nil, err
				}
				mu.Lock()
				keys = append(keys, k)
				mu.Unlock()
				return &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{agenttest.TextResponse(kind + " " + name + " " + s.Model)}}, nil
			}
		}},
	})
	ctx := t.Context()
	admin := auth.Principal{Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	if _, err := p.SetModelProvider(ctx, admin, "team", platform.ModelProviderInput{Kind: platform.KindAnthropic, Key: "sk-team"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Agents.Create(ctx, tenant, wire.AgentSpec{Name: "writer", Provider: "team", Model: "claude-sonnet-5-5", SystemPrompt: "Write."}, "ada"); err != nil {
		t.Fatal(err)
	}
	r, err := p.Converse(ctx, tenant, platform.Turn{ID: "t-team", Agent: "writer", Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	done := await(t, p, r.ID, runs.Completed)
	if done.Answer != "anthropic team claude-sonnet-5-5" {
		t.Fatalf("answer = %q", done.Answer)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) == 0 || keys[0] != "sk-team" {
		t.Fatalf("keys = %v", keys)
	}
}

// A run whose agent names no enabled model provider fails on its first
// attempt: a retry cannot enable one, so it is not attempted again.
func TestARunWithoutAModelProviderFailsOnce(t *testing.T) {
	p := startWith(t, platform.Config{ConfiguredModelsOnly: true})
	ctx := t.Context()
	if _, err := p.Agents.Create(ctx, tenant, wire.AgentSpec{Name: "writer", Provider: "team", Model: "claude-sonnet-5-5", SystemPrompt: "Write."}, "ada"); err != nil {
		t.Fatal(err)
	}
	stream, err := p.Subscribe(ctx, tenant, platform.EventFilter{Agent: "writer"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	r, err := p.Converse(ctx, tenant, platform.Turn{ID: "t-none", Agent: "writer", Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	done := await(t, p, r.ID, runs.Failed)
	if !strings.Contains(done.Error, "needs a model provider") {
		t.Fatalf("error = %q", done.Error)
	}
	var failed []event.Failed
	for {
		recvCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		e, err := stream.Recv(recvCtx)
		cancel()
		if err != nil {
			break
		}
		if f, ok := e.Data.(event.Failed); ok && e.Run == r.ID {
			failed = append(failed, f)
		}
	}
	if len(failed) != 1 || !failed[0].Terminal || failed[0].Attempt > 1 {
		t.Fatalf("run.failed events = %+v, want one terminal failure of the first attempt", failed)
	}
}
