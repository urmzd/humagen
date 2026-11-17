package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/urmzd/humagen/internal/toolprovider"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// Toolset is the callable view of one provider for one grant: the accepted
// tools the server still offers unchanged, minus those the grant does not
// permit. Its Lookup has the shape of pkg/turn/native's Tools interface.
type Toolset struct {
	session *Session
	grant   toolprovider.Grant
	diff    toolprovider.Diff
	byName  map[string]*callable
	denied  map[string]toolprovider.Tool
	names   []string
}

// Toolset lists the server's tools, compares them with accepted and returns
// the callable view for grant. accepted must be a snapshot of this session's
// provider.
func (s *Session) Toolset(ctx context.Context, accepted toolprovider.Snapshot, grant toolprovider.Grant) (*Toolset, error) {
	ts, err := s.toolset(ctx, accepted, grant)
	return ts, s.red.err(err)
}

func (s *Session) toolset(ctx context.Context, accepted toolprovider.Snapshot, grant toolprovider.Grant) (*Toolset, error) {
	if accepted.Provider != s.provider.Name || (accepted.Tenant != "" && accepted.Tenant != s.provider.Tenant) {
		return nil, fmt.Errorf("mcptools: snapshot of %s/%q given to a session on %s/%q",
			accepted.Tenant, accepted.Provider, s.provider.Tenant, s.provider.Name)
	}
	diff, err := s.diff(ctx, accepted)
	if err != nil {
		return nil, err
	}
	ts := &Toolset{session: s, grant: grant, diff: diff, byName: map[string]*callable{}, denied: map[string]toolprovider.Tool{}}
	for _, t := range diff.Unchanged {
		// A tool the grant does not permit is never converted, so its schema
		// cannot keep the tools the grant does permit from loading.
		if !grant.Permits(t.Class) {
			ts.denied[toolprovider.QualifiedName(s.provider.Name, t.Name)] = t
			continue
		}
		c, err := newCallable(s, t, grant)
		if err != nil {
			return nil, err
		}
		ts.byName[c.def.Name] = c
		ts.names = append(ts.names, c.def.Name)
	}
	slices.Sort(ts.names)
	return ts, nil
}

// Diff is the comparison this toolset was built from, for reporting which
// tools were held back.
func (ts *Toolset) Diff() toolprovider.Diff { return ts.diff }

// Lookup returns the callable with the qualified name, or ErrHeldBack for a
// tool that is not accepted as offered, or ErrDenied for one the grant does
// not permit.
func (ts *Toolset) Lookup(name string) (saigetypes.Tool, error) {
	c, err := ts.lookup(name)
	if err != nil {
		return nil, ts.session.red.err(err)
	}
	return c, nil
}

func (ts *Toolset) lookup(name string) (*callable, error) {
	if t, ok := ts.denied[name]; ok {
		return nil, fmt.Errorf("%w: %q is a %s tool and the grant is %s",
			ErrDenied, name, t.Class.Effective(), grantName(ts.grant))
	}
	c, ok := ts.byName[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q is not an accepted tool of provider %q, have %v",
			ErrHeldBack, name, ts.session.provider.Name, ts.names)
	}
	return c, nil
}

// All returns every callable the grant permits, sorted by name.
func (ts *Toolset) All() []saigetypes.Tool {
	out := []saigetypes.Tool{}
	for _, n := range ts.names {
		if c := ts.byName[n]; ts.grant.Permits(c.tool.Class) {
			out = append(out, c)
		}
	}
	return out
}

func grantName(g toolprovider.Grant) string {
	if g == toolprovider.GrantWrite {
		return "write"
	}
	return "read-only"
}

// callable is one accepted tool bound to a session and a grant.
type callable struct {
	session *Session
	tool    toolprovider.Tool
	grant   toolprovider.Grant
	def     saigetypes.ToolDef
}

