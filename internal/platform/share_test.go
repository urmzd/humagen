package platform_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/envelope"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/threads"
)

var bo = auth.Principal{Subject: "bo", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}

// A conversation the console starts with no visibility named is private:
// its starter's alone, the first participant, read by nobody else in the
// project.
func TestAConversationStartsPrivate(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	workIn(t, p, "Initech", operator, bo)
	got, err := p.StartThread(t.Context(), operator, platform.ThreadInput{Project: "Initech", Text: "a thought"})
	if err != nil {
		t.Fatal(err)
	}
	th := got.Thread
	if th.Visibility != threads.Private || th.Channel != projects.General || len(th.Participants) != 1 || th.Participants[0].Name != "opal" || !th.Participants[0].Owner {
		t.Fatalf("the conversation started as %+v", th)
	}
	if p.SeesThread(bo, th.ID) {
		t.Fatal("another member reads a private conversation")
	}
	shared, err := p.StartThread(t.Context(), operator, platform.ThreadInput{Project: "Initech", Visibility: threads.Shared, Text: "for everyone"})
	if err != nil || shared.Thread.Visibility != threads.Shared || !p.SeesThread(bo, shared.Thread.ID) {
		t.Fatalf("a conversation started shared = %+v, %v", shared.Thread, err)
	}
}

// Sharing a private conversation moves it into a channel of a project, general
// by default, and makes it shared there, so everyone who sees that project
// reads it; the people it names join it. Only a participant who is a member
// where it goes shares it, and it moves to another project only while it has
// no runs.
func TestSharingAConversationMakesItSharedInAChannel(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	workIn(t, p, "Initech", operator, bo)
	workIn(t, p, "Field", operator)
	seePeople(t, p, operator, bo, vic)
	ctx := t.Context()

	got, err := p.StartThread(ctx, operator, platform.ThreadInput{Project: "Initech", Text: "drafting the plan"})
	if err != nil {
		t.Fatal(err)
	}
	id := got.Thread.ID
	post(t, p, operator, id, platform.MessageInput{Text: "a second thought"})

	// Someone who cannot read it is told it is not there.
	if _, err := p.ShareThread(ctx, bo, id, platform.ThreadShare{}); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("a person who cannot read it shared it: %v", err)
	}
	// A name nobody goes by is refused, and nothing changes.
	if _, err := p.ShareThread(ctx, operator, id, platform.ThreadShare{Add: []string{"nobody"}}); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("adding nobody = %v", err)
	}
	// A project the person is not a member of takes nothing.
	if _, err := p.ShareThread(ctx, operator, id, platform.ThreadShare{Project: "Renewals"}); !errors.Is(err, projects.ErrForbidden) && !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("shared into a project its sharer is not a member of: %v", err)
	}
	if th, _ := p.Threads.Get(tenant, id); th.Visibility != threads.Private || th.Project != "Initech" {
		t.Fatalf("a refused share changed the thread to %+v", th)
	}

	cy := auth.Principal{Subject: "cy", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	workIn(t, p, "Field", cy)
	if p.SeesThread(cy, id) {
		t.Fatal("a member of Field reads a private conversation before it is shared")
	}
	// Shared into Field's general, with vic added.
	th, err := p.ShareThread(ctx, operator, id, platform.ThreadShare{Project: "Field", Add: []string{"vic"}})
	if err != nil {
		t.Fatal(err)
	}
	if th.Visibility != threads.Shared || th.Project != "Field" || th.Channel != projects.General || th.Version != got.Thread.Version+1 {
		t.Fatalf("shared, the thread is %+v", th)
	}
	if !slicesHas(th.Participants, "vic") || !slicesHas(th.Participants, "opal") {
		t.Fatalf("participants are %+v", th.Participants)
	}
	if !p.SeesThread(cy, id) {
		t.Fatal("the shared thread is not read by a member where it was shared")
	}
	if msgs, _, err := p.Threads.Messages(tenant, id, "", 10); err != nil || len(msgs) != 2 {
		t.Fatalf("the conversation's messages moved with it: %d, %v", len(msgs), err)
	}
	// It is now a shared thread in Field: a tagged agent answers there, and
	// once it has, the thread stays in Field.
	asked := post(t, p, operator, id, platform.MessageInput{Text: "@helper what do you think?"})
	if len(asked.Runs) != 1 || asked.Runs[0].Project != "Field" {
		t.Fatalf("the tag started %+v", asked.Runs)
	}
	await(t, p, asked.Runs[0].ID, runs.Completed)
	if _, err := p.ShareThread(ctx, operator, id, platform.ThreadShare{Project: "Initech"}); !errors.Is(err, platform.ErrStaysInProject) {
		t.Fatalf("a conversation with runs moved: %v", err)
	}
	// Sharing again where it is adds people and changes nothing else.
	again, err := p.ShareThread(ctx, operator, id, platform.ThreadShare{Add: []string{"bo"}})
	if err != nil || again.Project != "Field" || again.Visibility != threads.Shared || !slicesHas(again.Participants, "bo") {
		t.Fatalf("sharing again = %+v, %v", again, err)
	}
	// Someone who reads it but takes no part does not share it.
	if _, err := p.ShareThread(ctx, cy, id, platform.ThreadShare{}); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a reader who takes no part shared it: %v", err)
	}
}

