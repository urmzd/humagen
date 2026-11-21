package app

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/connectors/cron"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/people"
	"github.com/urmzd/humagen/pkg/spec"
)

var retireAdmin = auth.Principal{Tenant: "acme", Subject: "ada", Kind: auth.User, Roles: []auth.Role{auth.Admin}}

func platformFileOf(agents ...spec.Agent) spec.Config {
	return spec.Config{Platforms: []spec.Platform{{Metadata: spec.Metadata{Name: "acme"}, Spec: spec.PlatformSpec{Agents: agents}}}}
}

// An agent a person made and the file then declares, word for word, is the
// file's although no revision says so: nobody retires it, so the next start
// finds it as the file asks. An agent the file does not declare is retired
// as before.
func TestAnAgentTheFileDeclaresIsNotRetired(t *testing.T) {
	p := newProjectsPlatform(t)
	ctx := t.Context()
	for _, name := range []string{"ops", "mine"} {
		if _, err := p.Agents.Create(ctx, "acme", *fileAgent(name, "").Spec, "ada"); err != nil {
			t.Fatal(err)
		}
	}
	cfg := platformFileOf(fileAgent("ops", ""))
	if err := applyPlatforms(ctx, p, cfg, nil); err != nil {
		t.Fatal(err)
	}
	if h, err := p.Catalog().History(ctx, "acme", "ops"); err != nil || len(h) != 1 || h[0].CreatedBy != "ada" {
		t.Fatalf("the file's unchanged declaration wrote a revision: %+v, %v", h, err)
	}
	if err := p.RetireAgent(ctx, retireAdmin, "OPS", 0); !errors.Is(err, platform.ErrAgentProtected) || !strings.Contains(err.Error(), "declared in the platform file") {
		t.Fatalf("retiring an agent the file declares = %v", err)
	}
	if err := applyPlatforms(ctx, p, cfg, nil); err != nil {
		t.Fatalf("the next start = %v", err)
	}
	if _, err := p.Agents.Get(ctx, "acme", "ops", 0); err != nil {
		t.Fatalf("the file's agent after the refused retire: %v", err)
	}
	if err := p.RetireAgent(ctx, retireAdmin, "mine", 0); err != nil {
		t.Fatalf("retiring an agent the file does not declare = %v", err)
	}
}

// A name retired before the file declared it stays retired: the start goes
// on without it and says so, rather than failing for every workspace or
// handing what the retired agent left to a new one. The file's lists, home
// and schedule naming it are passed over the same way, and the rest of the
// file lands.
func TestAFileDeclaringARetiredNameStartsWithoutIt(t *testing.T) {
	p := newProjectsPlatform(t)
	ctx := t.Context()
	ops := fileAgent("ops", "")
	if _, err := p.Agents.Create(ctx, "acme", *ops.Spec, "ada"); err != nil {
		t.Fatal(err)
	}
	if err := p.Schedules.Add(cron.Schedule{Name: "ops-nightly", Expression: "0 3 * * *", Agent: "ops", Text: "sum", Tenant: "acme"}, platform.About{}); err != nil {
		t.Fatal(err)
	}
	if err := p.RetireAgent(ctx, retireAdmin, "ops", 0); err != nil {
		t.Fatal(err)
	}
	cfg := spec.Config{Platforms: []spec.Platform{{Metadata: spec.Metadata{Name: "acme"}, Spec: spec.PlatformSpec{
		Workspace: spec.Workspace{
			Agents: &spec.Agents{Enable: []string{spec.EveryAgent}, Default: "ops"},
			Projects: []spec.Project{{Name: "Field", Schedules: []spec.Schedule{
				{Name: "ops-recap", Cron: "41 8 * * 1", Agent: "ops", Text: "recap"},
				{Name: "lead-recap", Cron: "41 8 * * 1", Agent: "lead", Text: "recap"},
			}}},
		},
		Agents: []spec.Agent{fileAgent("ops", "Field"), fileAgent("lead", "Field")},
	}}}}
	for start := range 2 {
		var out bytes.Buffer
		if err := applyPlatforms(ctx, p, cfg, slog.New(slog.NewTextHandler(&out, nil))); err != nil {
			t.Fatalf("start %d = %v", start, err)
		}
		for _, at := range []string{"at=agents[0]", "at=workspace.agents", "at=workspace.projects[0].schedules[0]"} {
			if !strings.Contains(out.String(), "level=ERROR") || !strings.Contains(out.String(), "agent=ops") || !strings.Contains(out.String(), at) {
				t.Fatalf("start %d said nothing of the retired name at %s:\n%s", start, at, out.String())
			}
		}
		if _, err := p.Agents.Get(ctx, "acme", "ops", 0); !errors.Is(err, wire.ErrNotFound) {
			t.Fatalf("start %d revived the retired name: %v", start, err)
		}
	}
	if _, err := p.Agents.Create(ctx, "acme", *ops.Spec, "ada"); !errors.Is(err, wire.ErrRetired) {
		t.Fatalf("the retired name was freed: %v", err)
	}
	if h, err := p.Catalog().History(ctx, "acme", "ops"); err == nil && len(h) != 1 {
		t.Fatalf("the file wrote a revision under the retired name: %+v", h)
	}
	if _, paused, err := p.Schedules.Get("acme", "ops-nightly"); err != nil || !paused {
		t.Fatalf("the retired agent's schedule paused = %v, %v", paused, err)
	}
	if _, _, err := p.Schedules.Get("acme", "ops-recap"); err == nil {
		t.Fatal("the file's schedule for the retired agent was created")
	}
	if _, err := p.Agents.Get(ctx, "acme", "lead", 0); err != nil {
		t.Fatalf("the rest of the file did not land: %v", err)
	}
	if sc, _, err := p.Schedules.Get("acme", "lead-recap"); err != nil || sc.Agent != "lead" {
		t.Fatalf("the file's other schedule = %+v, %v", sc, err)
	}
}

