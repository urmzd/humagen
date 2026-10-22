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
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/memory"
	"github.com/urmzd/warp/pkg/projects"
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
//
// Recalling, it answers with what the search found, repeats it whenever it
// is in what it was sent, and as the memory agent proposes the answer of the
// exchange it is shown as a note.
type toolUser struct {
	mu     sync.Mutex
	sent   []string
	tools  [][]string
	recall bool
	// failing makes every call fail, so a run fails.
	failing bool
	// asks makes it ask a person first (ask_person): the question, then any
	// options. What they answer is what it searches for.
	asks []string
	// askOf is whom it asks them of (asked_of), such as sme:billing.
	askOf string
}

// found is what the issues search tells a recalling model.
const found = "The tracker holds PRIV-7, a payroll export bug."

func (m *toolUser) Stream(ctx context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
	var names []string
	for _, t := range req.Tools {
		names = append(names, t.Name)
	}
	text := userText(req.Messages)
	m.mu.Lock()
	if m.failing {
		m.mu.Unlock()
		return nil, errors.New("the model is unavailable")
	}
	m.sent = append(m.sent, fmt.Sprintf("%+v", req.Messages))
	m.tools = append(m.tools, names)
	recall, asks, askOf := m.recall, m.asks, m.askOf
	m.mu.Unlock()
	reply := agenttest.TextResponse("I have no issue tools.")
	if len(asks) > 0 && slices.Contains(names, "ask_person") {
		switch results := toolResults(req.Messages); {
		case len(results) == 0 && askOf != "":
			reply = asked(map[string]any{"question": asks[0], "asked_of": askOf})
		case len(results) == 0:
			reply = asking(asks[0], asks[1:]...)
		case len(results) == 1 && slices.Contains(names, searchTool):
			reply = agenttest.ToolCallResponse("call-search", searchTool, map[string]any{"q": results[0]})
		default:
			reply = agenttest.TextResponse("Done.")
		}
		return (&agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{reply}}).Stream(ctx, req)
	}
	switch {
	case recall && strings.Contains(text, "should remember for later conversations"):
		lines := strings.Split(strings.TrimSpace(text), "\n")
		_, answer, _ := strings.Cut(lines[len(lines)-1], ": ")
		reply = agenttest.TextResponse(answer)
	case hasToolResult(req.Messages) && recall:
		reply = agenttest.TextResponse(found)
	case hasToolResult(req.Messages):
		reply = agenttest.TextResponse("Done.")
	case slices.Contains(names, searchTool):
		reply = agenttest.ToolCallResponse("call-search", searchTool, map[string]any{"q": "bugs"})
	case recall && strings.Contains(text, found):
		reply = agenttest.TextResponse(found)
	}
	return (&agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{reply}}).Stream(ctx, req)
}

// userText is the text of every user message, in order.
func userText(msgs []saigetypes.Message) string {
	var b strings.Builder
	for _, msg := range msgs {
		if m, ok := msg.(saigetypes.UserMessage); ok {
			for _, p := range m.Parts {
				if t, ok := p.(saigetypes.TextPart); ok {
					b.WriteString(t.Text)
					b.WriteString("\n")
				}
			}
		}
	}
	return b.String()
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

// toolResults is the text of each tool result in msgs, in order.
func toolResults(msgs []saigetypes.Message) []string {
	var out []string
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
			if r, ok := p.(saigetypes.ToolResultPart); ok {
				out = append(out, fmt.Sprintf("%v", r.Parts))
			}
		}
	}
	return out
}

func (m *toolUser) ask(asks ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.asks, m.askOf = asks, ""
}

// askOfWhom makes it ask a question of someone (asked_of), such as the
// experts on a topic.
func (m *toolUser) askOfWhom(who, question string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.asks, m.askOf = []string{question}, who
}

