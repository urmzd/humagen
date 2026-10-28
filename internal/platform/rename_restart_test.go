package platform_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

// What a deployment stored before the rename still works after a restart
// on the renamed binary, on memory and on Postgres: a client's references
// to WARP_ variables, once the operator renamed the variables and their
// grants as the start note says; a person's reference under a WARP_ start;
// a run's warp.* tag keys; and the system sender's old name.
func TestWhatWasStoredBeforeTheRenameSurvivesARestart(t *testing.T) {
	eachBackend(t, func(t *testing.T, first platform.Backends, again func() platform.Backends) {
		ctx := t.Context()
		env := func(kv map[string]string) func(string) (string, bool) {
			look := func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
			return func(name string) (string, bool) { return legacy.Lookup(look, name) }
		}
		f := &fakes{}
		before := f.config()
		before.Backends = first
		before.Env = env(map[string]string{"WARP_SLACK_TOKEN_DOCS": "token-docs", "WARP_SLACK_SECRET_DOCS": "secret-docs"})
		before.SecretRefs = map[string][]string{tenant: {"WARP_SLACK_TOKEN_DOCS", "WARP_SLACK_SECRET_DOCS"}}
		p, stop := runningWith(t, before)
		if _, err := p.Agents.Create(ctx, tenant, wire.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead."}, "test"); err != nil {
			t.Fatal(err)
		}
		create(t, p, tenant, byRef(slackClient("docs", "A0DOCS", team, "lead"), "env:WARP_SLACK_TOKEN_DOCS", "env:WARP_SLACK_SECRET_DOCS"))
		if _, err := p.Credentials.Put(tenant, "ana", "issues", credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"}, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: "Launch"}); err != nil {
			t.Fatal(err)
		}
		run := wire.NewEvent(wire.Raise{ID: "run-old", Tenant: tenant, Agent: "lead", Origin: "api", Text: "hello", Context: map[string]string{"warp.project": "Launch"}})
		r, err := p.Invoke(ctx, run)
		if err != nil {
			t.Fatal(err)
		}
		await(t, p, r.ID, runs.Completed)
		// As a release before the rename posted its notes.
		old, _, err := p.Inbox.PostOnce(tenant, "release:0.15.0", inbox.Message{
			Subject: "What's new in warp 0.15.0", From: inbox.From{Kind: "system", Name: "warp"}, Severity: inbox.Info,
			Source: inbox.Source{Project: projects.Root},
			Parts:  []inbox.Part{{Type: inbox.PartText, Text: "- Inbox"}},
		}, time.Now(), inbox.DefaultPostCaps())
		if err != nil {
			t.Fatal(err)
		}
		stop()

		after := f.config()
		after.Backends = again()
		after.Env = env(map[string]string{"HUMAGEN_SLACK_TOKEN_DOCS": "token-docs", "HUMAGEN_SLACK_SECRET_DOCS": "secret-docs"})
		after.SecretRefs = map[string][]string{tenant: {"HUMAGEN_SLACK_TOKEN_DOCS", "HUMAGEN_SLACK_SECRET_DOCS"}}
		after.CredentialRefs = map[string][]string{tenant: {"env:HUMAGEN_PERSON_{subject}_"}}
		q, _ := runningWith(t, after)
		// The stored client's signing secret is read through its old name.
		eventually(t, "the stored client verifies a webhook signed with its renamed secret", func() bool {
			return signedSlack(q.IngressHandler(), "secret-docs", mentionOf("A0DOCS", team, "Ev1", "C01", "1700000000.000100")) == http.StatusOK
		})
		if code := signedSlack(q.IngressHandler(), "wrong", mentionOf("A0DOCS", team, "Ev2", "C01", "1700000000.000200")); code == http.StatusOK {
			t.Fatal("a webhook signed with another secret was accepted")
		}
		if c, ok := q.Credentials.Get(tenant, "ana", "issues"); !ok || c.Ref != "env:WARP_PERSON_ANA_ISSUES" || !credentials.Owns("env:HUMAGEN_PERSON_{subject}_", "ana", c.Ref) {
			t.Fatalf("ana's reference = %+v, %v", c, ok)
		}
		got, err := q.Runs.Get(tenant, r.ID)
		if err != nil || projects.NameOf(got.Project) != "Launch" {
			t.Fatalf("the run's project = %q, %v", got.Project, err)
		}
		m, err := q.InboxGet(admin, old.ID)
		if err != nil {
			t.Fatal(err)
		}
		if m.From.Name != "warp" || m.From.Display != "Humagen" {
			t.Fatalf("the old release notes are from %+v", m.From)
		}
	})
}

// Only the product's own sender is shown as Humagen; another part of the
// system, such as Limits, keeps its name.
func TestOnlyTheProductsOwnSenderIsShownAsHumagen(t *testing.T) {
	p, _ := runningWith(t, platform.Config{})
	for name, want := range map[string]string{"humagen": "Humagen", "warp": "Humagen", "Limits": ""} {
		m, _, err := p.Inbox.PostOnce(tenant, "k-"+name, inbox.Message{
			Subject: "from " + name, From: inbox.From{Kind: "system", Name: name}, Severity: inbox.Info,
			Source: inbox.Source{Project: projects.Root},
			Parts:  []inbox.Part{{Type: inbox.PartText, Text: "x"}},
		}, time.Now(), inbox.DefaultPostCaps())
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.InboxGet(admin, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.From.Name != name || got.From.Display != want {
			t.Errorf("a message from %s is shown from %+v, want display %q", name, got.From, want)
		}
	}
}