// An -agent flag's spec is held as the file's: not retired while the flag
// names it, and a name retired earlier is skipped, not revived and not a
// failed start.
func TestAnAgentFlagNamingARetiredNameStartsWithoutIt(t *testing.T) {
	p := newProjectsPlatform(t)
	ctx := t.Context()
	for _, name := range []string{"ops", "desk"} {
		if _, err := p.Agents.Create(ctx, "acme", *fileAgent(name, "").Spec, "ada"); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.RetireAgent(ctx, retireAdmin, "ops", 0); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	log := slog.New(slog.NewTextHandler(&out, nil))
	if err := registerFlagSpec(ctx, p, "acme", *fileAgent("ops", "").Spec, log); err != nil {
		t.Fatalf("a flag naming a retired name = %v", err)
	}
	if !strings.Contains(out.String(), "level=ERROR") || !strings.Contains(out.String(), "agent=ops") {
		t.Fatalf("nothing said of the retired name:\n%s", out.String())
	}
	if _, err := p.Agents.Get(ctx, "acme", "ops", 0); !errors.Is(err, wire.ErrNotFound) {
		t.Fatalf("the flag revived the retired name: %v", err)
	}
	if err := registerFlagSpec(ctx, p, "acme", *fileAgent("desk", "").Spec, log); err != nil {
		t.Fatal(err)
	}
	if err := p.RetireAgent(ctx, retireAdmin, "desk", 0); !errors.Is(err, platform.ErrAgentProtected) {
		t.Fatalf("retiring an agent a flag names = %v", err)
	}
}

// A retired agent's name stays taken for people too. Were a person signing
// in given it as their handle, a file that still names the retired agent
// would stop the next start on "a person goes by @ops": a sign-in must not
// be able to do that.
func TestAPersonIsNotGivenARetiredAgentsNameSoTheNextStartGoesOn(t *testing.T) {
	p := newProjectsPlatform(t)
	ctx := t.Context()
	if _, err := p.Agents.Create(ctx, "acme", *fileAgent("ops", "").Spec, "ada"); err != nil {
		t.Fatal(err)
	}
	if err := p.RetireAgent(ctx, retireAdmin, "ops", 0); err != nil {
		t.Fatal(err)
	}
	cfg := platformFileOf(fileAgent("ops", ""), fileAgent("lead", ""))
	if err := applyPlatforms(ctx, p, cfg, nil); err != nil {
		t.Fatalf("the start with a retired name in the file: %v", err)
	}
	who, err := p.People.Seen("acme", people.Sighting{Subject: "u-123", Name: "Ops", Source: people.SignIn}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if who.Handle == "ops" {
		t.Errorf("a person was given the retired agent's name as their handle")
	}
	if err := applyPlatforms(ctx, p, cfg, nil); err != nil {
		t.Errorf("the start after the sign-in: %v", err)
	}
	if err := checkNewAgentName(ctx, p, "acme", "ops"); err != nil {
		t.Errorf("the -agent path's name check after the sign-in: %v", err)
	}
}
