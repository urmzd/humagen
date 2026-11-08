package platform_test

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/threads"
)

func slackSession(channel, ts, message, text string) platform.SessionInput {
	return platform.SessionInput{
		Text: text, ActAs: &platform.ActAs{Client: "app", User: "U0ANA"},
		Surface: &platform.SurfaceRef{Client: "app", Channel: channel, TS: ts, Message: message, AppUser: "UAPP"},
	}
}

func TestASlackThreadHasOneRootSessionFoundByItsPlace(t *testing.T) {
	p, machine := slackRoom(t, nil)
	first, started, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession("C01RENEW", "1700000000.000100", "", "<@UAPP> what changed in the renewal?"), platform.ChannelPublic))
	if err != nil || !started {
		t.Fatalf("first message = %v, started %v", err, started)
	}
	switch {
	case first.Parent != "slack://T0TEAM/C01RENEW/1700000000.000100":
		t.Fatalf("the root's place = %q", first.Parent)
	case first.Agent != "helper":
		t.Fatalf("without an entry agent the client's default owns the root, got %q", first.Agent)
	case first.Project != "Field" || first.Thread.Channel != "renewals" || first.Thread.Visibility != threads.Shared:
		t.Fatalf("filed at %s #%s, %s", first.Project, first.Thread.Channel, first.Thread.Visibility)
	}
	if len(first.Messages) == 0 || first.Messages[0].Message == nil || first.Messages[0].Message.By != "ana" || first.Messages[0].Message.Text != "what changed in the renewal?" {
		t.Fatalf("the first message = %+v", first.Messages)
	}
	again, started, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession("C01RENEW", "1700000000.000100", "", "<@UAPP> what changed in the renewal?"), platform.ChannelPublic))
	if err != nil || started || again.ID != first.ID || len(again.Messages) != len(first.Messages) {
		t.Fatalf("the same message sent again = %+v, started %v, %v", again.Messages, started, err)
	}
	reply, started, err := p.StartSurfaceSession(t.Context(), machine, slackSession("C01RENEW", "1700000000.000100", "1700000000.000300", "<@UAPP> @lead plan it"))
	if err != nil || started || reply.ID != first.ID {
		t.Fatalf("a reply in the thread = %v, started %v, id %s", err, started, reply.ID)
	}
	if len(reply.Children) != 1 || reply.Children[0].Agent != "lead" {
		t.Fatalf("a mention in the thread did not start its agent's child: %+v", reply.Children)
	}
	other, started, err := p.StartSurfaceSession(t.Context(), machine, slackSession("C01RENEW", "1700000000.000900", "", "<@UAPP> another question"))
	if err != nil || !started || other.ID == first.ID {
		t.Fatalf("another Slack thread = %s, started %v, %v", other.ID, started, err)
	}
}

func TestOnlyTheClientsMachineStartsASurfaceSessionAndNeverSaysWhere(t *testing.T) {
	p, machine := slackRoom(t, nil)
	stranger := auth.Principal{Subject: "svc-other", Tenant: tenant, Kind: auth.Machine, Scopes: machine.Scopes}
	if _, _, err := p.StartSurfaceSession(t.Context(), stranger, slackSession("C01RENEW", "1700000000.000100", "", "hello")); err == nil {
		t.Fatal("another machine posted from the client's surface")
	}
	noPerson := slackSession("C01RENEW", "1700000000.000100", "", "hello")
	noPerson.ActAs = nil
	if _, _, err := p.StartSurfaceSession(t.Context(), machine, noPerson); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("a message for nobody = %v", err)
	}
	placed := slackSession("C01RENEW", "1700000000.000100", "", "hello")
	placed.Project = "Initech"
	if _, _, err := p.StartSurfaceSession(t.Context(), machine, placed); !errors.Is(err, platform.ErrSurfacePlace) {
		t.Fatalf("a message saying where to file it = %v", err)
	}
	owned := slackSession("C01RENEW", "1700000000.000100", "", "hello")
	owned.Agent = "lead"
	if _, _, err := p.StartSurfaceSession(t.Context(), machine, owned); !errors.Is(err, platform.ErrSurfacePlace) {
		t.Fatalf("a message naming the root's owner = %v", err)
	}
}

func TestAThreadThatStartedAsAConversationStaysOne(t *testing.T) {
	p, machine := slackRoom(t, nil)
	if _, err := p.StartThread(t.Context(), machine, fromSlack("C01RENEW", "1700000000.000100", "", "<@UAPP> @lead plan the renewal")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.StartSurfaceSession(t.Context(), machine, slackSession("C01RENEW", "1700000000.000100", "1700000000.000200", "<@UAPP> and the invoice")); !errors.Is(err, platform.ErrSurfaceThreadBeforeSessions) {
		t.Fatalf("a reply in a conversation thread = %v", err)
	}
}

func TestTheEntryAgentOwnsASlackThreadsRoot(t *testing.T) {
	p := withEntry(t, platform.Config{})
	field(t, p)
	seePeople(t, p, auth.Principal{Subject: "ana"})
	in := slackClient("app", "A0APP", "T0TEAM", "helper")
	in.Agents = []string{"*"}
	in.Slack.Machine, in.Slack.People = "svc-slack", map[string]string{"U0ANA": "ana"}
	in.Home = &clients.Home{Project: "Initech"}
	create(t, p, tenant, in)
	pol, err := auth.ParsePolicy("test", "role viewer { allow read on * }\nrole operator extends viewer { allow write on runs }\ngrant operator to subject \"ana\"\n")
	if err != nil {
		t.Fatal(err)
	}
	machine := auth.Principal{Subject: "svc-slack", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsRead, auth.RunsWrite}}.WithPolicy(pol)
	v, started, err := p.StartSurfaceSession(t.Context(), machine, slackSession("D0ANA", "1700000000.000100", "", "what changed?"))
	if err != nil || !started {
		t.Fatalf("a direct message = %v, started %v", err, started)
	}
	if v.Agent != platform.BuiltinEntry || v.Parent != "slack://T0TEAM/D0ANA/1700000000.000100" || v.Thread.Visibility != threads.Private {
		t.Fatalf("root = agent %q, place %q, %s", v.Agent, v.Parent, v.Thread.Visibility)
	}
}

