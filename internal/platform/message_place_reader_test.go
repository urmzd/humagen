package platform_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/journal"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
)

const (
	placeChannel = "G01SECRET"
	placeThread  = "1700000000.000200"
)

// placeRoom is a Slack workspace whose app maps ana, ben and vic, all
// members of the project the app files in, with a private channel's root
// session that ana started in front of ben. It returns the store the
// platform keeps its records in, the app's machine and the root's id.
func placeRoom(t *testing.T) (*platform.Platform, state.Store, auth.Principal, string) {
	t.Helper()
	st := state.NewMemory()
	p, machine := slackRoomOn(t, startWith(t, platform.Config{Backends: platform.Backends{State: st}}), func(in *platform.ClientInput) {
		in.Slack.People["U0BEN"] = "ben"
	})
	seePeople(t, p, auth.Principal{Subject: "ben"})
	workIn(t, p, "Initech", auth.Principal{Subject: "ana"}, auth.Principal{Subject: "ben"}, auth.Principal{Subject: "vic"})
	first, started, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession(placeChannel, placeThread, "", "<@UAPP> status?"), platform.ChannelPrivate, "U0ANA", "U0BEN"))
	if err != nil || !started || first.Thread.Visibility != threads.Private {
		t.Fatalf("the private channel's root = %s, started %v, %v", first.Thread.Visibility, started, err)
	}
	await(t, p, sessionRun(t, first).ID, runs.Completed)
	return p, st, machine, first.ID
}

