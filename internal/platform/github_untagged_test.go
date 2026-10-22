package platform_test

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/address"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/fiber/pkg/signature"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
)

// raiser is a GitHub connector of the test's own: it raises the tags it is
// handed, as fiber's GitHub connector raises a comment, and keeps what it is
// given to deliver.
type raiser struct {
	surfaceFake
	tags chan envelope.Tag
}

func (r *raiser) Ingest(ctx context.Context, out chan<- envelope.Tag) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case tag := <-r.tags:
			select {
			case out <- tag:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func (r *raiser) raise(t *testing.T, tag envelope.Tag) {
	t.Helper()
	select {
	case r.tags <- tag:
	case <-time.After(5 * time.Second):
		t.Fatal("the connector's trigger is not running")
	}
}

// delivered reports whether anything was delivered for the tag's run.
func (r *raiser) delivered(tag envelope.Tag) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.sent {
		if d.e.RunID == runIDOf(tag) {
			return true
		}
	}
	return false
}

func runIDOf(tag envelope.Tag) string { return runtime.RunID(tenant, tag.ID) }

// issue is the issue the comments in these tests are on.
var issue = address.MustParse("github://acme/handbook/issues/7")

// fromGitHub is a comment on the issue as fiber's GitHub connector raises
// it: for an agent it names, or, addressed to the app and naming none, for
// the name warp's connectors are given for a message that names no agent.
// It says things about its tenant and where it is filed that stamp writes
// over.
func fromGitHub(id, agent, text string) envelope.Tag {
	return envelope.Tag{
		ID: id, Agent: agent, Tenant: "globex", Origin: "github", Text: text, Source: issue,
		Actor: envelope.Actor{ID: "octocat"}, At: time.Now(),
		Meta: map[string]string{
			"github_event": "issue_comment", "github_owner": "acme", "github_repo": "handbook",
			"github_number": "7", "github_kind": "issue", "github_sender": "octocat",
			runs.MetaProject: "Vault", runs.MetaChannel: "deals",
		},
	}
}

