package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
)

// secondClient adds app2, another Slack client of the place's team, which
// maps U0ANA to ana and U0VIC to vic, and returns its machine.
func secondClient(t *testing.T, p *platform.Platform, machine auth.Principal) auth.Principal {
	t.Helper()
	in := slackClient("app2", "A0APP2", "T0TEAM", "helper")
	in.Slack.Machine, in.Slack.People = "svc-two", map[string]string{"U0ANA": "ana", "U0VIC": "vic"}
	create(t, p, tenant, in)
	two := machine
	two.Subject = "svc-two"
	return two
}

func TestAnotherClientsMessageAtATeamsRootKeepsNothing(t *testing.T) {
	p, st, machine, root := placeRoom(t)
	two := secondClient(t, p, machine)
	before := keptOf(t, p, st, root)
	for i, aud := range []struct {
		typ     string
		members []string
	}{
		{platform.ChannelPublic, nil},
		{platform.ChannelPrivate, []string{"U0ANA", "U0VIC"}},
	} {
		in := withAudience(slackSession(placeChannel, placeThread, "1700000000.00060"+string(rune('0'+i)), "<@UAPP> what was said?"), aud.typ, aud.members...)
		in.ActAs.Client, in.Surface.Client = "app2", "app2"
		v, started, err := p.StartSurfaceSession(t.Context(), two, in)
		if !errors.Is(err, threads.ErrInvalid) || started || v.ID != "" {
			t.Fatalf("app2's message (%s) at app's root = %q, started %v, %v; want refused", aud.typ, v.ID, started, err)
		}
		if after := keptOf(t, p, st, root); !sameKept(before, after) {
			t.Fatalf("after app2's message (%s) the root kept %+v, want it as it was: %+v", aud.typ, after, before)
		}
	}
	if p.SeesSession(placeOperator("vic", tenant), root) {
		t.Fatal("vic reads the root")
	}
	// The client that started the root still posts to it.
	v, started, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession(placeChannel, placeThread, "1700000000.000700", "<@UAPP> and now?"), platform.ChannelPrivate, "U0ANA", "U0BEN"))
	if err != nil || started || v.ID != root {
		t.Fatalf("app's message = %q, started %v, %v; want it in %s", v.ID, started, err, root)
	}
	if after := keptOf(t, p, st, root); after.Messages != before.Messages+1 || !slices.Equal(after.People, before.People) || !slices.Equal(after.Audience.Members, before.Audience.Members) {
		t.Fatalf("after app's message the root keeps %+v, from %+v", after, before)
	}
}

func TestABuiltInSurfaceOfAnotherClientKeepsNothingAtATeamsRoot(t *testing.T) {
	p, st, machine, root := placeRoom(t)
	secondClient(t, p, machine)
	before := keptOf(t, p, st, root)
	ref := placeRef("1700000000.000600", "U0ANA", "U0VIC")
	ref.Client, ref.User = "app2", "U0ANA"
	if out, err := p.SendSurfaceMessages(t.Context(), placeOperator("ana", tenant), sayIn("@helper what was said?", ref)); !errors.Is(err, threads.ErrInvalid) || out.Root != "" {
		t.Fatalf("app2's message at app's root = %+v, %v; want refused", out, err)
	}
	if after := keptOf(t, p, st, root); !sameKept(before, after) {
		t.Fatalf("the root kept %+v, want it as it was: %+v", after, before)
	}
	if p.SeesSession(placeOperator("vic", tenant), root) {
		t.Fatal("vic reads the root")
	}
}

// hidesSurfaceOnce is a store whose next read of a surface's thread finds
// none, as when another message of the thread starts its root between that
// read and the start.
type hidesSurfaceOnce struct {
	state.Store
	mu  sync.Mutex
	key string
}

func (s *hidesSurfaceOnce) hide(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.key = key
}

func (s *hidesSurfaceOnce) Lookup(ctx context.Context, namespace, tenant, key string) (state.Record, error) {
	s.mu.Lock()
	hidden := namespace == "threads" && s.key != "" && key == s.key
	if hidden {
		s.key = ""
	}
	s.mu.Unlock()
	if hidden {
		return state.Record{}, state.ErrNotFound
	}
	return s.Store.Lookup(ctx, namespace, tenant, key)
}