func placeOperator(subject, tenantName string) auth.Principal {
	return auth.Principal{Subject: subject, Tenant: tenantName, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
}

// placeRef names the private channel's thread as a request body would, with
// a client and an audience of the members given.
func placeRef(ts string, members ...string) *platform.ExternalRef {
	return &platform.ExternalRef{Type: platform.RefSlack, Client: "app", Team: "T0TEAM", Channel: placeChannel, TS: ts, ThreadTS: placeThread, Audience: &platform.SurfaceAudience{Type: platform.ChannelPrivate, Members: members}}
}

// placeKept is everything a root keeps that a message to its place could change.
type placeKept struct {
	Version  int
	People   []string
	Audience platform.SessionAudience
	Messages int
	Events   int
	Delivery bool
}

func keptOf(t *testing.T, p *platform.Platform, st state.Store, id string) placeKept {
	t.Helper()
	th, err := p.Threads.Get(tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	k := placeKept{Version: th.Version}
	for _, pt := range th.Participants {
		if pt.Kind == threads.Person {
			k.People = append(k.People, pt.Name)
		}
	}
	slices.Sort(k.People)
	k.Audience, _ = p.SessionAudience(tenant, id)
	msgs, _, err := p.Threads.Messages(tenant, id, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	k.Messages = len(msgs)
	events, err := p.Journal.Query(journal.Query{Tenant: tenant, Limit: journal.MaxLimit})
	if err != nil {
		t.Fatal(err)
	}
	k.Events = len(events)
	_, err = state.NewTable[json.RawMessage](st, "root-deliveries").Lookup(tenant, id)
	k.Delivery = err == nil
	return k
}

func sameKept(a, b placeKept) bool {
	return a.Version == b.Version && slices.Equal(a.People, b.People) && a.Audience.Kind == b.Audience.Kind && slices.Equal(a.Audience.Members, b.Audience.Members) && a.Audience.At.Equal(b.Audience.At) && a.Messages == b.Messages && a.Events == b.Events && a.Delivery == b.Delivery
}

func TestAMessageToAPlaceIsTakenOnlyFromSomeoneWhoReadsItsRoot(t *testing.T) {
	p, st, _, root := placeRoom(t)
	vic := placeOperator("vic", tenant)
	before := keptOf(t, p, st, root)
	if !slices.Equal(before.People, []string{"ana", "ben"}) || before.Delivery {
		t.Fatalf("the root starts with %+v", before)
	}
	out, err := p.SendMessages(t.Context(), vic, sayIn("@helper what was said here?", placeRef("1700000000.000900", "U0VIC")))
	if !errors.Is(err, threads.ErrNotFound) || out.Root != "" {
		t.Fatalf("a message from someone who does not read the root = %+v, %v; want not found", out, err)
	}
	if after := keptOf(t, p, st, root); !sameKept(before, after) {
		t.Fatalf("the root kept %+v, want it as it was: %+v", after, before)
	}
	if p.SeesSession(vic, root) {
		t.Fatal("vic reads the root after a message that was not taken")
	}
	if _, err := p.ReadSession(t.Context(), vic, root, "", "", false); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("vic reading the root = %v, want not found", err)
	}
	// The same from under the root, by its id.
	if _, err := p.SendMessagesUnder(t.Context(), vic, root, sayIn("@helper and now?", placeRef("1700000000.000950", "U0VIC"))); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("a reply under the root from someone who does not read it = %v, want not found", err)
	}
	if after := keptOf(t, p, st, root); !sameKept(before, after) {
		t.Fatalf("the root kept %+v, want it as it was: %+v", after, before)
	}
}

func TestAParticipantsMessageToAPlaceLandsInItsRootAndLeavesItsPeople(t *testing.T) {
	p, st, _, root := placeRoom(t)
	ana := placeOperator("ana", tenant)
	before := keptOf(t, p, st, root)
	// The request names an audience without ben: a request's audience is not read.
	out, err := p.SendMessages(t.Context(), ana, sayIn("@helper and the cost?", placeRef("1700000000.000900", "U0ANA")))
	if err != nil || out.Created || out.Root != root {
		t.Fatalf("a participant's message = root %q (created %v), %v; want %s", out.Root, out.Created, err, root)
	}
	after := keptOf(t, p, st, root)
	if !slices.Equal(after.People, before.People) {
		t.Fatalf("the root's people = %v, want %v", after.People, before.People)
	}
	if after.Audience.Kind != before.Audience.Kind || !slices.Equal(after.Audience.Members, before.Audience.Members) || !after.Audience.At.Equal(before.Audience.At) {
		t.Fatalf("the root's audience = %+v, want %+v", after.Audience, before.Audience)
	}
	if after.Messages != before.Messages+1 {
		t.Fatalf("the root holds %d messages, want %d", after.Messages, before.Messages+1)
	}
	if !p.SeesSession(placeOperator("ben", tenant), root) {
		t.Fatal("ben no longer reads the root")
	}
}

func TestASurfacesMessageStillBringsThePlacesMembersToItsRoot(t *testing.T) {
	p, st, machine, root := placeRoom(t)
	vic := placeOperator("vic", tenant)
	// vic joined the channel and ben left it, as the app reports with ana's next message.
	in := withAudience(slackSession(placeChannel, placeThread, "1700000000.000300", "<@UAPP> and now?"), platform.ChannelPrivate, "U0ANA", "U0VIC")
	if v, started, err := p.StartSurfaceSession(t.Context(), machine, in); err != nil || started || v.ID != root {
		t.Fatalf("the app's message = %s (started %v), %v", v.ID, started, err)
	}
	after := keptOf(t, p, st, root)
	if !slices.Equal(after.People, []string{"ana", "vic"}) || !slices.Equal(after.Audience.Members, []string{"ana", "vic"}) {
		t.Fatalf("the root's people = %v, audience %v; want ana and vic", after.People, after.Audience.Members)
	}
	if !p.SeesSession(vic, root) || p.SeesSession(placeOperator("ben", tenant), root) {
		t.Fatal("the root is not read by the member who joined, or still by the one who left")
	}
	// A surface built into the server reports the members the same way.
	ref := placeRef("1700000000.000400", "U0ANA", "U0BEN")
	ref.User = "U0ANA"
	out, err := p.SendSurfaceMessages(t.Context(), placeOperator("ana", tenant), sayIn("@helper and the cost?", ref))
	if err != nil || out.Created || out.Root != root {
		t.Fatalf("a built-in surface's message = root %q (created %v), %v; want %s", out.Root, out.Created, err, root)
	}
	if after := keptOf(t, p, st, root); !slices.Equal(after.People, []string{"ana", "ben"}) || !after.Delivery {
		t.Fatalf("after the built-in surface's message the root keeps %+v", after)
	}
}

// The app posts for vic, who is not in the private channel's root, with the
// channel's members as the app reports them (ana and ben): vic does not post
// there, and the root keeps nothing.
func TestTheAppsMessageForSomeoneOutsideTheRootIsNotTaken(t *testing.T) {
	p, st, machine, root := placeRoom(t)
	before := keptOf(t, p, st, root)
	in := withAudience(slackSession(placeChannel, placeThread, "1700000000.000500", "<@UAPP> what was said?"), platform.ChannelPrivate, "U0ANA", "U0BEN")
	in.ActAs.User = "U0VIC"
	if v, _, err := p.StartSurfaceSession(t.Context(), machine, in); err == nil {
		t.Fatalf("a message for vic, who does not read the root, was taken: root %s", v.ID)
	}
	if after := keptOf(t, p, st, root); !sameKept(before, after) {
		t.Fatalf("the root kept %+v, want it as it was: %+v", after, before)
	}
	if p.SeesSession(placeOperator("vic", tenant), root) {
		t.Fatal("vic reads the root")
	}
}

// A listening message that is not taken keeps nothing, its audience included.
func TestAListeningMessageNotTakenKeepsNothing(t *testing.T) {
	p, st, machine, root := placeRoom(t)
	before := keptOf(t, p, st, root)
	// The root listens for ana, who asked first; vic's listening message,
	// which names him a member, is not hers.
	in := withAudience(slackSession(placeChannel, placeThread, "1700000000.000500", "just chatting"), platform.ChannelPrivate, "U0ANA", "U0VIC")
	in.ActAs.User = "U0VIC"
	in.Listen = true
	if _, _, err := p.StartSurfaceSession(t.Context(), machine, in); !errors.Is(err, platform.ErrNotListening) {
		t.Fatalf("a listening message in a root nobody asked to listen = %v, want not listening", err)
	}
	if after := keptOf(t, p, st, root); !sameKept(before, after) {
		t.Fatalf("the root kept %+v, want it as it was: %+v", after, before)
	}
}

// Through the API, vic names a private channel's thread before the app has
// posted there. Only the surface starts that place's root: vic's message is
// not found, and ana's first message there starts the root, which vic does
// not read.
func TestOnlyTheSurfaceStartsASurfacePlacesRoot(t *testing.T) {
	p, _, machine, _ := placeRoom(t)
	vic := placeOperator("vic", tenant)
	const next = "1700000000.000700"
	ref := placeRef(next, "U0VIC")
	ref.ThreadTS = ""
	if out, err := p.SendMessages(t.Context(), vic, sayIn("@helper hello", ref)); !errors.Is(err, threads.ErrNotFound) || out.Root != "" {
		t.Fatalf("vic's message to a place with no root = %+v, %v; want not found", out, err)
	}
	gh := &platform.ExternalRef{Type: platform.RefGitHub, Repo: "acme/web", Kind: "pull", Number: 7}
	if out, err := p.SendMessages(t.Context(), vic, sayIn("@helper hello", gh)); !errors.Is(err, threads.ErrNotFound) || out.Root != "" {
		t.Fatalf("vic's message to a pull request with no root = %+v, %v; want not found", out, err)
	}
	in := withAudience(slackSession(placeChannel, next, "", "<@UAPP> the plan is X"), platform.ChannelPrivate, "U0ANA", "U0BEN")
	v, started, err := p.StartSurfaceSession(t.Context(), machine, in)
	if err != nil || !started {
		t.Fatalf("ana's first message in the thread = started %v, %v; want started", started, err)
	}
	if p.SeesSession(vic, v.ID) {
		t.Fatalf("vic reads ana's private channel root %s", v.ID)
	}
	if !p.SeesSession(placeOperator("ben", tenant), v.ID) {
		t.Fatalf("ben does not read the root %s", v.ID)
	}
}

func TestAPlacesRootIsItsOwnTenants(t *testing.T) {
	p, st, _, root := placeRoom(t)
	before := keptOf(t, p, st, root)
	elsewhere := placeOperator("vic", "other")
	out, err := p.SendMessages(t.Context(), elsewhere, sayIn("@helper what was said here?", placeRef("1700000000.000900", "U0VIC")))
	if err == nil && out.Root == root {
		t.Fatalf("another tenant's message landed in the root: %+v", out)
	}
	if p.SeesSession(elsewhere, root) {
		t.Fatal("another tenant reads the root")
	}
	if _, err := p.SendMessagesUnder(t.Context(), elsewhere, root, sayIn("@helper and now?", nil)); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("another tenant's reply under the root = %v, want not found", err)
	}
	if after := keptOf(t, p, st, root); !sameKept(before, after) {
		t.Fatalf("the root kept %+v, want it as it was: %+v", after, before)
	}
}

