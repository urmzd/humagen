package platform_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/integrations/slack"
	"github.com/urmzd/humagen/pkg/runs"
)

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("never happened: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Pressing stop on a Slack session interrupts the model call of the run
// answering in it, says so in the thread and takes the session off "Working".
// The message keeps what the agent had said, marked stopped, and the session
// ends active however late the run's own events arrive. A follow-up mention
// in the thread is answered. The press is by a person the client maps who
// did not ask for the run.
func TestStoppingASlackSessionStopsItsRun(t *testing.T) {
	f := &fakes{}
	c := &cast{
		says:  map[string][][]saigetypes.Delta{"lead": {say("Picked up again.")}},
		talks: map[string]talk{"lead": {after: 0, text: "Partial answer so far"}},
	}
	p := stage(t, c, f.config())
	create(t, p, tenant, slackClient("lead-app", "A0LEAD", team, "lead"))
	fake := f.slackFor("token-lead-app")
	ingress := p.IngressHandler()
	slackPost := func(t *testing.T, h http.Handler, body string) int {
		t.Helper()
		return signedSlack(h, "secret-lead-app", body)
	}

	const thread = "1700000000.000100"
	mention := func(event, ts string) string {
		return `{"type":"event_callback","team_id":"T01ABCDEF","event_id":"` + event + `","event":{"type":"app_mention",` +
			`"user":"U0HUMAN1","text":"<@U08BOTID> start","ts":"` + ts + `","thread_ts":"` + thread + `","channel":"C02CHANNEL"}}`
	}
	if code := slackPost(t, ingress, mention("Ev1", "1700000090.001200")); code != http.StatusOK {
		t.Fatalf("mention answered %d", code)
	}

	var run runs.Run
	eventually(t, "the mention's run is in flight", func() bool {
		for _, r := range p.Runs.List(tenant, 10) {
			if r.Agent == "lead" && r.Status == runs.Running {
				run = r
				return true
			}
		}
		return false
	})
	answer := func() (slack.FakeMessage, bool) {
		for _, m := range fake.Messages() {
			if strings.Contains(m.Text, "Partial answer so far") {
				return m, true
			}
		}
		return slack.FakeMessage{}, false
	}
	eventually(t, "the partial answer is on the surface", func() bool { _, ok := answer(); return ok })
	eventually(t, "slack shows the session working", func() bool {
		s := fake.Statuses()
		return len(s) > 0 && s[len(s)-1].Status == slack.StatusProcessing
	})

	stop := func(team, event string) string {
		return `{"type":"event_callback","team_id":"` + team + `","event_id":"` + event + `","event":{"type":"agent_session_stopped",` +
			`"user":"U0HUMAN2","channel":"C02CHANNEL","thread_ts":"` + thread + `","streaming_message_ts":[]}}`
	}
	// Another workspace's stop finds no client and reaches nothing.
	if code := slackPost(t, ingress, stop("T09OTHER", "Ev2")); code != http.StatusNotFound {
		t.Fatalf("a foreign stop answered %d", code)
	}
	if r, _ := p.Runs.Get(tenant, run.ID); r.Status != runs.Running || r.Stopping || c.interrupted("lead") {
		t.Fatalf("another workspace stopped the run: %s, stopping %v", r.Status, r.Stopping)
	}

	pressed := len(fake.Statuses())
	if code := slackPost(t, ingress, stop("T01ABCDEF", "Ev2")); code != http.StatusOK {
		t.Fatalf("stop answered %d", code)
	}
	// The model call was cancelled, not left to finish unheard.
	eventually(t, "the model call was interrupted", func() bool { return c.interrupted("lead") })
	got := await(t, p, run.ID, runs.Stopped)
	if got.Cause != "Stopped by slack:U0HUMAN2" || got.Answer != "Partial answer so far" {
		t.Fatalf("run = %q (%q), want the partial answer stopped by the slack user", got.Answer, got.Cause)
	}
	// What the agent had said stays on the surface. A client's installation
	// shows the run's state as a reaction, so the message is left as it was.
	if m, ok := answer(); !ok || !strings.Contains(m.Text, "Partial answer so far") {
		t.Fatalf("the partial answer left the surface: %+v", fake.Messages())
	}
	confirmed := false
	for _, m := range fake.Messages() {
		confirmed = confirmed || (m.ThreadTS == thread && m.Text == "Stopped by <@U0HUMAN2>.")
	}
	if !confirmed {
		t.Fatalf("nobody was told the run stopped: %+v", fake.Messages())
	}

	// Slack redelivers; a second stop is a no-op, not a second confirmation.
	if code := slackPost(t, ingress, stop("T01ABCDEF", "Ev2")); code != http.StatusOK {
		t.Fatalf("a repeated stop answered %d", code)
	}
	time.Sleep(200 * time.Millisecond)
	confirmations := 0
	for _, m := range fake.Messages() {
		if strings.HasPrefix(m.Text, "Stopped by") {
			confirmations++
		}
	}
	if confirmations != 1 {
		t.Fatalf("%d stop confirmations, want 1", confirmations)
	}
	// From the press on, the session is never set back to working.
	statuses := fake.Statuses()
	for _, s := range statuses[pressed:] {
		if s.Status != slack.StatusActive {
			t.Fatalf("after the stop the session went to %s: %+v", s.Status, statuses[pressed:])
		}
	}
	if last := statuses[len(statuses)-1]; last.Status != slack.StatusActive || last.ThreadTS != thread {
		t.Fatalf("session = %+v, want active in the thread", last)
	}

	// The thread is still a conversation: a follow-up mention is answered.
	// Slack mentions carry no history in humagen today, so the follow-up is
	// a run of its own that is not sent the stopped turn.
	if code := slackPost(t, ingress, mention("Ev3", "1700000095.001300")); code != http.StatusOK {
		t.Fatalf("follow-up answered %d", code)
	}
	eventually(t, "the follow-up is answered", func() bool {
		for _, m := range fake.Messages() {
			if strings.Contains(m.Text, "Picked up again.") {
				return true
			}
		}
		return false
	})
	eventually(t, "the session ends active", func() bool {
		s := fake.Statuses()
		return s[len(s)-1].Status == slack.StatusActive
	})
}

// stoppable is a run in flight that U0HUMAN1 asked for through help-app,
// a client that maps nobody itself: the team's people are on api-app.
type stoppable struct {
	p       *platform.Platform
	c       *cast
	fake    *slack.Fake
	ingress http.Handler
	run     runs.Run
	answer  string
}

const stopThread = "1700000000.000100"

func stageStoppable(t *testing.T) *stoppable {
	t.Helper()
	f := &fakes{}
	c := &cast{
		says:  map[string][][]saigetypes.Delta{"lead": nil},
		talks: map[string]talk{"lead": {after: 0, text: "Partial answer so far"}},
	}
	p := stage(t, c, f.config())
	create(t, p, tenant, mapsNobody(slackClient("help-app", "A0HELP", team, "lead")))
	create(t, p, tenant, slackClient("api-app", "A0API", team, "lead"))
	s := &stoppable{p: p, c: c, fake: f.slackFor("token-help-app"), ingress: p.IngressHandler()}
	if code := signedSlack(s.ingress, "secret-help-app", mentionOf("A0HELP", team, "Ev1", "C01", stopThread)); code != http.StatusOK {
		t.Fatalf("mention answered %d", code)
	}
	eventually(t, "the mention's run is in flight", func() bool {
		r, ok := runOf(p, tenant, "lead")
		s.run = r
		return ok && r.Status == runs.Running
	})
	eventually(t, "the partial answer is on the surface and on record", func() bool {
		for _, m := range s.fake.Messages() {
			if strings.Contains(m.Text, "Partial answer so far") {
				s.answer = m.TS
				return answeredIn(p, "help-app", "C01", m.TS)
			}
		}
		return false
	})
	return s
}

// press is the stop button pressed by a Slack user in the run's session.
func (s *stoppable) press(t *testing.T, event, user string) {
	t.Helper()
	body := `{"type":"event_callback","api_app_id":"A0HELP","team_id":"` + team + `","event_id":"` + event + `","event":{"type":"agent_session_stopped",` +
		`"user":"` + user + `","channel":"C01","thread_ts":"` + stopThread + `","streaming_message_ts":[]}}`
	if code := signedSlack(s.ingress, "secret-help-app", body); code != http.StatusOK {
		t.Fatalf("%s's stop answered %d", user, code)
	}
}

// raise is a raised-hand reaction a Slack user adds to the run's answer.
func (s *stoppable) raise(t *testing.T, event, user string) {
	t.Helper()
	body := `{"type":"event_callback","api_app_id":"A0HELP","team_id":"` + team + `","event_id":"` + event + `","event":{"type":"reaction_added",` +
		`"user":"` + user + `","reaction":"raised_hand","event_ts":"1700000200.000100","item":{"type":"message","channel":"C01","ts":"` + s.answer + `"}}}`
	if code := signedSlack(s.ingress, "secret-help-app", body); code != http.StatusOK {
		t.Fatalf("%s's reaction answered %d", user, code)
	}
}

// carriesOn fails when the run was asked to stop or anyone was told so.
func (s *stoppable) carriesOn(t *testing.T, after string) {
	t.Helper()
	if r, _ := s.p.Runs.Get(tenant, s.run.ID); r.Status != runs.Running || r.Stopping || s.c.interrupted("lead") {
		t.Fatalf("after %s the run is %s, stopping %v", after, r.Status, r.Stopping)
	}
	if said(s.fake, "Stopped by") {
		t.Fatalf("after %s a stop was confirmed: %+v", after, s.fake.Messages())
	}
	if st := s.fake.Statuses(); len(st) > 0 && st[len(st)-1].Status == slack.StatusActive {
		t.Fatalf("after %s the session was set to active", after)
	}
}

func (s *stoppable) stoppedBy(t *testing.T, user string) {
	t.Helper()
	if got := await(t, s.p, s.run.ID, runs.Stopped); got.Cause != "Stopped by slack:"+user {
		t.Fatalf("the run ended %q, want stopped by %s", got.Cause, user)
	}
}

// The stop button stops a run for whoever asked for it, though the team no
// longer maps them. A press from someone the team maps to nobody, who did
// not ask for it, is acknowledged and the run carries on.
func TestTheStopButtonStopsARunForWhoeverAskedForIt(t *testing.T) {
	s := stageStoppable(t)
	eventually(t, "slack shows the session working", func() bool {
		st := s.fake.Statuses()
		return len(st) > 0 && st[len(st)-1].Status == slack.StatusProcessing
	})
	s.press(t, "EvStop1", "U0STRANGER")
	s.carriesOn(t, "a press by someone unmapped")

	// The team stops mapping the person who asked: they start nothing new,
	// and still stop what they asked for.
	if err := s.p.DeleteClient(t.Context(), tenant, "api-app", "test"); err != nil {
		t.Fatal(err)
	}
	if code := signedSlack(s.ingress, "secret-help-app", mentionOf("A0HELP", team, "Ev2", "C02", "1700000000.000200")); code != http.StatusOK {
		t.Fatalf("mention answered %d", code)
	}
	if all := s.p.Runs.List(tenant, 10); len(all) != 1 {
		t.Fatalf("someone no longer mapped started a run: %d runs", len(all))
	}
	s.press(t, "EvStop2", "U0STRANGER")
	s.carriesOn(t, "a press by someone unmapped, with nobody mapped")
	s.press(t, "EvStop3", "U0HUMAN1")
	s.stoppedBy(t, "U0HUMAN1")
	eventually(t, "the stop is confirmed in the session", func() bool { return said(s.fake, "Stopped by <@U0HUMAN1>.") })
}

// A raised-hand reaction on a run's answer follows the same rule as the
// button: a person the team maps stops the run, whoever asked for it, and
// someone it maps to nobody does not.
func TestARaisedHandStopsARunForAMappedPerson(t *testing.T) {
	s := stageStoppable(t)
	s.raise(t, "EvHand1", "U0STRANGER")
	s.carriesOn(t, "a reaction by someone unmapped")
	s.raise(t, "EvHand2", "U0HUMAN2")
	s.stoppedBy(t, "U0HUMAN2")
}

// A raised-hand reaction stops a run for whoever asked for it, though the
// team no longer maps them.
func TestARaisedHandStopsARunForWhoeverAskedForIt(t *testing.T) {
	s := stageStoppable(t)
	if err := s.p.DeleteClient(t.Context(), tenant, "api-app", "test"); err != nil {
		t.Fatal(err)
	}
	s.raise(t, "EvHand1", "U0HUMAN2")
	s.carriesOn(t, "a reaction by someone no longer mapped who did not ask")
	s.raise(t, "EvHand2", "U0HUMAN1")
	s.stoppedBy(t, "U0HUMAN1")
}
