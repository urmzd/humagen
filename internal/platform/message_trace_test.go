package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn/native"
	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// releaseThread is the place get_context reads in these tests.
const releaseThread = "slack://T01ABCDEF/C01/1700000000.000100"

// contextTools answer get_context as the platform's tool does: what was
// pulled from the place, what it drew on, and the place it read.
func contextTools(runtime.Request, func(event.Action) error) (native.Tools, error) {
	return native.ToolsFunc(func(name string) (saigetypes.Tool, error) {
		if name != "get_context" {
			return nil, native.ErrNoSuchTool
		}
		return &saigetypes.ToolFunc{
			Def: saigetypes.ToolDef{Name: name, Description: "Pull context from where the message came from.", Parameters: saigetypes.ParameterSchema{
				Type: "object", Required: []string{"request"}, Properties: map[string]saigetypes.PropertyDef{"request": {Type: "string"}},
			}},
			Fn: func(context.Context, map[string]any) (string, error) {
				b, err := json.Marshal(map[string]any{"origin": releaseThread, "text": "They agreed to ship on Friday.", "sources": []map[string]string{{"title": "Release thread", "url": "https://slack.example/release"}}, "truncated": false, "note": ""})
				return string(b), err
			},
		}, nil
	}), nil
}

// tracedDeploy is a root whose entry agent hands a deploy to ops, which
// reads the release thread with get_context and writes a plan, a write ada
// approves. It returns the root and ops's run, ended.
func tracedDeploy(t *testing.T) (*platform.Platform, platform.SessionView, runs.Run) {
	t.Helper()
	c := entryCast([][]saigetypes.Delta{
		pickJSON(platform.PickDelegate, "Deploys are ops's.", "ops"),
	}, map[string][][]saigetypes.Delta{"ops": {
		agenttest.ToolCallResponse("ctx-1", "get_context", map[string]any{"request": "what they said about the release"}),
		writing("plan.md", "ship"),
		say("Shipped."),
	}})
	c.tools = map[string][]string{"ops": {"get_context", platform.ToolFileWrite}}
	brief := c.shape["ops"]
	c.shape["ops"] = func(s *wire.AgentSpec) {
		brief(s)
		s.Approvals, s.Approvers = []string{platform.ToolFileWrite}, []string{admin.Subject}
	}
	cfg := platform.Config{TestBuiltinProvider: "offline", TestBuiltinModel: "offline", Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}}
	cfg.Ext.Catalogs = []runtime.Catalog{contextTools}
	p := stage(t, c, cfg)
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "*"})
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	seePeople(t, p, operator, admin)
	v, err := p.StartSession(t.Context(), operator, platform.SessionInput{Project: "~", Text: "Why did the deploy fail last night?"})
	if err != nil {
		t.Fatal(err)
	}
	var parked runs.Run
	within(t, "ops parked on its write", func() bool {
		r, ok := runOf(p, tenant, "ops")
		parked = r
		return ok && r.Status == runs.Parked && r.Question != nil
	})
	questionFor(t, p, parked.ID)
	if _, err := p.AnswerQuestion(t.Context(), admin, parked.ID, parked.Question.ID, platform.QuestionAnswer{Action: platform.ActApprove, Note: "ship it"}); err != nil {
		t.Fatal(err)
	}
	done := await(t, p, parked.ID, runs.Completed)
	pickOf(t, p, v.ID)
	return p, v, done
}

