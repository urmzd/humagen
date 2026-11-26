package platform_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/runs"
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
	// A message that would start work while the root's agent works is a
	// follow-up, so the first answer ends before the mention is posted.
	await(t, p, sessionRun(t, first).ID, runs.Completed)
	reply, started, err := p.StartSurfaceSession(t.Context(), machine, slackSession("C01RENEW", "1700000000.000100", "1700000000.000300", "<@UAPP> @lead plan it"))
	if err != nil || started || reply.ID != first.ID {
		t.Fatalf("a reply in the thread = %v, started %v, id %s", err, started, reply.ID)
	}
	if len(reply.Children) != 0 || len(reply.Posted.Runs) != 1 || reply.Posted.Runs[0].Agent != "lead" || reply.Posted.Runs[0].Thread != first.ID {
		t.Fatalf("a mention in the thread: children %+v, runs %+v; want lead answering in the root", reply.Children, reply.Posted.Runs)
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

// A Slack thread a conversation started in before sessions, in a channel
// bound to nothing or in a direct message, is refused as a session with the
// one error that says so, for a mention and for a listened reply alike, and
// the same message posted as a conversation is answered in that thread.
func TestAThreadFromBeforeSessionsIsStillAnsweredAsAConversation(t *testing.T) {
	p, machine := slackRoomOn(t, startWith(t, platform.Config{Router: func(string) join.Asker {
		return join.AskerFunc(func(_ context.Context, c join.Candidate, _ string) (join.Decision, error) {
			return join.Decision{Agent: c.Name, Join: true, Reason: "it would take anything"}, nil
		})
	}}), nil)
	const root = "1700000000.000100"
	for _, tc := range []struct{ name, channel string }{
		{"a channel bound to nothing", "C0UNBOUND"},
		{"a direct message", "D0ANA"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, err := p.StartThread(t.Context(), machine, fromSlack(tc.channel, root, "", "<@UAPP> @lead plan it"))
			if err != nil || len(first.Runs) != 1 || first.Thread.Session != nil {
				t.Fatalf("the conversation's first message: %d runs, session %v, %v", len(first.Runs), first.Thread.Session, err)
			}
			settle(t, p, first.Runs[0].ID)
			both := func(ts, text string, listen bool) platform.Posted {
				t.Helper()
				in := slackSession(tc.channel, root, ts, text)
				in.Listen = listen
				if _, _, err := p.StartSurfaceSession(t.Context(), machine, in); !errors.Is(err, platform.ErrSurfaceThreadBeforeSessions) {
					t.Fatalf("as a session = %v, want ErrSurfaceThreadBeforeSessions", err)
				}
				again := fromSlack(tc.channel, root, ts, text)
				again.Listen = listen
				got, err := p.StartThread(t.Context(), machine, again)
				if err != nil || got.Thread.ID != first.Thread.ID || len(got.Runs) != 1 {
					t.Fatalf("as a conversation: thread %s (want %s), %d runs, %v", got.Thread.ID, first.Thread.ID, len(got.Runs), err)
				}
				settle(t, p, got.Runs[0].ID)
				return got
			}
			both("1700000000.000200", "<@UAPP> @lead and the invoice", false)
			if directMessage := tc.channel[0] == 'D'; directMessage {
				return // every message there is the app's: none is sent as heard
			}
			awaitListener(t, p, first.Thread.ID, "ana")
			both("1700000000.000300", "and the terms?", true)
		})
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
	if err != nil || !again.Posted.Repeated || len(again.Children) != 0 || len(again.Posted.Runs) != 1 || again.Posted.Runs[0].ID != first.Posted.Runs[0].ID {
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

// machineSays is one message from the client's machine for a person, as the
// Slack app sends it to the messages route.
func machineSays(channel, ts, thread, text string) platform.MessagesInput {
	in := sayIn(text, slackRef(channel, ts, thread))
	in.Ref.AppUser, in.Ref.Audience = "UAPP", &platform.SurfaceAudience{Type: platform.ChannelPublic}
	in.ActAs = &platform.ActAs{Client: "app", User: "U0ANA"}
	return in
}

func TestTheMessagesRouteStartsASlackThreadsSessionOnItsFirstMessage(t *testing.T) {
	p, machine := slackRoom(t, nil)
	first, err := p.SendMessages(t.Context(), machine, machineSays("C01RENEW", "1700000000.000100", "", "<@UAPP> what changed in the renewal?"))
	if err != nil || !first.Created || first.Root == "" || first.ID == "" || len(first.Posted.Runs) != 1 {
		t.Fatalf("the thread's first message = %+v, %v", first, err)
	}
	if first.Posted.Message.ID != first.ID || !strings.HasPrefix(first.ID, first.Root+":") {
		t.Fatalf("the message's id = %q, and %q in what it did; want one id, under the root", first.ID, first.Posted.Message.ID)
	}
	root, err := p.ReadSession(t.Context(), machine, first.Root, "", "", false)
	if err != nil || root.Parent != "slack://T0TEAM/C01RENEW/1700000000.000100" {
		t.Fatalf("the root's place = %q, %v", root.Parent, err)
	}
	if run := first.Posted.Runs[0]; run.Thread != first.Root || run.Actor.User != "ana" {
		t.Fatalf("the run answers in %q for %q; want the root, for ana", run.Thread, run.Actor.User)
	}
	await(t, p, first.Posted.Runs[0].ID, runs.Completed)
	// The same message again starts nothing, and a reply lands in the same root.
	again, err := p.SendMessages(t.Context(), machine, machineSays("C01RENEW", "1700000000.000100", "", "<@UAPP> what changed in the renewal?"))
	if err != nil || again.Created || again.Root != first.Root || !again.Posted.Repeated {
		t.Fatalf("the same message again = %+v, %v", again, err)
	}
	reply, err := p.SendMessages(t.Context(), machine, machineSays("C01RENEW", "1700000000.000300", "1700000000.000100", "<@UAPP> and the price?"))
	if err != nil || reply.Created || reply.Root != first.Root || len(reply.Posted.Runs) != 1 {
		t.Fatalf("a reply in the thread = %+v, %v", reply, err)
	}
	quiet := machineSays("C01RENEW", "1700000000.000200", "1700000000.000100", "said before the app was asked")
	quiet.PostOnly = true
	if got, err := p.SendMessages(t.Context(), machine, quiet); err != nil || got.Root != first.Root || len(got.Posted.Runs) != 0 {
		t.Fatalf("a message posted only = %+v, %v", got, err)
	}
}

func TestOnlyTheClientsMachinePostsForAPersonOnTheMessagesRoute(t *testing.T) {
	p, machine := slackRoom(t, nil)
	stranger := auth.Principal{Subject: "svc-other", Tenant: tenant, Kind: auth.Machine, Scopes: machine.Scopes}
	if _, err := p.SendMessages(t.Context(), stranger, machineSays("C01RENEW", "1700000000.000100", "", "hello")); err == nil {
		t.Fatal("another machine posted for a person of the client")
	}
	for name, change := range map[string]func(*platform.MessagesInput){
		"no ref":       func(in *platform.MessagesInput) { in.Ref = nil },
		"no client":    func(in *platform.MessagesInput) { in.Ref.Client = "" },
		"another team": func(in *platform.MessagesInput) { in.Ref.Team = "T0OTHER" },
		"a github ref": func(in *platform.MessagesInput) {
			in.Ref = &platform.ExternalRef{Type: platform.RefGitHub, Repo: "acme/web", Kind: "pull", Number: 9}
		},
		"two messages": func(in *platform.MessagesInput) { in.Messages = append(in.Messages, in.Messages[0]) },
		"a reply with no ts of its own": func(in *platform.MessagesInput) {
			in.Ref.ThreadTS, in.Ref.TS = "1700000000.000100", ""
		},
		"a ref of its own": func(in *platform.MessagesInput) { in.Messages[0].Ref = slackRef("C01RENEW", "1700000000.000100", "") },
	} {
		in := machineSays("C01RENEW", "1700000000.000100", "", "hello")
		change(&in)
		if _, err := p.SendMessages(t.Context(), machine, in); !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("%s = %v", name, err)
		}
	}
	only := sayIn("hello", slackRef("C01RENEW", "1700000000.000100", ""))
	only.PostOnly = true
	if _, err := p.SendMessages(t.Context(), machine, only); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("post_only with nobody to post for = %v", err)
	}
}

// The messages route listens as the threads route does: a session a
// thread's first message started is listened to, and a message there that
// addresses nobody is answered or kept; one a later reply started is not.
func TestTheMessagesRouteListensInASessionAThreadsFirstMessageStarted(t *testing.T) {
	p, machine := slackRoom(t, nil)
	first, err := p.SendMessages(t.Context(), machine, machineSays("C01RENEW", "1700000000.000100", "", "<@UAPP> what changed in the renewal?"))
	if err != nil || !first.Created {
		t.Fatalf("the thread's first message = %+v, %v", first, err)
	}
	// It is listened to once the app has answered there.
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		root, err := p.ReadSession(t.Context(), machine, first.Root, "", "", false)
		if err == nil && root.Thread.Addressed && root.Thread.Listening {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a session a thread's first message started: addressed %v, listening %v, %v", root.Thread.Addressed, root.Thread.Listening, err)
		}
	}
	heard := machineSays("C01RENEW", "1700000000.000200", "1700000000.000100", "and what about the price?")
	heard.Listen = true
	got, err := p.SendMessages(t.Context(), machine, heard)
	if err != nil || got.Root != first.Root || got.Posted.Message.ID == "" {
		t.Fatalf("a message that addresses nobody in a listened session = %+v, %v", got, err)
	}

	later, err := p.SendMessages(t.Context(), machine, machineSays("C01RENEW", "1700000000.000600", "1700000000.000500", "<@UAPP> can you summarise this thread?"))
	if err != nil || !later.Created {
		t.Fatalf("a mention in a later reply = %+v, %v", later, err)
	}
	if s, err := p.ReadSession(t.Context(), machine, later.Root, "", "", false); err != nil || s.Thread.Listening {
		t.Fatalf("a session a later reply started: listening %v, %v", s.Thread.Listening, err)
	}
	unheard := machineSays("C01RENEW", "1700000000.000700", "1700000000.000500", "thanks, and the other one?")
	unheard.Listen = true
	if _, err := p.SendMessages(t.Context(), machine, unheard); !errors.Is(err, platform.ErrNotListening) {
		t.Fatalf("a message that addresses nobody where nobody listens: %v", err)
	}
	nowhere := machineSays("C01RENEW", "1700000000.000950", "1700000000.000900", "anyone around?")
	nowhere.Listen = true
	if _, err := p.SendMessages(t.Context(), machine, nowhere); !errors.Is(err, platform.ErrNotListening) {
		t.Fatalf("a message that addresses nobody where there is no session: %v", err)
	}
}
