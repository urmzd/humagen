package platform_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/signature"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// mentionAt is a mention as fiber's GitHub connector raises it: at the
// issue's or pull request's address, by a GitHub login.
func mentionAt(id, agent, at, login string) event.Envelope {
	tag := fromGitHub(id, agent, "what changed?")
	tag.Source, tag.Channel = at, at
	tag.Data = event.MessagePosted{Request: event.Request{Text: "what changed?", Actor: event.Actor{ID: login}, Mentions: []string{agent}, Context: wire.ContextOf(tag)}}
	return tag
}

// githubSessionRoom is githubRoom whose client maps octocat to ana and
// monalisa to mona.
func githubSessionRoom(t *testing.T) (*platform.Platform, *raiser) {
	t.Helper()
	p, r, said := githubRoomWith(t, platform.Backends{}, func(in *platform.ClientInput) {
		in.GitHub.People = map[string]string{"octocat": "ana", "monalisa": "mona"}
	}, func(cfg *platform.Config) { cfg.Policy = operators(t, "ana", "mona") })
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(said.String())
		}
	})
	return p, r
}

func filedRun(t *testing.T, p *platform.Platform, r *raiser, tag event.Envelope) (runs.Run, threads.Thread) {
	t.Helper()
	run := await(t, p, runIDOf(tag), runs.Completed)
	eventually(t, "the answer reached GitHub at the address it was asked at", func() bool {
		_, to, ok := r.answer(run.ID)
		return ok && to.String() == tag.Channel
	})
	th, err := p.Threads.Get(tenant, run.Thread)
	if err != nil || th.Session == nil {
		t.Fatalf("run %s is in thread %q, not a session: %v", run.ID, run.Thread, err)
	}
	return run, th
}

