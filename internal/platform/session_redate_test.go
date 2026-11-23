package platform_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
)

// redateRoom is githubRoom on a store the test reads, whose client maps
// octocat to ana.
func redateRoom(t *testing.T) (*platform.Platform, state.Store) {
	t.Helper()
	st := state.NewMemory()
	p, _, _ := githubRoom(t, platform.Backends{State: st}, func(in *platform.ClientInput) {
		in.GitHub.People = map[string]string{"octocat": "ana", "monalisa": "mona"}
	})
	return p, st
}

// A session that was archived and deleted is left: its runs are reported
// as left by the dry run and the apply alike, nothing is written for it,
// and the places after it are still dated.
func TestBackfillLeavesADeletedSessionAndGoesOn(t *testing.T) {
	ctx := t.Context()
	p, st := redateRoom(t)
	pull12, pull13 := "github://acme/handbook/pull/12", "github://acme/handbook/pull/13"
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	a := pastGitHubRun(t, p, "gh-gone-a", "docs", pull12+"?head_sha="+strings.Repeat("a", 40), "octocat", 12, t0, false)
	pastGitHubRun(t, p, "gh-gone-b", "docs", pull13+"?head_sha="+strings.Repeat("b", 40), "octocat", 13, t0.Add(time.Hour), false)
	if _, err := p.BackfillSessions(ctx, tenant, true); err != nil {
		t.Fatal(err)
	}
	// Both carry the time an earlier backfill ran, and pull 12's was
	// archived and deleted since.
	migrated := t0.Add(30 * 24 * time.Hour)
	var ids []string
	for _, ts := range []string{"pull/12", "pull/13"} {
		th, ok, err := p.Threads.BySurface(tenant, threads.Surface{Client: "gh", Channel: "acme/handbook", TS: ts})
		if err != nil || !ok {
			t.Fatalf("%s has no session: %v", ts, err)
		}
		if _, err := p.Threads.Redate(tenant, th.ID, migrated, migrated); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, th.ID)
	}
	if _, err := p.Threads.SetArchive(tenant, ids[0], &threads.Archive{By: "ana", At: migrated, DeleteAfter: migrated}); err != nil {
		t.Fatal(err)
	}
	if err := p.Threads.Purge(tenant, ids[0], "ana", migrated); err != nil {
		t.Fatal(err)
	}

	before := stored(t, st)
	dry, err := p.BackfillSessions(ctx, tenant, false)
	if err != nil {
		t.Fatalf("the dry run stopped: %v", err)
	}
	if after := stored(t, st); !reflect.DeepEqual(before, after) {
		t.Fatal("a dry run wrote to the store")
	}
	applied, err := p.BackfillSessions(ctx, tenant, true)
	if err != nil {
		t.Fatalf("the apply stopped: %v", err)
	}
	if !reflect.DeepEqual(applied.Lines, dry.Lines) {
		t.Fatalf("the apply did\n%+v\nthe dry run said\n%+v", applied.Lines, dry.Lines)
	}
	left := false
	for _, l := range applied.Lines {
		switch {
		case l.Subject == a.ID && l.Kind == platform.BackfillRunLeft && strings.Contains(l.Detail, "was deleted"):
			left = true
		case l.Session == ids[0] && l.Kind != platform.BackfillRunLeft:
			t.Fatalf("the deleted session has a line that is not a run left: %+v", l)
		}
	}
	if !left || applied.Counts[platform.BackfillSessionDated] != 1 || applied.Changes() != 1 {
		t.Fatalf("the deleted session's run left: %v; counts %v", left, applied.Counts)
	}
	if th, err := p.Threads.Get(tenant, ids[1]); err != nil || !th.CreatedAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("pull 13's session starts %s, %v; want %s", th.CreatedAt, err, t0.Add(time.Hour))
	}
	after := stored(t, st)
	for ns := range before {
		if !reflect.DeepEqual(keysOf(before[ns], ids[0]), keysOf(after[ns], ids[0])) {
			t.Fatalf("the apply wrote the deleted session's records in %s", ns)
		}
	}
	if again, err := p.BackfillSessions(ctx, tenant, true); err != nil || again.Changes() != 0 {
		t.Fatalf("a second apply did %v, %v", again.Counts, err)
	}
}

