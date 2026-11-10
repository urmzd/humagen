package platform_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// tracedDeploy is a root whose entry agent calls ops about a deploy with the
// agent tool. ops asks get_context about the release (the console root has
// no place upstream, so it says so) and writes a plan, a write ada approves.
// It returns the root and ops's run, both ended.
func tracedDeploy(t *testing.T) (*platform.Platform, platform.SessionView, runs.Run) {
	t.Helper()
	p, v, parked := parkedDeploy(t)
	if _, err := p.AnswerQuestion(t.Context(), admin, parked.ID, parked.Question.ID, platform.QuestionAnswer{Action: platform.ActApprove, Note: "ship it"}); err != nil {
		t.Fatal(err)
	}
	done := await(t, p, parked.ID, runs.Completed)
	await(t, p, sessionRun(t, v).ID, runs.Completed)
	return p, v, done
}

// parkedDeploy is tracedDeploy up to ops waiting on ada to approve its write:
// the root, and ops's run, parked.
func parkedDeploy(t *testing.T) (*platform.Platform, platform.SessionView, runs.Run) {
	t.Helper()
	c := entryCast([][]saigetypes.Delta{
		agentCall("ask-ops", "ops", "Explain the deploy failure", "Why did the deploy fail last night?"),
		say("ops shipped the fix."),
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
	return p, v, parked
}

func TestAMessagesTraceShowsTheRootAndWhatTheAgentItCalledDid(t *testing.T) {
	p, v, ops := tracedDeploy(t)
	tr, err := p.MessageTrace(t.Context(), operator, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Message != v.ID || tr.Hidden != 0 || tr.Truncated || len(tr.Nodes) != 1 {
		t.Fatalf("trace = %+v", tr)
	}
	// One run answers the message: the entry agent's, with its answer. It
	// chose to call ops itself, so nothing picked for it.
	root := tr.Nodes[0]
	if root.Agent != platform.BuiltinEntry || root.Run != sessionRun(t, v).ID || root.Pick != nil || root.Answer != "ops shipped the fix." {
		t.Fatalf("the root node = %+v (pick %+v)", root, root.Pick)
	}
	if len(root.Tools) != 1 || root.Tools[0].Name != platform.ToolAgent || root.Tools[0].ID != "htu_"+root.Run+"_1" || root.Tools[0].Output != "Shipped." {
		t.Fatalf("the root's calls = %+v", root.Tools)
	}
	if len(root.Children) != 1 {
		t.Fatalf("beneath the root = %+v", root.Children)
	}
	child := root.Children[0]
	if child.Run != ops.ID || child.Agent != "ops" || child.Status != string(runs.Completed) || child.Answer != "Shipped." || child.Pick != nil || child.EndedAt.IsZero() {
		t.Fatalf("the child node = %+v", child)
	}
	if ops.Thread != v.ID || ops.CallID != root.Tools[0].ID {
		t.Fatalf("ops ran in %q under call %q, want the root's conversation under %q", ops.Thread, ops.CallID, root.Tools[0].ID)
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
	if read.Request != "what they said about the release" || !strings.HasPrefix(read.Reference, "console://") || !strings.Contains(read.Summary, "did not come from another place") || len(read.Sources) != 0 || read.Withheld {
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
	if err != nil || len(byMessage.Nodes) != 1 || byMessage.Nodes[0].Run != root.Run || len(byMessage.Nodes[0].Children) != 1 {
		t.Fatalf("by the person's message = %+v, %v", byMessage, err)
	}
	byRun, err := p.MessageTrace(t.Context(), operator, v.ID+":"+ops.ID)
	if err != nil || len(byRun.Nodes) != 1 || byRun.Nodes[0].Run != ops.ID || byRun.Nodes[0].Pick != nil || len(byRun.Nodes[0].Tools) != 1 {
		t.Fatalf("by ops's message = %+v, %v", byRun, err)
	}
	if _, err := p.MessageTrace(t.Context(), operator, v.ID+":m-999"); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("a message the root does not hold = %v", err)
	}
	other, err := p.StartSession(t.Context(), operator, platform.SessionInput{Project: "~", Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.MessageTrace(t.Context(), operator, other.ID+":"+ops.ID); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("a run of another session read under a root = %v", err)
	}
}

func TestAMessagesTraceLeavesOutACalledAgentItsReaderMayNotRead(t *testing.T) {
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
	if tr.Hidden != 1 || len(tr.Nodes) != 1 || len(tr.Nodes[0].Children) != 0 {
		t.Fatalf("with ops out of reach, the trace = %+v", tr)
	}
	// The call that brought ops in is still counted, with what it asked and
	// what came back kept from this reader.
	if calls := tr.Nodes[0].Tools; len(calls) != 1 || !calls[0].Withheld || calls[0].Input != nil || calls[0].Output != "" {
		t.Fatalf("the call to an agent out of reach = %+v", calls)
	}
	raw, _ := json.Marshal(tr)
	if strings.Contains(string(raw), ops.ID) || strings.Contains(string(raw), "plan.md") || strings.Contains(string(raw), "what they said about the release") || strings.Contains(string(raw), "Shipped.") {
		t.Fatalf("the trace shows what ops did: %s", raw)
	}
	if byRun, err := p.MessageTrace(t.Context(), operator, v.ID+":"+ops.ID); err != nil || len(byRun.Nodes) != 0 || byRun.Hidden != 1 {
		t.Fatalf("ops's own run, read by someone who may not read ops = %+v, %v", byRun, err)
	}
}
