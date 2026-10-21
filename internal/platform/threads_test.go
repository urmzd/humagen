package platform_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/envelope"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/people"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/threads"
)

// seePeople puts people in the directory, so they can be tagged.
func seePeople(t *testing.T, p *platform.Platform, who ...auth.Principal) {
	t.Helper()
	for _, w := range who {
		if _, err := p.People.Seen(tenant, people.Sighting{Subject: w.Subject, Name: strings.ToUpper(w.Subject[:1]) + w.Subject[1:], Source: people.SignIn}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

func startThread(t *testing.T, p *platform.Platform, who auth.Principal, in platform.ThreadInput) platform.Posted {
	t.Helper()
	if in.Visibility == "" {
		in.Visibility = threads.Shared
	}
	got, err := p.StartThread(t.Context(), who, in)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func post(t *testing.T, p *platform.Platform, who auth.Principal, id string, in platform.MessageInput) platform.Posted {
	t.Helper()
	got, err := p.PostMessage(t.Context(), who, id, in)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// An agent a message tags answers it, side by side with the others tagged,
// and is sent the room as it stood: people by name, agents by theirs, then
// the message. The run records the message and the channel, and the tag
// records the run.
func TestATaggedAgentAnswersSeeingTheRoomUpToTheMessage(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	first := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "@helper what is the renewal date?"})
	if first.Rule != threads.RuleTagged || len(first.Runs) != 1 {
		t.Fatalf("the first message started %+v", first)
	}
	run := first.Runs[0]
	if run.Agent != "helper" || run.Thread != first.Thread.ID || run.Message != "m-1" || run.Prev != "m-1" || run.Channel != projects.General || run.Project != "Field" {
		t.Fatalf("the run is %+v", run)
	}
	if m := first.Message.Mentions; len(m) != 1 || m[0].Run != run.ID {
		t.Fatalf("the tag records %+v", m)
	}
	if !slices.ContainsFunc(first.Thread.Participants, func(x threads.Participant) bool { return x.Kind == threads.Agent && x.Name == "helper" }) {
		t.Fatalf("the agent that answered does not take part: %+v", first.Thread.Participants)
	}
	answered := await(t, p, run.ID, runs.Completed)

	second := post(t, p, admin, first.Thread.ID, platform.MessageInput{Text: "@lead @helper check what was said"})
	if len(second.Runs) != 2 || second.Message.Prev != "m-1" {
		t.Fatalf("the second message started %+v", second)
	}
	for _, r := range second.Runs {
		text := r.Tag().Text
		order := []string{"Conversation so far:", "opal: @helper what is the renewal date?", "helper: " + answered.Answer, "ada: @lead @helper check what was said"}
		at := 0
		for _, want := range order {
			i := strings.Index(text[at:], want)
			if i < 0 {
				t.Fatalf("%s was sent, without %q in order:\n%s", r.Agent, want, text)
			}
			at += i + len(want)
		}
		if r.Text != "@lead @helper check what was said" {
			t.Fatalf("the record shows %q", r.Text)
		}
		await(t, p, r.ID, runs.Completed)
	}
	sum, _, err := p.ThreadPage(tenant, platform.ThreadsQuery{ThreadQuery: runs.ThreadQuery{Channel: projects.General}})
	if err != nil || len(sum) != 1 || sum[0].Branches != 0 || sum[0].Messages != 2 {
		t.Fatalf("answers to one message read as branches: %+v, %v", sum, err)
	}
}

// A message that tags only people starts no run, even where a default
// answers, and puts a mention in each tagged person's inbox. Their reply from
// the inbox is a message by them that starts nothing either.
func TestTaggingOnlyPeopleNotifiesThemAndStartsNoRun(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	seePeople(t, p, operator, admin, vic)
	got := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "@ada and @vic can you check `@lead`? @nobody too"})
	if got.Rule != threads.RulePeople || len(got.Runs) != 0 {
		t.Fatalf("tagging people started %+v", got)
	}
	if !slices.Equal(got.Notified, []string{"ada", "vic"}) {
		t.Fatalf("notified %v", got.Notified)
	}
	if len(got.Refused) != 1 || got.Refused[0].Name != "nobody" || !strings.Contains(got.Refused[0].Refused, "@nobody") {
		t.Fatalf("refused %+v", got.Refused)
	}
	m, ok := p.Inbox.ByKey(tenant, inbox.MentionKey(got.Thread.ID, "m-1", "ada"))
	if !ok || m.Kind != inbox.KindMention || m.To != "ada" || m.Parts[0].Mention.Excerpt == "" {
		t.Fatalf("ada was sent %+v", m)
	}
	if _, err := p.InboxGet(operator, m.ID); !errors.Is(err, inbox.ErrNotFound) {
		t.Fatalf("someone else read ada's mention: %v", err)
	}
	seen, err := p.InboxGet(admin, m.ID)
	if err != nil || !slices.Equal(seen.Parts[0].Actions, []string{platform.ActReply, platform.ActDone}) {
		t.Fatalf("ada reads %+v, %v", seen, err)
	}
	if _, err := p.InboxResolve(t.Context(), admin, m.ID, "p1", platform.Resolution{Action: platform.ActReply, Text: "On it."}); err != nil {
		t.Fatal(err)
	}
	reply, err := p.Threads.Message(tenant, got.Thread.ID, "m-2")
	if err != nil || reply.By != "ada" || reply.Text != "On it." || reply.Prev != "m-1" {
		t.Fatalf("the reply is %+v, %v", reply, err)
	}
	if _, err := p.Runs.Conversation(tenant, got.Thread.ID); !errors.Is(err, runs.ErrNotFound) {
		t.Fatalf("a reply to a mention started a run: %v", err)
	}
	if again, _ := p.InboxGet(admin, m.ID); again.State != inbox.Resolved {
		t.Fatalf("the mention stays %s after the reply", again.State)
	}
}

// With no tag, the default the thread inherits answers: the thread's, then
// its channel's, then its project's chain. Posted only, nothing answers. A
// default that has stopped being available is not passed over: nothing
// answers, and the answer says why.
func TestTheInheritedDefaultAnswersAndFailsClosed(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	if _, err := p.CreateChannel(t.Context(), tenant, "Globex", ada, platform.ChannelInput{Name: "renewals", Purpose: "Renewal work", Default: "helper"}); err != nil {
		t.Fatal(err)
	}
	got := startThread(t, p, operator, platform.ThreadInput{Project: "Globex", Channel: "renewals", Text: "when does it renew?"})
	if got.Rule != threads.RuleDefault || len(got.Runs) != 1 || got.Runs[0].Agent != "helper" {
		t.Fatalf("an untagged message started %+v", got)
	}
	if text := got.Runs[0].Tag().Text; !strings.Contains(text, "This conversation is in #renewals. Its purpose: Renewal work") {
		t.Fatalf("the channel's purpose was not sent:\n%s", text)
	}
	if d, _, err := p.DefaultFor(tenant, got.Thread.ID, "", ""); err != nil || d != (threads.Default{Agent: "helper", From: threads.FromChannel}) {
		t.Fatalf("the default is %+v, %v", d, err)
	}
	quiet := post(t, p, operator, got.Thread.ID, platform.MessageInput{Text: "noting this", PostOnly: true})
	if quiet.Rule != threads.RulePostOnly || len(quiet.Runs) != 0 {
		t.Fatalf("a message posted only started %+v", quiet)
	}
	// Globex stops allowing helper.
	setAgents(t, p, "Globex", platform.AgentsInput{Enable: []string{}, Allow: []string{"lead"}})
	stale := post(t, p, operator, got.Thread.ID, platform.MessageInput{Text: "and now?"})
	if stale.Rule != threads.RuleDefault || len(stale.Runs) != 0 || !strings.Contains(stale.Reason, "@helper answers untagged messages here, but it is not available here") {
		t.Fatalf("a stale default started %+v", stale)
	}
	// A tag of an agent that cannot answer is refused, and never handed to
	// the default instead.
	tagged := post(t, p, operator, got.Thread.ID, platform.MessageInput{Text: "@helper please"})
	if tagged.Rule != threads.RuleTagged || len(tagged.Runs) != 0 || len(tagged.Refused) != 1 || !strings.Contains(tagged.Refused[0].Refused, "helper is not available in Globex") {
		t.Fatalf("a refused tag started %+v", tagged)
	}
	// A default written where it cannot answer is refused.
	if _, err := p.CreateChannel(t.Context(), tenant, "Globex", ada, platform.ChannelInput{Name: "billing", Default: "helper"}); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a default that cannot answer was written: %v", err)
	}
	if _, err := p.UpdateThread(t.Context(), operator, got.Thread.ID, platform.ThreadUpdate{Version: got.Thread.Version, Default: "helper"}); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("a thread's default that cannot answer was written: %v", err)
	}
}

