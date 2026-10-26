package platform

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/urmzd/fiber/pkg/address"
	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/connector"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	saigetypes "github.com/urmzd/saige/agent/types"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type capabilityPeer struct{ writes int }

func (p *capabilityPeer) Name() string { return "slack" }
func (p *capabilityPeer) Ingest(ctx context.Context, _ chan<- envelope.Tag) error {
	<-ctx.Done()
	return ctx.Err()
}
func (p *capabilityPeer) Ingesting() bool             { return false }
func (p *capabilityPeer) Flush(context.Context) error { return nil }
func (p *capabilityPeer) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	p.writes++
	w.WriteHeader(200)
}
func (p *capabilityPeer) Deliver(context.Context, address.Address, envelope.Event) error {
	p.writes++
	return nil
}
func (p *capabilityPeer) Actions() []connector.Action {
	return []connector.Action{{Name: "slack_post_message", Description: "Post message", Schema: json.RawMessage(`{"type":"object"}`), Invoke: func(context.Context, address.Address, json.RawMessage) (connector.Result, error) {
		p.writes++
		return connector.Result{}, nil
	}}}
}

type capabilityVerifier struct{}

func (capabilityVerifier) Verify(h http.Header, _ []byte) error {
	if h.Get("X-Test-Signature") != "verified" {
		return errors.New("signature")
	}
	return nil
}
func TestInstallationCapabilitiesRecheckAtInvocationAndPersist(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		p, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: st}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		c, err := p.Clients.Create("t", clients.Client{ID: "app", Name: "App", Surface: clients.Slack, Identity: clients.Identity{Team: "T01"}, Enabled: true, Agents: []string{"a"}}, clients.DefaultCaps(), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		peer := &capabilityPeer{}
		station := &seat{client: c, peer: peer, verifier: capabilityVerifier{}}
		p.surfaces["slack"].seats = []*seat{station}
		who := auth.Principal{Kind: auth.User, Tenant: "t", Subject: "admin", Roles: []auth.Role{auth.Admin}}
		inventory, err := p.ClientCapabilities("t", "app")
		if err != nil || len(inventory.Actions) != 1 || !inventory.Legacy {
			t.Fatal(inventory, err)
		}
		req := runtime.Request{Tenant: "t", RunID: "run", Revision: agentrt.Revision{Spec: agentrt.Spec{Name: "a"}}}
		pinned := capabilityActor{p: p, st: station, req: req}.Actions()[0]
		changed, err := p.SetClientCapabilities(t.Context(), who, "app", inventory.Version, clients.Capabilities{Events: []string{}, Actions: []string{}, DeliverOutcomes: false})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pinned.Invoke(t.Context(), address.MustParse("slack://T01/C01"), json.RawMessage(`{}`)); !errors.Is(err, operations.ErrDenied) || peer.writes != 0 {
			t.Fatal("revoked action executed", err)
		}
		if _, err := p.SetClientCapabilities(t.Context(), who, "app", inventory.Version, clients.Capabilities{Events: []string{}, Actions: []string{}}); !errors.Is(err, clients.ErrExists) {
			t.Fatal("stale capability save accepted", err)
		}
		reopened, err := clients.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := reopened.Get("t", "app")
		if err != nil || stored.AllowsEvent("app_mention") || stored.AllowsAction("slack_post_message") || stored.AllowsOutcomes() {
			t.Fatal("controls lost on restart", err)
		}
		for _, signature := range []string{"bad", "verified"} {
			r := httptest.NewRequest("POST", "/connectors/slack", strings.NewReader(`{"team_id":"T01","event":{"type":"app_mention"}}`))
			r.Header.Set("X-Test-Signature", signature)
			w := httptest.NewRecorder()
			p.surfaces["slack"].ServeHTTP(w, r)
			want := 200
			if signature == "bad" {
				want = 401
			}
			if w.Code != want || peer.writes != 0 {
				t.Fatal("disabled events bypassed verification or reached adapter", w.Code, peer.writes)
			}
		}
		if _, err := p.SetClientCapabilities(t.Context(), who, "app", changed.Version, clients.Capabilities{Events: []string{"app_mention"}, Actions: []string{"slack_post_message"}, DeliverOutcomes: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := pinned.Invoke(t.Context(), address.MustParse("slack://T01/C01"), json.RawMessage(`{}`)); err != nil || peer.writes != 1 {
			t.Fatal("explicit action not restored", err)
		}
	})
}
func TestNativeGitHubAndSlackEffectsRespectExternalMessagesPolicy(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	scope := operations.Scope{Kind: "workspace"}
	policy := func(external, files operations.Level) {
		current := p.Operations.Policy("t", scope)
		if _, err := p.Operations.SetPolicy("t", operations.Policy{Scope: scope, Rules: map[operations.Category]operations.Level{operations.ExternalMessages: external, operations.WriteFiles: files}}, current.Version, "admin", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	req := runtime.Request{Tenant: "t", Revision: agentrt.Revision{Spec: agentrt.Spec{Name: "a"}}}
	policy(operations.Never, operations.Allow)
	for _, name := range []string{"github_comment", "github_request_review", "github_add_labels", "github_apply_changes", "slack_add_reaction", "slack_post_message"} {
		tool := p.operationTool(req, &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: name}, Fn: func(context.Context, map[string]any) (string, error) {
			t.Fatal("forbidden native effect executed")
			return "", nil
		}}, categoryOf(name))
		if _, err := tool.Execute(t.Context(), nil); !errors.Is(err, operations.ErrDenied) {
			t.Fatal(name, err)
		}
	}
	policy(operations.Allow, operations.Ask)
	tool := p.operationTool(req, &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: "github_apply_changes"}}, categoryOf("github_apply_changes"))
	if _, needed := tool.(agentrt.Approving).Approval(saigetypes.ToolCallPart{Name: "github_apply_changes"}); !needed {
		t.Fatal("file-write Ask did not use native approval")
	}
	policy(operations.Allow, operations.Never)
	if _, err := tool.Execute(t.Context(), nil); !errors.Is(err, operations.ErrDenied) {
		t.Fatal("commit bypassed file-write Never", err)
	}
}

func TestGitHubPersonUsesOnlyExactVerifiedInstallationMapping(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	c := clients.Client{ID: "github", Tenant: "t", Surface: clients.GitHub, Enabled: true, GitHub: &clients.GitHubOptions{People: map[string]string{"octocat": "ada"}}}
	for _, test := range []struct {
		id   string
		bot  bool
		want string
	}{{"octocat", false, "ada"}, {"Octocat", false, ""}, {" octocat", false, ""}, {"other", false, ""}, {"octocat", true, ""}} {
		got := p.surfacePerson(c, envelope.Tag{Actor: envelope.Actor{ID: test.id, Bot: test.bot}})
		if got != test.want {
			t.Fatalf("person(%q,bot=%v)=%q want%q", test.id, test.bot, got, test.want)
		}
	}
	c.Enabled = false
	if got := p.surfacePerson(c, envelope.Tag{Actor: envelope.Actor{ID: "octocat"}}); got != "" {
		t.Fatal("disabled client mapped person", got)
	}
}
