package platform

import (
	"context"
	"testing"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/orchagent/flow/runtime"
)

type followupAnswer string

func (a followupAnswer) Ask(context.Context, join.Candidate, string) (join.Decision, error) {
	return join.Decision{}, nil
}
func (a followupAnswer) Followup(context.Context, string, string) (join.Followup, error) {
	return join.Followup{Outcome: string(a), Reason: "person changed the request"}, nil
}

func TestFollowupWaitsForSettlementAndKeepsDecision(t *testing.T) {
	for _, outcome := range []string{"queue", "replace", "stop"} {
		t.Run(outcome, func(t *testing.T) {
			ctx := t.Context()
			p, err := New(Config{Workspace: t.TempDir(), BuiltinProvider: "offline", BuiltinModel: "offline", Router: func(string) join.Asker { return followupAnswer(outcome) }})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
				t.Fatal(err)
			}
			who := auth.Principal{Tenant: "acme", Subject: "ana", Name: "Ana", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
			first, err := p.StartThread(ctx, who, ThreadInput{Text: "hello", Agents: []string{BuiltinEntry}, Visibility: threads.Private})
			if err != nil || len(first.Runs) != 1 {
				t.Fatalf("start: %+v %v", first, err)
			}
			stopped := 0
			p.stops = stopFunc(func(context.Context, runtime.StopRequest) error { stopped++; return nil })
			p.asked = nil
			posted, err := p.PostMessage(ctx, who, first.Thread.ID, MessageInput{Text: "new ask"})
			if err != nil {
				t.Fatal(err)
			}
			if posted.Message.Followup == nil || posted.Message.Followup.Outcome != outcome || len(posted.Runs) != 0 {
				t.Fatalf("post: %+v", posted)
			}
			if (stopped > 0) != (outcome != "queue") {
				t.Fatalf("stops=%d", stopped)
			}
			r := first.Runs[0]
			p.Runs.End("acme", r.ID, runs.Completed, "progress", "", p.now())
			if err := p.drainFollowups(ctx, "acme", first.Thread.ID); err != nil {
				t.Fatal(err)
			}
			m, err := p.Threads.Message("acme", first.Thread.ID, posted.Message.ID)
			if err != nil || !m.Followup.Handled {
				t.Fatalf("message: %+v %v", m, err)
			}
			all, err := p.Runs.Conversation("acme", first.Thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if outcome == "stop" {
				want = 1
			}
			if len(all) != want {
				t.Fatalf("runs=%d want %d", len(all), want)
			}
			if err := p.drainFollowups(ctx, "acme", first.Thread.ID); err != nil {
				t.Fatal(err)
			}
			again, _ := p.Runs.Conversation("acme", first.Thread.ID)
			if len(again) != want {
				t.Fatal("duplicate dispatch")
			}
		})
	}
}