// @auto picks among the agents available in the thread's project.
func TestAutoPicksAmongTheAgentsAvailableInTheProject(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	for _, spec := range []agentspec.AgentSpec{
		{Name: "invoices", Model: "offline", Provider: "offline", SystemPrompt: "Billing.", Description: "invoices and billing disputes"},
		{Name: "contracts", Model: "offline", Provider: "offline", SystemPrompt: "Contracts.", Description: "invoices and contract terms"},
	} {
		if _, err := p.Agents.Create(t.Context(), tenant, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	setAgents(t, p, "Globex", platform.AgentsInput{Enable: []string{}, Allow: []string{"contracts", "lead", "helper"}})
	got := startThread(t, p, operator, platform.ThreadInput{Project: "Globex", Text: "@auto the invoices look wrong"})
	if len(got.Runs) != 1 || got.Runs[0].Agent != "contracts" || got.Runs[0].Picked == "" {
		t.Fatalf("@auto started %+v", got.Runs)
	}
	if got.Message.Mentions[0].Name != threads.AutoName || got.Message.Mentions[0].Run != got.Runs[0].ID {
		t.Fatalf("the tag records %+v", got.Message.Mentions)
	}
}

// Experts are found along the chain, the nearest project that lists one for
// the topic first. Tagging them notifies each; a topic nobody answers for is
// refused.
func TestExpertsAreFoundAlongTheChainAndNotifiedWhenTagged(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	set := func(project string, es ...projects.Expert) {
		t.Helper()
		cur, err := p.ProjectExperts(tenant, project)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.SetProjectExperts(tenant, project, ada, platform.Experts{Version: cur.Version, Experts: es}); err != nil {
			t.Fatal(err)
		}
	}
	set("Field", projects.Expert{Who: "vic", Topics: []string{"billing", "security"}})
	set("Globex", projects.Expert{Who: "role:oncall", Topics: []string{"security"}})
	got := startThread(t, p, operator, platform.ThreadInput{Project: "Globex", Text: "@sme:billing and @sme:security, then @sme:legal"})
	if got.Rule != threads.RulePeople || len(got.Runs) != 0 {
		t.Fatalf("tagging experts started %+v", got)
	}
	if !slices.Equal(got.Notified, []string{"vic", "role:oncall"}) {
		t.Fatalf("notified %v", got.Notified)
	}
	if len(got.Refused) != 1 || got.Refused[0].Name != "legal" {
		t.Fatalf("refused %+v", got.Refused)
	}
	oncall, ok := p.Inbox.ByKey(tenant, inbox.MentionKey(got.Thread.ID, "m-1", "role:oncall"))
	if !ok || !sees(p, pat, oncall.ID) || sees(p, grace, oncall.ID) {
		t.Fatalf("the experts named by a role are not the ones sent it: %+v", oncall)
	}
	// Every turn in the project is told which topics have experts.
	r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "t-1", Agent: "lead", Text: "hi", Project: "Globex"})
	if err != nil {
		t.Fatal(err)
	}
	if text := r.Tag().Text; !strings.Contains(text, "Experts here: billing, security. Ask one with ask_person, asked_of sme:<topic>.") {
		t.Fatalf("the experts line was not sent:\n%s", text)
	}
}

