package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	saigetypes "github.com/urmzd/saige/agent/types"
)

type privateOperationProbe struct {
	*saigetypes.ToolFunc
	rich func() (saigetypes.ToolResult, error)
}

func (*privateOperationProbe) ActsForPerson() bool { return true }
func (p *privateOperationProbe) ExecuteRich(context.Context, map[string]any) (saigetypes.ToolResult, error) {
	return p.rich()
}

func TestPersonScopedOperationPersistsPrivacyBeforeExecuting(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		for _, rich := range []bool{false, true} {
			t.Run(map[bool]string{false: "plain", true: "rich"}[rich], func(t *testing.T) {
				st := open(t)
				fault := statetest.NewFaulty(st)
				p, err := New(Config{Workspace: t.TempDir()})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = p.Close() })
				p.Runs, err = runs.Open(fault)
				if err != nil {
					t.Fatal(err)
				}
				tag := wire.NewEvent(wire.Raise{ID: "person-tool", Tenant: "t", Agent: "a", Origin: runs.OriginAPI, Text: "work", At: time.Now()})
				id := runtime.RunID(tag.Tenant, tag.ID)
				if err := p.Runs.Record(id, tag, 1); err != nil {
					t.Fatal(err)
				}
				req := runtime.Request{Tenant: "t", RunID: id, Definition: native.Revision{Spec: native.Spec{Name: "a"}}}
				calls := 0
				verify := func() {
					t.Helper()
					calls++
					r, _ := p.Runs.Get("t", id)
					if !r.UsesPrivateContext() {
						t.Fatal("private result executed before durable privacy classification")
					}
				}
				probe := &privateOperationProbe{ToolFunc: &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: "person_read"}, Fn: func(context.Context, map[string]any) (string, error) { verify(); return "private", nil }}, rich: func() (saigetypes.ToolResult, error) { verify(); return saigetypes.TextResult("private"), nil }}
				tool := p.operationTool(req, probe, operations.ReadContext)
				execute := func() error {
					if rich {
						_, err := tool.(saigetypes.RichTool).ExecuteRich(t.Context(), nil)
						return err
					}
					_, err := tool.Execute(t.Context(), nil)
					return err
				}
				fault.Fail(statetest.ErrInjected)
				if err := execute(); !errors.Is(err, statetest.ErrInjected) || calls != 0 {
					t.Fatalf("storage failure executed private tool: calls=%d err=%v", calls, err)
				}
				fault.Fail(nil)
				if err := execute(); err != nil || calls != 1 {
					t.Fatalf("private tool: calls=%d err=%v", calls, err)
				}
				reopened, err := runs.Open(st)
				if err != nil {
					t.Fatal(err)
				}
				r, _ := reopened.Get("t", id)
				if !r.UsesPrivateContext() {
					t.Fatal("private classification lost after restart")
				}
			})
		}
	})
}

func TestPrivateParentClassifiesStandaloneHelperAfterRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		index, err := runs.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		parent := wire.NewEvent(wire.Raise{ID: "parent", Tenant: "t", Agent: "a", Origin: runs.OriginAPI, Text: "work", At: time.Now()})
		child, err := call.New(parent, call.Request{Key: "helper", Agent: "b", Text: "help", Mode: call.Subagent}, call.State{}, call.Limits{MaxDepth: 3, MaxFanOut: 4}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		for _, tag := range []event.Envelope{parent, child} {
			if err := index.Record(runtime.RunID("t", tag.ID), tag, 1); err != nil {
				t.Fatal(err)
			}
		}
		if err := index.MarkPrivateContext("t", runtime.RunID("t", parent.ID)); err != nil {
			t.Fatal(err)
		}
		replay, err := runs.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		tree, err := replay.Tree("t", runtime.RunID("t", child.ID))
		if err != nil {
			t.Fatal(err)
		}
		private := false
		for _, run := range tree {
			private = private || run.UsesPrivateContext()
		}
		if !private {
			t.Fatal("standalone helper tree lost private parent classification after restart")
		}
	})
}
