package platform_test

import (
	"context"
	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/humagen/pkg/journal"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	saigetypes "github.com/urmzd/saige/agent/types"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

func TestPersonalMemoryStaysWithOwnerAndNeedsSharedProjectOptIn(t *testing.T) {
	p := start(t, call.Limits{})
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	personal, err := p.PersonalProject(who)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: "Shared"}); err != nil {
		t.Fatal(err)
	}
	owner := memory.PersonalOwner(who.Subject)
	note, err := p.Memory.Add(tenant, owner, "Prefer a brief executive summary", memory.Person, memory.Approved, who.Subject, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range []auth.Principal{{Kind: auth.User, Tenant: tenant, Subject: "governor", Roles: []auth.Role{auth.Admin}}, {Kind: auth.Machine, Tenant: tenant, Subject: "ada", Scopes: []auth.Permission{auth.ProjectsGovern}}} {
		if p.Seer(other).MemoryOwner(owner) {
			t.Fatal("governance or machine credential granted private memory")
		}
	}
	run := func(id, project string) runs.Run {
		t.Helper()
		got, err := p.Converse(t.Context(), tenant, platform.Turn{ID: id, Agent: "lead", Text: "Hello", Project: project, Actor: platform.ActorOf(who)})
		if err != nil {
			t.Fatal(err)
		}
		return await(t, p, got.ID, runs.Completed)
	}
	if got := run("personal-context", personal.Name); !strings.Contains(got.Tag().Text, note.Text) {
		t.Fatal("own personal context missing")
	}
	if got := run("shared-without-opt-in", "Shared"); strings.Contains(got.Tag().Text, note.Text) {
		t.Fatal("private memory sent to shared project by default")
	}
	next := prefs.DefaultPersonal()
	next.UsePersonalMemoryInProjects = true
	next.Briefing = "Start with the conclusion"
	if _, err := p.Prefs.SetPersonal(tenant, who.Subject, next); err != nil {
		t.Fatal(err)
	}
	if got := run("shared-with-opt-in", "Shared"); !strings.Contains(got.Tag().Text, note.Text) || !strings.Contains(got.Tag().Text, next.Briefing) {
		t.Fatal("explicit personal context choices ignored")
	}
	before := p.Memory.List(tenant, owner)[0].Uses
	if _, err := p.ProjectContextAs(t.Context(), who, personal.Name, projects.General, "lead"); err != nil {
		t.Fatal(err)
	}
	if p.Memory.List(tenant, owner)[0].Uses != before {
		t.Fatal("preview increased use count")
	}
	if err := p.RemoveMemory(tenant, owner, note.ID); err != nil {
		t.Fatal(err)
	}
	if got := run("after-personal-forget", personal.Name); strings.Contains(got.Tag().Text, note.Text) {
		t.Fatal("forgotten memory sent to later turn")
	}
}

// The security classification is durable before runtime acceptance. A failed
// flag write dispatches nothing, and a retry never rewrites the accepted input.
func TestPersonalContextClassificationFailsClosedAndSurvivesJournalExpiry(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := &faultyIn{Store: open(t)}
		provider := &privateContextWitness{Provider: script(say("Done.")), calls: make(chan struct{}, 10)}
		p := startWith(t, platform.Config{Backends: platform.Backends{State: st}, AgentOptions: []agentrt.Option{agentrt.WithProvider(agentrt.ProviderOffline, func(agentrt.Spec) (saigetypes.Provider, error) { return provider, nil })}})
		who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
		home, err := p.PersonalProject(who)
		if err != nil {
			t.Fatal(err)
		}
		note, err := p.Memory.Add(tenant, memory.PersonalOwner(who.Subject), "Prefer concise answers", memory.Person, memory.Approved, who.Subject, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		turn := platform.Turn{ID: "private-context-classification", Agent: "lead", Project: home.Name, Text: "Hello", Actor: platform.ActorOf(who)}
		st.fail("context.flags")
		if _, err := p.Converse(t.Context(), tenant, turn); err == nil {
			t.Fatal("failed classification admitted")
		}
		select {
		case <-provider.calls:
			t.Fatal("model saw notes without durable classification")
		default:
		}
		if p.Memory.List(tenant, memory.PersonalOwner(who.Subject))[0].Uses != 0 {
			t.Fatal("refused turn counted as use")
		}
		st.fail("")
		run, err := p.Converse(t.Context(), tenant, turn)
		if err != nil {
			t.Fatal(err)
		}
		done := await(t, p, run.ID, runs.Completed)
		if done.Tag().Meta["_humagen.context.personal_memory"] != "true" || !p.ContextFlags.Has(tenant, done.ID) {
			t.Fatal("classification missing before model")
		}
		if _, err := p.Memory.Add(tenant, memory.PersonalOwner(who.Subject), "A later private note", memory.Person, memory.Approved, who.Subject, time.Now()); err != nil {
			t.Fatal(err)
		}
		joined, err := p.Converse(t.Context(), tenant, turn)
		if err != nil || joined.ID != done.ID || joined.Tag().Text != done.Tag().Text {
			t.Fatal("same ID changed accepted context")
		}
		if got := p.Memory.List(tenant, memory.PersonalOwner(who.Subject))[0]; got.ID != note.ID || got.Uses != 1 {
			t.Fatalf("replay counted context twice: %+v", got)
		}
		if err := p.Journal.Flush(); err != nil {
			t.Fatal(err)
		}
		if _, err := journal.Open(st, 1); err != nil {
			t.Fatal(err)
		}
		again, err := runs.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		replayed, err := again.Get(tenant, done.ID)
		if err != nil || replayed.Tag().Meta["_humagen.context.personal_memory"] != "true" || !p.ContextFlags.Has(tenant, done.ID) {
			t.Fatal("journal expiry lost private classification")
		}
	})
}

type privateContextWitness struct {
	saigetypes.Provider
	calls chan struct{}
}

func (p *privateContextWitness) Stream(ctx context.Context, request saigetypes.Request) (<-chan saigetypes.Delta, error) {
	p.calls <- struct{}{}
	return p.Provider.Stream(ctx, request)
}