// A question an agent asks of the experts reaches the experts of its
// project's chain, and an expert's message in the thread that answers it
// resumes the run. The answer starts no run of its own.
func TestAQuestionForExpertsReachesThemAndAnAnswerInTheThreadResumesTheRun(t *testing.T) {
	c := &cast{continues: true,
		says:  map[string][][]saigetypes.Delta{"asker": {asked(map[string]any{"question": "What is the PO number?", "asked_of": "sme:billing"}), say("Filed.")}},
		tools: map[string][]string{"asker": {"ask_person"}},
	}
	p := stage(t, c, platform.Config{})
	field(t, p)
	cur, err := p.ProjectExperts(tenant, "Field")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetProjectExperts(tenant, "Field", ada, platform.Experts{Version: cur.Version, Experts: []projects.Expert{{Who: "olu", Topics: []string{"billing"}}}}); err != nil {
		t.Fatal(err)
	}
	r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "api-ask", Agent: "asker", Text: "File it", Project: "Globex", Actor: envelope.Actor{ID: admin.Subject}})
	if err != nil {
		t.Fatal(err)
	}
	run := await(t, p, r.ID, runs.Parked)
	q := questionFor(t, p, run.ID)
	if !sees(p, olu, q.ID) || sees(p, grace, q.ID) {
		t.Fatal("the question did not reach the expert alone")
	}
	// Someone the question is not for is refused, and nothing is posted.
	if _, err := p.PostMessage(t.Context(), grace, run.Thread, platform.MessageInput{Text: "x", Answers: &threads.AnswerRef{Run: run.ID, Question: run.Question.ID}}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("an answer from someone the question is not for = %v", err)
	}
	if _, err := p.Threads.Get(tenant, run.Thread); !errors.Is(err, threads.ErrNotFound) {
		t.Fatal("a refused answer wrote the thread's record")
	}
	got := post(t, p, olu, run.Thread, platform.MessageInput{Text: "PO-77", Answers: &threads.AnswerRef{Run: run.ID, Question: run.Question.ID}})
	if got.Rule != threads.RuleReply || len(got.Runs) != 0 || got.Message.Answers == nil || got.Message.Prev != run.ID {
		t.Fatalf("the answer started %+v", got)
	}
	done := await(t, p, run.ID, runs.Completed)
	if done.Answer != "Filed." {
		t.Fatalf("the run ended with %q", done.Answer)
	}
	if !strings.Contains(strings.Join(c.requests("asker"), "\n"), "PO-77") {
		t.Fatal("the asker's model never saw the answer")
	}
}

