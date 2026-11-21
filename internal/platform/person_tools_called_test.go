package platform

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"

	"github.com/urmzd/humagen/internal/mcptools"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/runs"
	runsettings "github.com/urmzd/humagen/pkg/settings"
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
		group, err := p.personalToolGroup(ctx, req)
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
