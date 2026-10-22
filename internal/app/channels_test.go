package app

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/spec"
)

// The file's channels and experts merge into the tree as its projects do: a
// channel it declares is created or taken over and changed only by the file,
// an unchanged file makes no version, a console channel survives, and a
// channel that left the file stays, unmanaged. Experts the file sets are the
// file's; experts it leaves out stay the console's.
func TestTheFilesChannelsAndExpertsMergeIntoTheTree(t *testing.T) {
	p := newProjectsPlatform(t)
	const tenant = "acme"
	ada := projects.Writer{By: "ada"}
	file := spec.Workspace{
		Channels: []spec.Channel{{Name: "announcements", Purpose: "Company news"}},
		Experts:  []spec.Expert{{Who: "role:security", Topics: []string{"security"}}},
		Projects: []spec.Project{
			{Name: "Globex", Kind: spec.ProjectAccount,
				Channels: []spec.Channel{{Name: "general", Purpose: "The account"}, {Name: "renewals", Title: "Renewals", Purpose: "Renewal work", Default: "auto", Slack: &spec.ChannelSlack{Client: "slack-acme", Channel: "C0123GLOBEX"}}},
				Experts:  []spec.Expert{{Who: "okta|00u1ana", Topics: []string{"billing", "renewals"}}}},
			{Name: "Field"},
		},
	}
	for range 2 {
		if err := applyProjects(p, tenant, file); err != nil {
			t.Fatal(err)
		}
	}
	renewals, err := p.Projects.Channel(tenant, "Globex", "renewals")
	if err != nil || renewals.Managed != projects.ManagedFile || renewals.Version != 1 || renewals.Default != "auto" || renewals.Purpose != "Renewal work" {
		t.Fatalf("the file's channel = %+v, %v", renewals, err)
	}
	if bound, ok := p.Projects.BoundTo(tenant, "slack-acme", "C0123GLOBEX"); !ok || bound.Name != "renewals" {
		t.Fatalf("the file's binding = %+v, %v", bound, ok)
	}
	if general, _ := p.Projects.Channel(tenant, "Globex", projects.General); general.Purpose != "The account" || general.Managed != projects.ManagedFile {
		t.Fatalf("general as the file declares it = %+v", general)
	}
	if news, _ := p.Projects.Channel(tenant, projects.Root, "announcements"); news.Managed != projects.ManagedFile {
		t.Fatalf("the workspace's channel = %+v", news)
	}
	if _, err := p.UpdateChannel(t.Context(), tenant, "Globex", "renewals", ada, platform.ChannelUpdate{Version: 1, Purpose: "mine"}); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("changing the file's channel = %v", err)
	}
	globex := mustGet(t, p, tenant, "Globex")
	if len(globex.Experts) != 1 || globex.Experts[0].Who != "okta|00u1ana" {
		t.Fatalf("the file's experts = %+v", globex.Experts)
	}
	if _, err := p.SetProjectExperts(tenant, "Globex", ada, platform.Experts{Version: globex.Version}); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("changing the file's experts = %v", err)
	}
	if root := mustGet(t, p, tenant, "~"); len(root.Experts) != 1 || root.Experts[0].Who != "role:security" {
		t.Fatalf("the workspace's experts = %+v", root.Experts)
	}
	// Field's experts are the console's.
	field := mustGet(t, p, tenant, "Field")
	if _, err := p.SetProjectExperts(tenant, "Field", ada, platform.Experts{Version: field.Version, Experts: []projects.Expert{{Who: "vic", Topics: []string{"ops"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateChannel(t.Context(), tenant, "Field", ada, platform.ChannelInput{Name: "console-made"}); err != nil {
		t.Fatal(err)
	}
	// renewals leaves the file; Field's console channel and experts stay.
	file.Projects[0].Channels = file.Projects[0].Channels[:1]
	if err := applyProjects(p, tenant, file); err != nil {
		t.Fatal(err)
	}
	if renewals, _ := p.Projects.Channel(tenant, "Globex", "renewals"); renewals.Managed != "" {
		t.Fatalf("a channel that left the file = %+v", renewals)
	}
	if _, err := p.Projects.Channel(tenant, "Field", "console-made"); err != nil {
		t.Fatalf("a console channel did not survive the file: %v", err)
	}
	if f := mustGet(t, p, tenant, "Field"); len(f.Experts) != 1 || f.Experts[0].Who != "vic" {
		t.Fatalf("the console's experts = %+v", f.Experts)
	}
}

// The knowledge sources the file names are the file's, on the workspace and
// on a project, even as []; a project that leaves them out keeps the
// console's. A run in a project is given its own and the workspace's.
func TestTheFilesKnowledgeIsTheFiles(t *testing.T) {
	p := newProjectsPlatform(t)
	const tenant = "acme"
	ada := projects.Writer{By: "ada"}
	file := spec.Workspace{
		Knowledge: []string{"handbook"},
		Projects: []spec.Project{
			{Name: "Field"},
			{Name: "Globex", Parent: "Field", Kind: spec.ProjectAccount, Knowledge: []string{"globex-notes"}},
		},
	}
	for range 2 {
		if err := applyProjects(p, tenant, file); err != nil {
			t.Fatal(err)
		}
	}
	globex := mustGet(t, p, tenant, "Globex")
	if !slices.Equal(globex.Knowledge, []string{"globex-notes"}) || globex.Version != 1 {
		t.Fatalf("the file's knowledge = %+v at version %d", globex.Knowledge, globex.Version)
	}
	if _, err := p.Projects.SetKnowledge(tenant, "Globex", globex.Version, nil, ada, time.Now()); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("changing the file's knowledge = %v", err)
	}
	if root := mustGet(t, p, tenant, "~"); !slices.Equal(root.Knowledge, []string{"handbook"}) {
		t.Fatalf("the workspace's knowledge = %+v", root.Knowledge)
	}
	field := mustGet(t, p, tenant, "Field")
	if _, err := p.Projects.SetKnowledge(tenant, "Field", field.Version, []string{"playbook"}, ada, time.Now()); err != nil {
		t.Fatalf("the console's knowledge on Field = %v", err)
	}
	if err := applyProjects(p, tenant, file); err != nil {
		t.Fatal(err)
	}
	var given []string
	for _, s := range p.ProjectTree(tenant).Knowledge("Globex") {
		given = append(given, s.Name)
	}
	if !slices.Equal(given, []string{"globex-notes", "playbook", "handbook"}) {
		t.Fatalf("a run in Globex is given %v", given)
	}
	// [] is the file's too: Globex names nothing, and the console cannot
	// change that.
	file.Projects[1].Knowledge = []string{}
	if err := applyProjects(p, tenant, file); err != nil {
		t.Fatal(err)
	}
	globex = mustGet(t, p, tenant, "Globex")
	if len(globex.Knowledge) != 0 {
		t.Fatalf("the file's [] = %+v", globex.Knowledge)
	}
	if _, err := p.Projects.SetKnowledge(tenant, "Globex", globex.Version, []string{"pricing"}, ada, time.Now()); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("changing the file's [] = %v", err)
	}
}

// The skills the file declares are the file's, on the workspace and on a
// project, even as []; a project that leaves them out keeps the console's.
// A message in a project can name its own and the workspace's.
func TestTheFilesSkillsAreTheFiles(t *testing.T) {
	p := newProjectsPlatform(t)
	const tenant = "acme"
	ada := projects.Writer{By: "ada"}
	skill := func(name string) spec.Skill {
		return spec.Skill{Name: name, Description: "Does " + name + ".", Instructions: "Do " + name + "."}
	}
	file := spec.Workspace{
		Skills: []spec.Skill{skill("summarize")},
		Projects: []spec.Project{
			{Name: "Field"},
			{Name: "Globex", Parent: "Field", Kind: spec.ProjectAccount, Skills: []spec.Skill{skill("renewal")}},
		},
	}
	for range 2 {
		if err := applyProjects(p, tenant, file); err != nil {
			t.Fatal(err)
		}
	}
	globex := mustGet(t, p, tenant, "Globex")
	if len(globex.Skills) != 1 || globex.Skills[0].Instructions != "Do renewal." || globex.Version != 1 {
		t.Fatalf("the file's skills = %+v at version %d", globex.Skills, globex.Version)
	}
	if _, err := p.Projects.SetSkills(tenant, "Globex", globex.Version, nil, ada, time.Now()); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("changing the file's skills = %v", err)
	}
	field := mustGet(t, p, tenant, "Field")
	if _, err := p.Projects.SetSkills(tenant, "Field", field.Version, []projects.Skill{{Name: "review", Description: "Review.", Instructions: "Review it."}}, ada, time.Now()); err != nil {
		t.Fatalf("the console's skills on Field = %v", err)
	}
	if err := applyProjects(p, tenant, file); err != nil {
		t.Fatal(err)
	}
	var offered []string
	for _, s := range p.ProjectTree(tenant).Skills("Globex") {
		offered = append(offered, s.Name+"<"+s.From)
	}
	if !slices.Equal(offered, []string{"renewal<Globex", "review<Field", "summarize<"}) {
		t.Fatalf("a message in Globex can name %v", offered)
	}
	file.Projects[1].Skills = []spec.Skill{}
	if err := applyProjects(p, tenant, file); err != nil {
		t.Fatal(err)
	}
	if globex = mustGet(t, p, tenant, "Globex"); len(globex.Skills) != 0 {
		t.Fatalf("the file's [] = %+v", globex.Skills)
	}
	if _, err := p.Projects.SetSkills(tenant, "Globex", globex.Version, []projects.Skill{{Name: "x", Description: "x", Instructions: "x"}}, ada, time.Now()); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("changing the file's [] = %v", err)
	}
}

// The mention kinds each Platform document declares are its workspace's.
func TestTheFilesMentionKindsAreItsWorkspaces(t *testing.T) {
	cfg := spec.Config{Platforms: []spec.Platform{
		{Metadata: spec.Metadata{Name: "acme"}, Spec: spec.PlatformSpec{Workspace: spec.Workspace{MentionKinds: []spec.MentionKind{{Kind: "team", Title: "Teams", Resolves: "role"}}}}},
		{Metadata: spec.Metadata{Name: "globex"}},
	}}
	got := mentionKinds(cfg)
	if len(got) != 1 || len(got["acme"]) != 1 || got["acme"][0] != (platform.MentionKind{Kind: "team", Title: "Teams", Resolves: "role"}) {
		t.Fatalf("mention kinds = %+v", got)
	}
}
