package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/toolprovider"

	"github.com/urmzd/warp/internal/mcpfake"
	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/credref"
	slackflow "github.com/urmzd/warp/pkg/integrations/slack"
)

// searcher is a model that calls issues__search whenever it is offered, and
// answers once it has the result.
type searcher struct {
	mu   sync.Mutex
	sent []string
}

func (m *searcher) Stream(ctx context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
	m.mu.Lock()
	m.sent = append(m.sent, fmt.Sprintf("%+v", req.Messages))
	m.mu.Unlock()
	reply := agenttest.TextResponse("No tools.")
	text := fmt.Sprintf("%+v", req.Messages)
	switch {
	case strings.Contains(text, "ToolResultPart") || strings.Contains(text, "call_id") || strings.Contains(text, "open issues"):
		reply = agenttest.TextResponse("Done.")
	case slices.ContainsFunc(req.Tools, func(t saigetypes.ToolDef) bool { return t.Name == "issues__search" }):
		reply = agenttest.ToolCallResponse("call-search", "issues__search", map[string]any{"q": "bugs"})
	}
	return (&agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{reply}}).Stream(ctx, req)
}

// toolFixture is a server whose acme workspace has one tool that acts as
// each person, issues, on a fake MCP server, an agent that uses it, and a
// Slack client that maps the Slack user U0ADA to ana for the Slack app's
// credential.
func toolFixture(t *testing.T) (*fixture, *mcpfake.Server, *searcher) {
	t.Helper()
	server, model := mcpfake.New(t), &searcher{}
	tokens := map[string]string{"WARP_PERSON_ANA": "tok-ana-httpapi", "WARP_PERSON_BEN": "tok-ben-httpapi"}
	f := newFixtureWith(t, platform.Config{
		AgentOptions: []agentrt.Option{agentrt.WithProvider(agentrt.ProviderOllama, func(agentrt.Spec) (saigetypes.Provider, error) { return model, nil })},
		Ext: platform.Extensions{
			ToolProviders: []platform.ToolProviderSeed{{Provider: toolprovider.Provider{
				Tenant: "acme", Name: "issues", Transport: toolprovider.TransportHTTP, Endpoint: server.URL, CredentialScope: toolprovider.ScopePerson,
			}, Accept: server.Tools(t)}},
			Resolver: credref.Resolver{LookupEnv: func(name string) (string, bool) { v, ok := tokens[name]; return v, ok }}.MCP(),
		},
		CredentialRefs: map[string][]string{"acme": {"env:WARP_PERSON_"}},
		SlackAPI:       func(string) slackflow.API { return slackflow.NewFake() },
		ClientInterval: -1,
	})
	t.Cleanup(func() { _ = f.p.Close() })
	if _, err := f.p.CreateClient(t.Context(), "acme", "test", platform.ClientInput{
		Client: clients.Client{
			ID: "slack-app", Name: "Slack app", Surface: clients.Slack, Enabled: true, Agents: []string{clients.AnyAgent},
			Identity: clients.Identity{Team: "T01ABCDEF"},
			Slack:    &clients.SlackOptions{Machine: "svc-acme", People: map[string]string{"U0ADA": "ana"}},
		},
		Token: clients.SecretInput{Value: "xoxb-test"}, SigningSecret: clients.SecretInput{Value: "signing"},
	}); err != nil {
		t.Fatal(err)
	}
	admin := f.machine(t, "acme", everything...)
	if res, out := f.do(t, nil, "POST", "/api/agents", admin, `{"name":"worker","model":"m","provider":"ollama","system_prompt":"Look it up.","tools":["mcp:issues"]}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("create worker = %d %v", res.StatusCode, out)
	}
	return f, server, model
}

func (f *fixture) person(t *testing.T, subject string) string {
	t.Helper()
	return f.iss.Sign(t, subject, audience, map[string]any{"org": "acme", "groups": []any{"warp-admins"}, "name": subject})
}

// ask starts worker with a body and waits for the run to end.
func (f *fixture) ask(t *testing.T, tok, body string) map[string]any {
	t.Helper()
	res, run := f.do(t, nil, "POST", "/api/runs", tok, body)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /api/runs %s = %d %v", body, res.StatusCode, run)
	}
	id, _ := run["id"].(string)
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		_, now := f.do(t, nil, "GET", "/api/runs/"+id, tok, "")
		if s := now["status"]; s == "completed" || s == "failed" || s == "stopped" {
			return now
		}
	}
	t.Fatalf("run %s never ended", id)
	return nil
}

// The Slack app's runs act as the person its client maps, never as the app:
// a person connects their own credential in the console, the app asks for
// them, and the tool is reached with that person's credential. Asked as
// itself, the app's run gets no tools. A console run, and Run Now, act for
// whoever is signed in.
func TestTheSlackAppsRunsActAsTheMappedPerson(t *testing.T) {
	f, server, _ := toolFixture(t)
	ana, ben := f.person(t, "ana"), f.person(t, "ben")
	app := f.machine(t, "acme", "warp:runs:read", "warp:runs:write")

	if res, out := f.do(t, nil, "PUT", "/api/me/credentials/issues", ana, `{"ref":"env:WARP_PERSON_ANA"}`); res.StatusCode != http.StatusOK || out["connected"] != true {
		t.Fatalf("ana connecting = %d %v", res.StatusCode, out)
	}
	if res, out := f.do(t, nil, "PUT", "/api/me/credentials/issues", app, `{"ref":"env:WARP_PERSON_BEN"}`); res.StatusCode != http.StatusForbidden {
		t.Fatalf("the app connected a credential of its own: %d %v", res.StatusCode, out)
	}

	run := f.ask(t, app, `{"agent":"worker","text":"Open bugs?","act_as":{"client":"slack-app","user":"U0ADA"}}`)
	if actor, _ := run["actor"].(map[string]any); actor["user"] != "ana" || actor["bot"] == true {
		t.Fatalf("the app's run for U0ADA acts for %v", run["actor"])
	}
	if calls := server.Calls(); len(calls) != 1 || calls[0] != "Bearer tok-ana-httpapi" {
		t.Fatalf("the app's run for ana reached the server with %q", calls)
	}
	if strings.Contains(fmt.Sprint(run), "tok-ana-httpapi") {
		t.Fatalf("the run carries ana's credential: %v", run)
	}

	own := f.ask(t, app, `{"agent":"worker","text":"Open bugs?"}`)
	if actor, _ := own["actor"].(map[string]any); actor["user"] != nil || actor["bot"] != true {
		t.Fatalf("the app's own run acts for %v", own["actor"])
	}
	if calls := server.Calls(); len(calls) != 1 {
		t.Fatalf("the app's own run reached the server: %q", calls)
	}

	f.ask(t, ana, `{"agent":"worker","text":"Open bugs?"}`)
	if calls := server.Calls(); len(calls) != 2 || calls[1] != "Bearer tok-ana-httpapi" {
		t.Fatalf("ana's console run reached the server with %q", calls)
	}
	f.ask(t, ben, `{"agent":"worker","text":"Open bugs?"}`)
	if calls := server.Calls(); len(calls) != 2 {
		t.Fatalf("ben, who connected nothing, reached the server: %q", calls)
	}
	// Run Now on a schedule is the person's say-so, so it acts for them.
	if res, out := f.do(t, nil, "POST", "/api/schedules", ana, `{"name":"bugs","expression":"0 3 * * *","agent":"worker","text":"Open bugs?"}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("create schedule = %d %v", res.StatusCode, out)
	}
	res, now := f.do(t, nil, "POST", "/api/schedules/bugs/run", ana, "")
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("run now = %d %v", res.StatusCode, now)
	}
	for deadline := time.Now().Add(10 * time.Second); len(server.Calls()) < 3 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
	}
	if calls := server.Calls(); len(calls) != 3 || calls[2] != "Bearer tok-ana-httpapi" {
		t.Fatalf("ana's run now reached the server with %q", calls)
	}
	for _, r := range server.Requests() {
		if r != "Bearer tok-ana-httpapi" {
			t.Fatalf("a request reached the server with %q", r)
		}
	}

	// What ana sees of her own, and ben of his.
	_, mine := f.do(t, nil, "GET", "/api/me/credentials", ana, "")
	_, his := f.do(t, nil, "GET", "/api/me/credentials", ben, "")
	first := func(out map[string]any) map[string]any {
		list, _ := out["providers"].([]any)
		if len(list) != 1 {
			t.Fatalf("providers = %v", out)
		}
		return list[0].(map[string]any)
	}
	if c := first(mine); c["connected"] != true || c["ref"] != "env:WARP_PERSON_ANA" || c["endpoint"] != server.URL {
		t.Fatalf("ana's issues = %v", c)
	}
	if c := first(his); c["connected"] != false || c["ref"] != nil {
		t.Fatalf("ben's issues = %v", c)
	}
}
