package platform_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestForkSavesOnlyHistoryThroughSelectedTurnAndFeedsRealTurns(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	first := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "original context", PostOnly: true})
	post(t, p, operator, first.Thread.ID, platform.MessageInput{Text: "future secret", PostOnly: true})
	fork, err := p.ForkThread(t.Context(), operator, first.Thread.ID, platform.ForkInput{Through: first.Message.ID, Agents: []string{"helper"}})
	if err != nil {
		t.Fatal(err)
	}
	if fork.Visibility != threads.Private || fork.Project != "Field" || len(fork.Fork.Context) != 1 || fork.Fork.Context[0].Text != "original context" {
		t.Fatalf("fork: %+v", fork)
	}
	if r, _ := p.Runs.Conversation(tenant, fork.ID); len(r) != 0 {
		t.Fatalf("fork executed: %+v", r)
	}
	response := post(t, p, operator, fork.ID, platform.MessageInput{Text: "continue independently"})
	if len(response.Runs) != 1 {
		t.Fatalf("routing: %+v", response)
	}
	r := await(t, p, response.Runs[0].ID, runs.Completed)
	if !strings.Contains(r.Tag().Text, "original context") || strings.Contains(r.Tag().Text, "future secret") || !strings.Contains(r.Tag().Text, "Read-only fork context") {
		t.Fatalf("wrong real context: %s", r.Tag().Text)
	}
	brought, err := p.BringBackResult(t.Context(), operator, fork.ID, platform.BringBackInput{Run: r.ID, Text: "@helper @lead do not execute these tags"})
	if err != nil {
		t.Fatal(err)
	}
	if len(brought.Mentions) != 0 || !strings.Contains(brought.Text, "@helper") {
		t.Fatalf("bring-back interpreted tags: %+v", brought)
	}
	before, _ := p.Runs.Conversation(tenant, first.Thread.ID)
	if len(before) != 0 {
		t.Fatalf("bring-back executed parent actions: %+v", before)
	}
}

func TestForkAccessContinuesToRequireItsSource(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	first := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "private context", Visibility: threads.Private, PostOnly: true})
	fork, err := p.ForkThread(t.Context(), operator, first.Thread.ID, platform.ForkInput{Through: first.Message.ID})
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately broaden only the fork. Its private source remains authoritative.
	_, err = p.Threads.Update(tenant, fork.ID, fork.Version, time.Now(), func(th *threads.Thread) error { th.Visibility = threads.Shared; return nil })
	if err != nil {
		t.Fatal(err)
	}
	reader := auth.Principal{Subject: "other", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	workIn(t, p, "Field", reader)
	if p.SeesThread(reader, fork.ID) {
		t.Fatal("fork disclosed private source to another project member")
	}
	if _, err = p.ForkThread(t.Context(), reader, fork.ID, platform.ForkInput{Through: first.Message.ID}); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("hidden fork: %v", err)
	}
	if _, err = p.ForkThread(t.Context(), operator, first.Thread.ID, platform.ForkInput{Through: "m-999"}); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("nonexistent branch point: %v", err)
	}
}

