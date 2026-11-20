package platform_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"
)

func modelFor(t *testing.T, c *cast, name string) *agenttest.ScriptedProvider {
	t.Helper()
	v, ok := c.models.Load(name)
	if !ok {
		t.Fatalf("no model for %s", name)
	}
	return v.(*agenttest.ScriptedProvider)
}

// callInput is a call's constructed input: its messages and the format the
// helper answers in.
func callInput(format call.ResponseFormat, messages ...call.Message) *call.Input {
	return &call.Input{Messages: messages, ResponseFormat: format}
}

func textMessage(role, text string) call.Message {
	return call.Message{Role: role, Content: []call.Part{call.TextPart{Text: text}}}
}

// A call with constructed input (what POST /api/runs/{id}/calls sends) gives
// the helper exactly those messages, with the return instruction its format
// asks for, and nothing of its caller's conversation.
func TestHelperReceivesConstructedMessagesAndReturnsOneResultToItsCaller(t *testing.T) {
	c := &cast{says: map[string][][]saigetypes.Delta{
		"lead":   {say("Parent answer")},
		"helper": {say("Three tests fail. Investigate fixtures.")},
	}, spawn: map[string][]string{"lead": {"helper"}}, gates: map[string]chan struct{}{"lead": make(chan struct{})}}
	p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "explicit-call", Agent: "lead", Text: "PRIVATE PARENT CONTEXT"})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, first.ID, runs.Running)
	child, err := p.Call(t.Context(), tenant, first.ID, call.Request{Key: "constructed", Agent: "helper", Mode: call.Subagent, Input: callInput(call.SummaryFormat{MaxWords: 8},
		textMessage("user", "Example question"), textMessage("assistant", "Example summary"), textMessage("user", "Inspect the tests"))})
	if err != nil {
		t.Fatal(err)
	}
	helper := await(t, p, child.ID, runs.Completed)
	c.release("lead")
	await(t, p, first.ID, runs.Completed)
	if helper.Input == nil || len(helper.Input.Messages) != 3 {
		t.Fatal("helper input is missing from run details")
	}
	requests := modelFor(t, c, "helper").Requests()
	if len(requests) != 1 {
		t.Fatalf("helper made %d calls", len(requests))
	}
	for _, tool := range requests[0].Tools {
		if tool.Name == platform.ToolAgent {
			t.Fatal("helper without a spawn grant received the agent tool")
		}
	}
	msgs := requests[0].Messages
	if len(msgs) != 4 {
		t.Fatalf("helper input has %d messages: %+v", len(msgs), msgs)
	}
	if msgs[0].Role() != saigetypes.RoleSystem || !strings.Contains(saigetypes.TextOf(msgs[0]), "within 8 words") {
		t.Fatalf("return instruction missing: %+v", msgs[0])
	}
	if msgs[2].Role() != saigetypes.RoleAssistant || saigetypes.TextOf(msgs[2]) != "Example summary" {
		t.Fatal("example role or text changed")
	}
	for _, m := range msgs {
		if strings.Contains(saigetypes.TextOf(m), "PRIVATE PARENT CONTEXT") {
			t.Fatal("parent context was inherited")
		}
	}
	if helper.Answer != "Three tests fail. Investigate fixtures." {
		t.Fatalf("helper answer = %q", helper.Answer)
	}
}

