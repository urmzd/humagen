package platform_test

import (
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
