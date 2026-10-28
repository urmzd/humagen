// Package mcpfake is an MCP server over streamable HTTP for tests. It offers
// one read tool and records the Authorization header of every request it is
// sent and of every tool call, so a test can say exactly which credential
// reached it, and whether any did.
//
// Only tests import it.
package mcpfake

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/urmzd/humagen/fiber/pkg/toolprovider"
)

// Its one tool, as the server offers it.
const (
	ToolName        = "search"
	ToolDescription = "Search issues"
)

var schema = map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}}

// Server is a running fake.
type Server struct {
	// URL is the endpoint a provider record names.
	URL string

	mu sync.Mutex
	// requests is the Authorization header of every HTTP request, in order.
	requests []string
	// calls is the Authorization header of every tool call, in order.
	calls []string
}

// New starts a fake for the length of the test. Its tool answers with what it
// was sent the request with, so a test also sees that the answer reaching
// the model is scrubbed of it.
func New(t testing.TB) *Server {
	t.Helper()
	s := &Server{}
	srv := mcp.NewServer(&mcp.Implementation{Name: "issues", Version: "1"}, nil)
	srv.AddTool(&mcp.Tool{Name: ToolName, Description: ToolDescription, InputSchema: schema},
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			auth := ""
			if req.Extra != nil && req.Extra.Header != nil {
				auth = req.Extra.Header.Get("Authorization")
			}
			s.mu.Lock()
			s.calls = append(s.calls, auth)
			s.mu.Unlock()
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "3 open issues, asked with " + auth}}}, nil
		})
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, r.Header.Get("Authorization"))
		s.mu.Unlock()
		h.ServeHTTP(w, r)
	}))
	// A client's event stream stays open until the client goes, so the
	// connections are cut before the server waits for its requests.
	t.Cleanup(func() {
		ts.CloseClientConnections()
		ts.Close()
	})
	s.URL = ts.URL + "/mcp"
	return s
}

// Tools is the tool's whole form, as a deployment declares it for a
// person-scoped provider, accepted as a read.
func (s *Server) Tools(t testing.TB) []toolprovider.Tool {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	tool, err := toolprovider.NewTool(ToolName, ToolDescription, raw)
	if err != nil {
		t.Fatal(err)
	}
	tool.Class = toolprovider.ClassRead
	return []toolprovider.Tool{tool}
}

// Requests is the Authorization header of every request so far.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// Calls is the Authorization header of every tool call so far.
func (s *Server) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
}
