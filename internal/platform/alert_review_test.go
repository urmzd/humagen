package platform_test

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/flow/event"
)

// failedTools gives a finished run failed tool calls in its trace and scores
// it, as the end of a run does. It returns the health alert it raised.
func failedTools(t *testing.T, p *platform.Platform, run runs.Run) alerts.Alert {
	t.Helper()
	at := time.Now().UTC()
	for i, done := range []event.ToolDone{
		{ID: "c1", Name: "web_fetch", Error: "403 Forbidden"},
		{ID: "c2", Name: "web_fetch", Error: "403 Forbidden"},
		{ID: "c3", Name: "search", Error: "timeout after 30s"},
		{ID: "c4", Name: "search", Result: "3 results"},
	} {
		e := event.Envelope{ID: run.ID + ":tool:" + done.ID, Tenant: run.Tenant, Run: run.ID, Agent: run.Agent, Seq: uint64(1000 + i), Time: at, Type: done.Type(), Data: done}
		if err := p.Journal.Append(e, runs.OriginAPI); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.ScoreOnline(run); err != nil {
		t.Fatal(err)
	}
	all, err := p.Alerts.Alerts(run.Tenant)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range all {
		if a.RunID == run.ID && a.Kind == "eval" {
			return a
		}
	}
	t.Fatalf("no health alert for %s in %+v", run.ID, all)
	return alerts.Alert{}
}

func firstRun(t *testing.T, p *platform.Platform, v platform.SessionView) runs.Run {
	t.Helper()
	for _, m := range v.Messages {
		if m.Run != nil {
			return await(t, p, m.Run.ID, runs.Completed)
		}
	}
	t.Fatalf("session %s started no run", v.ID)
	return runs.Run{}
}

func TestAHealthAlertSitsOnTheAnswerThatFailed(t *testing.T) {
	p := startWith(t, platform.Config{TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	field(t, p)
	p.SeePerson(admin)
	root, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "Field", Agent: "lead", Text: "Find the quarterly report"})
	if err != nil {
		t.Fatal(err)
	}
	run := firstRun(t, p, root)
	a := failedTools(t, p, run)
	if a.Session != root.ID || a.Message != run.ID || a.Action != "review" || !strings.Contains(a.Title, "tool_errors") {
		t.Fatalf("the alert is not on the answer that failed: %+v", a)
	}
	if len(a.Who) != 1 || a.Who[0] != admin.Subject {
		t.Fatalf("the alert is not for the person who asked: %v", a.Who)
	}
	// No empty session is opened to hold it.
	sessions, err := p.ListSessions(t.Context(), admin, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		if strings.HasPrefix(s.Title, "Agent health") {
			t.Fatalf("an alert session was opened: %+v", s)
		}
	}
	// A person who may not read the answer does not see its alert.
	if p.Seer(operator).Alert(a) {
		t.Fatal("the alert reached a person outside its session")
	}
	// The same run scored again raises nothing new.
	if again := failedTools(t, p, run); again.ID != a.ID {
		t.Fatalf("a second alert was raised: %s, then %s", a.ID, again.ID)
	}
}

func TestReviewingAnAlertStartsWithItsDocument(t *testing.T) {
	p := startWith(t, platform.Config{TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	field(t, p)
	p.SeePerson(admin)
	root, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "Field", Agent: "lead", Text: "Find the quarterly report"})
	if err != nil {
		t.Fatal(err)
	}
	run := firstRun(t, p, root)
	a := failedTools(t, p, run)

	review, started, err := p.ReviewAlert(t.Context(), admin, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !started || review.Parent != root.ID || review.Thread.Session.SourceAlert != a.ID {
		t.Fatalf("wrong review session: started %v, %+v", started, review)
	}
	recap := firstRun(t, p, review)
	// The person sees the alert's title, with the document attached.
	first, err := p.Threads.Message(admin.Tenant, review.ID, recap.Message)
	if err != nil {
		t.Fatal(err)
	}
	if first.Text != a.Title || len(first.Attachments) != 1 || first.Attachments[0].Name != platform.AlertDocumentName {
		t.Fatalf("the first message is not the title with the document: %q %+v", first.Text, first.Attachments)
	}
	doc, err := p.MessageAttachment(t.Context(), admin, review.ID, first.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# " + a.Title,
		"- tool_errors: 3 tools failed",
		"Tool calls in the run's trace: 4, 3 failed",
		`- web_fetch failed 2 times: "403 Forbidden"`,
		`- search failed 1 time: "timeout after 30s"`,
		"[the answer](/messages/" + root.ID + "?message=" + run.ID + ")",
		"> Find the quarterly report",
	} {
		if !strings.Contains(doc.Text, want) {
			t.Errorf("the document lacks %q:\n%s", want, doc.Text)
		}
	}
	// The agent gets the document with the message, and is asked to recap.
	if got := wire.RequestOf(recap.Tag()); len(got.Attachments) != 1 || !strings.Contains(got.Text, "review the alert in the attached document") {
		t.Fatalf("the recap was not asked with the document: %d files, %q", len(got.Attachments), got.Text)
	}
	if recap.Text != a.Title {
		t.Fatalf("the visible ask is not the title: %q", recap.Text)
	}
	if got, err := p.Alerts.Alert(admin.Tenant, a.ID); err != nil || got.Review != review.ID {
		t.Fatalf("the alert does not name its review: %+v %v", got, err)
	}

	// A follow-up is not asked for a recap: the transcript keeps the document.
	next, err := p.PostSession(t.Context(), admin, review.ID, platform.MessageInput{Text: "Which tool failed most?"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range next.Messages {
		if m.Run != nil && m.Run.ID != recap.ID {
			if r := await(t, p, m.Run.ID, runs.Completed); strings.Contains(wire.TextOf(r.Tag()), "attached document") {
				t.Fatal("a follow-up was asked for a recap again")
			}
		}
	}

	// A second review opens the same session and starts nothing.
	again, started, err := p.ReviewAlert(t.Context(), admin, a.ID)
	if err != nil || started || again.ID != review.ID {
		t.Fatalf("second review: %v, started %v, %s want %s", err, started, again.ID, review.ID)
	}
	// A person who may not read the answer cannot review its alert or open its document.
	if _, _, err := p.ReviewAlert(t.Context(), operator, a.ID); err == nil {
		t.Fatal("a person outside the session reviewed its alert")
	}
	if _, err := p.MessageAttachment(t.Context(), operator, review.ID, first.ID, 0); err == nil {
		t.Fatal("a person outside the session opened its document")
	}
}
