package platform

import (
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/pkg/call"

	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/runctx"
	runsettings "github.com/urmzd/humagen/pkg/settings"
)

// requestWith is a turn's request for an agent on anthropic/a with fallbacks
// anthropic/b and openai/c, whose trigger was pinned with c.
func requestWith(t *testing.T, c runctx.Context) runtime.Request {
	t.Helper()
	meta := map[string]string{}
	if err := runctx.Pin(meta, c); err != nil {
		t.Fatal(err)
	}
	var req runtime.Request
	req.Meta = meta
	req.Definition.Spec = native.Spec{
		Name: "lead", Provider: "anthropic", Model: "a",
		Fallbacks: []native.ModelRef{{Provider: "anthropic", Model: "b"}, {Provider: "openai", Model: "c"}},
		Tools:     []string{"file_read", "file_write", "mcp:github"},
	}
	return req
}

func turnWith(t *testing.T, c runctx.Context) (runtime.Request, error) {
	t.Helper()
	req, err := preferRunContext(requestWith(t, c))
	if err != nil {
		return req, err
	}
	return boundRunContext(req)
}

func TestATurnAppliesItsRunContext(t *testing.T) {
	ws := runsettings.Layer{
		Ceilings:    runsettings.Ceilings{Models: []string{"anthropic/b", "openai/*"}, Tools: []string{"file_read", "mcp:github"}},
		Preferences: runsettings.Preferences{CompactionTokens: 64_000},
	}
	req, err := turnWith(t, runctx.New("ada", runsettings.Resolve(ws), personctx.Context{}))
	if err != nil {
		t.Fatal(err)
	}
	spec := req.Definition.Spec
	// anthropic/a is outside the ceiling: the first allowed fallback is
	// the model.
	if spec.Provider != "anthropic" || spec.Model != "b" || len(spec.Fallbacks) != 1 || spec.Fallbacks[0].Model != "c" {
		t.Fatalf("models = %s/%s %+v", spec.Provider, spec.Model, spec.Fallbacks)
	}
	if !slices.Equal(spec.Tools, []string{"file_read", "mcp:github"}) {
		t.Fatalf("tools = %v", spec.Tools)
	}
	if req.Compaction == nil || req.Compaction.MaxInputTokens != 64_000 {
		t.Fatalf("compaction = %+v", req.Compaction)
	}

	// A person's preferred model, within the ceiling, replaces the agent's.
	person := runsettings.Layer{Preferences: runsettings.Preferences{Model: "openai/gpt"}}
	req, err = turnWith(t, runctx.New("ada", runsettings.Resolve(ws, person), personctx.Context{}))
	if err != nil {
		t.Fatal(err)
	}
	if s := req.Definition.Spec; s.Provider != "openai" || s.Model != "gpt" {
		t.Fatalf("preferred model = %s/%s", s.Provider, s.Model)
	}

	// A ceiling that allows none of the agent's models refuses the turn.
	none := runsettings.Layer{Ceilings: runsettings.Ceilings{Models: []string{"google/*"}}}
	if _, err := turnWith(t, runctx.New("", runsettings.Resolve(none), personctx.Context{})); !errors.Is(err, ErrRunContext) {
		t.Fatalf("no allowed model = %v", err)
	}

	// No context pinned (a run from before) leaves the definition as it is.
	bare := requestWith(t, runctx.Context{})
	runctx.Unpin(bare.Meta)
	got, err := boundRunContext(bare)
	if err != nil || got.Definition.Spec.Model != "a" || len(got.Definition.Spec.Tools) != 3 || got.Compaction != nil {
		t.Fatalf("unpinned = %+v, %v", got.Definition.Spec, err)
	}
}

// A context that does not match its digest fails the turn: it is never
// repaired or ignored.
func TestATurnRefusesATamperedContext(t *testing.T) {
	req := requestWith(t, runctx.Context{Person: "ada"})
	req.Meta = maps.Clone(req.Meta)
	req.Meta[runctx.MetaContext] = `{"person":"eve","settings":{},"tool_servers":[]}`
	if _, err := preferRunContext(req); !errors.Is(err, ErrRunContext) {
		t.Fatalf("tampered = %v", err)
	}
}

// Every attempt of a run reads its context from the run's trigger, so a
// retry sees what the first attempt saw whatever changed in between.
func TestARetrySeesThePinnedContext(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if err := p.SetRunSettings("acme", runsettings.Layer{Preferences: runsettings.Preferences{Model: "anthropic/b"}}, nil); err != nil {
		t.Fatal(err)
	}
	tag, err := p.pinRunContext(retryTag(), callRoot(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	first := requestWith(t, runctx.Context{})
	first.Meta = tagMeta(tag)
	if err := p.SetRunSettings("acme", runsettings.Layer{Preferences: runsettings.Preferences{Model: "openai/c"}}, nil); err != nil {
		t.Fatal(err)
	}
	// The retry is handed the same trigger.
	retry := requestWith(t, runctx.Context{})
	retry.Meta = tagMeta(tag)
	a, err := preferRunContext(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := preferRunContext(retry)
	if err != nil {
		t.Fatal(err)
	}
	if a.Definition.Spec.Model != "b" || b.Definition.Spec.Model != "b" {
		t.Fatalf("first %s, retry %s", a.Definition.Spec.Model, b.Definition.Spec.Model)
	}
}

func retryTag() event.Envelope {
	return wire.NewEvent(wire.Raise{ID: "retry-1", Tenant: "acme", Agent: "lead", Origin: "api", Text: "hi", Actor: event.Actor{ID: "ada", User: "ada"}})
}

func callRoot() call.Edge { return call.Edge{Root: "retry-1"} }

func tagMeta(tag event.Envelope) map[string]string { return maps.Clone(wire.Meta(tag)) }