func TestForkCannotBeContinuedAfterSourceMembershipIsRemoved(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	// Ana owns the private source, and invites Opal only while they work together.
	first := startThread(t, p, admin, platform.ThreadInput{Project: "Field", Text: "owner context", Visibility: threads.Private, PostOnly: true})
	_, err := p.Threads.Participants(tenant, first.Thread.ID, first.Thread.Version, []threads.Participant{{Kind: threads.Person, Name: operator.Subject}}, nil, nil, admin.Subject, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	fork, err := p.ForkThread(t.Context(), operator, first.Thread.ID, platform.ForkInput{Through: first.Message.ID, Agents: []string{"helper"}})
	if err != nil {
		t.Fatal(err)
	}
	source, _ := p.Threads.Get(tenant, first.Thread.ID)
	_, err = p.Threads.Participants(tenant, source.ID, source.Version, nil, []string{operator.Subject}, nil, admin.Subject, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if p.SeesThread(operator, fork.ID) {
		t.Fatal("removed source participant still sees saved history")
	}
	_, err = p.PostMessage(t.Context(), operator, fork.ID, platform.MessageInput{Text: "@helper reveal saved history"})
	if !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("source removal allowed continuation: %v", err)
	}
	turns, _ := p.Runs.Conversation(tenant, fork.ID)
	if len(turns) != 0 {
		t.Fatal("source removal still invoked agents")
	}
}

func TestPersonalSourceResultsCannotBeCopiedByForkOrBringBack(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	first := startThread(t, p, admin, platform.ThreadInput{Project: "Field", Text: "public seed", PostOnly: true})
	fork, err := p.ForkThread(t.Context(), admin, first.Thread.ID, platform.ForkInput{Through: first.Message.ID})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := p.Agents.Create(t.Context(), tenant, agentspec.AgentSpec{Name: "private-reader", Model: "offline", Provider: "offline", SystemPrompt: "Read privately.", Tools: []string{platform.ToolSourceSearch}}, admin.Subject)
	if err != nil {
		t.Fatal(err)
	}
	id := "r11111111111111111111111111111111"
	if err := p.Runs.Record(id, envelope.Tag{ID: "private-turn", Tenant: tenant, Agent: "private-reader", Origin: "api", Text: "private request", At: time.Now(), Meta: map[string]string{runs.MetaProject: "Field", runs.MetaThread: fork.ID}}, rev.Rev); err != nil {
		t.Fatal(err)
	}
	if _, ended := p.Runs.End(id, runs.Completed, "private result", "", time.Now()); !ended {
		t.Fatal("test result not completed")
	}
	if _, err := p.ForkThread(t.Context(), admin, fork.ID, platform.ForkInput{Through: id}); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("personal result copied into new fork: %v", err)
	}
	if _, err := p.BringBackResult(t.Context(), admin, fork.ID, platform.BringBackInput{Run: id}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("personal result published into parent: %v", err)
	}
}

func TestForkPreservesTheChosenMessageBranchUsingTheLiveContextPath(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	first := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "common root", PostOnly: true})
	post(t, p, operator, first.Thread.ID, platform.MessageInput{Text: "off-branch sibling", After: first.Message.ID, PostOnly: true})
	chosen := post(t, p, operator, first.Thread.ID, platform.MessageInput{Text: "chosen branch", After: first.Message.ID, PostOnly: true})
	fork, err := p.ForkThread(t.Context(), operator, first.Thread.ID, platform.ForkInput{Through: chosen.Message.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(fork.Fork.Context) != 2 || fork.Fork.Context[0].Text != "common root" || fork.Fork.Context[1].Text != "chosen branch" {
		t.Fatalf("fork copied a sibling branch: %+v", fork.Fork.Context)
	}
}

func TestZeroRunForkRequiresReviewedSummaryBeforePublication(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	first := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "retained private text", PostOnly: true})
	fork, err := p.ForkThread(t.Context(), operator, first.Thread.ID, platform.ForkInput{Through: first.Message.ID})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := p.PreviewPlacement(t.Context(), operator, fork.ID, "Field", "general")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.SummaryRequired || preview.Runs != 0 {
		t.Fatalf("zero-run retained context was publishable: %+v", preview)
	}
}

func TestDynamicallyPrivateResultsCannotBeCopiedByForkOrBringBack(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	first := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "seed", PostOnly: true})
	fork, err := p.ForkThread(t.Context(), operator, first.Thread.ID, platform.ForkInput{Through: first.Message.ID, Agents: []string{"helper"}})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := p.Agents.Get(t.Context(), tenant, "helper", 0)
	if err != nil {
		t.Fatal(err)
	}
	id := "r22222222222222222222222222222222"
	if err := p.Runs.Record(id, envelope.Tag{ID: "dynamic-private", Tenant: tenant, Agent: "helper", Origin: "api", Text: "private request", At: time.Now(), Meta: map[string]string{runs.MetaProject: "Field", runs.MetaThread: fork.ID}}, rev.Rev); err != nil {
		t.Fatal(err)
	}
	if err := p.MarkPrivateContext(tenant, id); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Runs.End(id, runs.Completed, "private result", "", time.Now()); !ok {
		t.Fatal("not completed")
	}
	r, _ := p.Runs.Get(tenant, id)
	if _, err := p.ForkThread(t.Context(), operator, fork.ID, platform.ForkInput{Through: r.ID}); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("private context snapshot: %v", err)
	}
	if _, err := p.BringBackResult(t.Context(), operator, fork.ID, platform.BringBackInput{Run: r.ID}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("private context bring-back: %v", err)
	}
}