// A mention on a pull request opens its session, keyed by the pull request
// whatever its head commit; a later mention there continues it and adds
// the person who asked; another pull request and an issue each open their
// own. The session is private to the people who asked, and lists for them.
func TestAGitHubPullRequestIsOneSessionForThePeopleWhoAsked(t *testing.T) {
	p, r := githubSessionRoom(t)
	pull := "github://acme/handbook/pull/12"
	first := mentionAt("gh-pr-1", "docs", pull+"?head_sha="+strings.Repeat("a", 40), "octocat")
	r.raise(t, first)
	run, th := filedRun(t, p, r, first)
	switch {
	case th.Session.Parent != pull || th.Session.Agent != "docs" || th.Session.Label != "":
		t.Fatalf("the session is %+v, want docs's under %s", th.Session, pull)
	case th.Visibility != threads.Private || th.CreatedBy != "ana" || th.Project != "Launch" || th.Channel != "general":
		t.Fatalf("the session is %s, by %q, filed at %q #%s", th.Visibility, th.CreatedBy, th.Project, th.Channel)
	case th.Surface == nil || th.Surface.Client != "gh" || th.Surface.Channel != "acme/handbook" || th.Surface.TS != "pull/12":
		t.Fatalf("the session's surface is %+v", th.Surface)
	case th.Title != "acme/handbook#12":
		t.Fatalf("the session is titled %q", th.Title)
	case run.Origin != runs.OriginGitHub || run.Actor.User != "ana":
		t.Fatalf("the run is %s's from %s", run.Actor.User, run.Origin)
	}
	rec, err := p.Sessions.Get(t.Context(), tenant, th.ID)
	if err != nil || rec.Key.Parent != pull {
		t.Fatalf("the session record = %+v, %v", rec, err)
	}

	again := mentionAt("gh-pr-2", "docs", pull+"?head_sha="+strings.Repeat("b", 40), "monalisa")
	r.raise(t, again)
	if run, th2 := filedRun(t, p, r, again); th2.ID != th.ID || run.Thread != th.ID {
		t.Fatalf("a second mention on the pull request is in %q, want %q", th2.ID, th.ID)
	}
	if bound, err := p.Sessions.Runs(t.Context(), tenant, th.ID, 10); err != nil || !slices.Equal(bound, []string{runIDOf(first), runIDOf(again)}) {
		t.Fatalf("the session's runs = %v, %v", bound, err)
	}
	// Another agent asked on the same pull request answers in its session.
	ops := mentionAt("gh-pr-ops", "ops", pull+"?head_sha="+strings.Repeat("b", 40), "octocat")
	r.raise(t, ops)
	if run := await(t, p, runIDOf(ops), runs.Completed); run.Thread != th.ID {
		t.Fatalf("ops's answer on the pull request is in %q, want %q", run.Thread, th.ID)
	}

	other := mentionAt("gh-pr-3", "docs", "github://acme/handbook/pull/13?head_sha="+strings.Repeat("c", 40), "octocat")
	r.raise(t, other)
	if _, th3 := filedRun(t, p, r, other); th3.ID == th.ID || th3.Session.Parent != "github://acme/handbook/pull/13" {
		t.Fatalf("another pull request is in %q under %s", th3.ID, th3.Session.Parent)
	}
	onIssue := mentionAt("gh-issue-7", "docs", issue.String(), "octocat")
	r.raise(t, onIssue)
	if _, th4 := filedRun(t, p, r, onIssue); th4.Session.Parent != issue.String() || th4.Surface.TS != "issues/7" {
		t.Fatalf("the issue's session is under %s on %+v", th4.Session.Parent, th4.Surface)
	}

	ana := auth.Principal{Subject: "ana", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
	mona := auth.Principal{Subject: "mona", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
	vic := auth.Principal{Subject: "vic", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
	if !p.SeesSession(ana, th.ID) || !p.SeesSession(mona, th.ID) {
		t.Fatal("a person who asked on the pull request cannot read its session")
	}
	if p.SeesSession(vic, th.ID) {
		t.Fatal("a person who did not ask on the pull request reads its session")
	}
	listed := func(who auth.Principal, mine bool) []string {
		t.Helper()
		page, _, err := p.SessionsPage(t.Context(), who, platform.SessionQuery{Roots: true, Mine: mine}, 50, "")
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, s := range page {
			ids = append(ids, s.ID)
			if s.ID == th.ID && (s.Conversation.Origin != runs.OriginGitHub || s.Parent != pull) {
				t.Fatalf("the listed session is from %s under %s", s.Conversation.Origin, s.Parent)
			}
		}
		return ids
	}
	if ids := listed(ana, true); !slices.Contains(ids, th.ID) || len(ids) != 3 {
		t.Fatalf("ana's sessions = %v, want the two pull requests and the issue", ids)
	}
	if ids := listed(mona, true); !slices.Equal(ids, []string{th.ID}) {
		t.Fatalf("mona's sessions = %v, want the pull request she asked on", ids)
	}
	if ids := listed(vic, false); len(ids) != 0 {
		t.Fatalf("vic lists %v", ids)
	}
}

// A login the client maps to nobody adds nobody: the run is still filed in
// the session, which nobody reads by it, and the next mapped asker is added.
func TestAnUnmappedGitHubLoginAddsNobodyToTheSession(t *testing.T) {
	p, r := githubSessionRoom(t)
	pull := "github://acme/handbook/pull/21"
	stranger := mentionAt("gh-unmapped", "docs", pull, "hubber")
	r.raise(t, stranger)
	_, th := filedRun(t, p, r, stranger)
	if th.CreatedBy != "" || slices.ContainsFunc(th.Participants, func(pt threads.Participant) bool { return pt.Kind == threads.Person }) {
		t.Fatalf("an unmapped login's session is %q's with %+v", th.CreatedBy, th.Participants)
	}
	asked := mentionAt("gh-mapped", "docs", pull, "octocat")
	r.raise(t, asked)
	_, th = filedRun(t, p, r, asked)
	ana := auth.Principal{Subject: "ana", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
	if !p.SeesSession(ana, th.ID) {
		t.Fatalf("ana asked and cannot read the session: %+v", th.Participants)
	}
}

// Through the real connector: a comment mentioning the app on an issue is
// filed in the issue's session, titled as GitHub titles the issue.
func TestAGitHubCommentThroughTheConnectorOpensItsSession(t *testing.T) {
	cfg := (&fakes{}).config()
	cfg.Policy = operators(t, "ana")
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"reviewer": {say("Looked.")}}}, cfg)
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{ID: "gh", Name: "GitHub", Surface: clients.GitHub, Enabled: true, Identity: clients.Identity{Owners: []string{"acme"}, Installation: "101"}, DefaultAgent: "reviewer", GitHub: &clients.GitHubOptions{Login: "humagen[bot]", People: map[string]string{"octocat": "ana"}}},
		Token:  clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	})
	body := `{"action":"created","installation":{"id":101},"repository":{"full_name":"acme/repo","name":"repo","owner":{"login":"acme"}},` +
		`"sender":{"login":"octocat","type":"User"},"issue":{"number":7,"title":"Fix the build"},` +
		`"comment":{"id":91,"body":"@humagen review this","user":{"login":"octocat"},"author_association":"MEMBER"}}`
	mac := hmac.New(sha256.New, []byte("secret-gh"))
	mac.Write([]byte(body))
	req := httptest.NewRequest(http.MethodPost, "/connectors/github", strings.NewReader(body))
	req.Header.Set(signature.HeaderGitHubSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("X-GitHub-Event", "issue_comment")
	req.Header.Set("X-GitHub-Delivery", "d-session")
	w := httptest.NewRecorder()
	p.IngressHandler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("the comment answered %d", w.Code)
	}
	run := await(t, p, runtime.RunID(tenant, "d-session"), runs.Completed)
	th, err := p.Threads.Get(tenant, run.Thread)
	if err != nil || th.Session == nil {
		t.Fatalf("the comment's run is in %q: %v", run.Thread, err)
	}
	if th.Session.Parent != "github://acme/repo/issues/7" || th.Title != "Fix the build" || th.CreatedBy != "ana" || th.Visibility != threads.Private {
		t.Fatalf("the comment's session = %+v titled %q by %q, %s", th.Session, th.Title, th.CreatedBy, th.Visibility)
	}
}