// A channel that is archived takes no conversation shared into it.
func TestSharingIntoAnArchivedChannelIsRefused(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	workIn(t, p, "Initech", operator)
	if _, err := p.CreateChannel(t.Context(), tenant, "Initech", ada, platform.ChannelInput{Name: "old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ArchiveChannel(tenant, "Initech", "old", ada); err != nil {
		t.Fatal(err)
	}
	got, err := p.StartThread(t.Context(), operator, platform.ThreadInput{Project: "Initech", Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ShareThread(t.Context(), operator, got.Thread.ID, platform.ThreadShare{Channel: "old"}); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("shared into an archived channel: %v", err)
	}
}

func slicesHas(ps []threads.Participant, name string) bool {
	for _, pt := range ps {
		if pt.Kind == threads.Person && pt.Name == name {
			return true
		}
	}
	return false
}

// Someone tagged into a private conversation takes part in it but does not
// publish it: making it shared, or moving it, takes whoever started it (or an
// owner of its project). Once it is shared where it is, any participant who
// may post there adds people, and a share that names no channel keeps the
// thread's own.
func TestOnlyItsStarterPublishesAPrivateConversation(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	workIn(t, p, "Initech", operator, bo)
	seePeople(t, p, operator, bo, vic)
	ctx := t.Context()
	if _, err := p.CreateChannel(ctx, tenant, "Initech", ada, platform.ChannelInput{Name: "design"}); err != nil {
		t.Fatal(err)
	}
	got, err := p.StartThread(ctx, operator, platform.ThreadInput{Project: "Initech", Channel: "design", Text: "@bo a draft for you"})
	if err != nil {
		t.Fatal(err)
	}
	id := got.Thread.ID
	if th, _ := p.Threads.Get(tenant, id); !slicesHas(th.Participants, "bo") {
		t.Fatalf("bo, tagged, takes no part: %+v", th.Participants)
	}
	if _, err := p.ShareThread(ctx, bo, id, platform.ThreadShare{}); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a tagged participant published a private conversation: %v", err)
	}
	th, err := p.ShareThread(ctx, operator, id, platform.ThreadShare{})
	if err != nil || th.Visibility != threads.Shared || th.Channel != "design" {
		t.Fatalf("shared with no channel named = %+v, %v", th, err)
	}
	added, err := p.ShareThread(ctx, bo, id, platform.ThreadShare{Add: []string{"vic"}})
	if err != nil || added.Channel != "design" || !slicesHas(added.Participants, "vic") {
		t.Fatalf("a participant adding someone = %+v, %v", added, err)
	}
	if _, err := p.ShareThread(ctx, bo, id, platform.ThreadShare{Channel: projects.General}); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a participant who did not start it moved it: %v", err)
	}
	// Once a run is filed it stays in its channel too.
	asked := post(t, p, operator, id, platform.MessageInput{Text: "@helper thoughts?"})
	await(t, p, asked.Runs[0].ID, runs.Completed)
	if _, err := p.ShareThread(ctx, operator, id, platform.ThreadShare{Channel: projects.General}); !errors.Is(err, platform.ErrStaysInChannel) {
		t.Fatalf("a conversation with runs changed channel: %v", err)
	}
}