// keysOf is the records whose key names id.
func keysOf(recs []state.Record, id string) []state.Record {
	var out []state.Record
	for _, r := range recs {
		if strings.Contains(r.Key, id) {
			out = append(out, r)
		}
	}
	return out
}

// A session whose runs carry no time is reported as not dated, each run,
// and keeps the times it has: it is no change, and the apply does not stop.
func TestBackfillReportsASessionWhoseRunsCarryNoTime(t *testing.T) {
	ctx := t.Context()
	p, _ := redateRoom(t)
	pull12 := "github://acme/handbook/pull/12"
	r := pastGitHubRun(t, p, "gh-zero-a", "docs", pull12+"?head_sha="+strings.Repeat("a", 40), "octocat", 12, time.Time{}, false)
	if !r.CreatedAt.IsZero() {
		t.Skipf("the run index dates a run recorded with no time: %s", r.CreatedAt)
	}
	first, err := p.BackfillSessions(ctx, tenant, true)
	if err != nil || first.Counts[platform.BackfillSessionUndated] != 1 || first.Counts[platform.BackfillSessionDated] != 0 {
		t.Fatalf("the apply did %v, %v", first.Counts, err)
	}
	th, ok, err := p.Threads.BySurface(tenant, threads.Surface{Client: "gh", Channel: "acme/handbook", TS: "pull/12"})
	if err != nil || !ok || th.CreatedAt.IsZero() {
		t.Fatalf("pull 12's session: %+v, %v, %v", th.CreatedAt, ok, err)
	}
	again, err := p.BackfillSessions(ctx, tenant, true)
	if err != nil || again.Changes() != 0 || again.Counts[platform.BackfillSessionUndated] != 1 {
		t.Fatalf("a second apply did %v, %v", again.Counts, err)
	}
	if got, err := p.Threads.Get(tenant, th.ID); err != nil || !got.CreatedAt.Equal(th.CreatedAt) || !got.UpdatedAt.Equal(th.UpdatedAt) {
		t.Fatalf("its times moved: %s, %s; %v", got.CreatedAt, got.UpdatedAt, err)
	}
}

// A message posted in a session after the backfill read it keeps the
// session as active as that message: the backfill dates when it started
// and leaves its last activity at the message.
func TestBackfillNeverDatesASessionBeforeItsLatestMessage(t *testing.T) {
	ctx := t.Context()
	p, _ := redateRoom(t)
	pull12 := "github://acme/handbook/pull/12"
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	pastGitHubRun(t, p, "gh-msg-a", "docs", pull12+"?head_sha="+strings.Repeat("a", 40), "octocat", 12, t0, false)
	if _, err := p.BackfillSessions(ctx, tenant, true); err != nil {
		t.Fatal(err)
	}
	th, ok, err := p.Threads.BySurface(tenant, threads.Surface{Client: "gh", Channel: "acme/handbook", TS: "pull/12"})
	if err != nil || !ok {
		t.Fatal(err)
	}
	// Dated at the time an earlier backfill ran, with a message since.
	migrated, said := t0.Add(30*24*time.Hour), t0.Add(40*24*time.Hour)
	if _, err := p.Threads.Redate(tenant, th.ID, migrated, migrated); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Threads.Post(tenant, th.ID, threads.Message{By: "ana", Name: "Ana", Text: "any news?"}, said); err != nil {
		t.Fatal(err)
	}
	if rep, err := p.BackfillSessions(ctx, tenant, true); err != nil || rep.Counts[platform.BackfillSessionDated] != 1 {
		t.Fatalf("the apply did %v, %v", rep.Counts, err)
	}
	got, err := p.Threads.Get(tenant, th.ID)
	if err != nil || !got.CreatedAt.Equal(t0) || !got.UpdatedAt.Equal(said) {
		t.Fatalf("the session is dated %s, %s, %v; want started %s and active %s", got.CreatedAt, got.UpdatedAt, err, t0, said)
	}
	if again, err := p.BackfillSessions(ctx, tenant, true); err != nil || again.Changes() != 0 {
		t.Fatalf("a second apply did %v, %v", again.Counts, err)
	}
}
