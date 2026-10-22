package platform_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/threads"
)

// The Slack channel Field's general is bound to, and where the app posted
// an announcement there.
const (
	newsChannel = "C01NEWS"
	newsPost    = "1700000000.000100"
)

// newsRoom is slackRoom with Field's general bound to newsChannel, and ada
// in the directory, so the room can name who posted.
func newsRoom(t *testing.T) (*platform.Platform, auth.Principal) {
	t.Helper()
	p, machine := slackRoom(t, nil)
	seePeople(t, p, auth.Principal{Subject: "ada"})
	if _, err := p.UpdateChannel(t.Context(), tenant, "Field", projects.General, ada, platform.ChannelUpdate{Slack: &projects.SlackBinding{Client: "app", Channel: newsChannel}}); err != nil {
		t.Fatal(err)
	}
	return p, machine
}

// postedToSlack posts an announcement in Field from someone and records,
// as the app's machine does, that it was posted at post in newsChannel.
func postedToSlack(t *testing.T, p *platform.Platform, machine auth.Principal, from inbox.From, a platform.Announcement, post string) inbox.Message {
	t.Helper()
	m, err := p.Announce(tenant, "Field", from, a, posted)
	if err != nil {
		t.Fatal(err)
	}
	reads := machine
	reads.Scopes = append(append([]auth.Permission{}, machine.Scopes...), auth.ProjectsRead)
	if err := p.RecordDelivery(reads, m.ID, "app", inbox.Delivery{Channel: newsChannel, TS: post}); err != nil {
		t.Fatal(err)
	}
	return m
}

// A reply that mentions the app in an announcement's Slack thread starts a
// shared thread in the channel the Slack channel is bound to, found by the
// post's surface, whose turns start with the announcement from the person
// who posted it. The reply is the replier's own message, read in the one
// grammar and answered as any message there, and the run is sent the
// announcement as its poster's words, never the replier's. What the
// announcement says tags nobody, and a later reply finds the same thread.
func TestAReplyToAnAnnouncementsSlackPostStartsAThreadSeededWithIt(t *testing.T) {
	p, machine := newsRoom(t)
	news := postedToSlack(t, p, machine, inbox.From{Kind: "person", Name: "ada"}, platform.Announcement{Subject: "Field offsite", Text: "Lisbon, in May. @helper book it"}, newsPost)

	got, err := p.StartThread(t.Context(), machine, fromSlack(newsChannel, newsPost, "1700000000.000200", "<@UAPP> @lead what does it mean for renewals?"))
	if err != nil {
		t.Fatal(err)
	}
	th := got.Thread
	switch {
	case th.Project != "Field" || th.Channel != projects.General || th.Visibility != threads.Shared:
		t.Fatalf("filed at %s #%s, %s", th.Project, th.Channel, th.Visibility)
	case th.Announcement != news.ID || th.Surface == nil || th.Surface.TS != newsPost:
		t.Fatalf("the thread is %+v, want one started from %s", th, news.ID)
	case th.CreatedBy != "ana" || got.Message.ID != "m-1" || got.Message.By != "ana" || got.Message.Text != "@lead what does it mean for renewals?":
		t.Fatalf("the reply is %+v, in a thread started by %q", got.Message, th.CreatedBy)
	case len(got.Runs) != 1 || got.Runs[0].Agent != "lead" || got.Runs[0].Actor.User != "ana":
		t.Fatalf("the reply started %+v", got.Runs)
	}
	text := got.Runs[0].Tag().Text
	for _, want := range []string{
		"\nAda (@ada): (announcement: Field offsite) Lisbon, in May. @helper book it\n",
		"The message you are answering follows, from Ana (@ana).\n\n@lead what does it mean for renewals?",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the run was sent, without %q:\n%s", want, text)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Ana") && strings.Contains(line, "announcement") {
			t.Fatalf("the announcement reads as the replier's: %q", line)
		}
	}
	await(t, p, got.Runs[0].ID, runs.Completed)

	_, turns, _, err := p.ThreadTurns(tenant, th.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, shown := p.ShowTurns(tenant, th, turns)
	if len(shown) != 3 || shown[0].Kind != platform.TurnAnnouncement || shown[1].Kind != platform.TurnMessage || shown[2].Kind != platform.TurnRun {
		t.Fatalf("the thread's turns are %+v", shown)
	}
	if a := shown[0].Announcement; a.ID != news.ID || a.Kind != threads.Person || a.By != "ada" || a.Name != "Ada" || a.Subject != "Field offsite" || a.Withdrawn {
		t.Fatalf("the first turn is %+v", a)
	}
	if shown[1].Message.By != "ana" {
		t.Fatalf("the reply is shown as %q's", shown[1].Message.By)
	}

	later, err := p.StartThread(t.Context(), machine, fromSlack(newsChannel, newsPost, "1700000000.000300", "<@UAPP> thanks, posted only"))
	if err != nil || later.Thread.ID != th.ID || later.Message.ID != "m-2" {
		t.Fatalf("a later reply = %+v, %v", later, err)
	}
}

