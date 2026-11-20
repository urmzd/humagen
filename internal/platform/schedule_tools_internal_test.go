package platform

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// A schedule's cron reads in words on its approval card and in the tool's
// answer; a shape the words do not know is the expression, said to be UTC.
func TestCronInWords(t *testing.T) {
	for expr, want := range map[string]string{
		"* * * * *":      "every minute",
		"*/1 * * * *":    "every minute",
		"*/15 * * * *":   "every 15 minutes",
		"0 * * * *":      "every hour",
		"5 * * * *":      "every hour at minute 5",
		"0 */2 * * *":    "every 2 hours",
		"30 */6 * * *":   "every 6 hours at minute 30",
		"0 9 * * *":      "daily at 09:00 UTC",
		"5 8 * * 1-5":    "weekdays at 08:05 UTC",
		"0 10 * * 0,6":   "weekends at 10:00 UTC",
		"9 8 * * 1":      "Mondays at 08:09 UTC",
		"0 7 * * 7":      "Sundays at 07:00 UTC",
		"0 9 1 * *":      `on the cron schedule "0 9 1 * *" (UTC)`,
		"0 9 * * 1,3":    `on the cron schedule "0 9 * * 1,3" (UTC)`,
		"nonsense":       `on the cron schedule "nonsense" (UTC)`,
		" */5  * * * * ": "every 5 minutes",
	} {
		if got := CronInWords(expr); got != want {
			t.Errorf("CronInWords(%q) = %q, want %q", expr, got, want)
		}
	}
}

// schedulePlatform is backgroundPlatform: ada's conversation with lead,
// filed in the project Field.
func schedulePlatform(t *testing.T) (*Platform, runs.Run, runtime.Request) {
	t.Helper()
	p, run, req := backgroundPlatform(t)
	th, err := p.ThreadRecord("acme", run.Thread)
	if err != nil {
		t.Fatal(err)
	}
	if th.Project != "Field" {
		t.Fatalf("the conversation is filed in %q", th.Project)
	}
	return p, run, req
}

// recordRun records a run of lead that acts for actor in the conversation,
// with the origin and meta a guard reads.
func recordRun(t *testing.T, p *Platform, id, origin, thread string, actor event.Actor, meta map[string]string) runtime.Request {
	t.Helper()
	ctx := map[string]string{runs.MetaThread: thread}
	for k, v := range meta {
		ctx[k] = v
	}
	tag := wire.NewEvent(wire.Raise{ID: id, Tenant: "acme", Agent: "lead", Origin: origin, Actor: actor, At: time.Now(), Context: ctx})
	if err := p.Runs.Record(id, tag, 1); err != nil {
		t.Fatal(err)
	}
	return runtime.Request{RunID: id, Tenant: "acme", Definition: native.Revision{Spec: native.Spec{Name: "lead"}, Version: wire.VersionOf(1)}}
}

// heldSchedule is a schedule tool as a turn has it, behind the one wrapper.
func heldSchedule(p *Platform, req runtime.Request, name string) native.Approving {
	return p.operationTool(req, &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: name}}, p.toolDeclaration(name)).(native.Approving)
}

