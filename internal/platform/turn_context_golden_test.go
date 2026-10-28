package platform_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/runs"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// The text a turn is raised with is composed from the project chain's
// instructions, its notes, the agent's notes and the person's own context,
// then the message. It is pinned byte for byte: a change to how a turn is
// composed shows here as a diff to review.
func TestTheComposedTurnMatchesItsGolden(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	field(t, p)
	instruct := func(name, text string) {
		t.Helper()
		pr, err := p.GetProject(tenant, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.UpdateProject(ctx, tenant, name, ada, platform.ProjectUpdate{Version: pr.Version, Title: pr.Title, Account: pr.Account, Instructions: text}); err != nil {
			t.Fatal(err)
		}
	}
	instruct("Field", "Answer as the field engineering team.")
	instruct("Globex", "Globex is on the enterprise plan.")
	instruct("Renewals", "Close renewals by the 15th.")
	at := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	owner, err := p.ProjectMemoryOwner(tenant, "Renewals")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Memory.Add(tenant, owner, "The renewal is in March.", memory.Person, memory.Approved, "ada", at); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Memory.Add(tenant, "lead", "Ada prefers tables.", memory.Person, memory.Approved, "ada", at); err != nil {
		t.Fatal(err)
	}
	next := prefs.DefaultPersonal()
	next.UsePersonalMemoryInProjects = true
	next.Briefing = "Start with the conclusion."
	if _, err := p.Prefs.SetPersonal(tenant, "ada", next); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Memory.Add(tenant, memory.PersonalOwner("ada"), "Ada works in Toronto.", memory.Person, memory.Approved, "ada", at); err != nil {
		t.Fatal(err)
	}
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	r, err := p.Converse(ctx, tenant, platform.Turn{ID: "golden", Agent: "lead", Text: "Where does the Globex renewal stand?", Project: "Renewals", Actor: platform.ActorOf(who)})
	if err != nil {
		t.Fatal(err)
	}
	got := wire.TextOf(await(t, p, r.ID, runs.Completed).Tag())
	golden := filepath.Join("testdata", "turn_context.golden")
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("the composed turn changed:\n--- got\n%s\n--- want\n%s", got, want)
	}
}