// sameRoot is sameKept without the workspace's event count, for a root
// compared across work done elsewhere.
func sameRoot(a, b placeKept) bool {
	a.Events, b.Events = 0, 0
	return sameKept(a, b)
}

func TestAMessageNamingANewPlaceStartsTheCallersOwnRoot(t *testing.T) {
	p, st, _, _ := placeRoom(t)
	vic := placeOperator("vic", tenant)
	// The request names the app's client, an audience, the app's user and
	// files, on the request's ref and on the message's: none is read.
	ref := &platform.ExternalRef{Type: platform.RefSlack, Client: "app", Team: "T0TEAM", Channel: "G01OTHER", TS: "1700000001.000100", User: "U0ANA", AppUser: "UAPP", Files: true, Audience: &platform.SurfaceAudience{Type: platform.ChannelPrivate, Members: []string{"U0ANA", "U0BEN"}}}
	own := *ref
	in := sayIn("@helper hello", ref)
	in.Messages[0].Ref = &own
	out, err := p.SendMessages(t.Context(), vic, in)
	if err != nil || !out.Created {
		t.Fatalf("a message naming a new place = %+v, %v", out, err)
	}
	th, err := p.Threads.Get(tenant, out.Root)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := p.PersonalProject(vic)
	if err != nil {
		t.Fatal(err)
	}
	if th.Project != mine.Name || th.Visibility != threads.Private || th.Surface != nil {
		t.Fatalf("the root is filed in %q, %s, surface %v; want %q, private, none", th.Project, th.Visibility, th.Surface, mine.Name)
	}
	got := keptOf(t, p, st, out.Root)
	if !slices.Equal(got.People, []string{"vic"}) || got.Delivery {
		t.Fatalf("the root keeps %+v, want vic alone and nowhere to deliver", got)
	}
	if got.Audience.Kind != "direct" || !slices.Equal(got.Audience.Members, []string{"vic"}) || got.Audience.Unmapped != 0 {
		t.Fatalf("the root's audience = %+v, want the caller's own", got.Audience)
	}
	for _, r := range out.Posted.Runs {
		if r.Client != "" || r.Tag().Channel == "slack://T0TEAM/G01OTHER?thread=1700000001.000100" {
			t.Fatalf("the run answers on %q through %q", r.Tag().Channel, r.Client)
		}
	}
	for _, who := range []string{"ana", "ben"} {
		if p.SeesSession(placeOperator(who, tenant), out.Root) {
			t.Fatalf("%s reads the root", who)
		}
	}
}