func (m *toolUser) fail(failing bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failing = failing
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
	// The inbox says so too, where ben is asked, and refuses his edit there.
	var msg inbox.Message
	eventually(t, "the approval reached ben's inbox", func() bool {
		page, _, _ := w.p.InboxPage(ben, inbox.Query{Kind: inbox.KindApproval})
		for _, m := range page {
			if m.Source.Run == run.ID {
				msg = m
				return true
			}
		}
		return false
	})
	if q := msg.Parts[0].Question; q == nil || q.Tool == nil || !q.Tool.ActsForPerson {
		t.Fatalf("the inbox's approval does not say the call acts as ana: %+v", q)
	}
	inboxEdit := platform.Resolution{Action: platform.ActApprove, Arguments: edited.Arguments}
	if _, err := w.p.InboxResolve(ctx, ben, msg.ID, msg.Parts[0].ID, inboxEdit); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("ben edited ana's call from the inbox: %v", err)
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

// What a person's own credential reached is theirs, and every note the memory
// agent writes is sent to other people's runs: a project's to every run in it
// and below it, an agent's to every run of that agent. So a turn whose
// conversation acted for a person with an agent granted a person-scoped
// provider teaches no memory, even with approval turned off, while turns that
// could not carry it are taught as before.
func TestWhatAPersonsCredentialReachedIsNotTaughtToMemory(t *testing.T) {
	w := newWorld(t, platform.Config{OfflineHooks: true})
	w.model.mu.Lock()
	w.model.recall = true
	w.model.mu.Unlock()
	ctx := t.Context()
	// lead, which every world has, is granted no tools.
	if _, err := w.p.Agents.Create(ctx, tenant, agentspec.AgentSpec{Name: "scribe", Model: "offline", Provider: "offline", SystemPrompt: "x"}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := w.p.SetSettings(ctx, tenant, platform.Settings{MemoryAgent: "scribe", MemoryAutoApprove: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.CreateProject(ctx, tenant, ada, platform.ProjectInput{Name: "Launch"}); err != nil {
		t.Fatal(err)
	}
	w.connect(t, ana, credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"})
	turn := func(id, agent, project, after, text string, actor envelope.Actor) runs.Run {
		t.Helper()
		r, err := w.p.Converse(ctx, tenant, platform.Turn{ID: id, Agent: agent, Text: text, Project: project, After: after, Actor: actor})
		if err != nil {
			t.Fatal(err)
		}
		return await(t, w.p, r.ID, runs.Completed)
	}

	// Ana's turn in Launch reads her issues with her own token.
	first := turn("api-ana-1", "worker", "Launch", "", "Any open bugs?", platform.ActorOf(ana))
	if first.Answer != found {
		t.Fatalf("ana's turn answered %q", first.Answer)
	}
	// An agent with no tools of its own answering her next turn is sent
	// what the first found, and repeats it.
	second := turn("api-ana-2", "lead", "", first.ID, "Say that again", platform.ActorOf(ana))
	if second.Answer != found {
		t.Fatalf("ana's follow-up answered %q", second.Answer)
	}
	// At the root, what is learned would be the agent's own.
	third := turn("api-ana-3", "worker", "", "", "Any open bugs?", platform.ActorOf(ana))

	// A turn that reached nobody's credential is taught as before: an agent
	// with no person's tools, and the agent that has them acting for nobody.
	turn("api-ben-1", "lead", "Launch", "", "Plan the launch", platform.ActorOf(ben))
	turn("api-app-1", "worker", "", "", "Any open bugs?", platform.ActorOf(slackApp))
	launch := memory.ProjectOwner("Launch")
	within(t, "the turns that reached no credential are taught", func() bool {
		return len(w.p.Memory.List(tenant, launch)) > 0 && len(w.p.Memory.List(tenant, "worker")) > 0
	})
	untaught := func(turns ...runs.Run) {
		t.Helper()
		for _, owner := range []string{launch, "worker", "lead"} {
			for _, n := range w.p.Memory.List(tenant, owner) {
				if strings.Contains(n.Text, "PRIV-7") {
					t.Fatalf("what ana's credential reached was kept in %s's notes: %+v", owner, n)
				}
			}
		}
		for _, r := range w.p.Runs.List(tenant, 200) {
			for _, taught := range turns {
				if r.Origin == runs.OriginSystem && r.TagID == "sys-memory-"+taught.ID {
					t.Fatalf("the memory agent was asked about %s", taught.TagID)
				}
			}
		}
	}
	untaught(first, second, third)

	// So ben's next turn in Launch is not sent it.
	bens := turn("api-ben-2", "lead", "Launch", "", "What do we know?", platform.ActorOf(ben))
	if strings.Contains(bens.Answer, "PRIV-7") {
		t.Fatalf("ben's turn answered with ana's issue: %q", bens.Answer)
	}
	sent, _ := w.model.requests()
	for _, s := range sent {
		if strings.Contains(s, "What do we know?") && strings.Contains(s, "PRIV-7") {
			t.Fatalf("ben's turn was sent ana's issue: %.400s", s)
		}
	}

	// A provider made shared since still counts for the turns before the
	// change: what they found was found with ana's token.
	rec, err := w.p.ToolProviders.GetProvider(ctx, tenant, "issues")
	if err != nil {
		t.Fatal(err)
	}
	rec.CredentialScope, rec.CredentialRef = toolprovider.ScopeShared, "env:WARP_SHARED_TOKEN"
	if _, err := w.p.ToolProviders.PutProvider(ctx, rec); err != nil {
		t.Fatal(err)
	}
	later := turn("api-ana-4", "lead", "", first.ID, "Say that again", platform.ActorOf(ana))
	if later.Answer != found {
		t.Fatalf("ana's later follow-up answered %q", later.Answer)
	}
	turn("api-ben-3", "lead", "", "", "Plan the week", platform.ActorOf(ben))
	within(t, "lead's turn at the root is taught", func() bool { return len(w.p.Memory.List(tenant, "lead")) > 0 })
	untaught(later)
	w.leaks(t, environment["WARP_PERSON_ANA_ISSUES"])
}

// failure asks worker once for an actor while its model fails, and returns
// the failed run and the inbox message about it.
func (w *world) failure(t *testing.T, id string, actor envelope.Actor) (runs.Run, inbox.Message) {
	t.Helper()
	return w.failing(t, platform.Turn{ID: id, Agent: "worker", Text: "Any open bugs?", Actor: actor})
}

// failing sends turn while the model fails, and waits for the failure to
// reach the inbox.
func (w *world) failing(t *testing.T, turn platform.Turn) (runs.Run, inbox.Message) {
	t.Helper()
	w.model.fail(true)
	defer w.model.fail(false)
	r, err := w.p.Converse(t.Context(), tenant, turn)
	if err != nil {
		t.Fatal(err)
	}
	failed := await(t, w.p, r.ID, runs.Failed)
	var msg inbox.Message
	eventually(t, "the failure reached the inbox", func() bool {
		page, _, _ := w.p.InboxPage(admin, inbox.Query{Kind: inbox.KindFailure})
		for _, m := range page {
			if m.Source.Run == failed.ID {
				msg = m
				return true
			}
		}
		return false
	})
	return failed, msg
}

// retried is the run a Retry started, from the note its part settled with.
func retried(t *testing.T, w *world, msg inbox.Message) runs.Run {
	t.Helper()
	id, ok := strings.CutPrefix(msg.Parts[0].Outcome.Note, "Started again as ")
	if !ok {
		t.Fatalf("the part settled with %+v", msg.Parts[0].Outcome)
	}
	return await(t, w.p, id, runs.Completed)
}

func offersRetry(t *testing.T, w *world, who auth.Principal, msg inbox.Message) bool {
	t.Helper()
	seen, err := w.p.InboxGet(who, msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	return slices.Contains(seen.Parts[0].Actions, platform.ActRetry)
}

// A retry asks the same turn again for the same person. Ana's failed turn,
// asked for her by the Slack app (act_as), acted for her: only she may press
// Retry, and hers reaches the provider with her token. Ben, who may start
// runs and has connected a credential of his own, is not offered it and is
// refused, and so is the app, so neither her words with his token nor her
// token on his choice is ever run. A turn that acted for nobody is retried
// for nobody, whoever presses it, and two people's presses never join one
// run.
func TestARetryActsOnlyForThePersonTheTurnActedFor(t *testing.T) {
	w := newWorld(t, platform.Config{Backoff: func(int) time.Duration { return time.Millisecond }})
	ctx := t.Context()
	w.connect(t, ana, credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"})
	w.connect(t, ben, credentials.Input{Value: benValue})

	failed, msg := w.failure(t, "api-ana-fails", platform.PersonActor(ana.Subject))
	if failed.Actor.User != "ana" || !msg.Parts[0].Failure.Retryable {
		t.Fatalf("the failed turn acts for %+v, retryable %t", failed.Actor, msg.Parts[0].Failure.Retryable)
	}
	// Asking opened her session with the provider before the model failed.
	reached := len(w.server.Requests())
	retry := platform.Resolution{Action: platform.ActRetry}
	app := slackApp
	app.Scopes = []auth.Permission{auth.RunsRead, auth.RunsWrite}
	for _, who := range []auth.Principal{ben, app} {
		if offersRetry(t, w, who, msg) {
			t.Fatalf("%s is offered Retry on ana's turn", who.Subject)
		}
		if _, err := w.p.InboxResolve(ctx, who, msg.ID, msg.Parts[0].ID, retry); !errors.Is(err, platform.ErrForbidden) {
			t.Fatalf("%s retrying ana's turn = %v, want forbidden", who.Subject, err)
		}
	}
	if got, _ := w.p.InboxGet(ana, msg.ID); got.Parts[0].State != inbox.PartOpen {
		t.Fatalf("a refused retry settled the part: %+v", got.Parts[0])
	}
	if got := w.server.Requests()[reached:]; len(got) != 0 {
		t.Fatalf("a refused retry reached the server with %q", got)
	}
	if !offersRetry(t, w, ana, msg) {
		t.Fatal("ana is not offered Retry on her own turn")
	}
	done, err := w.p.InboxResolve(ctx, ana, msg.ID, msg.Parts[0].ID, retry)
	if err != nil {
		t.Fatalf("ana retrying her turn: %v", err)
	}
	again := retried(t, w, done)
	if again.Actor.User != "ana" || again.Text != failed.Text || again.Answer != "Done." {
		t.Fatalf("ana's retry = actor %+v, text %q, answer %q", again.Actor, again.Text, again.Answer)
	}
	if calls := w.server.Calls(); len(calls) != 1 || calls[0] != "Bearer "+environment["WARP_PERSON_ANA_ISSUES"] {
		t.Fatalf("ana's retry reached the server with %q", calls)
	}

	// The app asking as itself acted for nobody. Ben may retry it, and his
	// retry acts for nobody: his credential is not put behind its words.
	before := len(w.server.Requests())
	_, msg = w.failure(t, "api-app-fails", platform.ActorOf(slackApp))
	if !offersRetry(t, w, ben, msg) {
		t.Fatal("ben is not offered Retry on a turn that acted for nobody")
	}
	done, err = w.p.InboxResolve(ctx, ben, msg.ID, msg.Parts[0].ID, retry)
	if err != nil {
		t.Fatalf("ben retrying the app's turn: %v", err)
	}
	again = retried(t, w, done)
	if again.Actor.User != "" || again.Actor.ID != ben.Subject || again.Answer != "I have no issue tools." {
		t.Fatalf("ben's retry of the app's turn = actor %+v, answer %q", again.Actor, again.Answer)
	}
	if got := w.server.Requests()[before:]; len(got) != 0 {
		t.Fatalf("ben's retry of the app's turn reached the server with %q", got)
	}

	// Two people pressing at once are two runs, each its presser's.
	failed, _ = w.failure(t, "api-app-fails-again", platform.ActorOf(slackApp))
	first, err := w.p.Retry(ctx, ben, failed.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.p.Retry(ctx, cyd, failed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.Actor.ID != ben.Subject || second.Actor.ID != cyd.Subject {
		t.Fatalf("two presses = %s for %+v and %s for %+v", first.ID, first.Actor, second.ID, second.Actor)
	}
	if joined, err := w.p.Retry(ctx, ben, failed.ID); err != nil || joined.ID != first.ID {
		t.Fatalf("ben pressing twice = %s (%v), want his first %s", joined.ID, err, first.ID)
	}
	w.leaks(t, environment["WARP_PERSON_ANA_ISSUES"], benValue)
}

// A message that tagged no agent was answered by the default where it was
// posted, and acted for the person who sent it. Its retry asks the agent that
// answered it again, for the same person, whatever the default is now: once
// the workspace has no default, an untagged message starts nothing, and the
// retry still runs, as Ana, with her own credential.
func TestARetryOfAnUntaggedTurnAsksTheAgentThatAnsweredForTheSamePerson(t *testing.T) {
	w := newWorld(t, platform.Config{Backoff: func(int) time.Duration { return time.Millisecond }})
	ctx := t.Context()
	w.connect(t, ana, credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"})
	setAgents(t, w.p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "worker"})

	failed, msg := w.failing(t, platform.Turn{ID: "api-ana-untagged", Text: "Any open bugs?", Actor: platform.ActorOf(ana)})
	if failed.Agent != "worker" || failed.Actor.User != "ana" {
		t.Fatalf("the untagged turn was answered by %q for %+v", failed.Agent, failed.Actor)
	}
	setAgents(t, w.p, "~", platform.AgentsInput{Enable: []string{"*"}})
	if _, err := w.p.Converse(ctx, tenant, platform.Turn{ID: "api-ana-untagged-now", Text: "Any open bugs?", Actor: platform.ActorOf(ana)}); !errors.Is(err, platform.ErrNoDefault) {
		t.Fatalf("an untagged turn with no default = %v", err)
	}
	done, err := w.p.InboxResolve(ctx, ana, msg.ID, msg.Parts[0].ID, platform.Resolution{Action: platform.ActRetry})
	if err != nil {
		t.Fatalf("ana retrying her untagged turn: %v", err)
	}
	again := retried(t, w, done)
	if again.Agent != "worker" || again.Actor.User != "ana" || again.Answer != "Done." {
		t.Fatalf("ana's retry = agent %q, actor %+v, answer %q", again.Agent, again.Actor, again.Answer)
	}
	if calls := w.server.Calls(); len(calls) != 1 || calls[0] != "Bearer "+environment["WARP_PERSON_ANA_ISSUES"] {
		t.Fatalf("ana's retry reached the server with %q", calls)
	}
	w.leaks(t, environment["WARP_PERSON_ANA_ISSUES"])
}

// A retry of the turn that started a conversation is filed where the
// conversation is, and is refused there once its agent is no longer
// available: the part stays open and nothing reaches the provider. Once the
// agent is available there again, the retry runs in the same project, for
// the same person.
func TestARetryIsRefusedWhereTheConversationIsFiledOnceItsAgentIsNotAvailableThere(t *testing.T) {
	w := newWorld(t, platform.Config{Backoff: func(int) time.Duration { return time.Millisecond }})
	ctx := t.Context()
	field(t, w.p)
	// Retrying a turn filed in Globex takes a member of it, and an account
	// is everyone's to view only, so ana is made one.
	if _, err := w.p.Projects.Update(tenant, "Globex", 1, ada, time.Now(), func(pr *projects.Project) error {
		pr.Members = append(pr.Members, projects.Entry{Who: "ana", Role: projects.Member})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w.connect(t, ana, credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"})

	failed, msg := w.failing(t, platform.Turn{ID: "api-ana-globex", Agent: "worker", Text: "Any open bugs?", Project: "Globex", Actor: platform.ActorOf(ana)})
	if failed.Project != "Globex" || failed.Prev != "" {
		t.Fatalf("the first turn was filed in %q after %q", failed.Project, failed.Prev)
	}
	reached := len(w.server.Requests())
	if err := w.p.SetAgentHome(ctx, tenant, "worker", "Initech", ada); err != nil {
		t.Fatal(err)
	}
	retry := platform.Resolution{Action: platform.ActRetry}
	_, err := w.p.InboxResolve(ctx, ana, msg.ID, msg.Parts[0].ID, retry)
	if !errors.Is(err, platform.ErrNotAvailable) || !strings.Contains(err.Error(), "worker is not available in Globex") {
		t.Fatalf("a retry in Globex of an agent that lives in Initech = %v, want ErrNotAvailable", err)
	}
	if got, _ := w.p.InboxGet(ana, msg.ID); got.Parts[0].State != inbox.PartOpen {
		t.Fatalf("a refused retry settled the part: %+v", got.Parts[0])
	}
	if got := w.server.Requests()[reached:]; len(got) != 0 {
		t.Fatalf("a refused retry reached the server with %q", got)
	}

	if err := w.p.SetAgentHome(ctx, tenant, "worker", "", ada); err != nil {
		t.Fatal(err)
	}
	done, err := w.p.InboxResolve(ctx, ana, msg.ID, msg.Parts[0].ID, retry)
	if err != nil {
		t.Fatalf("ana retrying once worker is available again: %v", err)
	}
	again := retried(t, w, done)
	if again.Project != "Globex" || again.Prev != "" || again.Agent != "worker" || again.Actor.User != "ana" || again.Answer != "Done." {
		t.Fatalf("ana's retry = project %q after %q, agent %q, actor %+v, answer %q", again.Project, again.Prev, again.Agent, again.Actor, again.Answer)
	}
	w.leaks(t, environment["WARP_PERSON_ANA_ISSUES"])
}

// A reply or a choice steers what a run does next. Where the run uses the
// credential of the person it acts for, only that person answers what it
// asks: ben's words, or ben's pick, never reach the provider with ana's
// token, whether he answers on the run or from the inbox. A run that reaches
// no one's own credential is anyone's to answer, as before.
func TestOnlyThePersonARunActsForAnswersWhatItAsksWithTheirCredential(t *testing.T) {
	w := newWorld(t, platform.Config{})
	ctx := t.Context()
	w.connect(t, ana, credentials.Input{Ref: "env:WARP_PERSON_ANA_ISSUES"})
	for _, spec := range []agentspec.AgentSpec{
		{Name: "asker", Model: "offline", Provider: "offline", SystemPrompt: workerPrompt, Tools: []string{"mcp:issues", "ask_person"}},
		{Name: "chatty", Model: "offline", Provider: "offline", SystemPrompt: workerPrompt, Tools: []string{"ask_person"}},
	} {
		if _, err := w.p.Agents.Create(ctx, tenant, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	parked := func(t *testing.T, id, agent string) (runs.Run, inbox.Message) {
		t.Helper()
		r, err := w.p.Converse(ctx, tenant, platform.Turn{ID: id, Agent: agent, Text: "Look something up for me", Actor: platform.ActorOf(ana)})
		if err != nil {
			t.Fatal(err)
		}
		run := await(t, w.p, r.ID, runs.Parked)
		if run.Question == nil {
			t.Fatalf("parked on nothing: %+v", run)
		}
		return run, questionFor(t, w.p, run.ID)
	}
	for _, c := range []struct {
		name      string
		asks      []string
		ben, anas platform.QuestionAnswer
	}{
		{"reply", []string{"What should I search for?"},
			platform.QuestionAnswer{Action: platform.ActReply, Text: "ben's words: payroll"},
			platform.QuestionAnswer{Action: platform.ActReply, Text: "flaky tests"}},
		{"choice", []string{"Which tracker?", "payroll", "billing"},
			platform.QuestionAnswer{Action: platform.ActChoose, Choice: "payroll"},
			platform.QuestionAnswer{Action: platform.ActChoose, Choice: "billing"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w.model.ask(c.asks...)
			calls := len(w.server.Calls())
			run, msg := parked(t, "api-asker-"+c.name, "asker")
			if sees(w.p, ben, msg.ID) || !sees(w.p, ana, msg.ID) {
				t.Errorf("the question is addressed to %v, want ana alone", msg.Audience)
			}
			if _, err := w.p.AnswerQuestion(ctx, ben, run.ID, run.Question.ID, c.ben); !errors.Is(err, platform.ErrForbidden) {
				t.Fatalf("ben answered what ana's run asks = %v, want forbidden", err)
			}
			resolve := platform.Resolution{Action: c.ben.Action, Choice: c.ben.Choice, Text: c.ben.Text}
			if _, err := w.p.InboxResolve(ctx, ben, msg.ID, msg.Parts[0].ID, resolve); err == nil {
				t.Fatal("ben answered what ana's run asks from the inbox")
			}
			if got, _ := w.p.Runs.Get(tenant, run.ID); got.Status != runs.Parked {
				t.Fatalf("a refused answer moved the run to %s", got.Status)
			}
			if got := w.server.Calls()[calls:]; len(got) != 0 {
				t.Fatalf("ben's answer reached the server with %q", got)
			}
			if _, err := w.p.AnswerQuestion(ctx, ana, run.ID, run.Question.ID, c.anas); err != nil {
				t.Fatalf("ana answering her own run: %v", err)
			}
			await(t, w.p, run.ID, runs.Completed)
			if got := w.server.Calls()[calls:]; len(got) != 1 || got[0] != "Bearer "+environment["WARP_PERSON_ANA_ISSUES"] {
				t.Fatalf("ana's answer reached the server with %q", got)
			}

			// Her run of an agent that reaches nobody's own credential is
			// anyone's to answer.
			run, msg = parked(t, "api-chatty-"+c.name, "chatty")
			if !sees(w.p, ben, msg.ID) {
				t.Errorf("a question no one's credential acts on is addressed to %v", msg.Audience)
			}
			if _, err := w.p.AnswerQuestion(ctx, ben, run.ID, run.Question.ID, c.ben); err != nil {
				t.Fatalf("ben answering a run that uses nobody's own credential: %v", err)
			}
			await(t, w.p, run.ID, runs.Completed)
		})
	}
	w.leaks(t, environment["WARP_PERSON_ANA_ISSUES"])
}
