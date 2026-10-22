package platform_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/threads"
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
	p := stage(t, c, f.config())
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
	as, mapped := p.SurfaceNoticeFor(machine, notice)
	if !mapped || as != (platform.ActAs{Client: "app", User: "U0ANA"}) {
		t.Fatalf("the machine hears of it for %+v, %v", as, mapped)
	}
	other := auth.Principal{Subject: "svc-other", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsRead, auth.RunsWrite}}.WithPolicy(machine.Policy())
	if _, heard := p.SurfaceNoticeFor(other, notice); heard {
		t.Fatal("another machine hears of ana's mention")
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