func TestASurfaceStartsItsOwnRootWhereSomeoneNamedThePlaceFirst(t *testing.T) {
	p, st, machine, _ := placeRoom(t)
	vic := placeOperator("vic", tenant)
	const thread = "1700000001.000100"
	mine, err := p.SendMessages(t.Context(), vic, sayIn("@helper hello", &platform.ExternalRef{Type: platform.RefSlack, Team: "T0TEAM", Channel: "G01OTHER", TS: thread}))
	if err != nil || !mine.Created {
		t.Fatalf("a message naming a new place = %+v, %v", mine, err)
	}
	for _, r := range mine.Posted.Runs {
		await(t, p, r.ID, runs.Completed)
	}
	before := keptOf(t, p, st, mine.Root)
	// The thread's first message on Slack, as the app brings it.
	first, started, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession("G01OTHER", thread, "", "<@UAPP> status?"), platform.ChannelPrivate, "U0ANA", "U0BEN"))
	if err != nil || !started {
		t.Fatalf("the app's first message there = started %v, %v", started, err)
	}
	if first.ID == mine.Root || first.Parent != "slack://T0TEAM/G01OTHER/"+thread || first.Thread.Surface == nil {
		t.Fatalf("the surface's root = %s at %q, surface %v; want its own at the place", first.ID, first.Parent, first.Thread.Surface)
	}
	if after := keptOf(t, p, st, mine.Root); !sameRoot(before, after) {
		t.Fatalf("the caller's root kept %+v, want it as it was: %+v", after, before)
	}
	if got := keptOf(t, p, st, first.ID); !slices.Equal(got.People, []string{"ana", "ben"}) || got.Audience.Kind != "private" {
		t.Fatalf("the surface's root keeps %+v", got)
	}
	if p.SeesSession(vic, first.ID) || p.SeesSession(placeOperator("ana", tenant), mine.Root) {
		t.Fatal("one root is read by the other's people")
	}
	await(t, p, sessionRun(t, first).ID, runs.Completed)
	// The thread's next message lands in the surface's root.
	next, started, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession("G01OTHER", thread, "1700000001.000300", "<@UAPP> and now?"), platform.ChannelPrivate, "U0ANA", "U0BEN"))
	if err != nil || started || next.ID != first.ID {
		t.Fatalf("the app's next message = %s (started %v), %v; want %s", next.ID, started, err, first.ID)
	}
	// The caller's own root is still theirs to post to by its id.
	if _, err := p.SendMessagesUnder(t.Context(), vic, mine.Root, sayIn("@helper and now?", nil)); err != nil {
		t.Fatalf("the caller's reply under their own root = %v", err)
	}
	if after := keptOf(t, p, st, mine.Root); !slices.Equal(after.People, before.People) || !slices.Equal(after.Audience.Members, before.Audience.Members) || after.Audience.Kind != before.Audience.Kind {
		t.Fatalf("the caller's root keeps %+v, want its people and audience as they were: %+v", after, before)
	}
}