// A default that cannot answer where a conversation is shared to is cleared,
// and the share says nothing of it.
func TestSharingClearsADefaultThatCannotAnswerThere(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	workIn(t, p, "Initech", operator)
	workIn(t, p, "Field", operator)
	got, err := p.StartThread(t.Context(), operator, platform.ThreadInput{Project: "Initech", Default: "helper", Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	setAgents(t, p, "Field", platform.AgentsInput{Enable: []string{}, Allow: []string{"lead"}})
	th, err := p.ShareThread(t.Context(), operator, got.Thread.ID, platform.ThreadShare{Project: "Field"})
	if err != nil || th.Project != "Field" || th.Default != "" {
		t.Fatalf("shared into a project helper cannot answer in = %+v, %v", th, err)
	}
}

// A selected agent that cannot answer where a conversation is shared to is
// dropped from the selection, in the same write; a move through an update
// that names it is refused, as selecting it there would be.
func TestSharingDropsSelectedAgentsThatCannotAnswerThere(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	workIn(t, p, "Initech", operator)
	workIn(t, p, "Field", operator)
	ctx := t.Context()
	start := func() threads.Thread {
		got, err := p.StartThread(ctx, operator, platform.ThreadInput{Project: "Initech", Agents: []string{"helper", "lead"}, Mode: threads.ModeAll, Text: "x", PostOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		return got.Thread
	}
	shared, moved := start(), start()
	setAgents(t, p, "Field", platform.AgentsInput{Enable: []string{}, Allow: []string{"lead"}})
	th, err := p.ShareThread(ctx, operator, shared.ID, platform.ThreadShare{Project: "Field"})
	if err != nil || th.Project != "Field" || !slices.Equal(th.Agents, []string{"lead"}) || th.Mode != "" {
		t.Fatalf("shared into a project helper cannot answer in = %+v, %v", th, err)
	}
	if _, err := p.UpdateThread(ctx, operator, moved.ID, platform.ThreadUpdate{Version: moved.Version, Project: "Field", Agents: moved.Agents, Mode: moved.Mode}); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("moving with helper selected into a project it cannot answer in = %v", err)
	}
}

// A share that lands after a message is posted and before what it starts is
// decided starts nothing for a poster who may not post where the
// conversation went: no run acts for them in a project they cannot read.
func TestAMessageStartsNothingWhereItsConversationMovedOutOfReach(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	workIn(t, p, "Initech", operator, bo)
	seePeople(t, p, operator, bo)
	ctx := t.Context()
	if _, err := p.CreateProject(ctx, tenant, ada, platform.ProjectInput{Name: "Vault"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Projects.Update(tenant, "Vault", 1, ada, time.Now(), func(pr *projects.Project) error {
		pr.Members = []projects.Entry{{Who: "opal", Role: projects.Member}, {Who: "ada", Role: projects.Owner}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := p.StartThread(ctx, operator, platform.ThreadInput{Project: "Initech", Text: "@bo join me"})
	if err != nil {
		t.Fatal(err)
	}
	id := got.Thread.ID
	once := false
	p.BeforeCarryOut(func() {
		if once {
			return
		}
		once = true
		if _, err := p.ShareThread(ctx, operator, id, platform.ThreadShare{Project: "Vault"}); err != nil {
			t.Error(err)
		}
	})
	posted := post(t, p, bo, id, platform.MessageInput{Text: "@helper go"})
	if len(posted.Runs) != 0 || posted.Reason == "" {
		t.Fatalf("a message whose conversation moved out of its poster's reach started %+v (%s)", posted.Runs, posted.Reason)
	}
	if _, err := p.Runs.Conversation(tenant, id); err == nil {
		t.Fatal("a run was filed for bo in Vault")
	}
}

// A mention raised in a private conversation is read where the conversation
// is now: after it is shared into another project, whoever reads the
// conversation reads the mention's subject, and nobody else.
func TestAMentionIsReadWhereItsConversationWent(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	workIn(t, p, "Globex", operator, bo)
	// Vault is read by its members alone.
	if _, err := p.CreateProject(t.Context(), tenant, ada, platform.ProjectInput{Name: "Vault"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Projects.Update(tenant, "Vault", 1, ada, time.Now(), func(pr *projects.Project) error {
		pr.Members = []projects.Entry{{Who: "opal", Role: projects.Member}, {Who: "ada", Role: projects.Owner}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	seePeople(t, p, operator, bo)
	got, err := p.StartThread(t.Context(), operator, platform.ThreadInput{Project: "Globex", Text: "@bo look at this"})
	if err != nil {
		t.Fatal(err)
	}
	id := got.Thread.ID
	if _, err := p.ShareThread(t.Context(), operator, id, platform.ThreadShare{Project: "Vault"}); err != nil {
		t.Fatal(err)
	}
	m, ok := p.Inbox.ByKey(tenant, inbox.MentionKey(id, "m-1", "bo"))
	if !ok {
		t.Fatal("bo was not sent a mention")
	}
	if p.SeesThread(viewer, id) {
		t.Fatal("the test needs someone who reads Globex and not Vault")
	}
	for _, who := range []auth.Principal{viewer, bo, operator} {
		if p.Seer(who).Message(m) != p.SeesThread(who, id) {
			t.Errorf("%s reads the mention: %v, and the conversation: %v", who.Subject, p.Seer(who).Message(m), p.SeesThread(who, id))
		}
	}
}

// A conversation from before threads reads with as many participants as a
// thread holds, not only as many as one writer adds.
func TestAConversationFromBeforeThreadsHoldsAsManyParticipantsAsAThread(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	first, err := p.Converse(ctx, tenant, platform.Turn{ID: "t-0", Agent: "lead", Text: "hi", Actor: envelope.Actor{ID: "p0", User: "p0"}})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, first.ID, runs.Completed)
	const people = threads.MaxParticipants + 10
	for i := 1; i < people; i++ {
		who := fmt.Sprintf("p%d", i)
		if _, err := p.Converse(ctx, tenant, platform.Turn{ID: "t-" + who, Agent: "lead", Text: "and?", After: first.ID, Actor: envelope.Actor{ID: who, User: who}}); err != nil {
			t.Fatal(err)
		}
	}
	rec, err := p.ThreadRecord(tenant, first.Thread)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(rec.Participants); n != people+1 {
		t.Fatalf("the record holds %d participants, want %d people and the agent", n, people)
	}
}

// A share adds only people who can read the project the conversation goes
// to, says who it did not add and why, and tells each person it added in
// their inbox, once, where the conversation is read.
func TestSharingTellsThePeopleItAddsAndAddsOnlyWhoCanRead(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	workIn(t, p, "Initech", operator, bo)
	seePeople(t, p, operator, bo, admin)
	ctx := t.Context()
	if _, err := p.CreateProject(ctx, tenant, ada, platform.ProjectInput{Name: "Vault"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Projects.Update(tenant, "Vault", 1, ada, time.Now(), func(pr *projects.Project) error {
		pr.Members = []projects.Entry{{Who: "opal", Role: projects.Member}, {Who: "ada", Role: projects.Owner}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := p.StartThread(ctx, operator, platform.ThreadInput{Project: "Initech", Title: "Renewal plan", Text: "a draft"})
	if err != nil {
		t.Fatal(err)
	}
	id := got.Thread.ID
	shared, err := p.Share(ctx, operator, id, platform.ThreadShare{Project: "Vault", Add: []string{"bo", "ada"}})
	if err != nil {
		t.Fatal(err)
	}
	if slicesHas(shared.Thread.Participants, "bo") || !slicesHas(shared.Thread.Participants, "ada") {
		t.Fatalf("participants are %+v", shared.Thread.Participants)
	}
	if len(shared.NotAdded) != 1 || shared.NotAdded[0].Handle != "bo" || shared.NotAdded[0].Reason != "they cannot read this project, so they were not added" {
		t.Fatalf("not added = %+v", shared.NotAdded)
	}
	told, ok := p.Inbox.ByKey(tenant, platform.AddedKey(id, "ada"))
	if !ok || told.To != "ada" || told.Subject != "Opal added you to Renewal plan in Vault #general" || len(told.Parts) != 1 || told.Parts[0].Mention == nil || told.Parts[0].Mention.Message != "m-1" {
		t.Fatalf("ada was told %+v, %v", told, ok)
	}
	if !p.Seer(admin).Message(told) || p.Seer(bo).Message(told) {
		t.Fatal("the message telling ada is not read where the conversation is")
	}
	if _, ok := p.Inbox.ByKey(tenant, platform.AddedKey(id, "bo")); ok {
		t.Fatal("bo, who was not added, was told")
	}
	if _, ok := p.Inbox.ByKey(tenant, platform.AddedKey(id, "opal")); ok {
		t.Fatal("the sharer was told they added themselves")
	}
	if _, err := p.Share(ctx, operator, id, platform.ThreadShare{Add: []string{"ada"}}); err != nil {
		t.Fatal(err)
	}
	if again, _ := p.Inbox.ByKey(tenant, platform.AddedKey(id, "ada")); again.ID != told.ID || !again.UpdatedAt.Equal(told.UpdatedAt) {
		t.Fatalf("ada was told again: %+v", again)
	}
}