// githubRoom is a platform whose GitHub client gh is answered by a
// connector the test drives, homed in Launch. Nothing answers an untagged
// message in the workspace unless the test says so.
func githubRoom(t *testing.T, b platform.Backends, mutate func(*platform.ClientInput)) (*platform.Platform, *raiser, *logs) {
	t.Helper()
	f := &fakes{}
	c := &cast{says: map[string][][]saigetypes.Delta{"docs": {say("Docs here.")}, "ops": {say("Ops here.")}}}
	cfg := f.config()
	cfg.Backends = b
	said := &logs{}
	cfg.Log = slog.New(slog.NewTextHandler(said, nil))
	p := stage(t, c, cfg)
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}})
	for _, name := range []string{"Launch", "Vault"} {
		if _, err := p.CreateProject(t.Context(), tenant, ada, platform.ProjectInput{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.CreateChannel(t.Context(), tenant, "Launch", ada, platform.ChannelInput{Name: "triage", Default: "ops"}); err != nil {
		t.Fatal(err)
	}
	in := platform.ClientInput{
		Client: clients.Client{
			ID: "gh", Name: "gh", Surface: clients.GitHub, Enabled: true,
			Identity: clients.Identity{Owners: []string{"acme"}}, DefaultAgent: "docs", Agents: []string{"ops"},
			GitHub: &clients.GitHubOptions{Login: "warp[bot]"},
			Home:   &clients.Home{Project: "Launch"},
		},
		Token: clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	}
	if mutate != nil {
		mutate(&in)
	}
	create(t, p, tenant, in)
	r := &raiser{tags: make(chan envelope.Tag)}
	if err := p.SeatPeer(tenant, "gh", r); err != nil {
		t.Fatal(err)
	}
	return p, r, said
}

func answered(t *testing.T, p *platform.Platform, r *raiser, tag envelope.Tag) runs.Run {
	t.Helper()
	run := await(t, p, runIDOf(tag), runs.Completed)
	eventually(t, "the answer reached the issue", func() bool {
		_, to, ok := r.answer(run.ID)
		return ok && to.String() == issue.String()
	})
	return run
}

// A GitHub comment addressed to the app that names no agent is answered as
// an untagged Slack message outside a bound channel is: by the client's
// default, in the client's home, whatever the connector said about where it
// is filed. GitHub logins are mapped to nobody, so it acts for nobody. A
// comment that names an agent is that agent's.
func TestAGitHubCommentToTheAppIsTheClientsDefaults(t *testing.T) {
	eachBackend(t, func(t *testing.T, b platform.Backends, _ func() platform.Backends) {
		p, r, _ := githubRoom(t, b, nil)
		untagged := fromGitHub("gh-untagged", platform.Untagged, "what changed?")
		r.raise(t, untagged)
		run := answered(t, p, r, untagged)
		if run.Agent != "docs" || run.Project != "Launch" || run.Channel != projects.General || run.Tenant != tenant {
			t.Fatalf("the untagged comment is %s's, filed at %s %q #%s", run.Agent, run.Tenant, run.Project, run.Channel)
		}
		if run.Actor.User != "" || run.Actor.ID != "octocat" || run.Tag().Meta[runs.MetaClient] != "gh" || run.Origin != "github" {
			t.Fatalf("the untagged comment's run is %+v for %q", run.Actor, cmp.Or(run.Actor.User, "nobody"))
		}
		tagged := fromGitHub("gh-tagged", "ops", "deploy it")
		r.raise(t, tagged)
		if run := answered(t, p, r, tagged); run.Agent != "ops" || run.Project != "Launch" {
			t.Fatalf("a comment naming ops is %s's, filed at %q", run.Agent, run.Project)
		}
	})
}

// With no default of its own, the client's home channel and its chain
// answer, as for a thread there.
func TestAnUntaggedGitHubCommentFallsToTheHomesChain(t *testing.T) {
	p, r, _ := githubRoom(t, platform.Backends{}, func(in *platform.ClientInput) {
		in.DefaultAgent = ""
		in.Home = &clients.Home{Project: "Launch", Channel: "triage"}
	})
	tag := fromGitHub("gh-home", platform.Untagged, "what changed?")
	r.raise(t, tag)
	if run := answered(t, p, r, tag); run.Agent != "ops" || run.Project != "Launch" || run.Channel != "triage" {
		t.Fatalf("the untagged comment is %s's, filed at %q #%s", run.Agent, run.Project, run.Channel)
	}
}

// With no default anywhere, an untagged comment starts nothing and nothing
// is posted. A default the client may not route to is refused, never swapped
// for one it may.
func TestAnUntaggedGitHubCommentNobodyMayAnswerStartsNothing(t *testing.T) {
	for name, c := range map[string]struct {
		mutate func(*platform.ClientInput)
		// why is what the platform says when it lets the comment go, and
		// agent one the client may route to, which answers only when named.
		why, agent string
	}{
		"no default anywhere": {
			mutate: func(in *platform.ClientInput) { in.DefaultAgent = "" },
			why:    "no default answers an untagged message", agent: "ops",
		},
		"a default the client may not route to": {
			mutate: func(in *platform.ClientInput) {
				in.DefaultAgent, in.Agents = "", []string{"docs"}
				in.Home = &clients.Home{Project: "Launch", Channel: "triage"}
			},
			why: "client may not route to that agent", agent: "docs",
		},
	} {
		t.Run(name, func(t *testing.T) {
			p, r, said := githubRoom(t, platform.Backends{}, c.mutate)
			tag := fromGitHub("gh-nobody", platform.Untagged, "what changed?")
			r.raise(t, tag)
			eventually(t, "the comment was let go", func() bool { return strings.Contains(said.String(), c.why) })
			// Anything the comment started would come through the same
			// trigger before a comment raised after it is answered.
			after := fromGitHub("gh-after", c.agent, "and now?")
			r.raise(t, after)
			answered(t, p, r, after)
			if _, err := p.Runs.Get(tenant, runIDOf(tag)); err == nil || r.delivered(tag) {
				t.Fatal("an untagged comment nobody may answer started a run or posted")
			}
			if got := p.Runs.List(tenant, 10); len(got) != 1 || got[0].Agent != c.agent {
				t.Fatalf("the runs are %+v, want only the named one", got)
			}
		})
	}
}

// Through fiber's own GitHub connector: a signed comment that mentions the
// app's login and names no agent is the client's default's, answered on the
// issue, and one that mentions neither the app nor an agent starts nothing.
func TestASignedCommentToTheAppIsAnsweredByTheClientsDefault(t *testing.T) {
	f := &fakes{}
	c := &cast{says: map[string][][]saigetypes.Delta{"docs": {say("Docs here.")}, "ops": {say("Ops here.")}}}
	p := stage(t, c, f.config())
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}})
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{
			ID: "gh", Name: "gh", Surface: clients.GitHub, Enabled: true,
			Identity: clients.Identity{Owners: []string{"acme"}}, DefaultAgent: "docs",
			GitHub: &clients.GitHubOptions{Login: "warp[bot]"},
		},
		Token: clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	})
	ingress := p.IngressHandler()
	post := func(delivery, text string) {
		t.Helper()
		body := `{"action":"created","repository":{"name":"handbook","owner":{"login":"acme"}},` +
			`"sender":{"login":"octocat","type":"User"},"issue":{"number":7},"comment":{"id":91,"body":"` + text + `"}}`
		mac := hmac.New(sha256.New, []byte("secret-gh"))
		mac.Write([]byte(body))
		r := httptest.NewRequest(http.MethodPost, "/connectors/github", strings.NewReader(body))
		r.Header.Set(signature.HeaderGitHubSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
		r.Header.Set("X-GitHub-Event", "issue_comment")
		r.Header.Set("X-GitHub-Delivery", delivery)
		w := httptest.NewRecorder()
		ingress.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("the comment answered %d", w.Code)
		}
	}
	post("to-nobody", "@octocat can you look?")
	post("to-the-app", "@warp what changed?")
	run := await(t, p, runtime.RunID(tenant, "to-the-app"), runs.Completed)
	if run.Agent != "docs" || run.Text != "what changed?" || run.Actor.User != "" {
		t.Fatalf("the comment to the app is %s's, %q, for %q", run.Agent, run.Text, run.Actor.User)
	}
	eventually(t, "the answer is on the issue", func() bool {
		cs := f.githubFor("token-gh").Comments()
		return len(cs) == 1 && strings.Contains(cs[0].Body, "Docs here.")
	})
	if got := p.Runs.List(tenant, 10); len(got) != 1 {
		t.Fatalf("%d runs, want one", len(got))
	}
}
