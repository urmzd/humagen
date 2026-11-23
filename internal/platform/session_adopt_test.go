package platform_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/session"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
)

// otherTenant is a second workspace on the same store.
const otherTenant = "globex-co"

// adoptRoom is slackRoom on a store the test reads, with Field and the
// projects under it open to its members alone, ana and vic, so ben reads
// nothing there, and globex-helper an agent that lives in Globex.
func adoptRoom(t *testing.T, st state.Store) (*platform.Platform, auth.Principal) {
	t.Helper()
	p, machine := slackRoomOn(t, startWith(t, platform.Config{Backends: platform.Backends{State: st}}), nil)
	// The deepest first: no sub-project may be more open than its parent.
	for _, name := range []string{"Renewals", "Globex", "Initech", "Field"} {
		pr, ok := p.Projects.Tree(tenant).Get(name)
		if !ok {
			t.Fatalf("no project %s", name)
		}
		if _, err := p.Projects.Update(tenant, name, pr.Version, ada, time.Now(), func(pr *projects.Project) error {
			pr.Members = []projects.Entry{{Who: "ada", Role: projects.Owner}, {Who: "ana", Role: projects.Member}, {Who: "vic", Role: projects.Member}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "globex-helper", Model: "offline", Provider: "offline", SystemPrompt: "You know the Globex account."}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAgentHome(t.Context(), tenant, "globex-helper", "Globex", ada); err != nil {
		t.Fatal(err)
	}
	return p, machine
}

// readers are the people and machines every conversation is read as: an
// administrator, two members, someone with no role in any project, the
// Slack client's machine, and people of another workspace, one of them
// with a member's own subject.
func readers(machine auth.Principal) []auth.Principal {
	user := func(subject, tenantName string, role auth.Role) auth.Principal {
		return auth.Principal{Subject: subject, Tenant: tenantName, Kind: auth.User, Roles: []auth.Role{role}}
	}
	return []auth.Principal{
		user("ada", tenant, auth.Admin), user("ana", tenant, auth.Operator), user("vic", tenant, auth.Operator), user("ben", tenant, auth.Operator),
		machine,
		user("ana", otherTenant, auth.Admin), user("zed", otherTenant, auth.Operator),
	}
}

func readerName(who auth.Principal) string { return who.Tenant + "/" + who.Subject }

// reads is what each reader reads of some conversations and their runs.
func reads(p *platform.Platform, who []auth.Principal, ids []string) map[string]bool {
	out := map[string]bool{}
	for _, w := range who {
		for _, id := range ids {
			out[readerName(w)+" thread "+id] = p.SeesThread(w, id)
			turns, _ := p.Runs.Conversation(tenant, id)
			for _, r := range turns {
				out[readerName(w)+" run "+r.ID] = p.Seer(w).RunID(r.ID)
			}
		}
	}
	return out
}

// conversation writes the record of a conversation from before sessions.
func conversation(t *testing.T, p *platform.Platform, tenantName string, th threads.Thread) threads.Thread {
	t.Helper()
	if th.Channel == "" {
		th.Channel = "general"
	}
	if th.CreatedBy == "" {
		th.CreatedBy = "ana"
	}
	got, err := p.Threads.Create(tenantName, th, t0Adopt)
	if err != nil {
		t.Fatalf("conversation %s: %v", th.ID, err)
	}
	return got
}

var t0Adopt = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

// threadID is the id of the nth conversation a test seeds.
func threadID(n int) string { return fmt.Sprintf("th-%016x", 0xf00000+n) }

// byTenant is the records of one tenant in what stored returns.
func byTenant(all map[string][]state.Record, tenantName string) map[string][]state.Record {
	out := map[string][]state.Record{}
	for ns, recs := range all {
		for _, r := range recs {
			if r.Tenant == tenantName {
				out[ns] = append(out[ns], r)
			}
		}
	}
	return out
}

// Every kind of conversation from before sessions is read, after the
// backfill, by exactly the readers who read it before: the ones it adopts
// as sessions, as conversations and as sessions, and the ones it leaves.
// A dry run writes nothing, an apply does what the dry run said, a second
// apply writes nothing, and nothing of another workspace is read as this
// one's or changed.
func TestBackfillAdoptsConversationsForTheReadersTheyHad(t *testing.T) {
	ctx := t.Context()
	st := state.NewMemory()
	p, machine := adoptRoom(t, st)
	ana := auth.Principal{Subject: "ana", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}

	started := func(who auth.Principal, in platform.ThreadInput) threads.Thread {
		t.Helper()
		got, err := p.StartThread(ctx, who, in)
		if err != nil {
			t.Fatalf("start %q: %v", in.Text, err)
		}
		for _, r := range got.Runs {
			settle(t, p, r.ID)
		}
		th, err := p.Threads.Get(tenant, got.Thread.ID)
		if err != nil {
			t.Fatal(err)
		}
		return th
	}
	const same = "th-00000000000000aa"
	type source struct {
		name    string
		th      threads.Thread
		agent   string // the session's agent, "" for one that is left
		slack   bool
		because string // why it is left
	}
	sources := []source{
		{name: "private", agent: "lead", th: started(ana, platform.ThreadInput{Project: "Field", Visibility: threads.Private, Text: "@lead keep this between us"})},
		{name: "shared", agent: "lead", th: started(ana, platform.ThreadInput{Project: "Field", Visibility: threads.Shared, Text: "@lead plan it"})},
		{name: "in a channel", agent: "helper", th: started(ana, platform.ThreadInput{Project: "Field", Channel: "renewals", Visibility: threads.Shared, Default: "helper", Text: "status?"})},
		{name: "a Slack thread in a bound channel", agent: "lead", slack: true, th: started(machine, fromSlack("C01RENEW", "1700000000.000100", "", "<@UAPP> @lead plan the renewal"))},
		{name: "a Slack direct message", agent: "helper", slack: true, th: started(machine, fromSlack("D0ANA", "1700000000.000200", "", "hello"))},
		{name: "an agent that lives where it is filed", agent: "globex-helper", th: conversation(t, p, tenant, threads.Thread{ID: "th-00000000000000b1", Project: "Globex", Visibility: threads.Shared, Default: "globex-helper"})},
		{name: "the same id as another workspace's", agent: "lead", th: conversation(t, p, tenant, threads.Thread{ID: same, Project: "Field", Visibility: threads.Private, Default: "lead"})},
		{name: "a surface of a client that is gone", slack: true, because: "is not in the store", th: conversation(t, p, tenant, threads.Thread{ID: "th-00000000000000c1", Project: "Field", Visibility: threads.Shared, Default: "lead", Surface: &threads.Surface{Client: "gone", Channel: "C0GONE", TS: "1700000000.000300"}})},
		{name: "an agent not every reader reads", because: "not everyone who reads", th: conversation(t, p, tenant, threads.Thread{ID: "th-00000000000000c2", Project: "Field", Visibility: threads.Shared, Default: "globex-helper"})},
		{name: "an agent not every reader reads, private", because: "not everyone who reads", th: conversation(t, p, tenant, threads.Thread{ID: "th-00000000000000c3", Project: "Field", Visibility: threads.Private, Default: "globex-helper"})},
		{name: "a Slack thread of an agent its machine does not read", slack: true, because: "lives in", th: conversation(t, p, tenant, threads.Thread{ID: "th-00000000000000c7", Project: "Globex", Visibility: threads.Shared, Default: "globex-helper", Surface: &threads.Surface{Client: "app", Channel: "C0GLOBEX", TS: "1700000000.000500"}})},
		{name: "two agents", because: "a session has one agent", th: conversation(t, p, tenant, threads.Thread{ID: "th-00000000000000c4", Project: "Field", Visibility: threads.Shared, Agents: []string{"lead", "helper"}, Mode: threads.ModeAll})},
		{name: "no agent", because: "names no agent", th: conversation(t, p, tenant, threads.Thread{ID: "th-00000000000000c5", Project: "Field", Visibility: threads.Shared})},
		{name: "an agent that is gone", because: "cannot answer there", th: conversation(t, p, tenant, threads.Thread{ID: "th-00000000000000c6", Project: "Field", Visibility: threads.Shared, Default: "nobody"})},
	}
	// The app answered in the channel thread, which then listens: its
	// record is read once that is written.
	awaitListener(t, p, sources[3].th.ID, "ana")
	for i := range sources {
		th, err := p.Threads.Get(tenant, sources[i].th.ID)
		if err != nil {
			t.Fatal(err)
		}
		sources[i].th = th
	}
	for _, s := range sources {
		if s.th.Session != nil || !strings.HasPrefix(s.th.ID, "th-") {
			t.Fatalf("%s: it is %s, session %+v, before the backfill", s.name, s.th.ID, s.th.Session)
		}
	}
	// Another workspace on the same store: a conversation of its own, and
	// one under an id this workspace uses too.
	theirs := []threads.Thread{
		conversation(t, p, otherTenant, threads.Thread{ID: same, Visibility: threads.Private, Default: "lead", CreatedBy: "zed"}),
		conversation(t, p, otherTenant, threads.Thread{ID: "th-00000000000000d1", Visibility: threads.Shared, Default: "lead", CreatedBy: "zed"}),
	}

	who := readers(machine)
	ids := make([]string, 0, len(sources)+1)
	for _, s := range sources {
		ids = append(ids, s.th.ID)
	}
	ids = append(ids, theirs[1].ID)
	was := reads(p, who, ids)
	// The fixture tells readers apart: each conversation has a reader, and
	// the private ones a member who is not one.
	for _, s := range sources {
		if !was["acme/ana thread "+s.th.ID] || was[otherTenant+"/zed thread "+s.th.ID] && s.th.ID != same {
			t.Fatalf("%s: ana reads it %v, another workspace's zed %v", s.name, was["acme/ana thread "+s.th.ID], was[otherTenant+"/zed thread "+s.th.ID])
		}
		if s.th.Visibility == threads.Private && (was["acme/vic thread "+s.th.ID] || was["acme/ben thread "+s.th.ID]) {
			t.Fatalf("%s: a private conversation is read by someone who takes no part in it", s.name)
		}
		if s.th.Visibility == threads.Shared && s.th.Project == "Field" && (!was["acme/vic thread "+s.th.ID] || was["acme/ben thread "+s.th.ID]) {
			t.Fatalf("%s: shared in Field, vic reads it %v and ben %v", s.name, was["acme/vic thread "+s.th.ID], was["acme/ben thread "+s.th.ID])
		}
	}

	before := stored(t, st)
	dry, err := p.BackfillSessions(ctx, tenant, false)
	if err != nil {
		t.Fatal(err)
	}
	if after := stored(t, st); !reflect.DeepEqual(before, after) {
		t.Fatal("a dry run wrote to the store")
	}
	lines := map[string]platform.BackfillLine{}
	for _, l := range dry.Lines {
		t.Logf("%s: %s -> %s (%s)", l.Kind, l.Subject, l.Session, l.Detail)
		lines[l.Subject] = l
	}
	want := map[string]int{}
	for _, s := range sources {
		kind := platform.BackfillThreadAdopted
		switch {
		case s.agent == "" && s.slack:
			kind = platform.BackfillSlackThreadLeft
		case s.agent == "":
			kind = platform.BackfillThreadLeft
		case s.slack:
			kind = platform.BackfillSlackThreadAdopted
		}
		want[kind]++
		l := lines[s.th.ID]
		if l.Kind != kind || !strings.Contains(l.Detail, s.because) || (s.agent != "" && !strings.Contains(l.Detail, "owned by "+s.agent)) {
			t.Fatalf("%s: the dry run says %q (%s), want %q", s.name, l.Kind, l.Detail, kind)
		}
		// The report says who reads it, as before.
		if s.agent != "" && s.th.Visibility == threads.Private && !strings.Contains(l.Detail, "private to ana, as before") {
			t.Fatalf("%s: the dry run does not say who reads it: %s", s.name, l.Detail)
		}
	}
	if !reflect.DeepEqual(dry.Counts, want) {
		t.Fatalf("the dry run counts %v, want %v", dry.Counts, want)
	}
	if _, listed := lines[theirs[1].ID]; listed {
		t.Fatal("the dry run lists another workspace's conversation")
	}

	applied, err := p.BackfillSessions(ctx, tenant, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(applied.Counts, dry.Counts) || !reflect.DeepEqual(applied.Lines, dry.Lines) {
		t.Fatalf("the apply did %v, the dry run said %v", applied.Counts, dry.Counts)
	}
	after := stored(t, st)
	if !reflect.DeepEqual(byTenant(before, otherTenant), byTenant(after, otherTenant)) {
		t.Fatal("the apply changed another workspace's records")
	}

	for _, s := range sources {
		got, err := p.Threads.Get(tenant, s.th.ID)
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		switch {
		case got.Project != s.th.Project || got.Channel != s.th.Channel || got.Visibility != s.th.Visibility || got.CreatedBy != s.th.CreatedBy:
			t.Fatalf("%s: it is filed in %q #%s, %s, by %s; was %q #%s, %s, by %s", s.name, got.Project, got.Channel, got.Visibility, got.CreatedBy, s.th.Project, s.th.Channel, s.th.Visibility, s.th.CreatedBy)
		case !reflect.DeepEqual(got.Participants, s.th.Participants):
			t.Fatalf("%s: its participants are %+v, were %+v", s.name, got.Participants, s.th.Participants)
		case !reflect.DeepEqual(got.Surface, s.th.Surface):
			t.Fatalf("%s: its surface is %+v, was %+v", s.name, got.Surface, s.th.Surface)
		case !got.CreatedAt.Equal(s.th.CreatedAt) || (s.th.Surface == nil && !got.UpdatedAt.Equal(s.th.UpdatedAt)):
			t.Fatalf("%s: its times moved", s.name)
		}
		if s.agent == "" {
			if got.Session != nil || got.Version != s.th.Version {
				t.Fatalf("%s: left, yet it changed: %+v", s.name, got)
			}
			if _, err := p.Sessions.Get(ctx, tenant, s.th.ID); err == nil {
				t.Fatalf("%s: left, yet it has a session's record", s.name)
			}
			continue
		}
		place := "console://" + s.th.ID
		if s.slack {
			place = "slack://T0TEAM/" + s.th.Surface.Channel + "/" + s.th.Surface.TS
		}
		if got.Session == nil || got.Session.Agent != s.agent || got.Session.Parent != place || got.Default != s.agent || !slices.Equal(got.Agents, []string{s.agent}) {
			t.Fatalf("%s: its session is %+v, default %q, agents %v; want %s at %s", s.name, got.Session, got.Default, got.Agents, s.agent, place)
		}
		rec, err := p.Sessions.Get(ctx, tenant, s.th.ID)
		if err != nil || rec.ID != s.th.ID || rec.Key != (session.Key{Tenant: tenant, Parent: place, Agent: s.agent}).Normalize() {
			t.Fatalf("%s: its session's record is %+v, %v", s.name, rec, err)
		}
	}

	// Read by exactly the readers it had: as a conversation and by its runs,
	// as a session, and in the list of sessions.
	now := reads(p, who, ids)
	for k, v := range was {
		if now[k] != v {
			t.Errorf("%s: read %v before the backfill and %v after", k, v, now[k])
		}
	}
	if len(now) != len(was) {
		t.Errorf("readers were asked of %d things before and %d after", len(was), len(now))
	}
	for _, w := range who {
		var listed []string
		if w.Kind == auth.User {
			all, err := p.ListSessions(ctx, w, "", "", "", "")
			if err != nil {
				t.Fatalf("%s's sessions: %v", readerName(w), err)
			}
			for _, s := range all {
				listed = append(listed, s.ID)
			}
		}
		for _, s := range sources {
			read := w.Tenant == tenant && was[readerName(w)+" thread "+s.th.ID]
			if got := p.SeesSession(w, s.th.ID); got != (read && s.agent != "") {
				t.Errorf("%s reads %s as a session: %v; read it as a conversation before: %v", readerName(w), s.name, got, read)
			}
			if w.Kind == auth.User && slices.Contains(listed, s.th.ID) != (read && s.agent != "") {
				t.Errorf("%s's sessions list %s: %v; read it before: %v", readerName(w), s.name, slices.Contains(listed, s.th.ID), read)
			}
		}
	}
	// The other workspace's conversation under the same id is its own still.
	for _, th := range theirs {
		if got, err := p.Threads.Get(otherTenant, th.ID); err != nil || got.Session != nil || got.Version != th.Version {
			t.Fatalf("another workspace's %s changed: %+v, %v", th.ID, got, err)
		}
		if _, err := p.Sessions.Get(ctx, otherTenant, th.ID); err == nil {
			t.Fatalf("another workspace's %s has a session's record", th.ID)
		}
	}

	// A second apply finds the first one's work.
	before = stored(t, st)
	again, err := p.BackfillSessions(ctx, tenant, true)
	if err != nil {
		t.Fatal(err)
	}
	if after := stored(t, st); !reflect.DeepEqual(before, after) {
		t.Fatal("a second apply wrote to the store")
	}
	adopted := want[platform.BackfillThreadAdopted] + want[platform.BackfillSlackThreadAdopted]
	if again.Changes() != 0 || again.Counts[platform.BackfillThreadAlreadyAdopted]+again.Counts[platform.BackfillSlackThreadAlreadyAdopted] != adopted ||
		again.Counts[platform.BackfillThreadLeft] != want[platform.BackfillThreadLeft] || again.Counts[platform.BackfillSlackThreadLeft] != want[platform.BackfillSlackThreadLeft] {
		t.Fatalf("a second apply did %v", again.Counts)
	}

	// The other workspace's backfill decides its own, and nothing of this one.
	before = stored(t, st)
	other, err := p.BackfillSessions(ctx, otherTenant, true)
	if err != nil {
		t.Fatal(err)
	}
	if after := stored(t, st); !reflect.DeepEqual(byTenant(before, tenant), byTenant(after, tenant)) {
		t.Fatal("another workspace's apply changed this one's records")
	}
	for _, l := range other.Lines {
		if !slices.ContainsFunc(theirs, func(th threads.Thread) bool { return th.ID == l.Subject }) {
			t.Fatalf("another workspace's apply reports %s", l.Subject)
		}
	}
}

// An adopted conversation is a session under the id it had: its old
// messages and runs are its own, a new message in the console is answered
// by its agent in that session, a Slack reply reaches it as a session's
// does, and neither changes who reads it or who takes part.
func TestAnAdoptedConversationIsAnsweredAsASessionUnderItsOwnID(t *testing.T) {
	ctx := t.Context()
	st := state.NewMemory()
	p, machine := adoptRoom(t, st)
	ana := auth.Principal{Subject: "ana", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	vic := auth.Principal{Subject: "vic", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}

	console, err := p.StartThread(ctx, ana, platform.ThreadInput{Project: "Field", Visibility: threads.Private, Text: "@lead keep this between us"})
	if err != nil || len(console.Runs) != 1 {
		t.Fatalf("the console conversation: %d runs, %v", len(console.Runs), err)
	}
	old := settle(t, p, console.Runs[0].ID)
	dm, err := p.StartThread(ctx, machine, fromSlack("D0ANA", "1700000000.000200", "", "hello"))
	if err != nil || len(dm.Runs) != 1 {
		t.Fatalf("the direct message: %d runs, %v", len(dm.Runs), err)
	}
	settle(t, p, dm.Runs[0].ID)
	const root = "1700000000.000100"
	channel, err := p.StartThread(ctx, machine, fromSlack("C01RENEW", root, "", "<@UAPP> @lead plan the renewal"))
	if err != nil || len(channel.Runs) != 1 {
		t.Fatalf("the channel thread: %d runs, %v", len(channel.Runs), err)
	}
	settle(t, p, channel.Runs[0].ID)

	if rep, err := p.BackfillSessions(ctx, tenant, true); err != nil || rep.Counts[platform.BackfillThreadAdopted] != 1 || rep.Counts[platform.BackfillSlackThreadAdopted] != 2 {
		t.Fatalf("the apply did %v, %v", rep.Counts, err)
	}

	// The old link opens the session, with what was said before.
	view, err := p.ReadSession(ctx, ana, console.Thread.ID, "", "", false)
	switch {
	case err != nil:
		t.Fatalf("reading the adopted conversation as a session: %v", err)
	case view.ID != console.Thread.ID || view.Agent != "lead" || view.Parent != "console://"+console.Thread.ID:
		t.Fatalf("it reads as %s, %s's, at %s", view.ID, view.Agent, view.Parent)
	case !slices.ContainsFunc(view.Messages, func(m platform.SessionMessage) bool { return m.Run != nil && m.Run.ID == old.ID }):
		t.Fatalf("its old run %s is not among its messages", old.ID)
	}
	if ms, _, err := p.Threads.Messages(tenant, console.Thread.ID, "", 10); err != nil || len(ms) != 1 || ms[0].ID != console.Message.ID {
		t.Fatalf("its old messages = %+v, %v", ms, err)
	}
	if got, _ := p.Runs.Get(tenant, old.ID); got.Thread != console.Thread.ID {
		t.Fatalf("its old run is in %q", got.Thread)
	}
	if _, err := p.ReadSession(ctx, vic, console.Thread.ID, "", "", false); err == nil {
		t.Fatal("vic, who takes no part in it, reads the adopted conversation as a session")
	}

	// A new message that names nobody is answered by the session's agent,
	// in the session, under the same id.
	posted, err := p.PostMessage(ctx, ana, console.Thread.ID, platform.MessageInput{Text: "and the invoice?"})
	if err != nil || len(posted.Runs) != 1 || posted.Runs[0].Agent != "lead" || posted.Runs[0].Thread != console.Thread.ID {
		t.Fatalf("a new message started %+v, %v", posted.Runs, err)
	}
	await(t, p, posted.Runs[0].ID, runs.Completed)
	if bound, err := p.Sessions.Runs(ctx, tenant, console.Thread.ID, 10); err != nil || !slices.Contains(bound, posted.Runs[0].ID) {
		t.Fatalf("the session binds %v, %v; want the new run %s", bound, err, posted.Runs[0].ID)
	}
	if _, err := p.PostMessage(ctx, vic, console.Thread.ID, platform.MessageInput{Text: "me too"}); err == nil {
		t.Fatal("vic posted in a private session they take no part in")
	}

	// A Slack reply reaches the adopted thread as a session, under its id.
	// A session started under it is its child, read by those who read it.
	child, err := p.StartSession(ctx, ana, platform.SessionInput{Parent: console.Thread.ID, Agent: "helper", Text: "look into the invoice"})
	if err != nil || child.Parent != console.Thread.ID || child.Thread.Visibility != threads.Private {
		t.Fatalf("a child of the adopted conversation = %s under %q, %s, %v", child.ID, child.Parent, child.Thread.Visibility, err)
	}
	settle(t, p, sessionRun(t, child).ID)
	if !p.SeesSession(ana, child.ID) || p.SeesSession(vic, child.ID) || p.SeesThread(vic, child.ID) {
		t.Fatal("the child is not read by ana alone")
	}

	// The reply says the place is a private channel of ana and vic. The
	// session stays shared where it was filed, and keeps no audience.
	filed, err := p.Threads.Get(tenant, channel.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	reply, startedNew, err := p.StartSurfaceSession(ctx, machine, withAudience(slackSession("C01RENEW", root, "1700000000.000300", "<@UAPP> and the terms?"), platform.ChannelPrivate, "U0ANA", "U0VIC"))
	switch {
	case err != nil || startedNew:
		t.Fatalf("a Slack reply = %v, started a new session %v", err, startedNew)
	case reply.ID != channel.Thread.ID || reply.Posted == nil || len(reply.Posted.Runs) != 1 || reply.Posted.Runs[0].Thread != channel.Thread.ID:
		t.Fatalf("a Slack reply reached %s with %+v, want %s", reply.ID, reply.Posted, channel.Thread.ID)
	}
	await(t, p, reply.Posted.Runs[0].ID, runs.Completed)
	if got, ok, err := p.Threads.BySurface(tenant, *channel.Thread.Surface); err != nil || !ok || got.ID != channel.Thread.ID || got.Visibility != filed.Visibility || got.Project != filed.Project || !samePeople(got.Participants, filed.Participants) {
		t.Fatalf("the Slack thread is %q (%s in %q, %+v), %v, %v; was %s in %q, %+v", got.ID, got.Visibility, got.Project, got.Participants, ok, err, filed.Visibility, filed.Project, filed.Participants)
	}
	if aud, kept := p.SessionAudience(tenant, channel.Thread.ID); kept {
		t.Fatalf("the adopted Slack thread keeps an audience: %+v", aud)
	}

	// A reply in the direct message says who is in the place now. The
	// adopted session keeps the people it had: nobody is added.
	had, err := p.Threads.Get(tenant, dm.Thread.ID)
	if err != nil || p.SeesThread(vic, dm.Thread.ID) {
		t.Fatalf("before the reply vic reads the direct message, or %v", err)
	}
	in := withAudience(slackSession("D0ANA", "1700000000.000200", "1700000000.000400", "and now?"), platform.ChannelPrivate, "U0ANA", "U0VIC")
	again, startedNew, err := p.StartSurfaceSession(ctx, machine, in)
	if err != nil || startedNew || again.ID != dm.Thread.ID {
		t.Fatalf("a reply in the direct message = %s, started %v, %v", again.ID, startedNew, err)
	}
	for _, r := range again.Posted.Runs {
		settle(t, p, r.ID)
	}
	got, err := p.Threads.Get(tenant, dm.Thread.ID)
	if err != nil || !samePeople(got.Participants, had.Participants) || got.Visibility != threads.Private {
		t.Fatalf("after the reply its people are %+v (%s), were %+v; %v", got.Participants, got.Visibility, had.Participants, err)
	}
	if p.SeesThread(vic, dm.Thread.ID) || p.SeesSession(vic, dm.Thread.ID) {
		t.Fatal("vic reads the direct message after a reply named them in its place")
	}
	if !p.SeesSession(ana, dm.Thread.ID) || !p.SeesSession(machine, dm.Thread.ID) {
		t.Fatal("ana, or the client's machine, no longer reads the direct message")
	}
}

// samePeople reports whether two lists name the same people, whatever
// agents joined since.
func samePeople(a, b []threads.Participant) bool {
	people := func(ps []threads.Participant) []string {
		var out []string
		for _, pt := range ps {
			if pt.Kind == threads.Person {
				out = append(out, pt.Name)
			}
		}
		return out
	}
	return slices.Equal(people(a), people(b))
}

// adopter is the session store's half of an adoption.
type adopter interface {
	Adopt(ctx context.Context, tenant, id string, o session.Open) (session.Record, error)
}

// An apply that stopped between its two writes left a session's record
// and a conversation. The next apply finishes that one as it was decided,
// adopts the ones the first never reached, leaves one whose record names
// another agent than it does now, and a later apply adopts only what is
// new.
func TestBackfillFinishesAStoreAnApplyLeftHalfDone(t *testing.T) {
	ctx := t.Context()
	st := state.NewMemory()
	p, machine := adoptRoom(t, st)
	half := conversation(t, p, tenant, threads.Thread{ID: "th-00000000000000e1", Project: "Field", Visibility: threads.Private, Default: "lead"})
	fresh := conversation(t, p, tenant, threads.Thread{ID: "th-00000000000000e2", Project: "Field", Visibility: threads.Shared, Default: "helper"})
	// Decided for helper by the apply that stopped; it names lead now.
	changed := conversation(t, p, tenant, threads.Thread{ID: "th-00000000000000e3", Project: "Field", Visibility: threads.Private, Default: "lead"})
	// It named no agent when the apply stopped, and none answers it now.
	silent := conversation(t, p, tenant, threads.Thread{ID: "th-00000000000000e4", Project: "Field", Visibility: threads.Private})
	store, ok := p.Sessions.(adopter)
	if !ok {
		t.Fatal("the session store adopts nothing")
	}
	recs := map[string]session.Record{}
	for id, agent := range map[string]string{half.ID: "lead", changed.ID: "helper", silent.ID: "helper"} {
		rec, err := store.Adopt(ctx, tenant, id, session.Open{Parent: "console://" + id, Agent: agent, Channel: wire.Channel(tenant)})
		if err != nil {
			t.Fatal(err)
		}
		recs[id] = rec
	}
	who := readers(machine)
	ids := []string{half.ID, fresh.ID, changed.ID, silent.ID}
	was := reads(p, who, ids)
	for _, w := range who {
		for _, id := range ids {
			if p.SeesSession(w, id) {
				t.Fatalf("%s reads %s as a session before it is one", readerName(w), id)
			}
		}
	}

	before := stored(t, st)
	dry, err := p.BackfillSessions(ctx, tenant, false)
	if err != nil {
		t.Fatal(err)
	}
	if after := stored(t, st); !reflect.DeepEqual(before, after) {
		t.Fatal("a dry run wrote to the store")
	}
	applied, err := p.BackfillSessions(ctx, tenant, true)
	if err != nil {
		t.Fatalf("the apply stopped: %v", err)
	}
	if !reflect.DeepEqual(applied.Lines, dry.Lines) || applied.Counts[platform.BackfillThreadAdopted] != 3 || applied.Counts[platform.BackfillThreadLeft] != 1 {
		t.Fatalf("the apply did %v, the dry run %v", applied.Counts, dry.Counts)
	}
	for id, agent := range map[string]string{half.ID: "lead", fresh.ID: "helper", silent.ID: "helper"} {
		th, err := p.Threads.Get(tenant, id)
		if err != nil || th.Session == nil || th.Session.Agent != agent {
			t.Fatalf("%s is %+v, %v; want %s's session", id, th.Session, err, agent)
		}
		rec, err := p.Sessions.Get(ctx, tenant, id)
		if err != nil || rec.Key.Agent != agent {
			t.Fatalf("%s's record is %+v, %v", id, rec, err)
		}
		if first, had := recs[id]; had && (first.ID != rec.ID || first.Key != rec.Key || !first.CreatedAt.Equal(rec.CreatedAt)) {
			t.Fatalf("%s's record was written again: %+v, was %+v", id, rec, first)
		}
	}
	if th, err := p.Threads.Get(tenant, changed.ID); err != nil || th.Session != nil || th.Version != changed.Version {
		t.Fatalf("the one decided for another agent changed: %+v, %v", th, err)
	}
	for _, l := range applied.Lines {
		if l.Subject == changed.ID && (l.Kind != platform.BackfillThreadLeft || !strings.Contains(l.Detail, "another agent")) {
			t.Fatalf("the one decided for another agent: %s (%s)", l.Kind, l.Detail)
		}
	}
	now := reads(p, who, ids)
	if !reflect.DeepEqual(now, was) {
		t.Fatalf("who reads them changed: %v, was %v", now, was)
	}
	for _, w := range who {
		for _, id := range ids {
			if got, want := p.SeesSession(w, id), id != changed.ID && was[readerName(w)+" thread "+id]; got != want {
				t.Errorf("%s reads %s as a session: %v, want %v", readerName(w), id, got, want)
			}
		}
	}

	if again, err := p.BackfillSessions(ctx, tenant, true); err != nil || again.Changes() != 0 || again.Counts[platform.BackfillThreadAlreadyAdopted] != 3 {
		t.Fatalf("a second apply did %v, %v", again.Counts, err)
	}
	// A conversation written since is the only one a later apply adopts.
	later := conversation(t, p, tenant, threads.Thread{ID: "th-00000000000000e5", Project: "Field", Visibility: threads.Shared, Default: "lead"})
	before = stored(t, st)
	third, err := p.BackfillSessions(ctx, tenant, true)
	if err != nil || third.Changes() != 1 || third.Counts[platform.BackfillThreadAdopted] != 1 || third.Counts[platform.BackfillThreadAlreadyAdopted] != 3 {
		t.Fatalf("a later apply did %v, %v", third.Counts, err)
	}
	after := stored(t, st)
	for _, id := range ids {
		for ns := range before {
			if !reflect.DeepEqual(recordsOf(before[ns], tenant, id), recordsOf(after[ns], tenant, id)) {
				t.Fatalf("a later apply wrote %s again in %s", id, ns)
			}
		}
	}
	if th, err := p.Threads.Get(tenant, later.ID); err != nil || th.Session == nil {
		t.Fatalf("the later conversation is %+v, %v", th.Session, err)
	}
}

// recordsOf is a tenant's records whose key names id.
func recordsOf(recs []state.Record, tenantName, id string) []state.Record {
	var out []state.Record
	for _, r := range recs {
		if r.Tenant == tenantName && strings.Contains(r.Key, id) {
			out = append(out, r)
		}
	}
	return out
}

// readLog counts the reads a store is asked for while it is on.
type readLog struct {
	state.Store
	mu      sync.Mutex
	on      bool
	loads   []string
	tenants map[string]int
	widest  int
}

func (l *readLog) note(tenantName string, limit int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.on {
		return
	}
	if l.tenants == nil {
		l.tenants = map[string]int{}
	}
	l.tenants[tenantName]++
	l.widest = max(l.widest, limit)
}

func (l *readLog) Get(ctx context.Context, ns, tenantName, key string) ([]byte, error) {
	l.note(tenantName, 1)
	return l.Store.Get(ctx, ns, tenantName, key)
}

func (l *readLog) Lookup(ctx context.Context, ns, tenantName, key string) (state.Record, error) {
	l.note(tenantName, 1)
	return l.Store.Lookup(ctx, ns, tenantName, key)
}

func (l *readLog) List(ctx context.Context, ns, tenantName string, q state.Query) ([]state.Record, string, error) {
	l.note(tenantName, q.Limit)
	return l.Store.List(ctx, ns, tenantName, q)
}

func (l *readLog) Load(ctx context.Context, ns string) ([]state.Record, error) {
	l.mu.Lock()
	if l.on {
		l.loads = append(l.loads, ns)
	}
	l.mu.Unlock()
	return l.Store.Load(ctx, ns)
}

// during counts the reads fn makes of each tenant.
func (l *readLog) during(fn func()) (reads map[string]int, loads []string, widest int) {
	l.mu.Lock()
	l.on, l.loads, l.tenants, l.widest = true, nil, map[string]int{}, 0
	l.mu.Unlock()
	fn()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.on = false
	return l.tenants, l.loads, l.widest
}

// The backfill reads one workspace: it never loads a namespace across
// workspaces, every page it reads is within the store's page cap, what it
// reads for each conversation is a fixed number of records, and another
// workspace's conversations, however many, add nothing to it.
func TestBackfillReadsOneWorkspaceWithinBounds(t *testing.T) {
	ctx := t.Context()
	log := &readLog{Store: state.NewMemory()}
	p, _ := adoptRoom(t, log)
	seed := func(tenantName string, from, n int) {
		t.Helper()
		for i := from; i < from+n; i++ {
			conversation(t, p, tenantName, threads.Thread{ID: threadID(i), Project: "", Visibility: threads.Private, Default: "lead"})
		}
	}
	dryRun := func(tenantName string) (int, int) {
		t.Helper()
		var rep platform.BackfillReport
		reads, loads, widest := log.during(func() {
			var err error
			if rep, err = p.BackfillSessions(ctx, tenantName, false); err != nil {
				t.Fatal(err)
			}
		})
		if len(loads) != 0 {
			t.Fatalf("the backfill loaded %v across workspaces", loads)
		}
		if widest > state.MaxList {
			t.Fatalf("the backfill asked for a page of %d records, more than the cap of %d", widest, state.MaxList)
		}
		for other, n := range reads {
			if other != tenantName && other != "" && n > readSlack {
				t.Fatalf("the backfill of %s made %d reads of %s", tenantName, n, other)
			}
		}
		return reads[tenantName], rep.Counts[platform.BackfillThreadAdopted]
	}
	const few, more = 10, 40
	seed(tenant, 0, few)
	base, adopted := dryRun(tenant)
	if adopted != few {
		t.Fatalf("the dry run would adopt %d, want %d", adopted, few)
	}
	// Another workspace's conversations add no reads to this one's.
	seed(otherTenant, 0, 200)
	if got, _ := dryRun(tenant); got > base+readSlack {
		t.Fatalf("with another workspace's 200 conversations the backfill made %d reads, was %d", got, base)
	}
	// More of its own add a fixed number of reads each.
	seed(tenant, few, more)
	grown, adopted := dryRun(tenant)
	if adopted != few+more {
		t.Fatalf("the dry run would adopt %d, want %d", adopted, few+more)
	}
	if per := (grown - base + more - 1) / more; per > perConversation {
		t.Fatalf("each conversation costs %d reads (%d for %d, %d for %d), want at most %d", per, base, few, grown, few+more, perConversation)
	}
}

// perConversation is the most records the backfill reads to decide one
// conversation, and readSlack what the platform's own loops may read
// meanwhile.
const (
	perConversation = 12
	readSlack       = 20
)
