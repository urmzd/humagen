package platform_test

import (
	"cmp"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/envelope"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// slackRoom is a platform with Field's renewals channel bound to C01RENEW
// through the Slack client app, whose machine is svc-slack and which maps
// U0ANA to ana and U0VIC to vic. The policy grants ana the operator role by
// subject, which is all that is known of a person a surface posts for.
func slackRoom(t *testing.T, mutate func(*platform.ClientInput)) (*platform.Platform, auth.Principal) {
	t.Helper()
	p := start(t, call.Limits{})
	field(t, p)
	seePeople(t, p, auth.Principal{Subject: "ana"}, auth.Principal{Subject: "vic"})
	in := slackClient("app", "A0APP", "T0TEAM", "helper")
	in.Agents = []string{clients.AnyAgent}
	in.Slack.Machine, in.Slack.People = "svc-slack", map[string]string{"U0ANA": "ana", "U0VIC": "vic"}
	in.Home = &clients.Home{Project: "Initech"}
	if mutate != nil {
		mutate(&in)
	}
	create(t, p, tenant, in)
	if _, err := p.CreateChannel(t.Context(), tenant, "Field", ada, platform.ChannelInput{Name: "renewals", Slack: &projects.SlackBinding{Client: "app", Channel: "C01RENEW"}}); err != nil {
		t.Fatal(err)
	}
	pol, err := auth.ParsePolicy("test", `
role viewer { allow read on * }
role operator extends viewer { allow write on runs }
grant operator to subject "ana"
`)
	if err != nil {
		t.Fatal(err)
	}
	machine := auth.Principal{Subject: "svc-slack", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsRead, auth.RunsWrite}}.WithPolicy(pol)
	return p, machine
}

func fromSlack(channel, ts, message, text string) platform.ThreadInput {
	return platform.ThreadInput{
		Text: text, ActAs: &platform.ActAs{Client: "app", User: "U0ANA"},
		Surface: &platform.SurfaceRef{Client: "app", Channel: channel, TS: ts, Message: message, AppUser: "UAPP"},
	}
}

// A Slack thread in a bound channel is one shared thread in the channel it
// is bound to: the app's mention is not a tag, an agent's handle is, and
// each message is posted once, however often the app sends it.
func TestASlackThreadInABoundChannelIsSharedThere(t *testing.T) {
	p, machine := slackRoom(t, nil)
	first, err := p.StartThread(t.Context(), machine, fromSlack("C01RENEW", "1700000000.000100", "", "<@UAPP> @lead plan the renewal"))
	if err != nil {
		t.Fatal(err)
	}
	th := first.Thread
	switch {
	case th.Project != "Field" || th.Channel != "renewals" || th.Visibility != threads.Shared:
		t.Fatalf("filed at %s #%s, %s", th.Project, th.Channel, th.Visibility)
	case first.Message.By != "ana" || first.Message.Via != "app" || first.Message.Text != "@lead plan the renewal":
		t.Fatalf("the message is %+v", first.Message)
	case len(first.Runs) != 1 || first.Runs[0].Agent != "lead":
		t.Fatalf("it started %+v", first.Runs)
	}
	again, err := p.StartThread(t.Context(), machine, fromSlack("C01RENEW", "1700000000.000100", "", "<@UAPP> @lead plan the renewal"))
	if err != nil || again.Thread.ID != th.ID || again.Message.ID != first.Message.ID || len(again.Runs) != 1 || again.Runs[0].ID != first.Runs[0].ID {
		t.Fatalf("sent again it did %+v, %v", again, err)
	}
	next, err := p.StartThread(t.Context(), machine, fromSlack("C01RENEW", "1700000000.000100", "1700000000.000300", "<@UAPP> and <@U0VIC|vic> too, <@UEVE|eve>"))
	if err != nil || next.Thread.ID != th.ID || next.Message.ID != "m-2" {
		t.Fatalf("a reply in the Slack thread = %+v, %v", next, err)
	}
	if next.Message.Text != "and @vic too, eve" || len(next.Refused) != 1 || next.Refused[0].Name != "UEVE" {
		t.Fatalf("the reply reads %q, refusing %+v", next.Message.Text, next.Refused)
	}
	if !p.Seer(machine).Place(platform.Place{Project: th.Project, Thread: th.ID}) {
		t.Fatal("the app's machine cannot read its own Slack thread")
	}
}

