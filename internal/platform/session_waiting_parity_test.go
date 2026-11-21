package platform_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// A session listing says a question waits on its reader by the rule
// answering holds them to. These tests hold the three places that say so to
// one answer, for each thing answering checks of a person: the listing
// (waiting_on_you), the thread (Ask.Yours, which is checkAnswer itself) and
// the inbox (the actions it offers).

// standsTo fails unless the listing, the thread and the inbox all say that
// the question m of run, in session, is or is not who's to answer.
func standsTo(t *testing.T, p *platform.Platform, who auth.Principal, session string, run runs.Run, m inbox.Message, yours bool) {
	t.Helper()
	row := listedAs(t, p, who, session)
	if row.WaitingOnYou != yours || row.WaitingUnknown {
		t.Errorf("%s: the listing says waiting_on_you=%v unknown=%v, want %v and known", who.Subject, row.WaitingOnYou, row.WaitingUnknown, yours)
	}
	if one, err := p.ReadSession(t.Context(), who, session, "", "", false); err != nil {
		t.Errorf("%s reads the session: %v", who.Subject, err)
	} else if one.Conversation.WaitingOnYou != yours || one.Conversation.WaitingUnknown {
		t.Errorf("%s: the session read alone says waiting_on_you=%v unknown=%v, want %v and known", who.Subject, one.Conversation.WaitingOnYou, one.Conversation.WaitingUnknown, yours)
	}
	turns := p.AsksIn(t.Context(), who, []platform.ThreadTurn{{Kind: "run", Run: &run}})
	if asked := turns[0].Ask != nil && turns[0].Ask.Yours; asked != yours {
		t.Errorf("%s: answering would say %v (ask %+v), and the listing %v", who.Subject, asked, turns[0].Ask, yours)
	}
	offered := false
	if got, err := p.InboxGet(who, m.ID); err == nil {
		for _, part := range got.Parts {
			offered = offered || len(part.Actions) > 0
		}
	}
	if offered != yours {
		t.Errorf("%s: the inbox offers an answer: %v, and the listing says %v", who.Subject, offered, yours)
	}
}