func TestAMessagesTraceShowsThePickAndWhatTheChildDid(t *testing.T) {
	p, v, ops := tracedDeploy(t)
	tr, err := p.MessageTrace(t.Context(), operator, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Message != v.ID || tr.Hidden != 0 || tr.Truncated || len(tr.Nodes) != 1 {
		t.Fatalf("trace = %+v", tr)
	}
	pick := tr.Nodes[0]
	if pick.Agent != platform.BuiltinEntry || pick.Pick == nil || pick.Pick.Decision != platform.PickDelegate || !slices.Equal(pick.Pick.Agents, []string{"ops"}) || pick.Pick.Reason != "Deploys are ops's." || pick.Answer != "" {
		t.Fatalf("the pick node = %+v (pick %+v)", pick, pick.Pick)
	}
	if len(pick.Children) != 1 {
		t.Fatalf("beneath the pick = %+v", pick.Children)
	}
	child := pick.Children[0]
	if child.Run != ops.ID || child.Agent != "ops" || child.Status != string(runs.Completed) || child.Answer != "Shipped." || child.Pick != nil || child.EndedAt.IsZero() {
		t.Fatalf("the child node = %+v", child)
	}
	if len(child.Tools) != 1 {
		t.Fatalf("the child's tools = %+v", child.Tools)
	}
	write := child.Tools[0]
	if write.Name != platform.ToolFileWrite || write.Withheld || !strings.Contains(string(write.Input), "plan.md") || write.Ms == nil || write.At.IsZero() {
		t.Fatalf("the write = %+v (input %s)", write, write.Input)
	}
	if len(child.Context) != 1 {
		t.Fatalf("the child's context reads = %+v", child.Context)
	}
	read := child.Context[0]
	want := []platform.TraceSource{{Title: "Release thread", URL: "https://slack.example/release"}}
	if read.Request != "what they said about the release" || read.Reference != releaseThread || read.Summary != "They agreed to ship on Friday." || !slices.Equal(read.Sources, want) || read.Withheld {
		t.Fatalf("the context read = %+v", read)
	}
	if len(child.Approvals) != 1 {
		t.Fatalf("the child's approvals = %+v", child.Approvals)
	}
	ok := child.Approvals[0]
	if ok.Action != platform.ToolFileWrite || ok.Decision != platform.TraceAllow || ok.By != "Ada" || ok.Note != "ship it" || ok.At.IsZero() {
		t.Fatalf("the approval = %+v", ok)
	}
	// Calls carry the ids the response stream gives them, in the order the
	// run asked: context reads count too. The approval names the call it
	// gated, and a context read has a time to sit among the calls.
	ids := []string{write.ID, read.ID}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"htu_" + ops.ID + "_1", "htu_" + ops.ID + "_2"}) {
		t.Fatalf("call ids = %v", ids)
	}
	if ok.Tool != write.ID || read.At.IsZero() || read.Ms == nil {
		t.Fatalf("the approval gates %q (the write is %q); the read is at %v for %v", ok.Tool, write.ID, read.At, read.Ms)
	}

	// The person's message reads the same; the child's own message reads
	// its run alone.
	first, _, err := p.Threads.Messages(tenant, v.ID, "", 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("the root's first message: %v %v", first, err)
	}
	byMessage, err := p.MessageTrace(t.Context(), operator, v.ID+":"+first[0].ID)
	if err != nil || len(byMessage.Nodes) != 1 || byMessage.Nodes[0].Run != pick.Run || len(byMessage.Nodes[0].Children) != 1 {
		t.Fatalf("by the person's message = %+v, %v", byMessage, err)
	}
	byRun, err := p.MessageTrace(t.Context(), operator, ops.Thread+":"+ops.ID)
	if err != nil || len(byRun.Nodes) != 1 || byRun.Nodes[0].Run != ops.ID || byRun.Nodes[0].Pick != nil || len(byRun.Nodes[0].Tools) != 1 {
		t.Fatalf("by ops's message = %+v, %v", byRun, err)
	}
	if _, err := p.MessageTrace(t.Context(), operator, v.ID+":m-999"); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("a message the root does not hold = %v", err)
	}
	if _, err := p.MessageTrace(t.Context(), operator, v.ID+":"+ops.ID); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("a run of another session read under the root = %v", err)
	}
}

func TestAMessagesTraceLeavesOutAChildItsReaderMayNotRead(t *testing.T) {
	p, v, ops := tracedDeploy(t)
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "olu"}, platform.ProjectInput{Name: "Vault", Kind: projects.Account}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAgentHome(t.Context(), tenant, "ops", "Vault", govern); err != nil {
		t.Fatal(err)
	}
	tr, err := p.MessageTrace(t.Context(), operator, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Hidden != 1 || len(tr.Nodes) != 1 || len(tr.Nodes[0].Children) != 0 || len(tr.Nodes[0].Pick.Agents) != 0 {
		t.Fatalf("with ops out of reach, the trace = %+v", tr)
	}
	raw, _ := json.Marshal(tr)
	if strings.Contains(string(raw), ops.ID) || strings.Contains(string(raw), "plan.md") || strings.Contains(string(raw), releaseThread) {
		t.Fatalf("the trace shows what ops did: %s", raw)
	}
	if _, err := p.MessageTrace(t.Context(), operator, ops.Thread+":"+ops.ID); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("ops's own message = %v", err)
	}
}