// Every schedule call is asked of the person the run acts for, who may
// answer it themselves, even where the action policy allows the category;
// and the card says what the call does.
func TestEveryScheduleCallIsAskedOfThePerson(t *testing.T) {
	p, _, req := schedulePlatform(t)
	admin := auth.Principal{Tenant: "acme", Subject: "root", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	if _, err := p.SetActionPolicy(admin, operations.Scope{Kind: "workspace"}, map[operations.Category]operations.Level{operations.Other: operations.Allow}, 0); err != nil {
		t.Fatal(err)
	}
	if got := p.operationDecision(req, operations.Other); got != operations.Allow {
		t.Fatalf("the policy for other is %s, want allow", got)
	}
	for name, c := range map[string]struct {
		args   map[string]any
		prompt string
	}{
		ToolCreateSchedule: {map[string]any{"name": "plaud-new", "cron": "*/15 * * * *", "check": "New recordings?"}, "Create a schedule: plaud-new, every 15 minutes, keeps each answer in its own session"},
		ToolUpdateSchedule: {map[string]any{"name": "plaud-new", "cron": "0 9 * * 1-5", "check": "x", "agent": "helper", "post": "slack", "connection": "ops", "to": "C0123ABC"}, "Change the schedule plaud-new, weekdays at 09:00 UTC, run by helper, posts in Slack channel C0123ABC only when something fires"},
		ToolPauseSchedule:  {map[string]any{"name": "plaud-new"}, "Pause the schedule plaud-new"},
		ToolResumeSchedule: {map[string]any{"name": "plaud-new"}, "Resume the schedule plaud-new"},
		ToolRunSchedule:    {map[string]any{"name": "plaud-new"}, "Run plaud-new once now"},
	} {
		got, needed := heldSchedule(p, req, name).Approval(saigetypes.ToolCallPart{Name: name, Arguments: c.args})
		if !needed {
			t.Errorf("%s ran unasked where the policy allows", name)
			continue
		}
		if got.Prompt != c.prompt || !slices.Equal(got.Approvers, []string{"ada"}) || !got.SelfApproval || got.ExpiresIn != 24*time.Hour {
			t.Errorf("%s asks %+v, want %q of ada alone, for a day", name, got, c.prompt)
		}
	}
	if _, err := p.SetActionPolicy(admin, operations.Scope{Kind: "workspace"}, map[operations.Category]operations.Level{operations.Other: operations.Never}, 1); err != nil {
		t.Fatal(err)
	}
	// Where the policy refuses the category, the wrapper refuses the call.
	tool := p.operationTool(req, p.createScheduleTool(req), p.toolDeclaration(ToolCreateSchedule))
	if _, err := tool.Execute(t.Context(), map[string]any{"name": "x"}); !errors.Is(err, operations.ErrDenied) {
		t.Errorf("a policy of never let a schedule be made: %v", err)
	}
}

// In a Slack thread, "here" posts in that thread's channel, through the
// connection it came in by, and the card says so; outside one it is refused.
func TestHereIsTheConversationsSlackChannel(t *testing.T) {
	th := threads.Thread{ID: "th-1", Project: "Field", Surface: &threads.Surface{Client: "ops-slack", Channel: "C0PLAUD", TS: "1.2"}}
	deliver, client, to, err := scheduleDelivery(th, map[string]any{"post": "here"})
	if err != nil || deliver != DeliverSlack || client != "ops-slack" || to != "C0PLAUD" {
		t.Fatalf("here = %q %q %q %v", deliver, client, to, err)
	}
	if got := postsIn(th, deliver, to); got != "posts in this Slack channel (C0PLAUD) only when something fires" {
		t.Errorf("posts in: %q", got)
	}
	if _, _, _, err := scheduleDelivery(threads.Thread{ID: "th-2", Project: "Field"}, map[string]any{"post": "here"}); err == nil {
		t.Error("here was taken outside a Slack thread")
	}
	for _, args := range []map[string]any{{"post": "slack"}, {"post": "github", "connection": "gh"}, {"post": "email"}} {
		if _, _, _, err := scheduleDelivery(th, args); err == nil {
			t.Errorf("%v was taken", args)
		}
	}
}

// An evaluation, housekeeping, a run for nobody, background work and a
// scheduled run make and run no schedules: each is refused before the API
// is reached, and none is asked about, since it would be refused anyway. A
// conversation in no project has nowhere to file one.
func TestScheduleToolsRefuseRunsThatActForNoPerson(t *testing.T) {
	p, run, _ := schedulePlatform(t)
	reached := 0
	p.SetAPIDispatch(func(context.Context, auth.Principal, string, string, url.Values, []byte) (int, []byte) {
		reached++
		return 500, nil
	})
	ada := event.Actor{User: "ada"}
	for why, req := range map[string]runtime.Request{
		"an evaluation":     recordRun(t, p, "r-eval", OriginEval, run.Thread, ada, nil),
		"a dry run":         recordRun(t, p, "r-dry", runs.OriginAPI, run.Thread, ada, map[string]string{metaDryRun: "true"}),
		"housekeeping":      recordRun(t, p, "r-hook", runs.OriginSystem, run.Thread, ada, nil),
		"a run for nobody":  recordRun(t, p, "r-nobody", runs.OriginAPI, run.Thread, event.Actor{}, nil),
		"a bot":             recordRun(t, p, "r-bot", runs.OriginAPI, run.Thread, event.Actor{User: "ada", Bot: true}, nil),
		"background work":   recordRun(t, p, "r-bg", runs.OriginAPI, run.Thread, ada, map[string]string{metaBackground: "bg-1"}),
		"a scheduled run":   recordRun(t, p, "r-cron", runs.OriginManual, run.Thread, ada, map[string]string{wire.MetaCronSchedule: "nightly"}),
		"a missing run":     {RunID: "r-missing", Tenant: "acme", Definition: native.Revision{Spec: native.Spec{Name: "lead"}}},
		"another workspace": {RunID: run.ID, Tenant: "other", Definition: native.Revision{Spec: native.Spec{Name: "lead"}}},
	} {
		for _, name := range ScheduleTools() {
			args := map[string]any{"name": "plaud-new", "cron": "*/15 * * * *", "check": "x"}
			if _, _, err := p.UseTool(t.Context(), req, name, args); err == nil {
				t.Errorf("%s used %s", why, name)
			}
			if _, needed := declaredApproval(req, p.toolDeclaration(name), saigetypes.ToolCallPart{Name: name, Arguments: args}); needed {
				t.Errorf("%s was asked about %s, which it may not use", why, name)
			}
		}
	}
	// A conversation in no project.
	root, err := p.Converse(t.Context(), "acme", Turn{ID: "api-root", Agent: "lead", Text: "hi", Actor: event.Actor{User: "ada"}})
	if err != nil {
		t.Fatal(err)
	}
	root = awaitEnd(t, p, root.ID)
	rootReq := runtime.Request{RunID: root.ID, Tenant: "acme", Definition: native.Revision{Spec: native.Spec{Name: "lead"}}}
	if _, _, err := p.UseTool(t.Context(), rootReq, ToolCreateSchedule, map[string]any{"name": "x", "cron": "* * * * *", "check": "x"}); !errors.Is(err, auth.ErrForbidden) || !strings.Contains(err.Error(), "no project") {
		t.Errorf("a conversation in no project made a schedule: %v", err)
	}
	if reached != 0 {
		t.Errorf("a refused call reached the API %d times", reached)
	}
}