// A conversation from before threads reads as shared, in general of its
// project, with its people and agents taking part, at version 0. A message
// posted in it writes its record, follows its last run, and an agent tagged
// then is sent the old turns as they always read.
func TestAConversationFromBeforeThreadsGainsItsRecordWhenSomeonePosts(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	old, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "t-old", Agent: "lead", Text: "OLD-QUESTION", Project: "Field", Actor: envelope.Actor{ID: "opal"}})
	if err != nil {
		t.Fatal(err)
	}
	old = await(t, p, old.ID, runs.Completed)
	rec, err := p.ThreadRecord(tenant, old.Thread)
	if err != nil || rec.Version != 0 || rec.Channel != projects.General || rec.Visibility != threads.Shared || rec.Project != "Field" {
		t.Fatalf("the record reads %+v, %v", rec, err)
	}
	if len(rec.Participants) != 2 || !rec.Participants[0].Owner || rec.Participants[1].Name != "lead" {
		t.Fatalf("participants read %+v", rec.Participants)
	}
	if _, err := p.Threads.Get(tenant, old.Thread); !errors.Is(err, threads.ErrNotFound) {
		t.Fatal("reading a conversation wrote its record")
	}
	got := post(t, p, admin, old.Thread, platform.MessageInput{Text: "@helper and then?"})
	if got.Thread.Version != 1 || got.Message.Prev != old.ID || len(got.Runs) != 1 {
		t.Fatalf("posting in it did %+v", got)
	}
	if text := got.Runs[0].Tag().Text; !strings.Contains(text, "User: OLD-QUESTION\nlead: "+old.Answer) || !strings.HasSuffix(text, "ada: @helper and then?") {
		t.Fatalf("the agent was sent:\n%s", text)
	}
	_, turns, next, err := p.ThreadTurns(tenant, old.Thread, "", 0)
	if err != nil || next != "" || len(turns) != 3 || turns[0].Run == nil || turns[1].Message == nil || turns[2].Run == nil {
		t.Fatalf("its turns are %+v, %q, %v", turns, next, err)
	}
}

