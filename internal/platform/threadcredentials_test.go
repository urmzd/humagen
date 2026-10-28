package platform_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// A run a message starts in a shared thread acts for the person who posted
// the message, and for nobody else in the thread: Ana's tag reaches the
// provider with her token, Ben's later tag in the same thread with his, an
// untagged message in the console reaches it with nobody's, as it starts
// nothing (a default answering on a surface acts for its poster too:
// TestOnASurfaceTheDefaultAnswersAndFailsClosed), and a person who connected
// nothing gets no tools, with nobody's credential in
// their place. Each run is still sent what the others said in the room.
func TestARunAMessageStartsActsForThePersonWhoPostedIt(t *testing.T) {
	w := newWorld(t, platform.Config{})
	w.connect(t, ana, credentials.Input{Ref: "env:HUMAGEN_PERSON_ANA_ISSUES"})
	w.connect(t, ben, credentials.Input{Value: benValue})
	anas, bens := "Bearer "+environment["HUMAGEN_PERSON_ANA_ISSUES"], "Bearer "+benValue

	// answer waits for the one run a message started, which acts for who.
	answer := func(posted platform.Posted, who string) runs.Run {
		t.Helper()
		if len(posted.Runs) != 1 {
			t.Fatalf("%s's message started %d runs: %+v", who, len(posted.Runs), posted)
		}
		run := await(t, w.p, posted.Runs[0].ID, runs.Completed)
		if run.Actor.User != who || run.Actor.Bot {
			t.Fatalf("the run %s's message started acts for %+v", who, run.Actor)
		}
		return run
	}

	calls := len(w.server.Calls())
	first := startThread(t, w.p, ana, platform.ThreadInput{Text: "@worker any open bugs?", Default: "worker"})
	answer(first, "ana")
	if got := w.server.Calls()[calls:]; len(got) != 1 || got[0] != anas {
		t.Fatalf("ana's tag reached the server with %q", got)
	}

	calls = len(w.server.Calls())
	second := post(t, w.p, ben, first.Thread.ID, platform.MessageInput{Text: "@worker and the ones assigned to me?"})
	run := answer(second, "ben")
	if got := w.server.Calls()[calls:]; len(got) != 1 || got[0] != bens {
		t.Fatalf("ben's tag in ana's thread reached the server with %q", got)
	}
	// The room went with it: ana's words, then his.
	if text := wire.TextOf(run.Tag()); !strings.Contains(text, "@worker any open bugs?") || !strings.Contains(text, "@worker and the ones assigned to me?") {
		t.Fatalf("ben's run was not sent the room:\n%s", text)
	}

	calls = len(w.server.Calls())
	third := post(t, w.p, ben, first.Thread.ID, platform.MessageInput{Text: "anything new since?"})
	if third.Rule != threads.RuleUntagged || len(third.Runs) != 0 {
		t.Fatalf("an untagged message in the console was carried out by %q: %+v", third.Rule, third.Runs)
	}
	if got := w.server.Calls()[calls:]; len(got) != 0 {
		t.Fatalf("an untagged message reached the server with %q", got)
	}

	calls = len(w.server.Calls())
	fourth := post(t, w.p, cyd, first.Thread.ID, platform.MessageInput{Text: "@worker and for me?"})
	if run := answer(fourth, "cyd"); run.Answer != "I have no issue tools." {
		t.Fatalf("cyd, who connected nothing, was answered %q", run.Answer)
	}
	if got := w.server.Calls()[calls:]; len(got) != 0 {
		t.Fatalf("cyd's tag reached the server with %q", got)
	}
	w.leaks(t, environment["HUMAGEN_PERSON_ANA_ISSUES"], benValue)
}