func TestAListeningMessageThatIsNotTakenLeavesTheRootsPeople(t *testing.T) {
	p, st, machine, _ := placeRoom(t)
	// A thread the app was not asked in: its root listens to nobody.
	quiet := withAudience(slackSession("G01OTHER", "1700000002.000100", "", "the deploy failed at 2am"), platform.ChannelPrivate, "U0ANA", "U0BEN")
	quiet.PostOnly = true
	root, started, err := p.StartSurfaceSession(t.Context(), machine, quiet)
	if err != nil || !started {
		t.Fatalf("a thread's first message, posted only = started %v, %v", started, err)
	}
	before := keptOf(t, p, st, root.ID)
	heard := withAudience(slackSession("G01OTHER", "1700000002.000100", "1700000002.000300", "and again at 3am"), platform.ChannelPrivate, "U0ANA", "U0VIC")
	heard.Listen = true
	if _, _, err := p.StartSurfaceSession(t.Context(), machine, heard); !errors.Is(err, platform.ErrNotListening) {
		t.Fatalf("a message that addressed nobody, where nobody listens = %v", err)
	}
	if after := keptOf(t, p, st, root.ID); !sameKept(before, after) {
		t.Fatalf("the root kept %+v, want it as it was: %+v", after, before)
	}
}
