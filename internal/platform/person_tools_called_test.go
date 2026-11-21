package platform

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/mcptools"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/runs"
	runsettings "github.com/urmzd/humagen/pkg/settings"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// A run another agent called acts for nobody, so it is offered none of the
// tool servers the person who started the tree connected, and never reaches
// one with that person's credential (docs/architecture/security.md).
func TestACalledRunGetsNoneOfItsCallersPersonTools(t *testing.T) {
	const secret = "ana-notes-token"
	t.Setenv("HUMAGEN_PERSON_ANA_NOTES", secret)

	var mu sync.Mutex
	var bearers []string
	server := mcp.NewServer(&mcp.Implementation{Name: "notes", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "lookup", Description: "Read notes", InputSchema: map[string]any{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "a private note"}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	httpServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); h != "" {
			mu.Lock()
			bearers = append(bearers, h)
			mu.Unlock()
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)
	seen := func() int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, b := range bearers {
			if b == "Bearer "+secret {
				n++
			}
		}
		bearers = nil
		return n
	}

	p, err := New(Config{
		Workspace:      t.TempDir(),
		Limits:         call.Limits{MaxDepth: 2, MaxFanOut: 2},
		PersonalHTTP:   httpServer.Client(),
		CredentialRefs: map[string][]string{"acme": {"env:HUMAGEN_PERSON_{subject}_"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	t.Cleanup(func() {
		stop()
		if err := <-done; err != nil {
			t.Errorf("platform stopped with %v", err)
		}
		_ = p.Close()
	})
	for _, spec := range []wire.AgentSpec{
		{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead.", Access: wire.Access{Spawn: []string{"helper"}}},
		{Name: "helper", Model: "offline", Provider: "offline", SystemPrompt: "Help.", Tools: []string{"person_tools"}},
	} {
		if _, err := p.Agents.Create(ctx, "acme", spec, "test"); err != nil {
			t.Fatal(err)
		}
	}

	host := personctx.ToolServer{Name: "notes", URL: httpServer.URL, Credential: "env:HUMAGEN_PERSON_ANA_NOTES"}
	if err := p.SetRunSettings("acme", runsettings.Layer{Ceilings: runsettings.Ceilings{ToolServers: []string{host.Host()}}}, nil); err != nil {
		t.Fatal(err)
	}
	ana := auth.Principal{Tenant: "acme", Subject: "ana"}
	tools, err := p.DiscoverPersonTools(ctx, ana, host)
	if err != nil {
		t.Fatal(err)
	}
	host.Tools = tools
	if _, err := p.SetPersonContext(ctx, ana, personctx.Input{ToolServers: []personctx.ToolServer{host}}); err != nil {
		t.Fatal(err)
	}

	// groupOf is the person-tools group a run's turn is offered, from the
	// context pinned to its tag, or from pinned when it is given. A group
	// refused for want of a person's credential offers none.
	groupOf := func(runID string, pinned map[string]string) int {
		t.Helper()
		run, err := p.Runs.Get("acme", runID)
		if err != nil {
			t.Fatal(err)
		}
		if pinned == nil {
			pinned = wire.Meta(run.Tag())
		}
		req := runtime.Request{Tenant: "acme", RunID: run.ID, Meta: pinned}
		req.Definition.Spec.Tools = []string{personToolsGrant}
		group, err := p.personalToolGroup(ctx, req, run.Actor)
		if errors.Is(err, mcptools.ErrNoPersonCredential) {
			return 0
		}
		if err != nil {
			t.Fatalf("run %s: %v", runID, err)
		}
		return len(group.Tools)
	}

	// The call is made as soon as ana's run is admitted, while it still
	// works. An offline turn can end first, so a tree that did is started
	// again.
	var root, child runs.Run
	for i := range 20 {
		var err error
		root, err = p.Invoke(ctx, wire.NewEvent(wire.Raise{ID: fmt.Sprintf("tree-%d", i), Tenant: "acme", Agent: "lead", Origin: "api", Text: "hello",
			Actor: event.Actor{ID: "ana", Display: "ana", User: "ana"}}))
		if err != nil {
			t.Fatal(err)
		}
		child, err = p.Call(ctx, "acme", root.ID, call.Request{Key: "k1", Agent: "helper", Text: "read her notes", Mode: call.Delegate})
		if err == nil {
			break
		}
		if !errors.Is(err, call.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if child.ID == "" {
		t.Skip("every root finished before the call was made")
	}
	_ = seen()
	// The run ana started is hers: it reaches her server with her token.
	if n := groupOf(root.ID, nil); n != 1 || seen() == 0 {
		t.Fatalf("ana's own run: %d tools", n)
	}
	if c, _, err := p.RunContext("acme", child.ID); err != nil || len(c.ToolServers) != 0 {
		t.Errorf("the called helper's pinned context holds ana's servers: %+v %v", c.ToolServers, err)
	}
	if n := groupOf(child.ID, nil); n != 0 {
		t.Errorf("the called helper was offered %d of ana's personal tools", n)
	}
	// A called run whose context was pinned with the caller's servers, as
	// before Narrow dropped them, is still offered none.
	rootRun, err := p.Runs.Get("acme", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n := groupOf(child.ID, wire.Meta(rootRun.Tag())); n != 0 {
		t.Errorf("a called helper with its caller's pinned context was offered %d of ana's personal tools", n)
	}
	if n := seen(); n != 0 {
		t.Errorf("the called helper reached ana's server with her token %d times", n)
	}
}

// runsFault fails every read of the run index once failRuns is set, as a
// replayed or still-admitting run whose record cannot be read.
type runsFault struct {
	state.Store
	failRuns atomic.Bool
}

func (s *runsFault) Get(ctx context.Context, ns, tenant, key string) ([]byte, error) {
	if ns == runsNamespace && s.failRuns.Load() {
		return nil, statetest.ErrInjected
	}
	return s.Store.Get(ctx, ns, tenant, key)
}

func (s *runsFault) Lookup(ctx context.Context, ns, tenant, key string) (state.Record, error) {
	if ns == runsNamespace && s.failRuns.Load() {
		return state.Record{}, statetest.ErrInjected
	}
	return s.Store.Lookup(ctx, ns, tenant, key)
}

func (s *runsFault) List(ctx context.Context, ns, tenant string, q state.Query) ([]state.Record, string, error) {
	if ns == runsNamespace && s.failRuns.Load() {
		return nil, "", statetest.ErrInjected
	}
	return s.Store.List(ctx, ns, tenant, q)
}

const runsNamespace = "runs.v2"

type noGroups struct{}

func (noGroups) Expand(context.Context, string, string) ([]saigetypes.Tool, bool, error) {
	return nil, false, nil
}

// A called run whose record cannot be read is still a bot's run. The
// catalog resolves its actor from admission, and personal tools follow that
// actor: the caller's pinned servers are never discovered and her credential
// is never sent.
func TestACalledRunWithAnUnreadableRecordGetsNoPersonTools(t *testing.T) {
	const secret = "ana-notes-token"
	t.Setenv("HUMAGEN_PERSON_ANA_NOTES", secret)

	var mu sync.Mutex
	var requests, bearers int
	server := mcp.NewServer(&mcp.Implementation{Name: "notes", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "lookup", Description: "Read notes", InputSchema: map[string]any{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "a private note"}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	httpServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		if r.Header.Get("Authorization") == "Bearer "+secret {
			bearers++
		}
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)
	reset := func() {
		mu.Lock()
		defer mu.Unlock()
		requests, bearers = 0, 0
	}
	counts := func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return requests, bearers
	}

	fault := &runsFault{Store: state.NewMemory()}
	p, err := New(Config{
		Workspace:      t.TempDir(),
		Backends:       Backends{State: fault},
		PersonalHTTP:   httpServer.Client(),
		CredentialRefs: map[string][]string{"acme": {"env:HUMAGEN_PERSON_{subject}_"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	host := personctx.ToolServer{Name: "notes", URL: httpServer.URL, Credential: "env:HUMAGEN_PERSON_ANA_NOTES"}
	if err := p.SetRunSettings("acme", runsettings.Layer{Ceilings: runsettings.Ceilings{ToolServers: []string{host.Host()}}}, nil); err != nil {
		t.Fatal(err)
	}
	ana := auth.Principal{Tenant: "acme", Subject: "ana"}
	tools, err := p.DiscoverPersonTools(t.Context(), ana, host)
	if err != nil {
		t.Fatal(err)
	}
	host.Tools = tools
	if _, err := p.SetPersonContext(t.Context(), ana, personctx.Input{ToolServers: []personctx.ToolServer{host}}); err != nil {
		t.Fatal(err)
	}
	// The context ana's own run was pinned with, servers and all.
	pinned, err := p.newRunContext("acme", "", nil, "ana")
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned.ToolServers) != 1 {
		t.Fatalf("ana's pinned context: %+v", pinned.ToolServers)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	groupFor := func(runID string, actor event.Actor) (native.ToolGroup, error) {
		t.Helper()
		req := requestWith(t, pinned)
		req.Tenant, req.RunID = "acme", runID
		req.Definition.Spec.Tools = []string{personToolsGrant}
		p.admitting.Store(runID, admitted{tenant: "acme", person: "ana", actor: actor})
		p.operationContexts.Store(operationExecutionKey{"acme", runID, 0}, ctx)
		defer p.admitting.Delete(runID)
		defer p.operationContexts.Delete(operationExecutionKey{"acme", runID, 0})
		c := &operationsCatalog{p: p, req: req, groups: noGroups{}}
		return c.ToolGroup(personToolsGrant)
	}

	fault.failRuns.Store(true)
	if _, err := p.Runs.Get("acme", "called"); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("run index read = %v, want the injected failure", err)
	}

	// The fault leaves ana's own run working: it is her actor, so it reaches
	// her server with her token. This proves the fixture can discover.
	reset()
	group, err := groupFor("own", PersonActor("ana"))
	if err != nil || len(group.Tools) != 1 {
		t.Fatalf("ana's own run: %d tools, %v", len(group.Tools), err)
	}
	if _, n := counts(); n == 0 {
		t.Fatal("ana's own run never sent her credential")
	}

	// A run lead called acts for nobody, and its record cannot be read.
	reset()
	group, err = groupFor("called", event.Actor{ID: "lead", Display: "lead", Bot: true})
	if !errors.Is(err, mcptools.ErrNoPersonCredential) || len(group.Tools) != 0 {
		t.Errorf("the called run was offered %d of ana's personal tools, err %v", len(group.Tools), err)
	}
	if r, n := counts(); r != 0 || n != 0 {
		t.Errorf("the called run reached ana's server %d times, %d with her token", r, n)
	}
}