func TestInvalidHelperResultFailsWithoutPublishingAnAnswer(t *testing.T) {
	c := &cast{says: map[string][][]saigetypes.Delta{
		"lead":   {say("Parent answer")},
		"helper": {say("This is longer than two words.")},
	}, spawn: map[string][]string{"lead": {"helper"}}, gates: map[string]chan struct{}{"lead": make(chan struct{})}}
	p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "bad-format", Agent: "lead", Text: "Start"})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, first.ID, runs.Running)
	child, err := p.Call(t.Context(), tenant, first.ID, call.Request{Key: "bad-format", Agent: "helper", Mode: call.Subagent, Input: callInput(call.SummaryFormat{MaxWords: 2}, textMessage("user", "Return a brief summary"))})
	if err != nil {
		t.Fatal(err)
	}
	helper := await(t, p, child.ID, runs.Failed)
	c.release("lead")
	if !strings.Contains(helper.Error, "summary exceeds 2 words") {
		t.Fatalf("helper error = %q", helper.Error)
	}
	for _, e := range kept(p.Journal.Events(tenant, []string{helper.ID})) {
		if e.Type == event.TypeText || e.Type == event.TypeCompleted {
			t.Fatalf("invalid answer published as %s", e.Type)
		}
	}
}

func TestCallReferencesAreTenantScopedAndFrozenBeforeExecution(t *testing.T) {
	c := &cast{
		says:  map[string][][]saigetypes.Delta{"source": {say("Reference answer")}, "lead": {say("Parent answer")}, "helper": {say("Understood")}},
		spawn: map[string][]string{"lead": {"helper"}},
		gates: map[string]chan struct{}{"lead": make(chan struct{}), "helper": make(chan struct{})},
	}
	p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 1}})
	source, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "source", Agent: "source", Text: "Reference question"})
	if err != nil {
		t.Fatal(err)
	}
	source = await(t, p, source.ID, runs.Completed)
	info, err := p.Blobs.Put(t.Context(), tenant, "reference.txt", strings.NewReader("ORIGINAL ARTIFACT"), 100)
	if err != nil {
		t.Fatal(err)
	}
	art, err := p.Artifacts.Publish(tenant, artifacts.Artifact{ID: "reference", Key: info.Key, Title: "ref.txt", Thread: source.Thread, Kind: artifacts.Document, Size: info.Size})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "reference-parent", Agent: "lead", Text: "PARENT SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, parent.ID, runs.Running)
	if err := p.Runs.Record("foreign", wire.NewEvent(wire.Raise{ID: "foreign", Tenant: "other", Agent: "source", Origin: runs.OriginAPI, Text: "OTHER TENANT SECRET"}), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Artifacts.Publish("other", artifacts.Artifact{ID: "foreign-artifact", Key: "secret.txt", Title: "secret.txt", Thread: "foreign", Kind: artifacts.Document}); err != nil {
		t.Fatal(err)
	}
	inputFor := func(part call.Part) *call.Input {
		return &call.Input{Messages: []call.Message{{Role: "user", Content: []call.Part{part}}}, ResponseFormat: call.TextFormat{}}
	}
	for _, tc := range []struct {
		name string
		part call.Part
		want error
	}{
		{"missing run", call.RunResultPart{Run: "missing"}, runs.ErrNotFound},
		{"foreign run", call.RunResultPart{Run: "foreign"}, runs.ErrNotFound},
		{"foreign artifact", call.ArtifactPart{ID: "foreign-artifact", Version: 1}, artifacts.ErrNotFound},
		{"wrong version", call.ArtifactPart{ID: art.ID, Version: 2}, call.ErrInvalid},
		{"wrong thread", call.ConversationRange{Thread: "wrong", From: source.ID, Through: source.ID}, call.ErrInvalid},
		{"unrelated endpoints", call.ConversationRange{Thread: source.Thread, From: parent.ID, Through: source.ID}, call.ErrInvalid},
		{"running result", call.RunResultPart{Run: parent.ID}, call.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := p.Call(t.Context(), tenant, parent.ID, call.Request{Key: tc.name, Agent: "helper", Mode: call.Subagent, Input: inputFor(tc.part)})
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	input := &call.Input{Messages: []call.Message{{Role: "user", Content: []call.Part{
		call.TextPart{Text: "Inspect these references"},
		call.ConversationRange{Thread: source.Thread, From: source.ID, Through: source.ID},
		call.RunResultPart{Run: source.ID}, call.ArtifactPart{ID: art.ID, Version: 1},
	}}}, ResponseFormat: call.TextFormat{}}
	request := call.Request{Key: "frozen", Agent: "helper", Mode: call.Subagent, Input: input}
	child, err := p.Call(t.Context(), tenant, parent.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	// Change and then remove the original reference after acceptance. Neither
	// execution nor an idempotent replay may resolve it again.
	if _, err := p.Blobs.Put(t.Context(), tenant, info.Key, strings.NewReader("CHANGED ARTIFACT"), 100); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Artifacts.Delete(tenant, art.ID); err != nil {
		t.Fatal(err)
	}
	replay, err := p.Call(t.Context(), tenant, parent.ID, request)
	if err != nil || replay.ID != child.ID {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
	conflict := request
	conflict.Input = inputFor(call.TextPart{Text: "Different request"})
	if _, err := p.Call(t.Context(), tenant, parent.ID, conflict); !errors.Is(err, call.ErrInvalid) {
		t.Fatalf("changed input reused call ID: %v", err)
	}
	c.release("helper")
	await(t, p, child.ID, runs.Completed)
	messages := modelFor(t, c, "helper").Requests()[0].Messages
	var seen strings.Builder
	for _, m := range messages {
		seen.WriteString(saigetypes.TextOf(m))
	}
	for _, text := range []string{"Inspect these references", "Reference question", "Reference answer", "ORIGINAL ARTIFACT"} {
		if !strings.Contains(seen.String(), text) {
			t.Errorf("resolved input missing %q", text)
		}
	}
	for _, text := range []string{"CHANGED ARTIFACT", "PARENT SECRET", "OTHER TENANT SECRET"} {
		if strings.Contains(seen.String(), text) {
			t.Errorf("input contains %q", text)
		}
	}
	var frozen call.Prepared
	if err := json.Unmarshal([]byte(wire.Meta(child.Tag())[call.MetaInput]), &frozen); err != nil {
		t.Fatal(err)
	}
	if _, ok := frozen.Input.Messages[0].Content[3].(call.ArtifactPart); !ok {
		t.Fatal("reference provenance was lost")
	}
	c.release("lead")
	await(t, p, parent.ID, runs.Completed)
}

// A call is idempotent on its key: the same key under the same caller joins
// the run it started, however often it is made, and starts no second run.
func TestTheSameCallKeyJoinsTheSameRun(t *testing.T) {
	c := &cast{
		says:  map[string][][]saigetypes.Delta{"lead": {say("Parent answer")}, "helper": {say("Once")}},
		spawn: map[string][]string{"lead": {"helper"}},
		gates: map[string]chan struct{}{"lead": make(chan struct{}), "helper": make(chan struct{})},
	}
	p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 2}})
	parent, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "idempotent-parent", Agent: "lead", Text: "Start"})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, parent.ID, runs.Running)
	request := call.Request{Key: "once", Agent: "helper", Mode: call.Subagent, Text: "Do it once"}
	first, err := p.Call(t.Context(), tenant, parent.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		again, err := p.Call(t.Context(), tenant, parent.ID, request)
		if err != nil || again.ID != first.ID {
			t.Fatalf("call %d with the same key = %q, %v, want %q", i+2, again.ID, err, first.ID)
		}
	}
	other, err := p.Call(t.Context(), tenant, parent.ID, call.Request{Key: "twice", Agent: "helper", Mode: call.Subagent, Text: "Do it once"})
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == first.ID {
		t.Fatal("a different key joined the first call's run")
	}
	children := 0
	for _, r := range p.Runs.List(tenant, 50) {
		if r.Parent == parent.ID && r.Agent == "helper" {
			children++
		}
	}
	if children != 2 {
		t.Fatalf("the caller has %d helper runs, want 2: one per key", children)
	}
	c.release("helper")
	await(t, p, first.ID, runs.Completed)
}
