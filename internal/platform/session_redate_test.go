package platform_test

import (
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/session"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
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

// liveMention opens pull 13's session as a mention does now, at opened,
// and records the mention's run, asked at when and filed in it. The run is
// still queued.
func liveMention(t *testing.T, p *platform.Platform, when, opened time.Time) (string, string) {
	t.Helper()
	ctx := t.Context()
	pull13 := "github://acme/handbook/pull/13"
	rec, err := p.Sessions.Open(ctx, tenant, session.Open{Parent: pull13, Agent: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Threads.Create(tenant, threads.Thread{
		ID: rec.ID, Channel: runs.DefaultChannel, Title: "Fix the handbook", Visibility: threads.Private, CreatedBy: "ana", Default: "docs", Agents: []string{"docs"},
		Surface: &threads.Surface{Client: "gh", Channel: "acme/handbook", TS: "pull/13"},
		Session: &threads.SessionIdentity{Agent: "docs", Parent: pull13},
	}, opened); err != nil {
		t.Fatal(err)
	}
	at := pull13 + "?head_sha=" + strings.Repeat("c", 40)
	context := map[string]string{runs.MetaClient: "gh", "github_event": "issue_comment", "github_owner": "acme", "github_repo": "handbook", "github_sender": "octocat"}
	maps.Copy(context, map[string]string{wire.MetaSession: rec.ID})
	tag := wire.NewEvent(wire.Raise{ID: "gh-live", Agent: "docs", Tenant: tenant, Origin: "github", Text: "@docs what changed?", Channel: at, Actor: event.Actor{ID: "octocat"}, At: when, Context: context})
	tag.Source, tag.Type = at, event.TypeGitHubIssueCommentCreated
	tag.Data = event.GitHubIssueCommentCreated{
		Request:   event.Request{Text: "@docs what changed?", Actor: event.Actor{ID: "octocat"}, Mentions: []string{"docs"}, Context: wire.ContextOf(tag)},
		GitHubRef: event.GitHubRef{Owner: "acme", Repo: "handbook", Number: 13, Pull: true, Title: "Fix the handbook", Sender: "octocat"},
	}
	runID := runtime.RunID(tenant, "gh-live")
	if err := p.Runs.Record(runID, tag, 1); err != nil {
		t.Fatal(err)
	}
	if err := p.Sessions.BindRun(ctx, tenant, rec.ID, runID); err != nil {
		t.Fatal(err)
	}
	if r, err := p.Runs.Get(tenant, runID); err != nil || r.Thread != rec.ID || r.Status.Terminal() {
		t.Fatalf("the live run is %+v, %v", r, err)
	}
	return rec.ID, runID
}

// A session a mention just opened is left while its run is queued, and
// after it ends: the mention's tag is stamped before its session is
// opened, and the servers keep running while the backfill does.
func TestBackfillLeavesASessionWhoseMentionIsStillInFlight(t *testing.T) {
	ctx := t.Context()
	p, st := redateRoom(t)
	asked := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	id, runID := liveMention(t, p, asked, asked.Add(200*time.Millisecond))
	before, err := p.Threads.Get(tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	was := stored(t, st)
	dry, err := p.BackfillSessions(ctx, tenant, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(was, stored(t, st)) {
		t.Fatal("a dry run wrote to the store")
	}
	if dry.Counts[platform.BackfillSessionDated] != 0 || dry.Counts[platform.BackfillSessionUndated] != 0 || dry.Changes() != 0 {
		t.Fatalf("the dry run would date a session a mention opened, or reports it: %+v", dry.Lines)
	}
	if rep, err := p.BackfillSessions(ctx, tenant, true); err != nil || rep.Changes() != 0 {
		t.Fatalf("the apply did %v, %v", rep.Counts, err)
	}
	if _, ok := p.Runs.End(tenant, runID, runs.Completed, "Done.", "", asked.Add(time.Minute)); !ok {
		t.Fatal("the run did not end")
	}
	if rep, err := p.BackfillSessions(ctx, tenant, true); err != nil || rep.Changes() != 0 {
		t.Fatalf("the apply after the run ended did %v, %v", rep.Counts, err)
	}
	after, err := p.Threads.Get(tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	if !after.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("the mention's session started %s and now starts %s", before.CreatedAt, after.CreatedAt)
	}
}

// An archived session that is not deleted is dated from its runs, as any
// other, and keeps its archive, visibility, owner and version.
func TestBackfillDatesAnArchivedSession(t *testing.T) {
	ctx := t.Context()
	p, _ := redateRoom(t)
	pull12 := "github://acme/handbook/pull/12"
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	pastGitHubRun(t, p, "gh-arch-a", "docs", pull12+"?head_sha="+strings.Repeat("a", 40), "octocat", 12, t0, false)
	if _, err := p.BackfillSessions(ctx, tenant, true); err != nil {
		t.Fatal(err)
	}
	th, ok, err := p.Threads.BySurface(tenant, threads.Surface{Client: "gh", Channel: "acme/handbook", TS: "pull/12"})
	if err != nil || !ok {
		t.Fatalf("pull 12 has no session: %v", err)
	}
	migrated := t0.Add(30 * 24 * time.Hour)
	if _, err := p.Threads.Redate(tenant, th.ID, migrated, migrated); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Threads.SetArchive(tenant, th.ID, &threads.Archive{By: "ana", At: migrated, DeleteAfter: migrated.Add(threads.DeletionDelay)}); err != nil {
		t.Fatal(err)
	}
	before, err := p.Threads.Get(tenant, th.ID)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := p.BackfillSessions(ctx, tenant, true)
	if err != nil || rep.Counts[platform.BackfillSessionDated] != 1 {
		t.Fatalf("the apply did %v, %v", rep.Counts, err)
	}
	after, err := p.Threads.Get(tenant, th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.CreatedAt.Equal(t0) {
		t.Fatalf("the archived session starts %s; want %s", after.CreatedAt, t0)
	}
	if !reflect.DeepEqual(after.Archive, before.Archive) || after.Visibility != before.Visibility || after.Session.Agent != before.Session.Agent || after.Version != before.Version {
		t.Fatalf("dating changed more than the times: %+v -> %+v", before, after)
	}
	if again, err := p.BackfillSessions(ctx, tenant, true); err != nil || again.Changes() != 0 {
		t.Fatalf("a second apply did %v, %v", again.Counts, err)
	}
}

// A session dated less than a second after its work ended reads so in the
// dated line: its old times keep their fraction of a second.
func TestBackfillReportsTimesToTheSubSecond(t *testing.T) {
	ctx := t.Context()
	p, _ := redateRoom(t)
	pull12 := "github://acme/handbook/pull/12"
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	pastGitHubRun(t, p, "gh-sub-a", "docs", pull12+"?head_sha="+strings.Repeat("a", 40), "octocat", 12, t0, false)
	if _, err := p.BackfillSessions(ctx, tenant, true); err != nil {
		t.Fatal(err)
	}
	th, ok, err := p.Threads.BySurface(tenant, threads.Surface{Client: "gh", Channel: "acme/handbook", TS: "pull/12"})
	if err != nil || !ok {
		t.Fatalf("pull 12 has no session: %v", err)
	}
	// Opened half a second after its run ended at t0 plus a minute.
	opened := t0.Add(time.Minute + 500*time.Millisecond)
	if _, err := p.Threads.Redate(tenant, th.ID, opened, opened); err != nil {
		t.Fatal(err)
	}
	dry, err := p.BackfillSessions(ctx, tenant, false)
	if err != nil || dry.Counts[platform.BackfillSessionDated] != 1 {
		t.Fatalf("the dry run did %v, %v", dry.Counts, err)
	}
	const want = "started 2026-10-01T09:00:00Z, last active 2026-10-01T09:01:00Z; was started 2026-10-01T09:01:00.5Z, last active 2026-10-01T09:01:00.5Z"
	for _, l := range dry.Lines {
		if l.Kind == platform.BackfillSessionDated && l.Detail != want {
			t.Fatalf("the dated line reads %q; want %q", l.Detail, want)
		}
	}
}

// A session an earlier release opened and dated, whose first run never
// reached an ending and is not the mention that opened it, keeps its times
// and is reported as not dated, with the run, in the dry run and the apply.
func TestBackfillReportsASessionWhoseFirstRunHasNotEnded(t *testing.T) {
	ctx := t.Context()
	p, st := redateRoom(t)
	pull12 := "github://acme/handbook/pull/12"
	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	stuck := recordGitHubRun(t, p, "gh-stuck", "docs", pull12+"?head_sha="+strings.Repeat("b", 40), "octocat", 12, t0, nil)
	pastGitHubRun(t, p, "gh-later", "docs", pull12+"?head_sha="+strings.Repeat("a", 40), "octocat", 12, t0.Add(time.Hour), false)
	if _, err := p.BackfillSessions(ctx, tenant, true); err != nil {
		t.Fatal(err)
	}
	th, ok, err := p.Threads.BySurface(tenant, threads.Surface{Client: "gh", Channel: "acme/handbook", TS: "pull/12"})
	if err != nil || !ok {
		t.Fatalf("pull 12 has no session: %v", err)
	}
	if r, err := p.Runs.Get(tenant, stuck); err != nil || r.Status.Terminal() || r.Thread != th.ID {
		t.Fatalf("the first run is %+v, %v", r, err)
	}
	// Dated at the time an earlier backfill ran.
	migrated := t0.Add(60 * 24 * time.Hour)
	if _, err := p.Threads.Redate(tenant, th.ID, migrated, migrated); err != nil {
		t.Fatal(err)
	}
	was := stored(t, st)
	dry, err := p.BackfillSessions(ctx, tenant, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(was, stored(t, st)) {
		t.Fatal("a dry run wrote to the store")
	}
	applied, err := p.BackfillSessions(ctx, tenant, true)
	if err != nil || !reflect.DeepEqual(applied.Lines, dry.Lines) || applied.Changes() != 0 {
		t.Fatalf("the apply did %v, %v; the dry run said %v", applied.Counts, err, dry.Counts)
	}
	var line platform.BackfillLine
	for _, l := range applied.Lines {
		if l.Kind == platform.BackfillSessionUndated {
			line = l
		}
	}
	if line.Session != th.ID || line.Subject != pull12 || line.Detail != "its first run "+stuck+" has not ended" {
		t.Fatalf("the session is reported %+v; lines %+v", line, applied.Lines)
	}
	if got, err := p.Threads.Get(tenant, th.ID); err != nil || !got.CreatedAt.Equal(migrated) || !got.UpdatedAt.Equal(migrated) {
		t.Fatalf("its times moved: %s, %s, %v", got.CreatedAt, got.UpdatedAt, err)
	}
}
