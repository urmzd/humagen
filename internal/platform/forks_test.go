package platform_test

import (
	"errors"
	"strings"
	"testing"
	"time"

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
