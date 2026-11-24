package platform_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/session"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
)

// A GitHub run that could not be filed in its pull request's session (its
// channel was archived) stays its own conversation, while the runtime keeps
// the pull request's record with the run bound to it and no conversation
// at the record's id. Through the API that record is not found: naming the
// pull request, posting under the record's id and starting a child of it
// each answer not found and write nothing. A later comment on the pull
// request by another login opens the session at the record, private to
// whom that login maps to; the earlier run stays in its own conversation,
// read by whom it was read by, and the session shows only the later run.
func TestAGitHubRecordWithoutAConversationIsOpenedOnlyByItsSurface(t *testing.T) {
	ctx := t.Context()
	st := state.NewMemory()
	p, r, _ := githubRoomWith(t, platform.Backends{State: st}, func(in *platform.ClientInput) {
		in.GitHub.People = map[string]string{"octocat": "ana", "monalisa": "mona"}
		in.Home = &clients.Home{Project: "Launch", Channel: "triage"}
	}, func(cfg *platform.Config) { cfg.Policy = operators(t, "ana", "mona", "vic") })
	workIn(t, p, "Launch", auth.Principal{Subject: "ana"}, auth.Principal{Subject: "mona"}, auth.Principal{Subject: "vic"})
	if _, err := p.ArchiveChannel(tenant, "Launch", "triage", ada); err != nil {
		t.Fatal(err)
	}
	pull := "github://acme/handbook/pull/31"
	first := mentionAt("gh-own-1", "docs", pull+"?head_sha="+strings.Repeat("a", 40), "octocat")
	r.raise(t, first)
	past := await(t, p, runIDOf(first), runs.Completed)
	if past.Thread != past.ID {
		t.Fatalf("the first run is in %q, want its own conversation", past.Thread)
	}
	recs, err := p.Sessions.List(ctx, tenant, session.Filter{Parent: pull, Agent: "docs"})
	if err != nil || len(recs) != 1 {
		t.Fatalf("the pull request's records = %v, %v", recs, err)
	}
	rec := recs[0].ID
	bound := func() []string {
		t.Helper()
		ids, err := p.Sessions.Runs(ctx, tenant, rec, 10)
		if err != nil {
			t.Fatal(err)
		}
		return ids
	}
	if got := bound(); !slices.Equal(got, []string{past.ID}) {
		t.Fatalf("the record's runs = %v, want the first run", got)
	}
	if _, err := p.Threads.Get(tenant, rec); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("a conversation is at the record: %v", err)
	}

	ana := placeOperator("ana", tenant)
	mona := placeOperator("mona", tenant)
	vic := placeOperator("vic", tenant)
	readers := func() map[string]bool {
		return map[string]bool{"ana": p.Seer(ana).RunID(past.ID), "mona": p.Seer(mona).RunID(past.ID), "vic": p.Seer(vic).RunID(past.ID)}
	}
	before := readers()
	kept := func() map[string][]state.Record {
		t.Helper()
		out := map[string][]state.Record{}
		for _, ns := range []string{"threads", "agent-sessions", "session-audiences"} {
			got, err := st.Load(ctx, ns)
			if err != nil {
				t.Fatal(err)
			}
			out[ns] = got
		}
		return out
	}
	was := kept()
	ref := &platform.ExternalRef{Type: platform.RefGitHub, Repo: "acme/handbook", Kind: "pull", Number: 31}
	if out, err := p.SendMessages(ctx, vic, sayIn("@docs what did we do here so far?", ref)); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("naming the pull request through the API = %q, created %v, %v; want not found", out.Root, out.Created, err)
	}
	if out, err := p.SendMessagesUnder(ctx, vic, rec, sayIn("@docs so far?", nil)); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("posting under the record = %q, %v; want not found", out.Root, err)
	}
	if v, err := p.StartSession(ctx, vic, platform.SessionInput{Text: "@docs so far?", Parent: rec}); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("a child of the record = %q, %v; want not found", v.ID, err)
	}
	if now := kept(); !reflect.DeepEqual(now, was) {
		t.Fatal("a refused message wrote a conversation, a record or an audience")
	}
	if got := bound(); !slices.Equal(got, []string{past.ID}) {
		t.Fatalf("after the refused messages the record's runs = %v", got)
	}

	// The channel is the client's no longer: the next comment can be filed.
	c, err := p.Clients.Get(tenant, "gh")
	if err != nil {
		t.Fatal(err)
	}
	c.Home = &clients.Home{Project: "Launch"}
	if _, err := p.UpdateClient(ctx, tenant, "gh", "test", platform.ClientInput{Client: c}); err != nil {
		t.Fatal(err)
	}
	r = &raiser{tags: make(chan event.Envelope)}
	if err := p.SeatPeer(tenant, "gh", r); err != nil {
		t.Fatal(err)
	}
	later := mentionAt("gh-own-2", "docs", pull+"?head_sha="+strings.Repeat("b", 40), "monalisa")
	r.raise(t, later)
	run := await(t, p, runIDOf(later), runs.Completed)
	if run.Thread != rec {
		t.Fatalf("the later comment's run is in %q, want the session at the record %q", run.Thread, rec)
	}
	th, err := p.Threads.Get(tenant, rec)
	if err != nil || th.Visibility != threads.Private || th.Surface == nil || th.Surface.Client != "gh" {
		t.Fatalf("the session at the record = %+v, %v", th, err)
	}
	var people []string
	for _, pt := range th.Participants {
		if pt.Kind == threads.Person {
			people = append(people, pt.Name)
		}
	}
	if !slices.Equal(people, []string{"mona"}) {
		t.Fatalf("the session's people = %v, want who asked in it", people)
	}
	if got := bound(); !slices.Equal(got, []string{past.ID, run.ID}) {
		t.Fatalf("the record's runs = %v", got)
	}
	if again, err := p.Runs.Get(tenant, past.ID); err != nil || again.Thread != past.ID {
		t.Fatalf("the first run moved to %q: %v", again.Thread, err)
	}
	if now := readers(); !reflect.DeepEqual(now, before) {
		t.Fatalf("who reads the first run = %v, was %v", now, before)
	}
	if p.SeesSession(vic, rec) || p.SeesSession(ana, rec) || !p.SeesSession(mona, rec) {
		t.Fatal("the session is read by someone other than who asked in it")
	}
	view, err := p.ReadSession(ctx, mona, rec, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range view.Messages {
		if m.Run != nil && m.Run.ID != run.ID {
			t.Fatalf("the session shows run %s", m.Run.ID)
		}
	}
	if out, err := p.SendMessages(ctx, vic, sayIn("@docs and now?", ref)); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("naming the pull request through the API after the session opened = %q, %v; want not found", out.Root, err)
	}
}