func TestAMessageForSomeoneOutsideARootAnotherMessageStartedKeepsNothing(t *testing.T) {
	st := &hidesSurfaceOnce{Store: state.NewMemory()}
	p, _, machine, root := placeRoomOn(t, st)
	surface := "s:app:" + placeChannel + ":" + placeThread
	before := keptOf(t, p, st, root)
	in := withAudience(slackSession(placeChannel, placeThread, "1700000000.000600", "<@UAPP> what was said?"), platform.ChannelPrivate, "U0ANA", "U0BEN")
	in.ActAs.User = "U0VIC"
	st.hide(surface)
	if v, started, err := p.StartSurfaceSession(t.Context(), machine, in); err == nil || started {
		t.Fatalf("a message for vic, who does not read the root, = %q, started %v; want refused", v.ID, started)
	}
	if after := keptOf(t, p, st, root); !sameKept(before, after) {
		t.Fatalf("the root kept %+v, want it as it was: %+v", after, before)
	}
	// ana's message, the same way, lands in the root.
	st.hide(surface)
	v, started, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession(placeChannel, placeThread, "1700000000.000700", "<@UAPP> and now?"), platform.ChannelPrivate, "U0ANA", "U0BEN"))
	if err != nil || started || v.ID != root {
		t.Fatalf("ana's message = %q, started %v, %v; want it in %s", v.ID, started, err, root)
	}
}

func TestAReportedMemberJoinsARootWithTheirMessage(t *testing.T) {
	p, st, machine, root := placeRoom(t)
	pol, err := auth.ParsePolicy("test", `
role viewer { allow read on * }
role operator extends viewer { allow write on runs }
grant operator to subject "ana"
grant operator to subject "vic"
`)
	if err != nil {
		t.Fatal(err)
	}
	machine = auth.Principal{Subject: machine.Subject, Tenant: machine.Tenant, Kind: machine.Kind, Scopes: machine.Scopes}.WithPolicy(pol)
	vic := placeOperator("vic", tenant)
	if p.SeesSession(vic, root) {
		t.Fatal("vic reads the root before joining")
	}
	// vic joined the private channel; the app reports him a member with his message.
	in := withAudience(slackSession(placeChannel, placeThread, "1700000000.000600", "<@UAPP> what did I miss?"), platform.ChannelPrivate, "U0ANA", "U0BEN", "U0VIC")
	in.ActAs.User = "U0VIC"
	v, started, err := p.StartSurfaceSession(t.Context(), machine, in)
	if err != nil || started || v.ID != root {
		t.Fatalf("vic's message = %q, started %v, %v; want it in %s", v.ID, started, err, root)
	}
	for _, r := range v.Posted.Runs {
		await(t, p, r.ID, runs.Completed)
	}
	if after := keptOf(t, p, st, root); !slices.Equal(after.People, []string{"ana", "ben", "vic"}) {
		t.Fatalf("the root's people = %v, want ana, ben and vic", after.People)
	}
	if !p.SeesSession(vic, root) {
		t.Fatal("vic does not read the root he joined")
	}
}

// builtInRoom is placeRoom's workspace with the private channel's root
// started by the app's surface built into the server, rather than the
// app's machine. It returns the store, the app's machine and the root's id.
func builtInRoom(t *testing.T) (*platform.Platform, state.Store, auth.Principal, string) {
	t.Helper()
	st := state.NewMemory()
	p, machine := slackRoomOn(t, startWith(t, platform.Config{Backends: platform.Backends{State: st}}), func(in *platform.ClientInput) {
		in.Slack.People["U0BEN"] = "ben"
	})
	seePeople(t, p, auth.Principal{Subject: "ben"})
	workIn(t, p, "Initech", auth.Principal{Subject: "ana"}, auth.Principal{Subject: "ben"}, auth.Principal{Subject: "vic"})
	ref := placeRef(placeThread, "U0ANA", "U0BEN")
	ref.User = "U0ANA"
	out, err := p.SendSurfaceMessages(t.Context(), placeOperator("ana", tenant), sayIn("@helper status?", ref))
	if err != nil || !out.Created {
		t.Fatalf("the private channel's root = %+v, %v", out, err)
	}
	for _, r := range out.Posted.Runs {
		await(t, p, r.ID, runs.Completed)
	}
	return p, st, machine, out.Root
}

// deliveredThrough is the client a root's kept delivery names.
func deliveredThrough(t *testing.T, st state.Store, root string) string {
	t.Helper()
	it, err := state.NewTable[struct {
		Client string `json:"client"`
	}](st, "root-deliveries").Lookup(tenant, root)
	if err != nil {
		t.Fatalf("the root's delivery: %v", err)
	}
	return it.Value.Client
}

