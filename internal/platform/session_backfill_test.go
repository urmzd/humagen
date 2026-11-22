package platform_test

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/session"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
)

// pastGitHubRun records a GitHub run as a release before GitHub sessions
// recorded it, and as production holds them: raised at the pull request's
// address with its head commit, by a login the client maps or not, in no
// thread and no project, with GitHub's own body, and answered.
func pastGitHubRun(t *testing.T, p *platform.Platform, id, agent, at, login string, number int, when time.Time, private bool) runs.Run {
	t.Helper()
	tag := wire.NewEvent(wire.Raise{ID: id, Agent: agent, Tenant: tenant, Origin: "github", Text: "@" + agent + " what changed?", Channel: at, Actor: event.Actor{ID: login}, At: when, Context: map[string]string{
		runs.MetaClient: "gh", "github_event": "issue_comment", "github_owner": "acme", "github_repo": "handbook", "github_sender": login,
	}})
	tag.Source, tag.Type = at, event.TypeGitHubIssueCommentCreated
	tag.Data = event.GitHubIssueCommentCreated{
		Request:   event.Request{Text: "@" + agent + " what changed?", Actor: event.Actor{ID: login}, Mentions: []string{agent}, Context: wire.ContextOf(tag)},
		GitHubRef: event.GitHubRef{Owner: "acme", Repo: "handbook", Number: number, Pull: strings.Contains(at, "/pull/"), Title: "Fix the handbook", Sender: login},
	}
	runID := runtime.RunID(tenant, id)
	if err := p.Runs.Record(runID, tag, 1); err != nil {
		t.Fatal(err)
	}
	if private {
		if err := p.Runs.MarkPrivateContext(tenant, runID); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := p.Runs.End(tenant, runID, runs.Completed, "Done.", "", when.Add(time.Minute)); !ok {
		t.Fatalf("run %s did not end", runID)
	}
	r, err := p.Runs.Get(tenant, runID)
	if err != nil || r.Thread != runID || r.Project != "" || r.Origin != runs.OriginGitHub {
		t.Fatalf("the past run is %+v, %v", r, err)
	}
	return r
}

// stored is every record the backfill could write, by namespace.
func stored(t *testing.T, st state.Store) map[string][]state.Record {
	t.Helper()
	out := map[string][]state.Record{}
	for _, ns := range []string{"threads", "runs.v2", "agent-sessions", "session-audiences"} {
		recs, err := st.Load(context.Background(), ns)
		if err != nil {
			t.Fatal(err)
		}
		out[ns] = recs
	}
	return out
}

// Past work is filed into sessions as a mention files it now. A dry run
// writes nothing and says what an apply does; the apply opens the session
// of each pull request, moves each run's conversation into it oldest first,
// binds the owner's runs to its record, and makes it private to the people
// the client maps the askers to; a second apply writes nothing. Nobody
// reads a run afterwards that they could not read before, and
// conversations from before sessions are left as they are.
func TestBackfillFilesPastGitHubRunsInTheirSessions(t *testing.T) {
	ctx := t.Context()
	st := state.NewMemory()
	p, _, _ := githubRoom(t, platform.Backends{State: st}, func(in *platform.ClientInput) {
		in.GitHub.People = map[string]string{"octocat": "ana", "monalisa": "mona"}
	})
	pull12, pull13 := "github://acme/handbook/pull/12", "github://acme/handbook/pull/13"
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	a := pastGitHubRun(t, p, "gh-past-a", "docs", pull12+"?head_sha="+strings.Repeat("a", 40), "octocat", 12, t0, false)
	b := pastGitHubRun(t, p, "gh-past-b", "docs", pull12+"?head_sha="+strings.Repeat("b", 40), "monalisa", 12, t0.Add(time.Hour), false)
	c := pastGitHubRun(t, p, "gh-past-c", "ops", pull12+"?head_sha="+strings.Repeat("b", 40), "octocat", 12, t0.Add(2*time.Hour), false)
	d := pastGitHubRun(t, p, "gh-past-d", "docs", pull13+"?head_sha="+strings.Repeat("c", 40), "ghost", 13, t0.Add(3*time.Hour), false)
	// Used monalisa's private context: filed with octocat, ana would read it.
	private := pastGitHubRun(t, p, "gh-past-private", "docs", pull12+"?head_sha="+strings.Repeat("b", 40), "monalisa", 12, t0.Add(4*time.Hour), true)
	// Raised at the repository, not an issue or pull request.
	repo := pastGitHubRun(t, p, "gh-past-repo", "docs", "github://acme/handbook", "octocat", 0, t0.Add(5*time.Hour), false)
	// The runtime opened pull 12's record for docs when a ran, and bound it.
	rec, err := p.Sessions.Open(ctx, tenant, session.Open{Parent: pull12, Agent: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Sessions.BindRun(ctx, tenant, rec.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	// A conversation from before sessions, and a Slack direct message's.
	plain, err := p.Threads.Create(tenant, threads.Thread{ID: "th-00112233445566aa", Project: "", Channel: "general", Visibility: threads.Private, CreatedBy: "ana", Title: "Old notes"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	dm, err := p.Threads.Create(tenant, threads.Thread{ID: "th-00112233445566bb", Project: "", Channel: "general", Visibility: threads.Private, CreatedBy: "ana", Title: "A direct message", Surface: &threads.Surface{Client: "slack-app-t0b3qnbk4fl", Channel: "D0CBZBLGLNA", TS: "1760000000.000100"}}, t0)
	if err != nil {
		t.Fatal(err)
	}

	people := []auth.Principal{
		{Subject: "ana", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}},
		{Subject: "mona", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}},
		{Subject: "vic", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}},
	}
	all := []runs.Run{a, b, c, d, private, repo}
	saw := map[string]bool{}
	for _, who := range people {
		for _, r := range all {
			saw[who.Subject+" "+r.ID] = p.Seer(who).RunID(r.ID)
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
	want := map[string]int{
		platform.BackfillSessionOpened:    2, // pull 12 and pull 13
		platform.BackfillRunFiled:         4, // a, b, c and d
		platform.BackfillRunBound:         2, // b and d: a is bound, c is ops's
		platform.BackfillParticipantAdded: 1, // mona; ana started it, ghost maps to nobody
		platform.BackfillRunLeft:          2, // private and repo
		platform.BackfillThreadLeft:       1,
		platform.BackfillSlackThreadLeft:  1,
	}
	for _, l := range dry.Lines {
		t.Logf("%s: %s -> %s (%s)", l.Kind, l.Subject, l.Session, l.Detail)
	}
	if !reflect.DeepEqual(dry.Counts, want) {
		t.Fatalf("the dry run counts %v, want %v", dry.Counts, want)
	}

	applied, err := p.BackfillSessions(ctx, tenant, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(applied.Counts, dry.Counts) || !reflect.DeepEqual(applied.Lines, dry.Lines) {
		t.Fatalf("the apply did %v, the dry run said %v", applied.Counts, dry.Counts)
	}

	th, ok, err := p.Threads.BySurface(tenant, threads.Surface{Client: "gh", Channel: "acme/handbook", TS: "pull/12"})
	switch {
	case err != nil || !ok:
		t.Fatalf("pull 12 has no session: %v", err)
	case th.ID != rec.ID || th.Session == nil || th.Session.Parent != pull12 || th.Session.Agent != "docs":
		t.Fatalf("pull 12's session is %s %+v, want %s", th.ID, th.Session, rec.ID)
	case th.Visibility != threads.Private || th.CreatedBy != "ana" || th.Project != "":
		t.Fatalf("pull 12's session is %s, by %q, in %q", th.Visibility, th.CreatedBy, th.Project)
	case !slices.ContainsFunc(th.Participants, func(pt threads.Participant) bool { return pt.Name == "mona" }):
		t.Fatalf("mona, who asked on pull 12, is not one of %+v", th.Participants)
	}
	for _, r := range []runs.Run{a, b, c} {
		if got, _ := p.Runs.Get(tenant, r.ID); got.Thread != th.ID {
			t.Fatalf("run %s is in %q, want %q", r.ID, got.Thread, th.ID)
		}
		if old, _ := p.Runs.ConversationSummary(tenant, r.ID); old.Turns != 0 {
			t.Fatalf("run %s's own conversation is still listed: %+v", r.ID, old)
		}
	}
	turns, err := p.Runs.Conversation(tenant, th.ID)
	if err != nil || len(turns) != 3 || turns[0].ID != a.ID || turns[2].ID != c.ID {
		t.Fatalf("pull 12's turns = %v, %v", turns, err)
	}
	if sum, err := p.Runs.ConversationSummary(tenant, th.ID); err != nil || sum.Turns != 3 || !slices.Equal(sum.Agents, []string{"docs", "ops"}) {
		t.Fatalf("pull 12's summary = %+v, %v", sum, err)
	}
	if bound, err := p.Sessions.Runs(ctx, tenant, th.ID, 10); err != nil || !slices.Equal(bound, []string{a.ID, b.ID}) {
		t.Fatalf("pull 12's session binds %v, %v", bound, err)
	}
	for _, r := range []runs.Run{private, repo} {
		if got, _ := p.Runs.Get(tenant, r.ID); got.Thread != r.ID {
			t.Fatalf("run %s, which is left, moved to %q", r.ID, got.Thread)
		}
	}
	for _, kept := range []threads.Thread{plain, dm} {
		if got, err := p.Threads.Get(tenant, kept.ID); err != nil || got.Session != nil || got.Version != kept.Version {
			t.Fatalf("conversation %s changed: %+v, %v", kept.ID, got, err)
		}
	}
	if got, ok, err := p.Threads.BySurface(tenant, *dm.Surface); err != nil || !ok || got.ID != dm.ID {
		t.Fatalf("the direct message's replies reach %q, %v", got.ID, err)
	}

	// Listed for the people who asked, as Recent and Sessions list it.
	for _, who := range people[:2] {
		listed, err := p.ListSessions(ctx, who, "", "", "", "")
		if err != nil || !slices.ContainsFunc(listed, func(s platform.SessionSummary) bool { return s.ID == th.ID }) {
			t.Fatalf("%s's sessions do not list pull 12's: %v", who.Subject, err)
		}
	}
	// Nobody reads a run they could not read before.
	for _, who := range people {
		for _, r := range all {
			if now := p.Seer(who).RunID(r.ID); now && !saw[who.Subject+" "+r.ID] {
				t.Fatalf("%s reads run %s, which they could not read before", who.Subject, r.ID)
			}
		}
	}
	vic := people[2]
	if p.SeesSession(vic, th.ID) || p.Seer(vic).RunID(a.ID) {
		t.Fatal("vic, who asked nowhere, reads pull 12's session")
	}
	pull13Session := session.Key{Tenant: tenant, Parent: pull13, Agent: "docs"}.ID()
	if p.SeesSession(people[0], pull13Session) || p.Seer(people[0]).RunID(d.ID) {
		t.Fatal("ana reads pull 13's session, where only a login mapped to nobody asked")
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
	if again.Changes() != 0 || again.Counts[platform.BackfillSessionFound] != 2 || again.Counts[platform.BackfillRunAlreadyFiled] != 4 {
		t.Fatalf("a second apply did %v", again.Counts)
	}
}