// An agent may ask the experts (asked_of sme:<topic>), but in a run that
// uses the credential of the person it acts for, an answer steers that
// person's tools. So the question goes to that person alone, and an
// expert's answer is refused on the run, from the inbox and in the thread,
// where nothing is posted; the person's own answer in the thread resumes it
// with their token. The same question from a run that reaches no one's own
// credential goes to the experts, and an expert's answer in the thread
// resumes it.
func TestAnExpertDoesNotAnswerWhatARunAsksWithAnotherPersonsCredential(t *testing.T) {
	w := newWorld(t, platform.Config{})
	ctx := t.Context()
	w.connect(t, ana, credentials.Input{Ref: "env:HUMAGEN_PERSON_ANA_ISSUES"})
	w.connect(t, ben, credentials.Input{Value: benValue})
	for _, spec := range []wire.AgentSpec{
		{Name: "asker", Model: "offline", Provider: "offline", SystemPrompt: workerPrompt, Tools: []string{"mcp:issues", "ask_person"}},
		{Name: "chatty", Model: "offline", Provider: "offline", SystemPrompt: workerPrompt, Tools: []string{"ask_person"}},
	} {
		if _, err := w.p.Agents.Create(ctx, tenant, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.p.CreateProject(ctx, tenant, ada, platform.ProjectInput{Name: "Launch"}); err != nil {
		t.Fatal(err)
	}
	cur, err := w.p.ProjectExperts(tenant, "Launch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.SetProjectExperts(tenant, "Launch", ada, platform.Experts{Version: cur.Version, Experts: []projects.Expert{{Who: "ben", Topics: []string{"billing"}}}}); err != nil {
		t.Fatal(err)
	}
	w.model.askOfWhom("sme:billing", "What is the PO number?")
	parked := func(text string) (threads.Thread, runs.Run, inbox.Message) {
		t.Helper()
		got := startThread(t, w.p, ana, platform.ThreadInput{Project: "Launch", Text: text})
		if len(got.Runs) != 1 {
			t.Fatalf("%q started %+v", text, got)
		}
		run := await(t, w.p, got.Runs[0].ID, runs.Parked)
		if run.Question == nil || run.Actor.User != "ana" {
			t.Fatalf("the run is %+v", run)
		}
		return got.Thread, run, questionFor(t, w.p, run.ID)
	}

	th, run, msg := parked("@asker file the PO")
	if sees(w.p, ben, msg.ID) || !sees(w.p, ana, msg.ID) {
		t.Errorf("a question for the experts, in a run with ana's credential, is addressed to %v, want ana alone", msg.Audience)
	}
	// The thread tells the expert it is ana's to answer, and offers it to ana.
	_, turns, _, err := w.p.ThreadTurns(tenant, th.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	asks := map[string]*platform.Ask{}
	for _, who := range []auth.Principal{ana, ben} {
		for _, turn := range w.p.AsksIn(ctx, who, slices.Clone(turns)) {
			if turn.Run != nil && turn.Run.ID == run.ID {
				asks[who.Subject] = turn.Ask
			}
		}
	}
	if a := asks["ben"]; a == nil || a.Yours || a.For != platform.ForPerson || a.Person != "ana" {
		t.Errorf("the expert's ask = %+v, want it for ana and not theirs", a)
	}
	if a := asks["ana"]; a == nil || !a.Yours || a.For != platform.ForPerson {
		t.Errorf("ana's ask = %+v, want it theirs", a)
	}
	calls := len(w.server.Calls())
	ref := &threads.AnswerRef{Run: run.ID, Question: run.Question.ID}
	if _, err := w.p.PostMessage(ctx, ben, th.ID, platform.MessageInput{Text: "PO-77", Answers: ref}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("the expert answering in the thread = %v, want forbidden", err)
	}
	if _, err := w.p.AnswerQuestion(ctx, ben, run.ID, run.Question.ID, platform.QuestionAnswer{Action: platform.ActReply, Text: "PO-77"}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("the expert answering on the run = %v, want forbidden", err)
	}
	if _, err := w.p.InboxResolve(ctx, ben, msg.ID, msg.Parts[0].ID, platform.Resolution{Action: platform.ActReply, Text: "PO-77"}); err == nil {
		t.Fatal("the expert answered from the inbox")
	}
	if now, err := w.p.Threads.Get(tenant, th.ID); err != nil || now.Version != th.Version {
		t.Fatalf("a refused answer posted in the thread: %+v, %v", now, err)
	}
	if got, _ := w.p.Runs.Get(tenant, run.ID); got.Status != runs.Parked {
		t.Fatalf("a refused answer moved the run to %s", got.Status)
	}
	if got := w.server.Calls()[calls:]; len(got) != 0 {
		t.Fatalf("the expert's answer reached the server with %q", got)
	}
	post(t, w.p, ana, th.ID, platform.MessageInput{Text: "PO-12", Answers: ref})
	await(t, w.p, run.ID, runs.Completed)
	if got := w.server.Calls()[calls:]; len(got) != 1 || got[0] != "Bearer "+environment["HUMAGEN_PERSON_ANA_ISSUES"] {
		t.Fatalf("ana's answer reached the server with %q", got)
	}

	th, run, msg = parked("@chatty file the PO")
	if !sees(w.p, ben, msg.ID) {
		t.Errorf("a question for the experts that no one's credential acts on is addressed to %v", msg.Audience)
	}
	got := post(t, w.p, ben, th.ID, platform.MessageInput{Text: "PO-77", Answers: &threads.AnswerRef{Run: run.ID, Question: run.Question.ID}})
	if got.Rule != threads.RuleReply || len(got.Runs) != 0 {
		t.Fatalf("the expert's answer started %+v", got)
	}
	await(t, w.p, run.ID, runs.Completed)
	w.leaks(t, environment["HUMAGEN_PERSON_ANA_ISSUES"], benValue)
}

// A run that another calls acts for nobody, and inherits its caller's thread
// and project, but what it is answered is its caller's tool result. So when
// ana's run in a thread calls one that asks the billing experts, the
// question goes to ana alone, and the expert's answer is refused on the run,
// from the inbox and in the thread, where nothing is posted: it would be
// searched with ana's token. Her own answer in the thread is.
func TestAnExpertDoesNotAnswerARunCalledByOneWithAnotherPersonsCredential(t *testing.T) {
	p, server, model := steeredWorld(t, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	ctx := t.Context()
	if _, err := p.CreateProject(ctx, tenant, ada, platform.ProjectInput{Name: "Launch"}); err != nil {
		t.Fatal(err)
	}
	cur, err := p.ProjectExperts(tenant, "Launch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetProjectExperts(tenant, "Launch", ada, platform.Experts{Version: cur.Version, Experts: []projects.Expert{{Who: "ben", Topics: []string{"billing"}}}}); err != nil {
		t.Fatal(err)
	}
	posted := startThread(t, p, ana, platform.ThreadInput{Project: "Launch", Text: "@envoy file the PO"})
	if len(posted.Runs) != 1 || posted.Runs[0].Actor.User != "ana" {
		t.Fatalf("ana's message started %+v", posted.Runs)
	}
	scout := called(t, p, posted.Runs[0].ID, "scout", runs.Parked)
	if scout.Thread != posted.Thread.ID || scout.Project != "Launch" || scout.Question == nil {
		t.Fatalf("the called run is %+v", scout)
	}
	msg := questionFor(t, p, scout.ID)
	if sees(p, ben, msg.ID) || !sees(p, ana, msg.ID) {
		t.Errorf("a question for the experts, from a run ana's run called, is addressed to %v, want ana alone", msg.Audience)
	}
	ref := &threads.AnswerRef{Run: scout.ID, Question: scout.Question.ID}
	words := "BEN SAYS: payroll salaries"
	if _, err := p.PostMessage(ctx, ben, posted.Thread.ID, platform.MessageInput{Text: words, Answers: ref}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("the expert answering in the thread = %v, want forbidden", err)
	}
	if _, err := p.AnswerQuestion(ctx, ben, scout.ID, scout.Question.ID, platform.QuestionAnswer{Action: platform.ActReply, Text: words}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("the expert answering on the run = %v, want forbidden", err)
	}
	if _, err := p.InboxResolve(ctx, ben, msg.ID, msg.Parts[0].ID, platform.Resolution{Action: platform.ActReply, Text: words}); err == nil {
		t.Fatal("the expert answered from the inbox")
	}
	if now, err := p.Threads.Get(tenant, posted.Thread.ID); err != nil || now.Version != posted.Thread.Version {
		t.Fatalf("a refused answer posted in the thread: %+v, %v", now, err)
	}
	if got := server.Calls(); len(got) != 0 {
		t.Fatalf("the server was reached with %q", got)
	}
	post(t, p, ana, posted.Thread.ID, platform.MessageInput{Text: "PO-12", Answers: ref})
	await(t, p, posted.Runs[0].ID, runs.Completed)
	if q, got := model.searched(), server.Calls(); len(q) != 1 || strings.Contains(q[0], "BEN") || !strings.Contains(q[0], "PO-12") ||
		!slices.Equal(got, []string{"Bearer " + environment["HUMAGEN_PERSON_ANA_ISSUES"]}) {
		t.Fatalf("searched %q and reached the server with %q, want her words alone with her token", q, got)
	}
}

// A failed run that answered a message in a shared thread is asked again
// as an answer to that message, beside the failed run, and for the person
// who posted it: only they may retry it, and their retry reaches the
// provider with their token.
func TestARetryOfAThreadsRunAnswersTheSameMessageForThePoster(t *testing.T) {
	w := newWorld(t, platform.Config{Backoff: func(int) time.Duration { return time.Millisecond }})
	ctx := t.Context()
	w.connect(t, ana, credentials.Input{Ref: "env:HUMAGEN_PERSON_ANA_ISSUES"})
	w.connect(t, ben, credentials.Input{Value: benValue})

	w.model.fail(true)
	posted := startThread(t, w.p, ana, platform.ThreadInput{Text: "@worker any open bugs?"})
	if len(posted.Runs) != 1 {
		t.Fatalf("the message started %+v", posted)
	}
	failed := await(t, w.p, posted.Runs[0].ID, runs.Failed)
	w.model.fail(false)
	var msg inbox.Message
	eventually(t, "the failure reached the inbox", func() bool {
		page, _, _ := w.p.InboxPage(admin, inbox.Query{Kind: inbox.KindFailure})
		for _, m := range page {
			if m.Source.Run == failed.ID {
				msg = m
				return true
			}
		}
		return false
	})
	calls := len(w.server.Calls())
	retry := platform.Resolution{Action: platform.ActRetry}
	if offersRetry(t, w, ben, msg) {
		t.Fatal("ben is offered Retry on ana's turn in the thread")
	}
	if _, err := w.p.InboxResolve(ctx, ben, msg.ID, msg.Parts[0].ID, retry); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("ben retrying ana's turn in the thread = %v, want forbidden", err)
	}
	if !offersRetry(t, w, ana, msg) {
		t.Fatal("ana is not offered Retry on her own turn in the thread")
	}
	done, err := w.p.InboxResolve(ctx, ana, msg.ID, msg.Parts[0].ID, retry)
	if err != nil {
		t.Fatalf("ana retrying her turn in the thread: %v", err)
	}
	again := retried(t, w, done)
	if again.Actor.User != "ana" || again.Thread != failed.Thread || again.Message != failed.Message || again.Prev != failed.Prev || again.Agent != "worker" {
		t.Fatalf("ana's retry = %+v, want worker answering %s of %s for ana", again, failed.Message, failed.Thread)
	}
	if got := w.server.Calls()[calls:]; len(got) != 1 || got[0] != "Bearer "+environment["HUMAGEN_PERSON_ANA_ISSUES"] {
		t.Fatalf("ana's retry reached the server with %q", got)
	}
	w.leaks(t, environment["HUMAGEN_PERSON_ANA_ISSUES"], benValue)
}

// A turn in a shared thread is sent the room, so what an earlier answer in
// it found with a person's own credential can reach any later turn there.
// The memory agent is asked about none of them; a turn in another thread,
// whose room reached no one's credential, is taught as before.
func TestWhatAPersonsCredentialReachedInARoomIsNotTaughtToMemory(t *testing.T) {
	w := newWorld(t, platform.Config{OfflineHooks: true})
	w.model.mu.Lock()
	w.model.recall = true
	w.model.mu.Unlock()
	ctx := t.Context()
	if _, err := w.p.Agents.Create(ctx, tenant, wire.AgentSpec{Name: "scribe", Model: "offline", Provider: "offline", SystemPrompt: "x"}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := w.p.SetSettings(ctx, tenant, platform.Settings{MemoryAgent: "scribe", MemoryAutoApprove: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.CreateProject(ctx, tenant, ada, platform.ProjectInput{Name: "Launch"}); err != nil {
		t.Fatal(err)
	}
	w.connect(t, ana, credentials.Input{Ref: "env:HUMAGEN_PERSON_ANA_ISSUES"})

	room := startThread(t, w.p, ana, platform.ThreadInput{Project: "Launch", Text: "@worker any open bugs?"})
	first := await(t, w.p, room.Runs[0].ID, runs.Completed)
	if first.Answer != found {
		t.Fatalf("ana's turn answered %q", first.Answer)
	}
	// Ben asks an agent with no tools in the same room: it is sent what
	// ana's turn found, and repeats it.
	later := post(t, w.p, ben, room.Thread.ID, platform.MessageInput{Text: "@lead say that again"})
	second := await(t, w.p, later.Runs[0].ID, runs.Completed)
	if second.Answer != found || second.Actor.User != "ben" {
		t.Fatalf("ben's turn in the room = %q for %+v", second.Answer, second.Actor)
	}
	// In another room, nothing reached anyone's credential.
	other := startThread(t, w.p, ben, platform.ThreadInput{Project: "Launch", Text: "@lead plan the launch"})
	taught := await(t, w.p, other.Runs[0].ID, runs.Completed)

	launch := memory.ProjectOwner("Launch")
	within(t, "the turn whose room reached no credential is taught", func() bool {
		for _, r := range w.p.Runs.List(tenant, 200) {
			if r.Origin == runs.OriginSystem && r.TagID == "sys-memory-"+taught.ID {
				return true
			}
		}
		return false
	})
	for _, n := range w.p.Memory.List(tenant, launch) {
		if strings.Contains(n.Text, "PRIV-7") {
			t.Fatalf("what ana's credential reached was kept in Launch's notes: %+v", n)
		}
	}
	for _, r := range w.p.Runs.List(tenant, 200) {
		for _, turn := range []runs.Run{first, second} {
			if r.Origin == runs.OriginSystem && r.TagID == "sys-memory-"+turn.ID {
				t.Fatalf("the memory agent was asked about %s", turn.TagID)
			}
		}
	}
	w.leaks(t, environment["HUMAGEN_PERSON_ANA_ISSUES"])
}
