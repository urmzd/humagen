package platform_test

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

func TestPinnedSkillResourcesUseTheSelectedImmutablePackageAndTenant(t *testing.T) {
	p := start(t, call.Limits{})
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	if _, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: "Shared"}); err != nil {
		t.Fatal(err)
	}
	d := library.Definition{Skill: projects.Skill{Name: "brief"}, Tools: []string{"file_read"}, Files: []library.File{
		{Path: "SKILL.md", Data: []byte("---\nname: brief\ndescription: Brief decisions\n---\nRead references/style.md.\n")},
		{Path: "references/style.md", Data: []byte("Exact published resource")},
		{Path: "assets/icon.bin", Data: []byte{255, 0}},
	}}
	first, err := p.DraftSkill(who, library.Scope{Kind: "project", Project: "Shared"}, d, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	tested, err := p.TestSkill(t.Context(), who, first.ID, 1, "lead", "Shared", "Try this skill")
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, tested.ID, runs.Completed)
	if _, err := p.PublishSkill(who, first.ID, 1); err != nil {
		t.Fatal(err)
	}
	posted, err := p.StartThread(t.Context(), who, platform.ThreadInput{Project: "Shared", Agents: []string{"lead"}, Text: "$brief Summarize the decision"})
	if err != nil || len(posted.Runs) != 1 {
		t.Fatalf("start skill: %v", err)
	}
	run := await(t, p, posted.Runs[0].ID, runs.Completed)
	req := runtime.Request{RunID: run.ID, Tenant: tenant, Revision: agentrt.Revision{Spec: agentrt.Spec{Name: "lead"}, Rev: 1}}
	if !strings.Contains(run.Tag().Meta[runs.MetaSkills], "references/style.md") {
		t.Fatal("resource manifest was not pinned")
	}
	d.Files[1].Data = []byte("Unpublished changed resource")
	if _, err := p.DraftSkill(who, first.Scope, d, 1, nil); err != nil {
		t.Fatal(err)
	}
	out, err := use(t, p, req, platform.ToolFileRead, map[string]any{"path": "skill:brief/references/style.md"})
	if err != nil || out != "Exact published resource" {
		t.Fatalf("changed pinned resource: %q, %v", out, err)
	}
	for _, path := range []string{"skill:other/references/style.md", "skill:brief/../../secret", "skill:brief/missing.md", "skill:brief/assets/icon.bin"} {
		if _, err := use(t, p, req, platform.ToolFileRead, map[string]any{"path": path}); err == nil {
			t.Errorf("read unselected resource %s", path)
		}
	}
	req.Tenant = "other"
	if _, err := use(t, p, req, platform.ToolFileRead, map[string]any{"path": "skill:brief/references/style.md"}); err == nil {
		t.Fatal("resource crossed tenant")
	}
}

func TestMemoryFoldersRespectNativePermissionsExactPersonalOwnershipAndManagement(t *testing.T) {
	p := start(t, call.Limits{})
	for _, subject := range []string{"ada", "bob"} {
		if _, err := p.Memory.Add(tenant, memory.PersonalOwner(subject), subject+" private preference", memory.Person, memory.Approved, subject, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Scopes: []auth.Permission{auth.RunsRead}}
	rows, err := p.MemoryFolders(t.Context(), who, "", 1, "agent:lead")
	if err != nil || len(rows) != 1 || rows[0].ID != "personal" || rows[0].Notes != 1 || rows[0].CanWrite || rows[0].CanPromote || rows[0].CanChat || rows[0].Management != "opt_in_required" {
		t.Fatalf("personal-only catalog: %+v, %v", rows, err)
	}
	if strings.Contains(rows[0].Title, "bob") || rows[0].Owner != "" {
		t.Fatal("personal identity leaked")
	}
	who.Scopes = append(who.Scopes, auth.RunsWrite)
	rows, err = p.MemoryFolders(t.Context(), who, "", 1)
	if err != nil || !rows[0].CanWrite || !rows[0].CanChat || rows[0].CanPromote {
		t.Fatal("personal capabilities were not confined")
	}
	preferences := p.Prefs.Get(tenant, who.Subject).Personal
	preferences.Remember = true
	if _, err := p.Prefs.SetPersonal(tenant, who.Subject, preferences); err != nil {
		t.Fatal(err)
	}
	rows, _ = p.MemoryFolders(t.Context(), who, "", 1)
	if rows[0].Management != "disabled" {
		t.Fatal("unconfigured automation appeared active")
	}
	who.Kind = auth.Machine
	rows, _ = p.MemoryFolders(t.Context(), who, "", 1, "personal")
	if len(rows) != 0 {
		t.Fatal("machine read personal memory metadata")
	}
	who.Kind = auth.User
	who.Tenant = "another"
	rows, _ = p.MemoryFolders(t.Context(), who, "", 1)
	if len(rows) != 1 || rows[0].Notes != 0 {
		t.Fatal("counts crossed tenant")
	}
}

func TestMemoryFolderSelectionIsResolvedOutsideTheBoundedSearchPage(t *testing.T) {
	p := start(t, call.Limits{})
	who := auth.Principal{Kind: auth.User, Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
	for _, name := range []string{"A", "Z"} {
		if _, err := p.CreateProject(t.Context(), tenant, govern, platform.ProjectInput{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := p.MemoryFolders(t.Context(), who, "nothing-matches", 1, "project:Z")
	if err != nil || len(rows) != 1 || rows[0].ID != "project:Z" || !rows[0].SelectedOnly {
		t.Fatalf("selected owner vanished: %+v, %v", rows, err)
	}
	rows, err = p.MemoryFolders(t.Context(), who, "", 1)
	if err != nil || len(rows) > 4 {
		t.Fatal("catalog exceeded its per-kind bounds")
	}
	for _, row := range rows {
		if row.ID == "workspace" && row.Management != "manual" {
			t.Fatal("company learning was invented")
		}
	}
	if _, err := p.MemoryFolders(t.Context(), who, "", 51); err == nil {
		t.Fatal("unbounded query accepted")
	}
}
