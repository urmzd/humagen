package platform_test

import (
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// listedAs is the row a session listing shows who for the session id.
func listedAs(t *testing.T, p *platform.Platform, who auth.Principal, id string) platform.ThreadSummary {
	t.Helper()
	rows, _, err := p.SessionsPage(t.Context(), who, platform.SessionQuery{}, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == id {
			return r.Conversation
		}
	}
	t.Fatalf("%s's listing has no session %s", who.Subject, id)
	return platform.ThreadSummary{}
}

// A question the agent asks of olu, when only grace may answer it, waits on
// grace: the listing says so to grace, and not to olu, whom answering would
// refuse. Whom it waits on is read from the question as it was raised to
// the inbox, not from the agent's hint.
func TestAListingSaysAQuestionWaitsOnWhoMayAnswerItNotOnWhomTheAgentNamed(t *testing.T) {
	p := stage(t, &cast{continues: true,
		says: map[string][][]saigetypes.Delta{
			"picker": {asked(map[string]any{"question": "Which region should I deploy to?", "asked_of": "olu"}), say("Done.")},
		},
		tools: map[string][]string{"picker": {"ask_person"}},
		shape: map[string]func(*wire.AgentSpec){"picker": func(s *wire.AgentSpec) { s.Answerers = []string{"grace"} }},
	}, platform.Config{})
	seePeople(t, p, admin, olu, grace)
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Agent: "picker", Text: "Deploy it"})
	if err != nil {
		t.Fatal(err)
	}
	var parked runs.Run
	within(t, "picker parked on its question", func() bool {
		r, ok := runOf(p, tenant, "picker")
		parked = r
		return ok && r.Status == runs.Parked && r.Question != nil
	})
	questionFor(t, p, parked.ID)
	// olu and grace take part, so the session is listed to them.
	if _, err := p.Threads.Join(tenant, v.ID, []threads.Participant{
		{Kind: threads.Person, Name: "olu", At: time.Now()},
		{Kind: threads.Person, Name: "grace", At: time.Now()},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if row := listedAs(t, p, olu, v.ID); row.WaitingOnYou {
		t.Fatalf("olu, whom answering refuses, is told it waits on him: %+v", row)
	}
	row := listedAs(t, p, grace, v.ID)
	if !row.WaitingOnYou || row.WaitingKind != platform.WaitingQuestion || row.WaitingFor != "Which region should I deploy to?" {
		t.Fatalf("grace, the one who may answer, is not told: %+v", row)
	}
}

// An agent called in a session parks on a write its spec has ada and opal
// approve, and opal started the session. The question is the session's: ada,
// who may approve, is told it waits on her, though the run that asked is a
// called agent's and not the session's own turn. opal is not, though she is
// listed as an approver: she asked for the work, and answering refuses her.
func TestACalledAgentsApprovalWaitsOnItsApproverAndNeverOnTheRequester(t *testing.T) {
	c := entryCast([][]saigetypes.Delta{
		agentCall("ask-ops", "ops", "Write the plan", "Write the deploy plan."),
		say("ops wrote it."),
	}, map[string][][]saigetypes.Delta{"ops": {writing("plan.md", "ship"), say("Written.")}})
	c.tools = map[string][]string{"ops": {platform.ToolFileWrite}}
	brief := c.shape["ops"]
	c.shape["ops"] = func(s *wire.AgentSpec) {
		if brief != nil {
			brief(s)
		}
		s.Approvals, s.Approvers = []string{platform.ToolFileWrite}, []string{admin.Subject, operator.Subject}
	}
	p := stage(t, c, platform.Config{TestBuiltinProvider: "offline", TestBuiltinModel: "offline", Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "*"})
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	seePeople(t, p, operator, admin)
	v, err := p.StartSession(t.Context(), operator, platform.SessionInput{Project: "~", Text: "Write the deploy plan."})
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
	row := listedAs(t, p, admin, v.ID)
	if !row.WaitingOnYou || row.WaitingKind != platform.WaitingApproval || row.WaitingFor != platform.ToolFileWrite {
		t.Fatalf("ada, who may approve the called agent's write, is not told: %+v", row)
	}
	if row := listedAs(t, p, operator, v.ID); row.WaitingOnYou {
		t.Fatalf("opal asked for the work and may not approve it, but is told it waits on her: %+v", row)
	}
}
