package platform_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runctx"
	"github.com/urmzd/humagen/pkg/settings"
)

// personTag is a run a person's message starts.
func personTag(tenantName, id, agent, person string, context map[string]string) event.Envelope {
	return wire.NewEvent(wire.Raise{ID: id, Tenant: tenantName, Agent: agent, Origin: "api", Text: "hello",
		Actor: event.Actor{ID: person, Display: person, User: person}, Context: context})
}

func setPersonContext(t *testing.T, p *platform.Platform, who auth.Principal, in personctx.Input) personctx.Context {
	t.Helper()
	got, err := p.SetPersonContext(t.Context(), who, in)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func pinnedContext(t *testing.T, p *platform.Platform, tenantName, runID string) runctx.Context {
	t.Helper()
	c, ok, err := p.RunContext(tenantName, runID)
	if err != nil || !ok {
		t.Fatalf("run %s: context %v, %v", runID, ok, err)
	}
	return c
}

func digest(t *testing.T, c runctx.Context) string {
	t.Helper()
	d, err := c.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// The context is resolved from the workspace, the project chain and the
// person when the run is admitted, and a change made after it reaches only
// the runs admitted after the change.
func TestARunPinsItsContextAtAdmission(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	if err := p.SetRunSettings(tenant, settings.Layer{
		Ceilings:    settings.Ceilings{Models: []string{"offline/*"}, ToolServers: []string{"mcp.example.com"}},
		Preferences: settings.Preferences{CompactionTokens: 50_000},
	}, nil); err != nil {
		t.Fatal(err)
	}
	mine := setPersonContext(t, p, admin, personctx.Input{
		Run: settings.Layer{Preferences: settings.Preferences{Model: "offline/offline"}},
		ToolServers: []personctx.ToolServer{
			{Name: "notes", URL: "https://mcp.example.com/notes"},
			{Name: "elsewhere", URL: "https://other.example.com"},
		},
	})
	run, err := p.Invoke(ctx, personTag(tenant, "pin-1", "lead", admin.Subject, nil))
	if err != nil {
		t.Fatal(err)
	}
	c := pinnedContext(t, p, tenant, run.ID)
	if c.Person != admin.Subject || c.Memory != mine.Memory || c.Settings.Preferences.Model != "offline/offline" ||
		c.Settings.Preferences.CompactionTokens != 50_000 || len(c.ToolServers) != 1 || c.ToolServers[0].Name != "notes" {
		t.Fatalf("pinned = %+v", c)
	}
	before := digest(t, c)

	// The person and the workspace change their minds while it works.
	setPersonContext(t, p, admin, personctx.Input{Version: mine.Version, Run: settings.Layer{Preferences: settings.Preferences{Model: "offline/other"}}})
	if err := p.SetRunSettings(tenant, settings.Layer{}, nil); err != nil {
		t.Fatal(err)
	}
	if got := digest(t, pinnedContext(t, p, tenant, run.ID)); got != before {
		t.Fatalf("the pinned context changed: %s != %s", got, before)
	}
	next, err := p.Invoke(ctx, personTag(tenant, "pin-2", "lead", admin.Subject, nil))
	if err != nil {
		t.Fatal(err)
	}
	if c := pinnedContext(t, p, tenant, next.ID); c.Settings.Preferences.Model != "offline/other" || len(c.ToolServers) != 0 {
		t.Fatalf("a later run = %+v", c)
	}
}

// A trigger cannot bring its own context: what it says is dropped, and the
// platform's is pinned in its place.
func TestATriggerCannotPinItsOwnContext(t *testing.T) {
	p := start(t, call.Limits{})
	wide := map[string]string{}
	if err := runctx.Pin(wide, runctx.Context{Person: "eve", ToolServers: []personctx.ToolServer{{Name: "x", URL: "https://evil.test"}}}); err != nil {
		t.Fatal(err)
	}
	zero := 0
	if err := p.SetRunSettings(tenant, settings.Layer{Ceilings: settings.Ceilings{CallDepth: &zero}}, nil); err != nil {
		t.Fatal(err)
	}
	run, err := p.Invoke(t.Context(), personTag(tenant, "claim-1", "lead", admin.Subject, wide))
	if err != nil {
		t.Fatal(err)
	}
	if c := pinnedContext(t, p, tenant, run.ID); c.Person != admin.Subject || len(c.ToolServers) != 0 || c.Settings.Ceilings.Depth(5) != 0 {
		t.Fatalf("pinned = %+v", c)
	}
}

// A called run inherits its caller's context: the same person, ceilings no
// wider. The call depth ceiling holds a call the deployment would allow.
func TestACalledRunInheritsItsCallersContext(t *testing.T) {
	p := start(t, call.Limits{MaxDepth: 2, MaxFanOut: 2})
	ctx := t.Context()
	one := 1
	if err := p.SetRunSettings(tenant, settings.Layer{Ceilings: settings.Ceilings{CallDepth: &one, Tools: []string{"file_read"}}}, nil); err != nil {
		t.Fatal(err)
	}
	root, err := p.Invoke(ctx, personTag(tenant, "tree-1", "lead", admin.Subject, nil))
	if err != nil {
		t.Fatal(err)
	}
	parent := pinnedContext(t, p, tenant, root.ID)
	child, err := p.Call(ctx, tenant, root.ID, call.Request{Key: "k1", Agent: "helper", Text: "help", Mode: call.Delegate})
	if errors.Is(err, call.ErrInvalid) {
		t.Skip("the root finished before the call was made")
	}
	if err != nil {
		t.Fatal(err)
	}
	c := pinnedContext(t, p, tenant, child.ID)
	if c.Person != admin.Subject || digest(t, c) != digest(t, parent) {
		t.Fatalf("child = %+v, parent %+v", c, parent)
	}

	// A depth of 0 refuses a call the deployment's limit of 2 allows.
	zero := 0
	if err := p.SetRunSettings(tenant, settings.Layer{Ceilings: settings.Ceilings{CallDepth: &zero}}, nil); err != nil {
		t.Fatal(err)
	}
	shallow, err := p.Invoke(ctx, personTag(tenant, "tree-2", "lead", admin.Subject, nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Call(ctx, tenant, shallow.ID, call.Request{Key: "k", Agent: "helper", Text: "x", Mode: call.Delegate}); !errors.Is(err, call.ErrDepth) {
		t.Fatalf("a call under a depth ceiling of 0 = %v", err)
	}
}

// A run may call only agents the person it acts for may read: what its
// agent's grants allow and what its requester may do.
func TestACallIsHeldToWhatTheRequesterMayRead(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	if _, err := p.CreateProject(ctx, tenant, govern, platform.ProjectInput{Name: "Vault", Members: []projects.Entry{{Who: "owner-1", Role: projects.Owner}}}); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []wire.AgentSpec{
		{Name: "vaulted", Model: "offline", Provider: "offline", SystemPrompt: "Keep."},
		{Name: "boss", Model: "offline", Provider: "offline", SystemPrompt: "Ask.", Access: wire.Access{Spawn: []string{"vaulted"}}},
	} {
		if _, err := p.Agents.Create(ctx, tenant, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Projects.SetHome(tenant, "vaulted", "Vault", govern, time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	try := func(id, person string) error {
		run, err := p.Invoke(ctx, personTag(tenant, id, "boss", person, nil))
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Call(ctx, tenant, run.ID, call.Request{Key: "k", Agent: "vaulted", Text: "x", Mode: call.Delegate})
		return err
	}
	if err := try("req-1", "outsider"); !errors.Is(err, platform.ErrForbidden) || !strings.Contains(err.Error(), "may not read") {
		t.Fatalf("a run for someone who may not read the callee = %v", err)
	}
	// A member gets past the requester and stops at the zero limits.
	if err := try("req-2", "owner-1"); !errors.Is(err, call.ErrDepth) {
		t.Fatalf("a run for a member = %v", err)
	}
}

// One workspace's person context never reaches another workspace's runs,
// and the person store answers each person for themselves.
func TestPersonContextsAreApartByTenant(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	if _, err := p.Agents.Create(ctx, "globex", wire.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead."}, "test"); err != nil {
		t.Fatal(err)
	}
	for _, tn := range []string{tenant, "globex"} {
		if err := p.SetRunSettings(tn, settings.Layer{Ceilings: settings.Ceilings{ToolServers: []string{"mcp.example.com"}}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	setPersonContext(t, p, admin, personctx.Input{
		Run:         settings.Layer{Preferences: settings.Preferences{Model: "offline/offline"}},
		ToolServers: []personctx.ToolServer{{Name: "notes", URL: "https://mcp.example.com"}},
	})
	run, err := p.Invoke(ctx, personTag("globex", "g-1", "lead", admin.Subject, nil))
	if err != nil {
		t.Fatal(err)
	}
	if c := pinnedContext(t, p, "globex", run.ID); c.Person != admin.Subject || c.Memory != "" || len(c.ToolServers) != 0 || c.Settings.Preferences.Model != "" {
		t.Fatalf("globex's run got acme's context: %+v", c)
	}
	elsewhere := admin
	elsewhere.Tenant = "globex"
	if got, err := p.PersonContext(elsewhere); err != nil || got.Version != 0 {
		t.Fatalf("globex's ada = %+v, %v", got, err)
	}
	if _, err := p.PersonContext(auth.Principal{Subject: "bot", Tenant: tenant, Kind: auth.Machine}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("a machine's context = %v", err)
	}
}

// A tool server's credential is a reference under the person's own start,
// never someone else's and never a value.
func TestAToolServerCredentialIsThePersonsOwnReference(t *testing.T) {
	p := startWith(t, platform.Config{CredentialRefs: map[string][]string{tenant: {"env:HUMAGEN_PERSON_{subject}_"}}})
	in := func(ref string) personctx.Input {
		return personctx.Input{ToolServers: []personctx.ToolServer{{Name: "notes", URL: "https://mcp.example.com", Credential: ref}}}
	}
	if _, err := p.SetPersonContext(t.Context(), admin, in("env:HUMAGEN_PERSON_ADA_NOTES")); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"env:HUMAGEN_PERSON_EVE_NOTES", "op://People/eve/notes", "sk-live-123"} {
		if _, err := p.SetPersonContext(t.Context(), admin, in(ref)); !errors.Is(err, personctx.ErrInvalid) {
			t.Errorf("%s: %v", ref, err)
		}
	}
}