func newCallable(s *Session, t toolprovider.Tool, grant toolprovider.Grant) (*callable, error) {
	params, stats, err := parameterSchema(t.InputSchema)
	log := s.logger.With("tool", t.Name, "schema_hash", t.SchemaHash)
	if err != nil {
		// The schema itself goes to debug only: it can be large, and the
		// error already names the location and the reason.
		log.Warn("mcptools: input schema refused", "err", err)
		log.Debug("mcptools: refused input schema", "schema", string(t.InputSchema))
		return nil, fmt.Errorf("mcptools: tool %q of %q: %w", t.Name, s.provider.Name, err)
	}
	if stats.Inlined > 0 {
		log.Debug("mcptools: inlined $defs", "defs", stats.Defs, "refs", stats.Inlined)
	}
	capability := saigetypes.ToolCapabilityWrite
	if t.Class.Effective() == toolprovider.ClassRead {
		capability = saigetypes.ToolCapabilityRead
	}
	return &callable{session: s, tool: t, grant: grant, def: saigetypes.ToolDef{
		Name:        toolprovider.QualifiedName(s.provider.Name, t.Name),
		Description: t.Description,
		Parameters:  params,
		Capability:  capability,
	}}, nil
}

func (c *callable) Definition() saigetypes.ToolDef { return c.def }

// ActsForPerson reports whether the tool runs with the credential of the
// person the turn acts for, which it does when its provider is
// person-scoped. It is native.PersonScoped, so an approval of a call to it
// says so and nobody but that person changes what it is called with.
func (c *callable) ActsForPerson() bool {
	return c.session.provider.Scope() == toolprovider.ScopePerson
}

// Execute checks the grant, checks the server still offers the tool exactly
// as accepted, and only then calls it.
func (c *callable) Execute(ctx context.Context, args map[string]any) (string, error) {
	result, err := c.ExecuteRich(ctx, args)
	return textProjection(result), err
}

// ExecuteRich preserves the accepted server's ordered content. Credential
// redaction and the same grant/revision checks apply to both entry points.
func (c *callable) ExecuteRich(ctx context.Context, args map[string]any) (saigetypes.ToolResult, error) {
	result, err := c.execute(ctx, args)
	if err != nil {
		return saigetypes.ToolResult{}, c.session.red.err(err)
	}
	return result, nil
}

var _ saigetypes.RichTool = (*callable)(nil)

func (c *callable) execute(ctx context.Context, args map[string]any) (saigetypes.ToolResult, error) {
	if !c.grant.Permits(c.tool.Class) {
		return saigetypes.ToolResult{}, fmt.Errorf("%w: %q is a %s tool and the grant is %s",
			ErrDenied, c.def.Name, c.tool.Class.Effective(), grantName(c.grant))
	}
	offered, ok, err := c.session.current(ctx, c.tool.Name)
	if err != nil {
		return saigetypes.ToolResult{}, err
	}
	switch {
	case !ok:
		return saigetypes.ToolResult{}, fmt.Errorf("%w: %q is no longer offered by %q", ErrHeldBack, c.def.Name, c.session.provider.Name)
	case offered.Description != c.tool.Description || offered.SchemaHash != c.tool.SchemaHash:
		return saigetypes.ToolResult{}, fmt.Errorf("%w: %q changed on %q since it was accepted", ErrHeldBack, c.def.Name, c.session.provider.Name)
	}
	if args == nil {
		args = map[string]any{}
	}
	log := c.session.logger.With("tool", c.def.Name)
	res, err := c.session.cs.CallTool(ctx, &mcp.CallToolParams{Name: c.tool.Name, Arguments: args})
	if err != nil {
		log.Warn("mcptools: call failed", "err", err)
		return saigetypes.ToolResult{}, fmt.Errorf("mcptools: call %q: %w", c.def.Name, err)
	}
	result, err := richResult(res, c.session.red)
	if err != nil {
		return saigetypes.ToolResult{}, err
	}
	if res.IsError {
		log.Warn("mcptools: tool reported failure", "result", result.Text())
		return saigetypes.ToolResult{}, fmt.Errorf("%w: %s: %s", ErrToolFailed, c.def.Name, result.Text())
	}
	log.Debug("mcptools: call", "class", string(c.tool.Class.Effective()))
	return result, nil
}

// marshalSchema renders the SDK's decoded input schema back to JSON.
func marshalSchema(schema any) (json.RawMessage, error) {
	if schema == nil {
		return nil, nil
	}
	if raw, ok := schema.(json.RawMessage); ok {
		return raw, nil
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("input schema: %w", err)
	}
	return b, nil
}