// checkOtherClientRefused sends app2's messages for the private channel's
// thread, through its built-in surface and from its machine, and checks
// that the root at root keeps nothing of them.
func checkOtherClientRefused(t *testing.T, p *platform.Platform, st state.Store, two auth.Principal, root string) {
	t.Helper()
	before := keptOf(t, p, st, root)
	ref := placeRef("1700000000.000600", "U0ANA", "U0VIC")
	ref.Client, ref.User = "app2", "U0ANA"
	if out, err := p.SendSurfaceMessages(t.Context(), placeOperator("ana", tenant), sayIn("@helper what was said?", ref)); !errors.Is(err, threads.ErrInvalid) || out.Root != "" {
		t.Fatalf("app2's message at app's root = %+v, %v; want refused", out, err)
	}
	if after := keptOf(t, p, st, root); !sameKept(before, after) {
		t.Fatalf("after app2's message the root kept %+v, want it as it was: %+v", after, before)
	}
	if got := deliveredThrough(t, st, root); got != "app" {
		t.Fatalf("the root answers through %q, want app", got)
	}
	if p.SeesSession(placeOperator("vic", tenant), root) {
		t.Fatal("vic reads the root")
	}
	in := withAudience(slackSession(placeChannel, placeThread, "1700000000.000650", "<@UAPP> hi"), platform.ChannelPrivate, "U0ANA", "U0VIC")
	in.ActAs.Client, in.Surface.Client = "app2", "app2"
	if v, _, err := p.StartSurfaceSession(t.Context(), two, in); err == nil && v.ID == root {
		t.Fatalf("app2's machine posted to app's root %s", root)
	}
	// On a root without a surface record, app2's machine opens its own root
	// beside it, so only the root's own records are compared.
	after := keptOf(t, p, st, root)
	after.Events = before.Events
	if !sameKept(before, after) {
		t.Fatalf("after app2's machine the root kept %+v, want it as it was: %+v", after, before)
	}
	if got := deliveredThrough(t, st, root); got != "app" {
		t.Fatalf("after app2's machine the root answers through %q, want app", got)
	}
}

// checkOwnClientLands sends the app's next message for the thread through
// its built-in surface and checks that it lands in root.
func checkOwnClientLands(t *testing.T, p *platform.Platform, st state.Store, root string) {
	t.Helper()
	before := keptOf(t, p, st, root)
	ref := placeRef("1700000000.000700", "U0ANA", "U0BEN")
	ref.User = "U0ANA"
	out, err := p.SendSurfaceMessages(t.Context(), placeOperator("ana", tenant), sayIn("@helper and now?", ref))
	if err != nil || out.Root != root {
		t.Fatalf("app's message = %+v, %v; want it in %s", out, err, root)
	}
	for _, r := range out.Posted.Runs {
		await(t, p, r.ID, runs.Completed)
	}
	if after := keptOf(t, p, st, root); after.Messages <= before.Messages || !slices.Equal(after.People, []string{"ana", "ben"}) {
		t.Fatalf("after app's message the root keeps %+v, from %+v", after, before)
	}
	if got := deliveredThrough(t, st, root); got != "app" {
		t.Fatalf("the root answers through %q, want app", got)
	}
}

func TestARootABuiltInSurfaceStartedIsKeptByItsClient(t *testing.T) {
	p, st, machine, root := builtInRoom(t)
	th, err := p.Threads.Get(tenant, root)
	if err != nil {
		t.Fatal(err)
	}
	if want := (threads.Surface{Client: "app", Channel: placeChannel, TS: placeThread}); th.Surface == nil || *th.Surface != want {
		t.Fatalf("the root's surface = %+v, want %+v", th.Surface, want)
	}
	checkOtherClientRefused(t, p, st, secondClient(t, p, machine), root)
	checkOwnClientLands(t, p, st, root)
	// The app's machine posts to the same root.
	v, started, err := p.StartSurfaceSession(t.Context(), machine, withAudience(slackSession(placeChannel, placeThread, "1700000000.000800", "<@UAPP> anything else?"), platform.ChannelPrivate, "U0ANA", "U0BEN"))
	if err != nil || started || v.ID != root {
		t.Fatalf("app's machine's message = %q, started %v, %v; want it in %s", v.ID, started, err, root)
	}
}

// TestAStoredRootWithoutASurfaceIsKeptByItsClient covers a root kept before
// roots recorded their surface: it answers through the client that started it.
func TestAStoredRootWithoutASurfaceIsKeptByItsClient(t *testing.T) {
	p, st, machine, root := builtInRoom(t)
	// Store the root as it was kept before: no surface, and no record of one.
	heads := state.NewTable[map[string]json.RawMessage](st, "threads")
	it, err := heads.Lookup(tenant, "t:"+root)
	if err != nil {
		t.Fatal(err)
	}
	delete(it.Value, "surface")
	if err := heads.Put(tenant, "t:"+root, it.Value); err != nil {
		t.Fatal(err)
	}
	if err := heads.Delete(tenant, "s:app:"+placeChannel+":"+placeThread); err != nil {
		t.Fatal(err)
	}
	if th, err := p.Threads.Get(tenant, root); err != nil || th.Surface != nil {
		t.Fatalf("the stored root = %+v, %v; want no surface", th.Surface, err)
	}
	checkOtherClientRefused(t, p, st, secondClient(t, p, machine), root)
	checkOwnClientLands(t, p, st, root)
}