// A direct message, or the assistant pane, is a private thread in the
// client's home, which its default answers; a message in an unbound channel
// is shared there.
func TestADirectMessageIsPrivateInTheClientsHome(t *testing.T) {
	p, machine := slackRoom(t, nil)
	dm, err := p.StartThread(t.Context(), machine, fromSlack("D0ANA", "1700000000.000100", "", "what changed?"))
	if err != nil {
		t.Fatal(err)
	}
	if th := dm.Thread; th.Project != "Initech" || th.Channel != projects.General || th.Visibility != threads.Private {
		t.Fatalf("a direct message is filed at %s #%s, %s", th.Project, th.Channel, th.Visibility)
	}
	if dm.Rule != threads.RuleDefault || len(dm.Runs) != 1 || dm.Runs[0].Agent != "helper" {
		t.Fatalf("an untagged direct message started %+v (%s)", dm.Runs, dm.Rule)
	}
	if p.SeesThread(operator, dm.Thread.ID) || !p.SeesThread(auth.Principal{Subject: "ana", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}}, dm.Thread.ID) {
		t.Fatal("a direct message is read by someone other than the person in it")
	}
	open, err := p.StartThread(t.Context(), machine, fromSlack("C09OPEN", "1700000000.000200", "", "anyone?"))
	if err != nil || open.Thread.Project != "Initech" || open.Thread.Visibility != threads.Shared {
		t.Fatalf("an unbound channel's thread = %+v, %v", open.Thread, err)
	}
}

// Only the client's machine posts from its surface, for one of its own
// people, and never says where the thread goes.
func TestOnlyTheClientsMachinePostsFromItsSurface(t *testing.T) {
	p, machine := slackRoom(t, nil)
	other := auth.Principal{Subject: "svc-other", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsRead, auth.RunsWrite}}.WithPolicy(machine.Policy())
	if _, err := p.StartThread(t.Context(), other, fromSlack("C01RENEW", "1", "", "hi")); !errors.Is(err, clients.ErrNotFound) {
		t.Fatalf("another machine = %v, want the client not found", err)
	}
	in := fromSlack("C01RENEW", "1", "", "hi")
	in.Project = "Globex"
	if _, err := p.StartThread(t.Context(), machine, in); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("a surface's message naming its project = %v", err)
	}
	in = fromSlack("C01RENEW", "1", "", "hi")
	in.ActAs = nil
	if _, err := p.StartThread(t.Context(), machine, in); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("a surface's message for nobody = %v", err)
	}
	posted, err := p.StartThread(t.Context(), machine, fromSlack("C01RENEW", "1", "", "hi"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.PostMessage(t.Context(), machine, posted.Thread.ID, platform.MessageInput{
		Text: "x", ActAs: &platform.ActAs{Client: "app", User: "U0ANA"},
		Surface: &platform.SurfaceRef{Client: "app", Channel: "C01RENEW", TS: "2", Message: "3"},
	}); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("posting into a thread as another surface's = %v", err)
	}
}

