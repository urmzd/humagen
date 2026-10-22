package platform_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/toolprovider"

	"github.com/urmzd/warp/internal/mcpfake"
	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/credentials"
	"github.com/urmzd/warp/pkg/credref"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/state/statetest"
)

// The people of these tests, and the secrets their references name.
var (
	ana      = auth.Principal{Subject: "ana", Name: "Ana", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	ben      = auth.Principal{Subject: "ben", Name: "Ben", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	cyd      = auth.Principal{Subject: "cyd", Name: "Cyd", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	slackApp = auth.Principal{Subject: "svc-slack", Tenant: tenant, Kind: auth.Machine, Scopes: []auth.Permission{auth.RunsWrite}}

	environment = map[string]string{
		"WARP_PERSON_ANA_ISSUES": "tok-ana-7f3a",
		"WARP_PERSON_BEN_ISSUES": "tok-ben-91c2",
		"WARP_SHARED_TOKEN":      "tok-shared-55e0",
	}
)

const (
	benValue     = "tok-ben-stored-4d1e"
	searchTool   = "issues__search"
	workerPrompt = "Look things up."
)

// toolUser is a model that calls issues__search whenever it is offered,
// answers once it has the result, and says it has no tools when it is
// offered none. It keeps every request it is sent.
type toolUser struct {
	mu    sync.Mutex
	sent  []string
	tools [][]string
}

func (m *toolUser) Stream(ctx context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
	var names []string
	for _, t := range req.Tools {
		names = append(names, t.Name)
	}
	m.mu.Lock()
	m.sent = append(m.sent, fmt.Sprintf("%+v", req.Messages))
	m.tools = append(m.tools, names)
	m.mu.Unlock()
	reply := agenttest.TextResponse("I have no issue tools.")
	switch {
	case hasToolResult(req.Messages):
		reply = agenttest.TextResponse("Done.")
	case slices.Contains(names, searchTool):
		reply = agenttest.ToolCallResponse("call-search", searchTool, map[string]any{"q": "bugs"})
	}
	return (&agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{reply}}).Stream(ctx, req)
}

func hasToolResult(msgs []saigetypes.Message) bool {
	for _, msg := range msgs {
		var parts []any
		switch m := msg.(type) {
		case saigetypes.UserMessage:
			for _, p := range m.Parts {
				parts = append(parts, p)
			}
		case saigetypes.SystemMessage:
			for _, p := range m.Parts {
				parts = append(parts, p)
			}
		}
		for _, p := range parts {
			if _, ok := p.(saigetypes.ToolResultPart); ok {
				return true
			}
		}
	}
	return false
}

func (m *toolUser) requests() ([]string, [][]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.sent), slices.Clone(m.tools)
}

// logs is a log a test reads back.
type logs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// world is a platform with one person-scoped MCP provider, issues, on a fake
// server, and an agent, worker, granted its tools.
type world struct {
	p      *platform.Platform
	server *mcpfake.Server
	model  *toolUser
	log    *logs
}

func issues(server *mcpfake.Server, scope toolprovider.CredentialScope, ref string) toolprovider.Provider {
	return toolprovider.Provider{
		Tenant: tenant, Name: "issues", Transport: toolprovider.TransportHTTP, Endpoint: server.URL,
		CredentialScope: scope, CredentialRef: ref,
	}
}

// newWorld is a world on a fresh server, where issues takes each person's
// own credential.
func newWorld(t *testing.T, cfg platform.Config) *world {
	t.Helper()
	server := mcpfake.New(t)
	return newWorldOn(t, cfg, server, issues(server, toolprovider.ScopePerson, ""))
}

// newWorldOn is a world on server, declaring provider with the server's tool.
func newWorldOn(t *testing.T, cfg platform.Config, server *mcpfake.Server, provider toolprovider.Provider) *world {
	t.Helper()
	w := &world{server: server, model: &toolUser{}, log: &logs{}}
	cfg.Ext.ToolProviders = append(cfg.Ext.ToolProviders, platform.ToolProviderSeed{Provider: provider, Accept: server.Tools(t)})
	cfg.Ext.Resolver = credref.Resolver{LookupEnv: func(name string) (string, bool) {
		v, ok := environment[name]
		return v, ok
	}}.MCP()
	if cfg.CredentialRefs == nil {
		cfg.CredentialRefs = map[string][]string{tenant: {"env:WARP_PERSON_{subject}_"}}
	}
	if cfg.CredentialValues == nil {
		cfg.CredentialValues = map[string]bool{tenant: true}
	}
	cfg.Log = slog.New(slog.NewTextHandler(w.log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg.AgentOptions = []agentrt.Option{agentrt.WithProvider(agentrt.ProviderOffline, func(agentrt.Spec) (saigetypes.Provider, error) {
		return w.model, nil
	})}
	w.p = startWith(t, cfg)
	// The MCP sessions are closed before the platform stops.
	t.Cleanup(func() { _ = w.p.Close() })
	spec := agentspec.AgentSpec{Name: "worker", Model: "offline", Provider: "offline", SystemPrompt: workerPrompt, Tools: []string{"mcp:issues"}}
	if _, err := w.p.Agents.Create(t.Context(), tenant, spec, "test"); err != nil {
		t.Fatal(err)
	}
	return w
}

// ask runs worker once for an actor and waits for it to finish.
func (w *world) ask(t *testing.T, id string, actor envelope.Actor) runs.Run {
	t.Helper()
	r, err := w.p.Converse(t.Context(), tenant, platform.Turn{ID: id, Agent: "worker", Text: "Any open bugs?", Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	return await(t, w.p, r.ID, runs.Completed)
}

func (w *world) connect(t *testing.T, who auth.Principal, in credentials.Input) {
	t.Helper()
	if _, err := w.p.ConnectTool(t.Context(), who, "issues", in); err != nil {
		t.Fatalf("%s could not connect: %v", who.Subject, err)
	}
}

// lastPrompt is the newest request worker's model was sent, and the tools it
// was offered with it.
func (w *world) lastPrompt(t *testing.T) (string, []string) {
	t.Helper()
	sent, tools := w.model.requests()
	if len(sent) == 0 {
		t.Fatal("the model was never asked")
	}
	return sent[len(sent)-1], tools[len(tools)-1]
}

// leaks fails the test if any secret appears anywhere a person or a log
// could read it: answers, events, what the model was sent, and the log.
func (w *world) leaks(t *testing.T, secrets ...string) {
	t.Helper()
	sent, _ := w.model.requests()
	var places []string
	places = append(places, strings.Join(sent, "\n"), w.log.String())
	for _, r := range w.p.Runs.List(tenant, 200) {
		places = append(places, r.Answer, r.Error)
		for _, e := range w.p.Journal.Events(tenant, []string{r.ID}) {
			places = append(places, string(e.Payload))
		}
	}
	for _, s := range secrets {
		for _, place := range places {
			if strings.Contains(place, s) {
				t.Fatalf("a credential (%s...) reached somewhere it should not: %.300s", s[:6], place)
			}
		}
	}
}

// Two people, one provider: each run reaches it with the credential of the
// person it acts for, one by a reference and one by a stored value, and
// with nobody else's.
func TestEachPersonsRunReachesTheProviderWithTheirOwnCredential(t *testing.T) {
	t.Run("memory", func(t *testing.T) { eachPersonsOwn(t, platform.Backends{}) })
	t.Run("postgres", func(t *testing.T) {
		pool := statetest.Pool(t)
		b, release, err := platform.OpenPostgres(t.Context(), pool)
		if err != nil {
			t.Fatal(err)
		}
		// Registered before the platform starts, so it runs once the
		// platform has stopped and before the pool is closed.
		t.Cleanup(func() { _ = release() })
		eachPersonsOwn(t, b)
	})
}

func eachPersonsOwn(t *testing.T, b platform.Backends) {
	w := newWorld(t, platform.Config{Backends: b})
	w.connect(t, ana, credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"})
	w.connect(t, ben, credentials.Input{Value: benValue})

	for _, c := range []struct {
		who  auth.Principal
		want string
	}{{ana, "Bearer " + environment["WARP_PERSON_ANA_ISSUES"]}, {ben, "Bearer " + benValue}, {ana, "Bearer " + environment["WARP_PERSON_ANA_ISSUES"]}} {
		before := len(w.server.Calls())
		run := w.ask(t, "api-"+c.who.Subject+fmt.Sprint(before), platform.ActorOf(c.who))
		calls := w.server.Calls()[before:]
		if len(calls) != 1 || calls[0] != c.want {
			t.Fatalf("%s's run reached the server with %d calls %q", c.who.Subject, len(calls), calls)
		}
		for _, r := range w.server.Requests() {
			if r != "Bearer "+environment["WARP_PERSON_ANA_ISSUES"] && r != "Bearer "+benValue {
				t.Fatalf("a request reached the server with %q", r)
			}
		}
		if run.Answer != "Done." {
			t.Fatalf("%s's run answered %q", c.who.Subject, run.Answer)
		}
	}
	w.leaks(t, environment["WARP_PERSON_ANA_ISSUES"], benValue)
}

// A person with no credential gets no tools from the provider, the model is
// told why, and the server is never reached: not with the shared credential
// it had before it was made person-scoped, not with anyone else's.
func TestAPersonWithNoCredentialGetsNoToolsAndNothingFallsBackToTheSharedOne(t *testing.T) {
	// The provider was shared, with a credential of its own, and its record
	// is kept: the second platform finds it and makes it person-scoped.
	store, server := toolprovider.NewMemory(), mcpfake.New(t)
	shared := newWorldOn(t, platform.Config{Ext: platform.Extensions{ToolStore: store}}, server, issues(server, toolprovider.ScopeShared, "env:WARP_SHARED_TOKEN"))
	shared.ask(t, "api-shared", platform.ActorOf(cyd))
	if calls := server.Calls(); len(calls) != 1 || calls[0] != "Bearer "+environment["WARP_SHARED_TOKEN"] {
		t.Fatalf("while shared, the provider was reached with %q", calls)
	}

	w := newWorldOn(t, platform.Config{Ext: platform.Extensions{ToolStore: store}}, server, issues(server, toolprovider.ScopePerson, ""))
	rec, err := store.GetProvider(t.Context(), tenant, "issues")
	if err != nil || rec.Scope() != toolprovider.ScopePerson || rec.CredentialRef != "" {
		t.Fatalf("the record was not made person-scoped: %+v, %v", rec, err)
	}
	w.connect(t, ana, credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"})

	before := len(w.server.Requests())
	for i, actor := range []envelope.Actor{
		platform.ActorOf(cyd), // signed in, and connected nothing
		{},                    // a run for nobody
		platform.ActorOf(slackApp),
	} {
		run := w.ask(t, fmt.Sprintf("api-nobody-%d", i), actor)
		if got := w.server.Requests()[before:]; len(got) != 0 {
			t.Fatalf("a run for %+v reached the server with %q", actor, got)
		}
		prompt, tools := w.lastPrompt(t)
		if slices.Contains(tools, searchTool) {
			t.Fatalf("a run for %+v was offered %s", actor, searchTool)
		}
		if !strings.Contains(prompt, "mcp:issues") || !strings.Contains(prompt, "unavailable") {
			t.Fatalf("the model was not told the tools are unavailable: %.400s", prompt)
		}
		if run.Answer != "I have no issue tools." {
			t.Fatalf("answer = %q", run.Answer)
		}
	}
	// cyd is told she has not connected one.
	sent, _ := w.model.requests()
	if !slices.ContainsFunc(sent, func(s string) bool { return strings.Contains(s, "they have not connected one for issues") }) {
		t.Fatal("the model was not told the person has not connected a credential")
	}

	// ana, who has, reaches it as herself, and never as the provider was.
	w.ask(t, "api-ana", platform.ActorOf(ana))
	for _, r := range w.server.Requests()[before:] {
		if r != "Bearer "+environment["WARP_PERSON_ANA_ISSUES"] {
			t.Fatalf("after the change, a request reached the server with %q", r)
		}
	}
	if len(w.server.Requests()[before:]) == 0 {
		t.Fatal("ana's run never reached the server")
	}
	w.leaks(t, environment["WARP_SHARED_TOKEN"], environment["WARP_PERSON_ANA_ISSUES"])
}

// A machine asking as itself acts for nobody, even when a credential is held
// under its subject: the app's own runs never reach a person's provider.
func TestAMachinesOwnRunActsForNobody(t *testing.T) {
	w := newWorld(t, platform.Config{})
	// Written straight into the store: ConnectTool refuses a machine.
	if _, err := w.p.Credentials.Put(tenant, slackApp.Subject, "issues", credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.ConnectTool(t.Context(), slackApp, "issues", credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("a machine connected a credential: %v", err)
	}
	w.ask(t, "api-app", platform.ActorOf(slackApp))
	// A machine's actor names the subject as its id, and nothing else
	// can turn it into a person.
	w.ask(t, "api-app-user", envelope.Actor{ID: slackApp.Subject, User: slackApp.Subject, Bot: true})
	if got := w.server.Requests(); len(got) != 0 {
		t.Fatalf("the app's own runs reached the server with %q", got)
	}
}

// A Slack connector's run acts for the person the team's clients map its
// Slack user to, and a Slack user nobody maps is nobody.
func TestASlackConnectorsRunActsForTheMappedPerson(t *testing.T) {
	f := &fakes{}
	cfg := f.config()
	w := newWorld(t, cfg)
	w.connect(t, ana, credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"})
	in := slackClient("help-app", "A0HELP", team, "worker")
	create(t, w.p, tenant, in)
	// The people map is on the client the Slack app built on the API signs
	// in as, which is not the client that receives the mention.
	mapper := slackClient("api-app", "A0API", team, "worker")
	mapper.Slack.Machine, mapper.Slack.People = slackApp.Subject, map[string]string{"U0HUMAN1": "ana"}
	create(t, w.p, tenant, mapper)

	ingress := w.p.IngressHandler()
	if code := signedSlack(ingress, "secret-help-app", mentionOf("A0HELP", team, "Ev1", "C01", "1700000000.000100")); code != http.StatusOK {
		t.Fatalf("the mention answered %d", code)
	}
	eventually(t, "the mapped person's run reached the server as her", func() bool {
		calls := w.server.Calls()
		return len(calls) == 1 && calls[0] == "Bearer "+environment["WARP_PERSON_ANA_ISSUES"]
	})
	r, ok := runOf(w.p, tenant, "worker")
	if !ok || r.Actor.User != "ana" || r.Actor.ID != "U0HUMAN1" {
		t.Fatalf("the run acts for %+v", r.Actor)
	}

	stranger := strings.ReplaceAll(mentionOf("A0HELP", team, "Ev2", "C01", "1700000000.000200"), "U0HUMAN1", "U0STRANGER")
	if code := signedSlack(ingress, "secret-help-app", stranger); code != http.StatusOK {
		t.Fatalf("the mention answered %d", code)
	}
	eventually(t, "the stranger's run finished", func() bool {
		n := 0
		for _, r := range w.p.Runs.List(tenant, 50) {
			if r.Status == runs.Completed {
				n++
			}
		}
		return n == 2
	})
	if calls := w.server.Calls(); len(calls) != 1 {
		t.Fatalf("an unmapped Slack user's run reached the server: %q", calls)
	}
	w.leaks(t, environment["WARP_PERSON_ANA_ISSUES"])
}

// Two clients mapping one Slack user to two people make the run act for
// nobody, rather than for either.
func TestASlackUserMappedToTwoPeopleActsForNobody(t *testing.T) {
	f := &fakes{}
	w := newWorld(t, f.config())
	w.connect(t, ana, credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"})
	w.connect(t, ben, credentials.Input{Value: benValue})
	create(t, w.p, tenant, slackClient("help-app", "A0HELP", team, "worker"))
	for id, who := range map[string]string{"one": "ana", "two": "ben"} {
		in := slackClient("map-"+id, "A0MAP"+strings.ToUpper(id), team, "worker")
		in.Slack.Machine, in.Slack.People = "svc-"+id, map[string]string{"U0HUMAN1": who}
		create(t, w.p, tenant, in)
	}
	if code := signedSlack(w.p.IngressHandler(), "secret-help-app", mentionOf("A0HELP", team, "Ev1", "C01", "1700000000.000100")); code != http.StatusOK {
		t.Fatalf("the mention answered %d", code)
	}
	eventually(t, "the run finished", func() bool {
		r, ok := runOf(w.p, tenant, "worker")
		return ok && r.Status == runs.Completed
	})
	if r, _ := runOf(w.p, tenant, "worker"); r.Actor.User != "" {
		t.Fatalf("an ambiguous Slack user acts for %q", r.Actor.User)
	}
	if got := w.server.Requests(); len(got) != 0 {
		t.Fatalf("an ambiguous Slack user's run reached the server with %q", got)
	}
}

// What a person may give is the workspace's to decide: references only from
// where the operator listed, values only where the workspace keeps them,
// and only for a provider that takes a person's own.
func TestWhatAPersonMayConnect(t *testing.T) {
	w := newWorld(t, platform.Config{
		CredentialRefs:   map[string][]string{tenant: {"env:WARP_PERSON_{subject}_", "op://People/{subject}/"}},
		CredentialValues: map[string]bool{tenant: false},
	})
	if _, err := w.p.ToolProviders.PutProvider(t.Context(), toolprovider.Provider{
		Tenant: tenant, Name: "wiki", Transport: toolprovider.TransportHTTP, Endpoint: w.server.URL, CredentialRef: "env:WARP_SHARED_TOKEN",
	}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	for _, ref := range []string{"env:WARP_PERSON_ANA_ISSUES", "op://People/ana/issues"} {
		if _, err := w.p.ConnectTool(ctx, ana, "issues", credentials.Input{Ref: ref}); err != nil {
			t.Errorf("%s was refused: %v", ref, err)
		}
	}
	var refusals []string
	for _, ref := range []string{
		"env:WARP_SHARED_TOKEN", "env:ANTHROPIC_API_KEY", "env:WARP_OIDC_CLIENT_SECRET", "keychain:issues", "op://Infra/db/password", "op://PeopleX/a/b",
		// Under a listed start, but somebody else's.
		"env:WARP_PERSON_BEN_ISSUES", "op://People/ben/issues", "op://People/an/issues", "op://People/ana.b/issues",
	} {
		_, err := w.p.ConnectTool(ctx, ana, "issues", credentials.Input{Ref: ref})
		if !errors.Is(err, credentials.ErrInvalid) {
			t.Errorf("%s was not refused: %v", ref, err)
			continue
		}
		refusals = append(refusals, err.Error())
	}
	for _, r := range refusals {
		if r != refusals[0] || strings.Contains(r, "env:") || strings.Contains(r, "op:") {
			t.Fatalf("refusals tell references apart: %q", refusals)
		}
	}
	if _, err := w.p.ConnectTool(ctx, ana, "issues", credentials.Input{Value: "tok"}); !errors.Is(err, platform.ErrForbidden) {
		t.Errorf("a value was kept where the workspace keeps none: %v", err)
	}
	if _, err := w.p.ConnectTool(ctx, ana, "wiki", credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"}); !errors.Is(err, platform.ErrNotPersonScoped) {
		t.Errorf("a credential was given for a shared provider: %v", err)
	}
	for _, name := range []string{"ghost", "Not A Name"} {
		if _, err := w.p.ConnectTool(ctx, ana, name, credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"}); !errors.Is(err, platform.ErrProviderNotFound) {
			t.Errorf("a credential was given for %q: %v", name, err)
		}
	}
	other := ana
	other.Tenant = "globex"
	if _, err := w.p.ConnectTool(ctx, other, "issues", credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"}); !errors.Is(err, platform.ErrProviderNotFound) {
		t.Errorf("another workspace's person connected to this one's provider: %v", err)
	}

	list, err := w.p.ToolCredentials(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Providers) != 1 || list.Providers[0].Provider != "issues" || !list.Providers[0].Connected ||
		list.Providers[0].Ref != "op://People/ana/issues" || list.Values ||
		!slices.Equal(list.References, []string{"env:WARP_PERSON_ANA_", "op://People/ana/"}) {
		t.Fatalf("ana's credentials = %+v", list)
	}
	if theirs, _ := w.p.ToolCredentials(ctx, slackApp); len(theirs.References) != 0 {
		t.Fatalf("a machine was told where it may reference: %q", theirs.References)
	}
	if theirs, _ := w.p.ToolCredentials(ctx, ben); theirs.Providers[0].Connected {
		t.Fatal("ben sees ana's credential as his")
	}
	if err := w.p.DisconnectTool(ctx, ana, "issues"); err != nil {
		t.Fatal(err)
	}
	if err := w.p.DisconnectTool(ctx, ana, "issues"); !errors.Is(err, credentials.ErrNotFound) {
		t.Fatalf("removing it twice = %v", err)
	}
}

// A reference is only ever the person's own: under a start every person
// shares, Ben can neither connect Ana's reference nor, had one been kept
// under his name some other way, have his runs reach the provider with it.
func TestNobodyConnectsAnotherPersonsReference(t *testing.T) {
	w := newWorld(t, platform.Config{})
	w.connect(t, ana, credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"})
	_, err := w.p.ConnectTool(t.Context(), ben, "issues", credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"})
	if !errors.Is(err, credentials.ErrInvalid) {
		t.Fatalf("ben connected ana's reference: %v", err)
	}
	if theirs, _ := w.p.ToolCredentials(t.Context(), ben); theirs.Providers[0].Connected {
		t.Fatal("ben holds a credential after his refusal")
	}
	// Written straight into the store, as an older server would have kept it.
	if _, err := w.p.Credentials.Put(tenant, ben.Subject, "issues", credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	w.ask(t, "api-ben", platform.ActorOf(ben))
	if got := w.server.Requests(); len(got) != 0 {
		t.Fatalf("ben's run reached the server with %q", got)
	}
	if !strings.Contains(w.log.String(), "not this person's own") {
		t.Fatal("the unused reference was not logged")
	}
	w.ask(t, "api-ana", platform.ActorOf(ana))
	if calls := w.server.Calls(); len(calls) != 1 || calls[0] != "Bearer "+environment["WARP_PERSON_ANA_ISSUES"] {
		t.Fatalf("ana's run reached the server with %q", calls)
	}
	w.leaks(t, environment["WARP_PERSON_ANA_ISSUES"])
}

// An approval of a call that acts as the person a run acts for is a second
// pair of eyes on their own request, never a way to act as them: ben may
// approve ana's call as she asked it, but not with arguments of his own,
// which would reach the provider with her token.
func TestAnApproverCannotEditACallThatActsAsThePerson(t *testing.T) {
	w := newWorld(t, platform.Config{})
	ctx := t.Context()
	spec := agentspec.AgentSpec{Name: "careful", Model: "offline", Provider: "offline", SystemPrompt: workerPrompt,
		Tools: []string{"mcp:issues"}, Approvals: []string{searchTool}}
	if _, err := w.p.Agents.Create(ctx, tenant, spec, "test"); err != nil {
		t.Fatal(err)
	}
	w.connect(t, ana, credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"})
	r, err := w.p.Converse(ctx, tenant, platform.Turn{ID: "api-careful", Agent: "careful", Text: "Any open bugs?", Actor: platform.ActorOf(ana)})
	if err != nil {
		t.Fatal(err)
	}
	run := await(t, w.p, r.ID, runs.Parked)
	if run.Question == nil || run.Question.Tool == nil || run.Question.Tool.Name != searchTool || !run.Question.Tool.ActsForPerson {
		t.Fatalf("parked on %+v, want an approval of a call that acts as ana", run.Question)
	}
	edited := platform.QuestionAnswer{Action: platform.ActApprove, Arguments: json.RawMessage(`{"q":"payroll"}`)}
	if _, err := w.p.AnswerQuestion(ctx, ben, run.ID, run.Question.ID, edited); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("ben edited ana's call: %v", err)
	}
	if got, _ := w.p.Runs.Get(tenant, run.ID); got.Status != runs.Parked {
		t.Fatalf("a refused edit moved the run to %s", got.Status)
	}
	if got := w.server.Calls(); len(got) != 0 {
		t.Fatalf("the tool was called before the call was approved: %q", got)
	}
	if _, err := w.p.AnswerQuestion(ctx, ben, run.ID, run.Question.ID, platform.QuestionAnswer{Action: platform.ActApprove}); err != nil {
		t.Fatalf("ben approving as asked: %v", err)
	}
	await(t, w.p, run.ID, runs.Completed)
	if calls := w.server.Calls(); len(calls) != 1 || calls[0] != "Bearer "+environment["WARP_PERSON_ANA_ISSUES"] {
		t.Fatalf("the approved call reached the server with %q", calls)
	}
	w.leaks(t, environment["WARP_PERSON_ANA_ISSUES"])
}

// Starts that could put one reference under two people's are refused before
// the platform starts.
func TestAPlatformRefusesStartsThatNameNobody(t *testing.T) {
	for name, refs := range map[string]map[string][]string{
		"a start for everyone":      {tenant: {"env:WARP_PERSON_"}},
		"two workspaces, one place": {tenant: {"op://People/{subject}/"}, "globex": {"op://People/{subject}/"}},
	} {
		if _, err := platform.New(platform.Config{CredentialRefs: refs}); !errors.Is(err, credentials.ErrInvalid) {
			t.Errorf("%s: New = %v", name, err)
		}
	}
}

// A credential the workspace no longer allows is not used, however it was
// kept: a value once values are turned off, and a reference from somewhere
// no longer listed.
func TestACredentialTheWorkspaceNoLongerAllowsIsNotUsed(t *testing.T) {
	w := newWorld(t, platform.Config{
		CredentialRefs:   map[string][]string{tenant: {"op://People/{subject}/"}},
		CredentialValues: map[string]bool{tenant: false},
	})
	at := time.Now()
	if _, err := w.p.Credentials.Put(tenant, "ana", "issues", credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.Credentials.Put(tenant, "ben", "issues", credentials.Input{Value: benValue}, at); err != nil {
		t.Fatal(err)
	}
	w.ask(t, "api-ana", platform.ActorOf(ana))
	w.ask(t, "api-ben", platform.ActorOf(ben))
	if got := w.server.Requests(); len(got) != 0 {
		t.Fatalf("a credential the workspace does not allow reached the server: %q", got)
	}
	w.leaks(t, environment["WARP_PERSON_ANA_ISSUES"], benValue)
}

// The run a signed-in person asks for records them as who it acts for, and
// a surface acting for a person records that person.
func TestWhoARunActsFor(t *testing.T) {
	if a := platform.ActorOf(ana); a.User != "ana" || a.ID != "ana" || a.Bot {
		t.Errorf("a person's actor = %+v", a)
	}
	if a := platform.ActorOf(slackApp); a.User != "" || !a.Bot {
		t.Errorf("a machine's actor = %+v", a)
	}
	if a := platform.PersonActor("okta|00u1ana"); a.User != "okta|00u1ana" || a.Bot {
		t.Errorf("a mapped person's actor = %+v", a)
	}
}
