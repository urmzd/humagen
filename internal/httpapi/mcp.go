package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
)

// The humagen MCP: the platform's system tools (platform.SystemTools) over
// MCP, at POST /api/mcp, for any MCP client a person signs in with. A caller
// is offered only the tools its permissions allow. Most tools are operations
// of the API contract, which reach their own routes in-process as the caller
// through dispatch, so a call is authorized, validated, audited and logged
// exactly as the same call to the API.

// mcpInstructions is what an MCP client is told about the server.
const mcpInstructions = "Humagen's own tools for running this workspace: its agents, their evals, sessions and runs, alerts, connections, model providers, schedules and settings. Each tool is made as you, with your permissions: you are offered only what you may do. Read before you change: a revise takes the revision it edits from (expected_rev)."

// dispatchedKey carries the caller of a call the MCP dispatches to a route.
// Only dispatch puts it in a request's context, and nothing outside the
// process can, so authed takes it as signed in: the caller signed in at
// /api/mcp, or is an agent's run calling as its person.
type dispatchedKey struct{}

// callerOf is who a request is from: the caller a dispatched call carries,
// or whoever signed in.
func (s *Server) callerOf(r *http.Request) (auth.Principal, error) {
	if who, ok := r.Context().Value(dispatchedKey{}).(auth.Principal); ok {
		return who, nil
	}
	return s.Authn.Authenticate(r)
}

// installDispatch gives the platform's system tools the way to the API's
// routes: routes, the server's own mux.
func (s *Server) installDispatch(routes http.Handler) {
	s.P.SetAPIDispatch(func(ctx context.Context, who auth.Principal, method, path string, q url.Values, body []byte) (int, []byte) {
		var r io.Reader = http.NoBody
		if body != nil {
			r = bytes.NewReader(body)
		}
		return dispatch(ctx, routes, who, method, path, q, r)
	})
}

// mcpEndpoint serves the humagen MCP for one signed-in caller: a server of
// the system tools they may use, stateless, answering in JSON.
func (s *Server) mcpEndpoint(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	tools, err := s.P.SystemTools()
	if err != nil {
		s.Log.Error("httpapi: the system tools could not be read", "error", err)
		fail(w, http.StatusInternalServerError, "the MCP is not available")
		return
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "humagen", Title: "Humagen", Version: "1"}, &mcp.ServerOptions{Instructions: mcpInstructions})
	for _, t := range tools {
		if t.Permission != "" && !who.Can(t.Permission) {
			continue
		}
		srv.AddTool(mcpToolOf(t), callTool(who, t))
	}
	mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}).ServeHTTP(w, r)
}

// mcpToolOf is a system tool as MCP describes one.
func mcpToolOf(t platform.SystemTool) *mcp.Tool {
	return &mcp.Tool{
		Name:        t.Name,
		Title:       t.Title,
		Description: t.Description,
		InputSchema: t.Input,
		Annotations: &mcp.ToolAnnotations{
			Title:           t.Title,
			ReadOnlyHint:    t.ReadOnly,
			DestructiveHint: ptr(t.Destructive),
			IdempotentHint:  t.Idempotent,
			OpenWorldHint:   ptr(false),
		},
	}
}

func ptr[T any](v T) *T { return &v }

// callTool calls t as who. An MCP client answers no message of a run, so a
// tool bound to one takes it from its arguments.
func callTool(who auth.Principal, t platform.SystemTool) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := t.Call(ctx, platform.SystemCall{Who: who}, req.Params.Arguments)
		if err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{IsError: res.IsError, Content: []mcp.Content{&mcp.TextContent{Text: res.Text}}}, nil
	}
}

// dispatch serves one API call in-process as who, through routes, the
// server's own mux: the route authorizes, validates, audits and logs it as it
// would the same call from outside.
func dispatch(ctx context.Context, routes http.Handler, who auth.Principal, method, path string, q url.Values, body io.Reader) (int, []byte) {
	target := path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	r, err := http.NewRequestWithContext(context.WithValue(ctx, dispatchedKey{}, who), method, target, body)
	if err != nil {
		return http.StatusBadRequest, []byte(`{"error":"the call could not be made"}`)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	routes.ServeHTTP(rec, r)
	return rec.status, rec.body.Bytes()
}

// recorder keeps a dispatched call's answer, bounded.
type recorder struct {
	header http.Header
	status int
	wrote  bool
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) {
	if !r.wrote {
		r.status, r.wrote = status, true
	}
}

func (r *recorder) Write(b []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	if room := platform.MaxSystemToolOutput + 1 - r.body.Len(); room > 0 {
		r.body.Write(b[:min(len(b), room)])
	}
	return len(b), nil
}