// A client's allow-list holds on its surface: an agent it may not route to
// is refused there, never swapped, and @auto is a name outside the list.
func TestAClientsListHoldsOnItsSurface(t *testing.T) {
	p, machine := slackRoom(t, func(in *platform.ClientInput) { in.Agents = []string{"helper"} })
	got, err := p.StartThread(t.Context(), machine, fromSlack("C01RENEW", "1", "", "<@UAPP> @lead and @auto look"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Runs) != 0 || len(got.Refused) != 2 {
		t.Fatalf("a client that lists helper started %+v, refusing %+v", got.Runs, got.Refused)
	}
}

// A client's allow-list holds wherever its credential posts for a person,
// on its surface or not: a thread it starts for them with no surface, and a
// message it posts for them in a thread started in the console. The person
// posting as themselves is held to no client's list.
func TestAClientsListHoldsWhereverItPostsForAPerson(t *testing.T) {
	p, machine := slackRoom(t, func(in *platform.ClientInput) { in.Agents = []string{"helper"} })
	ana := &platform.ActAs{Client: "app", User: "U0ANA"}
	agents := func(got platform.Posted) []string {
		out := []string{}
		for _, r := range got.Runs {
			out = append(out, r.Agent)
		}
		return out
	}
	started, err := p.StartThread(t.Context(), machine, platform.ThreadInput{
		Project: "Field", Channel: "renewals", Visibility: threads.Shared, Text: "@lead and @auto look", ActAs: ana,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(started.Runs) != 0 || len(started.Refused) != 2 {
		t.Fatalf("a thread the client started for ana with no surface started %v, refusing %+v", agents(started), started.Refused)
	}
	room := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "A room in the console.", PostOnly: true})
	posted, err := p.PostMessage(t.Context(), machine, room.Thread.ID, platform.MessageInput{Text: "@lead and @helper look", ActAs: ana})
	if err != nil {
		t.Fatal(err)
	}
	if got := agents(posted); !slices.Equal(got, []string{"helper"}) || len(posted.Refused) != 1 || posted.Refused[0].Name != "lead" {
		t.Fatalf("a message the client posted for ana in a console thread started %v, refusing %+v", got, posted.Refused)
	}
	herself := auth.Principal{Subject: "ana", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	own, err := p.PostMessage(t.Context(), herself, room.Thread.ID, platform.MessageInput{Text: "@lead look"})
	if err != nil {
		t.Fatal(err)
	}
	if got := agents(own); !slices.Equal(got, []string{"lead"}) {
		t.Fatalf("ana posting as herself started %v, refusing %+v", got, own.Refused)
	}
}

// A client's machine reads the inbox of a person it maps, as them, and
// nobody else reads through it.
func TestAMachineReadsAMappedPersonsInbox(t *testing.T) {
	p, machine := slackRoom(t, nil)
	if _, _, err := p.Inbox.Raise(tenant, "note:ana", inbox.Message{
		Kind: inbox.KindResult, Subject: "For ana", To: "ana", Severity: inbox.Info,
		Parts: []inbox.Part{{Type: inbox.PartText, Text: "Done."}},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	ana, err := p.ActingFor(machine, platform.ActAs{Client: "app", User: "U0ANA"})
	if err != nil || ana.Subject != "ana" {
		t.Fatalf("acting for U0ANA = %+v, %v", ana, err)
	}
	if c := p.InboxCount(ana); c.Open != 1 {
		t.Fatalf("ana's count through the app = %+v", c)
	}
	if _, err := p.ActingFor(machine, platform.ActAs{Client: "app", User: "U0NOBODY"}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("an unmapped person = %v", err)
	}
	other := auth.Principal{Subject: "svc-other", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsRead}}
	if _, err := p.ActingFor(other, platform.ActAs{Client: "app", User: "U0ANA"}); !errors.Is(err, clients.ErrNotFound) {
		t.Fatalf("another machine = %v", err)
	}
}

// A run a client's in-process connector raises is filed by the binding: in
// the channel its Slack channel is bound to, or else in the client's home,
// whatever the payload said. A mention that names no agent is answered as
// one in a thread there would be: in a bound channel by the channel's chain,
// never the client's default, and in the home by the client's default.
func TestAConnectorsRunIsFiledByItsBinding(t *testing.T) {
	f := &fakes{}
	c := &cast{says: map[string][][]saigetypes.Delta{"docs": {say("Docs here."), say("Again.")}, "ops": {say("Ops here.")}}}
	cfg := f.config()
	cfg.Policy = operators(t, "ada")
	p := stage(t, c, cfg)
	// mentionOf's Slack user is ada, mapped by the Slack app built on the
	// API in the same team: only people the client maps ask in a bound
	// channel.
	mapper := slackClient("api-app", "A0API", team, "docs")
	mapper.Slack.Machine, mapper.Slack.People = "svc-slack", map[string]string{"U0HUMAN1": "ada"}
	create(t, p, tenant, mapper)
	if _, err := p.CreateProject(t.Context(), tenant, ada, platform.ProjectInput{Name: "Field"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(t.Context(), tenant, ada, platform.ProjectInput{Name: "Launch"}); err != nil {
		t.Fatal(err)
	}
	in := slackClient("docs-app", "A0DOCS", team, "docs")
	in.Home, in.Agents = &clients.Home{Project: "Launch"}, []string{"ops"}
	create(t, p, tenant, in)
	if _, err := p.CreateChannel(t.Context(), tenant, "Field", ada, platform.ChannelInput{Name: "docs", Default: "ops", Slack: &projects.SlackBinding{Client: "docs-app", Channel: "C01"}}); err != nil {
		t.Fatal(err)
	}
	ingress := p.IngressHandler()
	if code := signedSlack(ingress, "secret-docs-app", mentionOf("A0DOCS", team, "Ev1", "C01", "1700000000.000100")); code != http.StatusOK {
		t.Fatalf("the mention answered %d", code)
	}
	if code := signedSlack(ingress, "secret-docs-app", mentionOf("A0DOCS", team, "Ev2", "C09", "1700000000.000200")); code != http.StatusOK {
		t.Fatalf("the mention answered %d", code)
	}
	var bound, homed runs.Run
	eventually(t, "both mentions were answered", func() bool {
		for _, r := range p.Runs.List(tenant, 20) {
			switch r.Tag().Meta["slack_channel"] {
			case "C01":
				bound = r
			case "C09":
				homed = r
			}
		}
		return bound.ID != "" && homed.ID != ""
	})
	if bound.Project != "Field" || bound.Channel != "docs" || bound.Agent != "ops" {
		t.Fatalf("a run in a bound channel is %s's, filed at %q #%s", bound.Agent, bound.Project, bound.Channel)
	}
	if homed.Project != "Launch" || homed.Channel != projects.General || homed.Agent != "docs" {
		t.Fatalf("a run in an unbound channel is %s's, filed at %q #%s", homed.Agent, homed.Project, homed.Channel)
	}
}

// A run a client's own connector raises acts for the person its team maps
// the Slack user to, so it is held to what that person may do where the
// binding files it, as the API holds a surface asking for a person: their
// own runs:write, and being a member of the project. A bound channel is the
// project's, so someone the team maps to nobody is not answered there; in
// an unbound channel they are, for nobody, as before.
func TestAConnectorsRunIsHeldToThePersonItActsFor(t *testing.T) {
	f := &fakes{}
	c := &cast{says: map[string][][]saigetypes.Delta{"docs": {say("Docs here."), say("Again.")}}}
	cfg := f.config()
	cfg.Policy = operators(t, "ada", "olu")
	said := &logs{}
	cfg.Log = slog.New(slog.NewTextHandler(said, nil))
	p := stage(t, c, cfg)
	if _, err := p.CreateProject(t.Context(), tenant, ada, platform.ProjectInput{Name: "Vault", Kind: projects.Account}); err != nil {
		t.Fatal(err)
	}
	create(t, p, tenant, slackClient("docs-app", "A0DOCS", team, "docs"))
	mapper := slackClient("api-app", "A0API", team, "docs")
	mapper.Slack.Machine, mapper.Slack.People = "svc-slack", map[string]string{"U0ADA": "ada", "U0OLU": "olu", "U0VIC": "vic"}
	create(t, p, tenant, mapper)
	if _, err := p.CreateChannel(t.Context(), tenant, "Vault", ada, platform.ChannelInput{Name: "deals", Default: "docs", Slack: &projects.SlackBinding{Client: "docs-app", Channel: "C0VAULT"}}); err != nil {
		t.Fatal(err)
	}
	ingress := p.IngressHandler()
	mention := func(event, user, channel, ts string) {
		t.Helper()
		body := strings.ReplaceAll(mentionOf("A0DOCS", team, event, channel, ts), "U0HUMAN1", user)
		if code := signedSlack(ingress, "secret-docs-app", body); code != http.StatusOK {
			t.Fatalf("the mention answered %d", code)
		}
	}
	// olu may start runs but is not a member of Vault; vic may start none;
	// U0NOBODY is mapped to nobody, in a channel bound to Vault.
	mention("Ev1", "U0OLU", "C0VAULT", "1700000000.000100")
	mention("Ev2", "U0VIC", "C09", "1700000000.000200")
	mention("Ev3", "U0NOBODY", "C0VAULT", "1700000000.000300")
	// Ada is Vault's owner; U0NOBODY in a channel that is not bound is
	// answered for nobody, in the client's home.
	mention("Ev4", "U0ADA", "C0VAULT", "1700000000.000400")
	mention("Ev5", "U0NOBODY", "C09", "1700000000.000500")
	var got []runs.Run
	eventually(t, "three mentions were refused and two answered", func() bool {
		got = p.Runs.List(tenant, 20)
		return strings.Count(said.String(), "a client's message was refused") == 3 && len(got) >= 2
	})
	who := map[string]string{}
	for _, r := range got {
		who[r.Tag().Meta["slack_channel"]+" "+r.Actor.ID] = r.Project + " for " + cmp.Or(r.Actor.User, "nobody")
	}
	want := map[string]string{"C0VAULT U0ADA": "Vault for ada", "C09 U0NOBODY": " for nobody"}
	if len(got) != 2 || who["C0VAULT U0ADA"] != want["C0VAULT U0ADA"] || who["C09 U0NOBODY"] != want["C09 U0NOBODY"] {
		t.Fatalf("the connector's runs are %v, want %v", who, want)
	}
}

// A client's machine hears of a mention of a person it maps, and for whom,
// reads it for them, and settles it for them: a reply posts in the thread
// as them, through the client. Another machine hears nothing.
func TestAMachineHearsOfAMentionOfAPersonItMapsAndSettlesItForThem(t *testing.T) {
	p, machine := slackRoom(t, nil)
	got := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "@ana can you check the price?"})
	m, ok := p.Inbox.ByKey(tenant, inbox.MentionKey(got.Thread.ID, "m-1", "ana"))
	if !ok {
		t.Fatal("ana was not told")
	}
	notice := inbox.Notice{ID: m.ID, Kind: inbox.KindMention, State: inbox.Open}
	if p.InboxNoticeFor(machine, notice) {
		t.Fatal("the machine reads ana's mention as its own")
	}
	heard := p.SurfaceNoticesFor(machine, notice)
	if len(heard) != 1 || heard[0] != (platform.ActAs{Client: "app", User: "U0ANA"}) {
		t.Fatalf("the machine hears of it for %+v", heard)
	}
	as := heard[0]
	other := auth.Principal{Subject: "svc-other", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsRead, auth.RunsWrite}}.WithPolicy(machine.Policy())
	if heard := p.SurfaceNoticesFor(other, notice); len(heard) != 0 {
		t.Fatalf("another machine hears of ana's mention for %+v", heard)
	}
	done, err := p.InboxResolve(t.Context(), machine, m.ID, m.Parts[0].ID, platform.Resolution{Action: platform.ActReply, Text: "On it.", ActAs: &as})
	if err != nil {
		t.Fatal(err)
	}
	if done.State != inbox.Resolved || done.Parts[0].Outcome.By != "ana" {
		t.Fatalf("settled as %+v", done.Parts[0].Outcome)
	}
	reply, err := p.Threads.Message(tenant, got.Thread.ID, "m-2")
	if err != nil || reply.By != "ana" || reply.Via != "app" || reply.Prev != "m-1" {
		t.Fatalf("the reply is %+v, %v", reply, err)
	}
}

// A person who replies in the thread, from anywhere, has answered being
// mentioned there.
func TestAReplyInTheThreadSettlesTheMention(t *testing.T) {
	p, _ := slackRoom(t, nil)
	got := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "@ana can you check the price?"})
	ana := auth.Principal{Subject: "ana", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	if _, err := p.PostMessage(t.Context(), ana, got.Thread.ID, platform.MessageInput{Text: "Checked: it holds.", After: "m-1", PostOnly: true}); err != nil {
		t.Fatal(err)
	}
	m, _ := p.Inbox.ByKey(tenant, inbox.MentionKey(got.Thread.ID, "m-1", "ana"))
	if m.State != inbox.Resolved || m.Parts[0].Outcome == nil || m.Parts[0].Outcome.Action != platform.ActReply {
		t.Fatalf("after ana replied, her mention is %s, %+v", m.State, m.Parts[0].Outcome)
	}
}

// Stopping a run takes whoever asked for it, the machine of the client that
// raised it, whose surface shows the stop control, or a member where it
// lives. A viewer of the project stops nobody else's run.
func TestAViewerStopsOnlyWhatTheyAskedFor(t *testing.T) {
	p, machine := slackRoom(t, nil)
	asked := runs.Run{ID: "r-1", Tenant: tenant, Project: "Globex", Actor: envelope.Actor{ID: "ana"}, Client: "app"}
	viewer := func(subject string) auth.Principal {
		return auth.Principal{Subject: subject, Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	}
	for _, tc := range []struct {
		who  auth.Principal
		run  runs.Run
		want bool
	}{
		{viewer("ana"), asked, true},
		{viewer("vic"), asked, false},
		{machine, asked, true},
		{machine, runs.Run{ID: "r-2", Tenant: tenant, Project: "Globex", Actor: envelope.Actor{ID: "ana"}}, false},
		{auth.Principal{Subject: "ada", Tenant: tenant, Kind: auth.User}, asked, true},
		// An agent called the run; a person who shares its name asked nothing.
		{viewer("lead"), runs.Run{ID: "r-3", Tenant: tenant, Project: "Globex", Actor: envelope.Actor{ID: "lead", Bot: true}}, false},
	} {
		err := p.MayStop(tc.who, tc.run)
		if (err == nil) != tc.want {
			t.Errorf("%s stopping %s asked by %s = %v, want allowed %v", tc.who.Subject, tc.run.ID, tc.run.Actor.ID, err, tc.want)
		}
	}
}

// A reply to a mention is a human response, so the reply a Slack person
// sends from the modal in their direct messages, through their inbox, posts
// in the thread as them and starts no run, though the thread has a default.
// Replying asks only what being mentioned does: a viewer who was tagged
// replies too.
func TestASlackReplyToAMentionStartsNoRun(t *testing.T) {
	p, machine := slackRoom(t, nil)
	got := startThread(t, p, admin, platform.ThreadInput{Project: "Globex", Default: "helper", Text: "@ana can you check the price?", PostOnly: true})
	if got.Thread.Default != "helper" || len(got.Runs) != 0 {
		t.Fatalf("the thread is %+v and started %+v", got.Thread, got.Runs)
	}
	ana, err := p.Acting(machine, platform.ActAs{Client: "app", User: "U0ANA"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.MayPost(ana, platform.Place{Project: "Globex"}); err == nil {
		t.Fatal("ana is a member of Globex; the test needs a viewer")
	}
	mention, ok := p.Inbox.ByKey(tenant, inbox.MentionKey(got.Thread.ID, "m-1", "ana"))
	if !ok {
		t.Fatal("ana was not told of the mention")
	}
	if _, err := p.InboxResolve(t.Context(), machine, mention.ID, mention.Parts[0].ID, platform.Resolution{
		Action: platform.ActReply, Text: "Checked: it holds.", ActAs: &platform.ActAs{Client: "app", User: "U0ANA"},
	}); err != nil {
		t.Fatal(err)
	}
	reply, err := p.Threads.Message(tenant, got.Thread.ID, "m-2")
	if err != nil || reply.By != "ana" || reply.Via != "app" || reply.Prev != "m-1" {
		t.Fatalf("the reply is %+v, %v", reply, err)
	}
	if turns, err := p.Runs.Conversation(tenant, got.Thread.ID); !errors.Is(err, runs.ErrNotFound) || len(turns) != 0 {
		t.Fatalf("the reply started %+v, %v", turns, err)
	}
	if m, _ := p.Inbox.ByKey(tenant, inbox.MentionKey(got.Thread.ID, "m-1", "ana")); m.State != inbox.Resolved {
		t.Fatalf("after the reply the mention is %s", m.State)
	}
}

// Experts named by a role are sent a mention as one message to the role, so
// a client's machine hears of it for every person it maps who holds the
// role, to tell each of them in Slack, and for nobody else.
func TestAMachineHearsOfAnExpertQuestionForEveryoneItMapsInTheRole(t *testing.T) {
	p, slackMachine := slackRoom(t, func(in *platform.ClientInput) {
		in.Slack.People["U0OLU"] = "olu"
	})
	seePeople(t, p, auth.Principal{Subject: "olu"})
	pol, err := auth.ParsePolicy("test", `
role viewer { allow read on * }
role operator extends viewer { allow write on runs }
role security {}
grant operator to subject "ana"
grant viewer, security to subject "vic"
grant viewer, security to subject "olu"
`)
	if err != nil {
		t.Fatal(err)
	}
	machine := auth.Principal{Subject: slackMachine.Subject, Tenant: tenant, Kind: auth.Machine, Scopes: slackMachine.Scopes}.WithPolicy(pol)
	cur, err := p.ProjectExperts(tenant, "Field")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetProjectExperts(tenant, "Field", ada, platform.Experts{Version: cur.Version, Experts: []projects.Expert{{Who: "role:security", Topics: []string{"security"}}}}); err != nil {
		t.Fatal(err)
	}
	got := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "@sme:security is this safe to ship?"})
	m, ok := p.Inbox.ByKey(tenant, inbox.MentionKey(got.Thread.ID, "m-1", "role:security"))
	if !ok {
		t.Fatalf("the experts were not told: %+v", got)
	}
	heard := p.SurfaceNoticesFor(machine, inbox.Notice{ID: m.ID, Kind: inbox.KindMention, State: inbox.Open})
	want := []platform.ActAs{{Client: "app", User: "U0OLU"}, {Client: "app", User: "U0VIC"}}
	if !slices.Equal(heard, want) {
		t.Fatalf("the machine hears of the experts' question for %+v, want %+v", heard, want)
	}
}

// On a client's surface an untagged message is answered by the default the
// thread inherits, here the client's, outside a bound channel, acting for
// the person it maps. A default that has stopped being available is not
// passed over: nothing answers, and the answer says why. A message a person
// posts in the console in the same thread starts nothing.
func TestOnASurfaceTheDefaultAnswersAndFailsClosed(t *testing.T) {
	p, machine := slackRoom(t, nil)
	open, err := p.StartThread(t.Context(), machine, fromSlack("C09OPEN", "1700000000.000200", "", "anyone?"))
	if err != nil {
		t.Fatal(err)
	}
	if open.Rule != threads.RuleDefault || len(open.Runs) != 1 || open.Runs[0].Agent != "helper" || open.Runs[0].Actor.User != "ana" {
		t.Fatalf("an untagged message from Slack started %+v (%s)", open.Runs, open.Rule)
	}
	console := post(t, p, auth.Principal{Subject: "ana", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}, open.Thread.ID, platform.MessageInput{Text: "and here?"})
	if console.Rule != threads.RuleUntagged || len(console.Runs) != 0 {
		t.Fatalf("an untagged message from the console started %+v (%s)", console.Runs, console.Rule)
	}
	setAgents(t, p, "Initech", platform.AgentsInput{Enable: []string{}, Allow: []string{"lead"}})
	stale, err := p.StartThread(t.Context(), machine, fromSlack("C09OPEN", "1700000000.000200", "1700000000.000300", "and now?"))
	if err != nil {
		t.Fatal(err)
	}
	if stale.Rule != threads.RuleDefault || len(stale.Runs) != 0 || !strings.Contains(stale.Reason, "@helper answers untagged messages here, but it is not available here") {
		t.Fatalf("a stale default started %+v", stale)
	}
}