func withAudience(in platform.SessionInput, typ string, members ...string) platform.SessionInput {
	in.Surface.Audience = &platform.SurfaceAudience{Type: typ, Members: members}
	return in
}

func TestASlackRootIsReadByThePlacesAudience(t *testing.T) {
	p, machine := slackRoom(t, nil)
	ana := auth.Principal{Subject: "ana", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
	vic := auth.Principal{Subject: "vic", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}}

	public, _, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession("C01RENEW", "1700000000.000100", "", "<@UAPP> status?"), platform.ChannelPublic))
	if err != nil || public.Thread.Visibility != threads.Shared {
		t.Fatalf("a public channel's root = %s, %v", public.Thread.Visibility, err)
	}
	if a, ok := p.SessionAudience(tenant, public.ID); !ok || a.Kind != "public" {
		t.Fatalf("public audience = %+v, %v", a, ok)
	}

	private, _, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession("G01SECRET", "1700000000.000200", "", "<@UAPP> status?"), platform.ChannelPrivate, "U0ANA", "U0EVE"))
	if err != nil || private.Thread.Visibility != threads.Private {
		t.Fatalf("a private channel's root = %s, %v", private.Thread.Visibility, err)
	}
	if !p.SeesSession(ana, private.ID) || p.SeesSession(vic, private.ID) {
		t.Fatal("a private channel's root is read by someone outside it, or not by its member")
	}
	if a, _ := p.SessionAudience(tenant, private.ID); a.Kind != "private" || a.Unmapped != 1 || len(a.Members) != 1 || a.Members[0] != "ana" {
		t.Fatalf("private audience = %+v, want ana and one unmapped member", a)
	}
	// vic joins the channel: the next message brings them in.
	if _, _, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession("G01SECRET", "1700000000.000200", "1700000000.000300", "<@UAPP> and now?"), platform.ChannelPrivate, "U0ANA", "U0VIC")); err != nil {
		t.Fatal(err)
	}
	if !p.SeesSession(vic, private.ID) {
		t.Fatal("a member who joined the channel cannot read its root")
	}

	dm, _, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession("D0ANA", "1700000000.000400", "", "my question"), platform.ChannelDM))
	if err != nil || dm.Thread.Visibility != threads.Private || p.SeesSession(vic, dm.ID) || !p.SeesSession(ana, dm.ID) {
		t.Fatalf("a direct message's root = %s, %v", dm.Thread.Visibility, err)
	}
	if _, _, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession("C01RENEW", "1700000000.000500", "", "x"), "town_square")); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("an unknown audience type = %v", err)
	}
}

func TestASurfaceMessageSaysWhatItStartedAndStartsNothingTwice(t *testing.T) {
	p, machine := slackRoom(t, nil)
	first, _, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession("C01RENEW", "1700000000.000100", "", "<@UAPP> @lead plan it"), platform.ChannelPublic))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Posted.Runs) != 1 || first.Posted.Runs[0].Agent != "lead" || first.Posted.Message.Text != "@lead plan it" || first.Posted.Repeated {
		t.Fatalf("posted = %+v", first.Posted)
	}
	again, _, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession("C01RENEW", "1700000000.000100", "", "<@UAPP> @lead plan it"), platform.ChannelPublic))
	if err != nil || !again.Posted.Repeated || len(again.Children) != 1 {
		t.Fatalf("sent again = %+v, children %d, %v", again.Posted, len(again.Children), err)
	}
	quiet := slackSession("C01RENEW", "1700000000.000100", "1700000000.000600", "just noting this")
	quiet.PostOnly = true
	got, _, err := p.StartSurfaceSession(t.Context(), machine, quiet)
	if err != nil || len(got.Posted.Runs) != 0 || got.Posted.Message.ID == "" {
		t.Fatalf("a post-only message = %+v, %v", got.Posted, err)
	}
}

func TestAChannelWithNoReportedAudienceIsPrivateToTheAsker(t *testing.T) {
	p, machine := slackRoom(t, nil)
	v, _, err := p.StartSurfaceSession(t.Context(), machine, slackSession("C01RENEW", "1700000000.000700", "", "<@UAPP> status?"))
	if err != nil {
		t.Fatal(err)
	}
	vic := auth.Principal{Subject: "vic", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
	if v.Thread.Visibility != threads.Private || p.SeesSession(vic, v.ID) {
		t.Fatalf("a channel with no reported audience = %s, read by another member: %v", v.Thread.Visibility, p.SeesSession(vic, v.ID))
	}
	if a, _ := p.SessionAudience(tenant, v.ID); a.Kind != "private" || len(a.Members) != 1 || a.Members[0] != "ana" {
		t.Fatalf("audience = %+v, want private to ana", a)
	}
}