// joined adds people to a session, so it is in their listing.
func joined(t *testing.T, p *platform.Platform, session string, who ...auth.Principal) {
	t.Helper()
	var people []threads.Participant
	for _, w := range who {
		people = append(people, threads.Participant{Kind: threads.Person, Name: w.Subject, At: time.Now()})
	}
	if _, err := p.Threads.Join(tenant, session, people, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// parkedIn starts a session with agent and waits for its run to park on a
// question that reached the inbox.
func parkedIn(t *testing.T, p *platform.Platform, by auth.Principal, agent string) (string, runs.Run, inbox.Message) {
	t.Helper()
	v, err := p.StartSession(t.Context(), by, platform.SessionInput{Project: "~", Agent: agent, Text: "Go"})
	if err != nil {
		t.Fatal(err)
	}
	var parked runs.Run
	within(t, agent+" parked on its question", func() bool {
		r, ok := runOf(p, tenant, agent)
		parked = r
		return ok && r.Status == runs.Parked && r.Question != nil
	})
	return v.ID, parked, questionFor(t, p, parked.ID)
}

// picking is a platform whose picker asks a question its answerers answer.
func picking(t *testing.T, cfg platform.Config, answerers ...string) *platform.Platform {
	t.Helper()
	return stage(t, &cast{continues: true,
		says: map[string][][]saigetypes.Delta{
			"picker": {asked(map[string]any{"question": "Which region should I deploy to?"}), say("Done.")},
		},
		tools: map[string][]string{"picker": {"ask_person"}},
		shape: map[string]func(*wire.AgentSpec){"picker": func(s *wire.AgentSpec) { s.Answerers = answerers }},
	}, cfg)
}

// A budget hold is addressed to everyone, and only billing:write grants it:
// the listing says it waits on who may grant, and never on an operator whom
// the inbox offers nothing and answering refuses.
func TestABudgetHoldWaitsOnlyOnWhoMayGrantIt(t *testing.T) {
	p := approving(t)
	seePeople(t, p, admin, operator, billing)
	v, err := p.StartSession(t.Context(), operator, platform.SessionInput{Project: "~", Agent: "lead", Text: "Go"})
	if err != nil {
		t.Fatal(err)
	}
	var parked runs.Run
	within(t, "lead held at its limit", func() bool {
		r, ok := runOf(p, tenant, "lead")
		parked = r
		return ok && r.Status == runs.Parked && r.Question != nil
	})
	m := heldOn(t, p, parked.ID)
	if len(m.Audience) != 0 {
		t.Fatalf("a budget hold is addressed to %v, so this tests nothing", m.Audience)
	}
	joined(t, p, v.ID, billing, admin)
	standsTo(t, p, operator, v.ID, parked, m, false)
	standsTo(t, p, billing, v.ID, parked, m, true)
	standsTo(t, p, admin, v.ID, parked, m, true)
	if row := listedAs(t, p, operator, v.ID); row.WaitingKind != platform.WaitingApproval || row.WaitingFor == "" {
		t.Fatalf("the operator's row no longer says what the run waits for: %+v", row)
	}
}

// A question for the spec's answerers waits on an answerer who may write
// runs: not on one who may only read, not on a participant it does not name,
// and not on whoever started the session.
func TestAQuestionWaitsOnItsAnswerersAsAnsweringDecides(t *testing.T) {
	p := picking(t, platform.Config{}, "grace", "vic")
	seePeople(t, p, admin, olu, grace, vic)
	session, parked, m := parkedIn(t, p, admin, "picker")
	joined(t, p, session, olu, grace, vic)
	standsTo(t, p, grace, session, parked, m, true)
	standsTo(t, p, vic, session, parked, m, false)
	standsTo(t, p, olu, session, parked, m, false)
	standsTo(t, p, admin, session, parked, m, false)
}

// An approval the spec has ada and opal approve, in a session opal started:
// opal asked for the work, under every name she goes by, so it waits on ada
// alone. A spec that allows self-approval lets it wait on opal too.
func TestAnApprovalWaitsOnItsApproversLessTheRequesterUnlessTheSpecAllowsIt(t *testing.T) {
	for _, self := range []bool{false, true} {
		t.Run(fmt.Sprintf("self_approval=%v", self), func(t *testing.T) {
			p := stage(t, &cast{continues: true,
				says:  map[string][][]saigetypes.Delta{"deployer": {writing("plan.md", "ship"), say("Written.")}},
				tools: map[string][]string{"deployer": {platform.ToolFileWrite}},
				shape: map[string]func(*wire.AgentSpec){"deployer": func(s *wire.AgentSpec) {
					s.Approvals, s.Approvers, s.SelfApproval = []string{platform.ToolFileWrite}, []string{admin.Subject, operator.Subject}, self
				}},
			}, platform.Config{})
			seePeople(t, p, admin, operator, olu)
			session, parked, m := parkedIn(t, p, operator, "deployer")
			joined(t, p, session, admin, olu)
			standsTo(t, p, admin, session, parked, m, true)
			standsTo(t, p, operator, session, parked, m, self)
			// olu may write runs and is no approver.
			standsTo(t, p, olu, session, parked, m, false)
		})
	}
}

// A session started from Slack for ana is asked for by ana, by her Slack
// name and by the app's credential: the approval waits on none of them in a
// listing, as answering refuses her through the app too, and waits on olu,
// who did not ask.
func TestAnApprovalAskedForThroughSlackWaitsOnNoNameOfItsRequester(t *testing.T) {
	staged := stage(t, &cast{continues: true,
		says:  map[string][][]saigetypes.Delta{"deployer": {writing("plan.md", "ship"), say("Written.")}},
		tools: map[string][]string{"deployer": {platform.ToolFileWrite}},
		shape: map[string]func(*wire.AgentSpec){"deployer": func(s *wire.AgentSpec) { s.Approvals = []string{platform.ToolFileWrite} }},
	}, (&fakes{}).config())
	p, machine := slackRoomOn(t, staged, func(in *platform.ClientInput) { in.DefaultAgent = "deployer" })
	ana := auth.Principal{Subject: "ana", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	seePeople(t, p, olu)
	v, started, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession("C01RENEW", "1700000000.000100", "", "<@UAPP> ship the plan"), platform.ChannelPublic))
	if err != nil || !started {
		t.Fatalf("the Slack session = %v, started %v", err, started)
	}
	var parked runs.Run
	within(t, "deployer parked on its write", func() bool {
		r, ok := runOf(p, tenant, "deployer")
		parked = r
		return ok && r.Status == runs.Parked && r.Question != nil
	})
	m := questionFor(t, p, parked.ID)
	if !slices.Contains(m.Excluded, "ana") || len(m.Excluded) < 2 {
		t.Fatalf("the requester is excluded as %v: not by every name, so this tests nothing", m.Excluded)
	}
	joined(t, p, v.ID, ana, olu)
	standsTo(t, p, ana, v.ID, parked, m, false)
	standsTo(t, p, olu, v.ID, parked, m, true)
	// Answering refuses ana through her Slack name as it does signed in.
	if _, err := p.AnswerQuestion(t.Context(), machine, parked.ID, parked.Question.ID, platform.QuestionAnswer{Action: platform.ActApprove, ActAs: &platform.ActAs{Client: "app", User: "U0ANA"}}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("ana approving her own request through Slack = %v, want forbidden", err)
	}
}

// Hiding, snoozing or deleting the notification changes the reader's inbox
// and not the run: it still waits on them, and the listing still says so.
func TestARunStillWaitsOnAReaderWhoPutItsNotificationAway(t *testing.T) {
	p := picking(t, platform.Config{}, "grace")
	seePeople(t, p, admin, grace)
	session, parked, m := parkedIn(t, p, admin, "picker")
	joined(t, p, session, grace)
	told := func(after string) {
		t.Helper()
		row := listedAs(t, p, grace, session)
		if !row.WaitingOnYou || row.WaitingUnknown || row.WaitingFor != "Which region should I deploy to?" {
			t.Fatalf("after grace %s the notification, her row says %+v", after, row)
		}
		turns := p.AsksIn(t.Context(), grace, []platform.ThreadTurn{{Kind: "run", Run: &parked}})
		if turns[0].Ask == nil || !turns[0].Ask.Yours {
			t.Fatalf("after grace %s the notification, answering would refuse her, so this tests nothing", after)
		}
	}
	now := time.Now()
	if _, _, err := p.Inbox.Hide(tenant, m.ID, "grace", now); err != nil {
		t.Fatal(err)
	}
	told("hid")
	if _, _, err := p.Inbox.Unhide(tenant, m.ID, "grace", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Inbox.Snooze(tenant, m.ID, "grace", now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	told("snoozed")
	if _, _, err := p.Inbox.Delete(tenant, m.ID, "grace", now); err != nil {
		t.Fatal(err)
	}
	told("deleted")
	// Answered, it waits on nobody.
	if _, err := p.AnswerQuestion(t.Context(), grace, parked.ID, parked.Question.ID, platform.QuestionAnswer{Action: platform.ActReply, Text: "eu"}); err != nil {
		t.Fatal(err)
	}
	within(t, "the run to end", func() bool { r, _ := p.Runs.Get(tenant, parked.ID); return r.Status == runs.Completed })
	if row := listedAs(t, p, grace, session); row.WaitingOnYou || row.WaitingUnknown || row.WaitingFor != "" {
		t.Fatalf("an answered question still shows: %+v", row)
	}
}

// A question nobody answered in time took its default: the run went on, and
// the session waits on nobody, in the listing as in the inbox.
func TestAnExpiredQuestionWaitsOnNobody(t *testing.T) {
	p := picking(t, platform.Config{QuestionTTL: 300 * time.Millisecond}, "grace")
	seePeople(t, p, admin, grace)
	session, parked, m := parkedIn(t, p, admin, "picker")
	joined(t, p, session, grace)
	within(t, "the run to take the default and end", func() bool { r, _ := p.Runs.Get(tenant, parked.ID); return r.Status.Terminal() })
	row := listedAs(t, p, grace, session)
	if row.WaitingOnYou || row.WaitingUnknown || row.WaitingFor != "" || row.Waiting != 0 {
		t.Fatalf("an expired question still shows: %+v", row)
	}
	got, err := p.InboxGet(grace, m.ID)
	if err != nil || len(got.Parts[0].Actions) != 0 {
		t.Fatalf("the inbox still offers an answer to an expired question: %+v, %v", got.Parts, err)
	}
}

// The listing reads the newest inbox.MaxAsking live messages of the
// workspace. A question raised before that many newer ones is not among
// them: the row then says it waits without saying on whom (waiting_unknown),
// never that it waits on someone else, and never on a reader who could not
// answer whatever was found. A reader the question is still found for, and
// every reader once the inbox is back within what is read, is told as before.
func TestAQuestionTheListingCouldNotReadIsSaidToWaitOnNobodyInParticular(t *testing.T) {
	p := picking(t, platform.Config{}, "grace")
	seePeople(t, p, admin, grace, olu, vic)
	session, parked, m := parkedIn(t, p, admin, "picker")
	joined(t, p, session, grace, olu, vic)
	standsTo(t, p, grace, session, parked, m, true)
	at := time.Now()
	ids := make([]string, 0, inbox.MaxAsking)
	for i := range inbox.MaxAsking {
		id := fmt.Sprintf("crowd-%d", i)
		raised, _, err := p.Inbox.Raise(tenant, id, inbox.Message{
			Kind: inbox.KindAlert, To: "someone-else", Subject: "Alert",
			Parts: []inbox.Part{{Type: inbox.PartAlert, Alert: &inbox.AlertPart{ID: id, Rule: "rule", Kind: "run_failed", Severity: "warning", Title: "Alert"}}},
		}, at.Add(time.Duration(i+1)*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, raised.ID)
	}
	for _, who := range []auth.Principal{grace, olu} {
		row := listedAs(t, p, who, session)
		if row.WaitingOnYou || !row.WaitingUnknown || row.WaitingKind != platform.WaitingQuestion || row.WaitingFor != "Which region should I deploy to?" {
			t.Errorf("%s: a question the listing could not read is said as %+v", who.Subject, row)
		}
		one, err := p.ReadSession(t.Context(), who, session, "", "", false)
		if err != nil || one.Conversation.WaitingOnYou || !one.Conversation.WaitingUnknown {
			t.Errorf("%s: the session read alone says %+v, %v", who.Subject, one.Conversation, err)
		}
	}
	// vic may not write runs: nothing found could wait on him.
	if row := listedAs(t, p, vic, session); row.WaitingOnYou || row.WaitingUnknown {
		t.Errorf("vic, who may answer nothing, is told %+v", row)
	}
	// grace's inbox still offers it: unknown was the honest answer.
	if got, err := p.InboxGet(grace, m.ID); err != nil || len(got.Parts[0].Actions) == 0 {
		t.Fatalf("grace's inbox no longer offers the question, so this tests nothing: %v", err)
	}
	// One fewer live message and the question is read again.
	if _, _, err := p.Inbox.Dismiss(tenant, ids[0], "someone-else", time.Now()); err != nil {
		t.Fatal(err)
	}
	standsTo(t, p, grace, session, parked, m, true)
	standsTo(t, p, olu, session, parked, m, false)
}