// A person posts only as themselves. A credential that acts for nobody
// cannot post, and one that acts for a mapped person posts as them, with that
// client's Slack people tagged by their own mentions.
func TestAPersonPostsOnlyAsThemselves(t *testing.T) {
	f := &fakes{}
	p := startWith(t, f.config())
	field(t, p)
	in := slackClient("app", "A0APP", team, "lead")
	in.Slack.Machine, in.Slack.People = "svc-slack", map[string]string{"U0OPAL": "opal", "U0OLU": "olu"}
	create(t, p, tenant, in)
	machine := auth.Principal{Subject: "svc-slack", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsRead, auth.RunsWrite}}
	if _, err := p.StartThread(t.Context(), machine, platform.ThreadInput{Project: "Field", Visibility: threads.Shared, Text: "hi", PostOnly: true}); !errors.Is(err, platform.ErrThreadAuthor) {
		t.Fatalf("a credential posted as itself: %v", err)
	}
	pol, err := auth.ParsePolicy("test", `
role viewer { allow read on * }
role operator extends viewer { allow write on runs }
grant operator to subject "opal"
`)
	if err != nil {
		t.Fatal(err)
	}
	machine = machine.WithPolicy(pol)
	got, err := p.StartThread(t.Context(), machine, platform.ThreadInput{
		Project: "Field", Visibility: threads.Shared, Text: "<@U0OLU> have a look", ActAs: &platform.ActAs{Client: "app", User: "U0OPAL"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Message.By != "opal" || got.Message.Via != "app" || got.Thread.CreatedBy != "opal" {
		t.Fatalf("posted as %+v", got.Message)
	}
	if !slices.Equal(got.Notified, []string{"olu"}) {
		t.Fatalf("the Slack person tagged was not notified: %+v", got)
	}
	if _, err := p.StartThread(t.Context(), machine, platform.ThreadInput{
		Project: "Field", Visibility: threads.Shared, Text: "hi", ActAs: &platform.ActAs{Client: "app", User: "U0OLU"},
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("a mapped person without runs:write posted: %v", err)
	}
}

// A thread is read a page at a time, newest page first, each page oldest
// first, and a cursor stays where it was as the thread grows.
func TestAThreadsTurnsArePagedNewestPageFirst(t *testing.T) {
	p := start(t, call.Limits{})
	got := startThread(t, p, operator, platform.ThreadInput{Text: "one", PostOnly: true})
	for _, text := range []string{"two", "three", "four", "five"} {
		post(t, p, operator, got.Thread.ID, platform.MessageInput{Text: text, PostOnly: true})
	}
	texts := func(ts []platform.ThreadTurn) []string {
		var out []string
		for _, t := range ts {
			out = append(out, t.Message.Text)
		}
		return out
	}
	_, page, next, err := p.ThreadTurns(tenant, got.Thread.ID, "", 2)
	if err != nil || !slices.Equal(texts(page), []string{"four", "five"}) || next == "" {
		t.Fatalf("first page %v, %q, %v", texts(page), next, err)
	}
	post(t, p, operator, got.Thread.ID, platform.MessageInput{Text: "six", PostOnly: true})
	_, page, next, err = p.ThreadTurns(tenant, got.Thread.ID, next, 2)
	if err != nil || !slices.Equal(texts(page), []string{"two", "three"}) || next == "" {
		t.Fatalf("second page %v, %q, %v", texts(page), next, err)
	}
	_, page, next, err = p.ThreadTurns(tenant, got.Thread.ID, next, 2)
	if err != nil || !slices.Equal(texts(page), []string{"one"}) || next != "" {
		t.Fatalf("last page %v, %q, %v", texts(page), next, err)
	}
	if _, _, _, err := p.ThreadTurns(tenant, got.Thread.ID, "bogus", 2); !errors.Is(err, platform.ErrTurnCursor) {
		t.Fatalf("a bad cursor = %v", err)
	}
}

// A thread started by a person that no agent has answered is listed with the
// conversations, in its channel, and a private thread is refused.
func TestAThreadNoAgentAnsweredIsListedInItsChannel(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	if _, err := p.CreateChannel(t.Context(), tenant, "Field", ada, platform.ChannelInput{Name: "releases"}); err != nil {
		t.Fatal(err)
	}
	quiet := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Channel: "releases", Title: "Ship notes", Text: "draft here", PostOnly: true})
	if _, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "t-1", Agent: "lead", Text: "hello", Project: "Field"}); err != nil {
		t.Fatal(err)
	}
	in := func(project, channel string) []string {
		t.Helper()
		page, _, err := p.ThreadPage(tenant, platform.ThreadsQuery{ThreadQuery: runs.ThreadQuery{Channel: channel}, Project: project, InProject: true})
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, s := range page {
			ids = append(ids, s.Title)
		}
		return ids
	}
	if got := in("Field", "releases"); !slices.Equal(got, []string{"Ship notes"}) {
		t.Fatalf("#releases lists %v", got)
	}
	if got := in("Field", projects.General); !slices.Equal(got, []string{"hello"}) {
		t.Fatalf("#general lists %v", got)
	}
	if got := in("Globex", ""); len(got) != 0 {
		t.Fatalf("Globex lists %v", got)
	}
	if _, err := p.StartThread(t.Context(), operator, platform.ThreadInput{Project: "Field", Visibility: threads.Private, Text: "secret"}); !errors.Is(err, threads.ErrPrivate) {
		t.Fatalf("a private thread = %v", err)
	}
	if _, err := p.UpdateThread(t.Context(), operator, quiet.Thread.ID, platform.ThreadUpdate{Version: quiet.Thread.Version, Visibility: threads.Private}); !errors.Is(err, threads.ErrPrivate) {
		t.Fatalf("a thread made private = %v", err)
	}
	if _, err := p.ArchiveChannel(tenant, "Field", "releases", ada); err != nil {
		t.Fatal(err)
	}
	if _, err := p.StartThread(t.Context(), operator, platform.ThreadInput{Project: "Field", Channel: "releases", Visibility: threads.Shared, Text: "more"}); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("an archived channel took a thread: %v", err)
	}
}

// Threads are their tenant's: another tenant's thread is not found.
func TestThreadsAreTheirTenants(t *testing.T) {
	p := start(t, call.Limits{})
	got := startThread(t, p, operator, platform.ThreadInput{Text: "ours", PostOnly: true})
	if _, err := p.PostMessage(t.Context(), stranger, got.Thread.ID, platform.MessageInput{Text: "theirs"}); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("another tenant posted: %v", err)
	}
	if _, _, _, err := p.ThreadTurns("globex", got.Thread.ID, "", 0); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("another tenant read: %v", err)
	}
}