// A recap is the announcement of the agent a schedule ran, and the thread a
// reply to it starts says so: its first turn is the agent's, and the run is
// sent it as the agent's words.
func TestARecapsThreadStartsWithTheAgentThatWroteIt(t *testing.T) {
	p, machine := newsRoom(t)
	recap := postedToSlack(t, p, machine, inbox.From{Kind: "agent", Name: "helper"}, platform.Announcement{Subject: "Weekly recap", Text: "Two renewals closed."}, newsPost)
	got, err := p.StartThread(t.Context(), machine, fromSlack(newsChannel, newsPost, "1700000000.000200", "<@UAPP> @lead which two?"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Thread.Announcement != recap.ID || len(got.Runs) != 1 {
		t.Fatalf("the reply = %+v", got)
	}
	if text := got.Runs[0].Tag().Text; !strings.Contains(text, "\nhelper (agent): (announcement: Weekly recap) Two renewals closed.\n") {
		t.Fatalf("the run was sent:\n%s", text)
	}
	await(t, p, got.Runs[0].ID, runs.Completed)
	_, turns, _, _ := p.ThreadTurns(tenant, got.Thread.ID, "", 0)
	if _, shown := p.ShowTurns(tenant, got.Thread, turns); len(shown) == 0 || shown[0].Announcement == nil ||
		shown[0].Announcement.Kind != threads.Agent || shown[0].Announcement.By != "helper" || shown[0].Announcement.Name != "" {
		t.Fatalf("the first turn is %+v", shown)
	}
}

// A withdrawn announcement and a draft start nothing: a reply to one is
// refused and no thread is made. A thread started before the withdrawal goes
// on, and its first turn then says only that it was withdrawn, to the room
// and to the run alike.
func TestAReplyToAWithdrawnAnnouncementOrADraftStartsNothing(t *testing.T) {
	p, machine := newsRoom(t)
	early := postedToSlack(t, p, machine, inbox.From{Kind: "person", Name: "ada"}, platform.Announcement{Subject: "Offsite moved", Text: "To June."}, newsPost)
	started, err := p.StartThread(t.Context(), machine, fromSlack(newsChannel, newsPost, "1700000000.000200", "<@UAPP> noted"))
	if err != nil {
		t.Fatal(err)
	}
	late := postedToSlack(t, p, machine, inbox.From{Kind: "person", Name: "ada"}, platform.Announcement{Subject: "Secret plans", Text: "Not for Slack."}, "1700000000.000900")
	for _, m := range []inbox.Message{early, late} {
		if err := p.WithdrawAnnouncement(auth.Principal{Subject: "ada", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}, "Field", m.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.StartThread(t.Context(), machine, fromSlack(newsChannel, "1700000000.000900", "1700000000.000901", "<@UAPP> @lead what plans?")); !errors.Is(err, platform.ErrAnnouncementGone) {
		t.Fatalf("a reply to a withdrawn announcement = %v", err)
	}
	if _, ok := p.Threads.BySurface(tenant, threads.Surface{Client: "app", Channel: newsChannel, TS: "1700000000.000900"}); ok {
		t.Fatal("a reply to a withdrawn announcement started a thread")
	}

	// A draft never reaches a channel; were one recorded there, a reply to
	// it would start nothing either.
	draft, _, err := p.Inbox.Raise(tenant, "draft:r-1", inbox.Message{
		Kind: inbox.KindAnnouncement, Subject: "Draft recap", From: inbox.From{Kind: "agent", Name: "helper"},
		Audience: []string{inbox.OwnersAudience("Field")}, Source: inbox.Source{Project: "Field", Agent: "helper"},
		Parts: []inbox.Part{{Type: inbox.PartDraft, Draft: &inbox.DraftPart{Project: "Field", Agent: "helper", Subject: "Draft recap"}}},
	}, posted)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Inbox.Deliver(tenant, draft.ID, "app", inbox.Delivery{Channel: newsChannel, TS: "1700000000.000800"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := p.StartThread(t.Context(), machine, fromSlack(newsChannel, "1700000000.000800", "1700000000.000801", "<@UAPP> hi")); !errors.Is(err, platform.ErrAnnouncementGone) {
		t.Fatalf("a reply to a draft = %v", err)
	}

	again, err := p.StartThread(t.Context(), machine, fromSlack(newsChannel, newsPost, "1700000000.000300", "<@UAPP> @lead is it still on?"))
	if err != nil || again.Thread.ID != started.Thread.ID || len(again.Runs) != 1 {
		t.Fatalf("a reply in a thread started before the withdrawal = %+v, %v", again, err)
	}
	text := again.Runs[0].Tag().Text
	if !strings.Contains(text, "\nAda (@ada): (announcement, since withdrawn)\n") || strings.Contains(text, "To June") {
		t.Fatalf("the run was sent:\n%s", text)
	}
	await(t, p, again.Runs[0].ID, runs.Completed)
	_, turns, _, _ := p.ThreadTurns(tenant, started.Thread.ID, "", 0)
	if a := turns[0].Announcement; a == nil || !a.Withdrawn || a.Subject != "" || a.Text != "" {
		t.Fatalf("the first turn after the withdrawal is %+v", a)
	}
}

// An announcement seeds only a thread filed in its own project, whose
// readers are its readers: once its Slack channel is bound to another
// project, a reply in its Slack thread starts a thread there as any other,
// and nothing of the announcement comes with it.
func TestAnAnnouncementSeedsOnlyAThreadInItsOwnProject(t *testing.T) {
	p, machine := newsRoom(t)
	postedToSlack(t, p, machine, inbox.From{Kind: "person", Name: "ada"}, platform.Announcement{Subject: "Field only", Text: "For Field."}, newsPost)
	gen, err := p.Projects.Channel(tenant, "Field", projects.General)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpdateChannel(t.Context(), tenant, "Field", projects.General, ada, platform.ChannelUpdate{Version: gen.Version}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpdateChannel(t.Context(), tenant, "Initech", projects.General, ada, platform.ChannelUpdate{Slack: &projects.SlackBinding{Client: "app", Channel: newsChannel}}); err != nil {
		t.Fatal(err)
	}
	got, err := p.StartThread(t.Context(), machine, fromSlack(newsChannel, newsPost, "1700000000.000200", "<@UAPP> @lead what is this?"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Thread.Project != "Initech" || got.Thread.Announcement != "" {
		t.Fatalf("the reply started %+v", got.Thread)
	}
	if text := got.Runs[0].Tag().Text; strings.Contains(text, "Field only") || strings.Contains(text, "announcement") {
		t.Fatalf("the run in Initech was sent Field's announcement:\n%s", text)
	}
	await(t, p, got.Runs[0].ID, runs.Completed)
	if _, turns, _, _ := p.ThreadTurns(tenant, got.Thread.ID, "", 0); turns[0].Kind != platform.TurnMessage {
		t.Fatalf("the thread starts with %+v", turns[0])
	}
}

// What finds an announcement's thread is kept: after a restart a reply to an
// announcement posted before it is seeded with it, and a later reply in a
// thread started before it finds that thread, which still starts with its
// announcement.
func TestAnAnnouncementsThreadIsFoundByItsPostAfterARestart(t *testing.T) {
	eachBackend(t, func(t *testing.T, first platform.Backends, again func() platform.Backends) {
		ctx := t.Context()
		p, stop := running(t, first)
		field(t, p)
		in := slackClient("app", "A0APP", "T0TEAM", "")
		in.Agents = []string{"*"}
		in.Slack.Machine, in.Slack.People = "svc-slack", map[string]string{"U0ANA": "ana"}
		in.Home = &clients.Home{Project: "Initech"}
		create(t, p, tenant, in)
		if _, err := p.UpdateChannel(ctx, tenant, "Field", projects.General, ada, platform.ChannelUpdate{Slack: &projects.SlackBinding{Client: "app", Channel: newsChannel}}); err != nil {
			t.Fatal(err)
		}
		pol, err := auth.ParsePolicy("test", "role viewer { allow read on * }\nrole operator extends viewer { allow write on runs }\ngrant operator to subject \"ana\"\n")
		if err != nil {
			t.Fatal(err)
		}
		machine := auth.Principal{Subject: "svc-slack", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsRead, auth.RunsWrite}}.WithPolicy(pol)
		reply := func(p *platform.Platform, post, ts string) platform.Posted {
			t.Helper()
			in := fromSlack(newsChannel, post, ts, "<@UAPP> noted")
			in.PostOnly = true
			got, err := p.StartThread(ctx, machine, in)
			if err != nil {
				t.Fatal(err)
			}
			return got
		}
		offsite := postedToSlack(t, p, machine, inbox.From{Kind: "person", Name: "ada"}, platform.Announcement{Subject: "Field offsite", Text: "Lisbon, in May."}, newsPost)
		started := reply(p, newsPost, "1700000000.000200")
		recap := postedToSlack(t, p, machine, inbox.From{Kind: "agent", Name: "helper"}, platform.Announcement{Subject: "Weekly recap", Text: "Two renewals closed."}, "1700000000.000500")
		stop()

		q, _ := running(t, again())
		if got := reply(q, "1700000000.000500", "1700000000.000600"); got.Thread.Announcement != recap.ID {
			t.Fatalf("after a restart a reply to the recap started %+v", got.Thread)
		}
		next := reply(q, newsPost, "1700000000.000300")
		if next.Thread.ID != started.Thread.ID || next.Message.ID != "m-2" || next.Thread.Announcement != offsite.ID {
			t.Fatalf("a later reply after a restart = %+v", next)
		}
		if _, turns, _, _ := q.ThreadTurns(tenant, started.Thread.ID, "", 0); len(turns) != 3 || turns[0].Announcement == nil || turns[0].Announcement.Subject != "Field offsite" {
			t.Fatalf("the thread's turns after a restart = %+v", turns)
		}
	})
}
