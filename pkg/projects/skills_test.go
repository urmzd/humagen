package projects_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/urmzd/saige/agent/skills"

	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

func skill(name string) projects.Skill {
	return projects.Skill{Name: name, Description: "Does " + name + ".", Instructions: "Do " + name + " carefully."}
}

func setSkills(t *testing.T, s *projects.Store, name string, w projects.Writer, names ...string) projects.Project {
	t.Helper()
	p, err := s.Get("acme", name)
	if err != nil {
		t.Fatal(err)
	}
	var ss []projects.Skill
	for _, n := range names {
		ss = append(ss, skill(n))
	}
	got, err := s.SetSkills("acme", name, p.Version, ss, w, t0)
	if err != nil {
		t.Fatalf("skills of %q: %v", name, err)
	}
	return got
}

// offered is a tree's skills for a project as "name<from" pairs.
func offered(tree *projects.Tree, name string) []string {
	var out []string
	for _, s := range tree.Skills(name) {
		out = append(out, s.Name+"<"+s.From)
	}
	return out
}

// A message in a project can name the skills its project and every project
// above it declare, nearest first, each once from the nearest project that
// declares it: never a sibling's, and never a child's.
func TestSkillsAreInheritedDownTheTreeNearestFirst(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Initech", Parent: "Field", Kind: projects.Account}, ada)
	setSkills(t, s, projects.Root, admin, "summarize")
	setSkills(t, s, "Field", ada, "review", "summarize")
	setSkills(t, s, "Globex", ada, "renewal")
	setSkills(t, s, "Initech", ada, "triage", "review")
	tree := s.Tree("acme")
	for _, c := range []struct {
		project string
		want    []string
	}{
		{"Globex", []string{"renewal<Globex", "review<Field", "summarize<Field"}},
		{"Initech", []string{"triage<Initech", "review<Initech", "summarize<Field"}},
		{"Field", []string{"review<Field", "summarize<Field"}},
		{projects.Root, []string{"summarize<"}},
		{"Nowhere", nil},
	} {
		if got := offered(tree, c.project); !reflect.DeepEqual(got, c.want) {
			t.Errorf("skills in %q = %v, want %v", c.project, got, c.want)
		}
	}
	// The nearest declaration is the one used.
	got, from, ok := tree.Skill("Initech", "review")
	if !ok || from != "Initech" || got.Instructions != "Do review carefully." {
		t.Fatalf("review in Initech = %+v from %q, %v", got, from, ok)
	}
	if _, _, ok := tree.Skill("Globex", "triage"); ok {
		t.Fatal("a sibling's skill was found")
	}
}

func TestAChainListsAtMostMaxChainSkillsTheNearestFirst(t *testing.T) {
	s := projects.NewStore()
	names := func(prefix string) []string {
		var out []string
		for j := range projects.MaxSkills {
			out = append(out, fmt.Sprintf("%s%d", prefix, j))
		}
		return out
	}
	setSkills(t, s, projects.Root, admin, names("root")...)
	parent := ""
	for i := range 4 {
		name := fmt.Sprintf("P%d", i)
		mustCreate(t, s, "acme", team(name, parent), ada)
		setSkills(t, s, name, ada, names(fmt.Sprintf("p%d-", i))...)
		parent = name
	}
	got := s.Tree("acme").Skills("P3")
	if len(got) != projects.MaxChainSkills || got[0].From != "P3" || got[len(got)-1].From != "P0" {
		t.Fatalf("%d skills, from %q to %q", len(got), got[0].From, got[len(got)-1].From)
	}
}

func TestSkillRules(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	many := make([]projects.Skill, projects.MaxSkills+1)
	for i := range many {
		many[i] = skill(fmt.Sprintf("skill-%d", i))
	}
	with := func(f func(*projects.Skill)) []projects.Skill {
		sk := skill("review")
		f(&sk)
		return []projects.Skill{sk}
	}
	for _, c := range []struct {
		name   string
		skills []projects.Skill
	}{
		{"too many", many},
		{"a name in capitals", with(func(s *projects.Skill) { s.Name = "Review" })},
		{"a name starting with a digit", with(func(s *projects.Skill) { s.Name = "5x" })},
		{"a name with two dashes", with(func(s *projects.Skill) { s.Name = "a--b" })},
		{"a name ending with a dash", with(func(s *projects.Skill) { s.Name = "a-" })},
		{"a name with an underscore", with(func(s *projects.Skill) { s.Name = "a_b" })},
		{"a long name", with(func(s *projects.Skill) { s.Name = strings.Repeat("a", 65) })},
		{"no description", with(func(s *projects.Skill) { s.Description = " " })},
		{"a long description", with(func(s *projects.Skill) { s.Description = strings.Repeat("a", projects.MaxSkillDescription+1) })},
		{"a description on two lines", with(func(s *projects.Skill) { s.Description = "a\nb" })},
		{"a long argument hint", with(func(s *projects.Skill) { s.ArgumentHint = strings.Repeat("a", projects.MaxArgumentHint+1) })},
		{"no instructions", with(func(s *projects.Skill) { s.Instructions = "" })},
		{"long instructions", with(func(s *projects.Skill) { s.Instructions = strings.Repeat("a", projects.MaxSkillInstructions+1) })},
		{"instructions with a control character", with(func(s *projects.Skill) { s.Instructions = "ring\a" })},
		{"a skill twice", []projects.Skill{skill("review"), skill("review")}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := s.SetSkills("acme", "Field", 1, c.skills, ada, t0); !errors.Is(err, projects.ErrInvalid) {
				t.Fatalf("SetSkills = %v, want ErrInvalid", err)
			}
		})
	}
	if _, err := s.SetSkills("acme", "Field", 7, []projects.Skill{skill("review")}, ada, t0); !errors.Is(err, projects.ErrVersion) {
		t.Fatalf("a stale version = %v", err)
	}
	if _, err := s.SetSkills("acme", projects.Root, 0, []projects.Skill{skill("review")}, ada, t0); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("the workspace's skills without govern = %v", err)
	}
	long := skill("long")
	long.Instructions = strings.Repeat("é", projects.MaxSkillInstructions)
	p, err := s.SetSkills("acme", "Field", 1, []projects.Skill{skill("review"), long}, ada, t0)
	if err != nil || p.Version != 2 || len(p.Skills) != 2 {
		t.Fatalf("Field = %+v, %v", p, err)
	}
	if h, _ := s.History("acme", "Field", 1, 0); h[0].What != projects.SkillsChanged || len(h[0].Before.Skills) != 0 || len(h[0].After.Skills) != 2 {
		t.Fatalf("the change = %+v", h[0])
	}
	if same, _ := s.SetSkills("acme", "Field", 2, []projects.Skill{skill("review"), long}, ada, t0); same.Version != 2 {
		t.Fatalf("the same skills again made version %d", same.Version)
	}
	if _, err := s.Update("acme", "Field", 2, ada, t0, func(p *projects.Project) error { p.Skills = nil; return nil }); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("changing skills through Update = %v, want ErrInvalid", err)
	}
	if cleared := setSkills(t, s, "Field", ada); cleared.Version != 3 || cleared.Skills != nil {
		t.Fatalf("cleared = %+v", cleared)
	}
}

func TestSkillsTheFileSetsAreTheFiles(t *testing.T) {
	s := projects.NewStore()
	got, err := s.Upsert("acme", projects.Project{Name: "Globex", Kind: projects.Account, Skills: []projects.Skill{skill("renewal")}},
		[]string{projects.FieldKind, projects.FieldSkills}, "file", t0)
	if err != nil || len(got.Skills) != 1 {
		t.Fatalf("Upsert = %+v, %v", got, err)
	}
	if _, err := s.SetSkills("acme", "Globex", got.Version, nil, ada, t0); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("changing the file's skills = %v, want ErrManaged", err)
	}
	left, err := s.Upsert("acme", projects.Project{Name: "Globex", Kind: projects.Account}, []string{projects.FieldKind}, "file", t0)
	if err != nil || len(left.Skills) != 1 {
		t.Fatalf("Upsert without skills = %+v, %v", left, err)
	}
	if _, err := s.SetSkills("acme", "Globex", left.Version, []projects.Skill{skill("review")}, ada, t0); err != nil {
		t.Fatalf("the console's skills again = %v", err)
	}
}

// What a project declares survives a restart, and so does the record of the
// change.
func TestSkillsSurviveARestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		mustCreate(t, s, "acme", team("Field", ""), ada)
		mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, ada)
		setSkills(t, s, projects.Root, admin, "summarize")
		setSkills(t, s, "Field", ada, "review")
		again, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got := offered(again.Tree("acme"), "Globex"); !reflect.DeepEqual(got, []string{"review<Field", "summarize<"}) {
			t.Fatalf("skills in Globex after a restart = %v", got)
		}
		if sk, _, _ := again.Tree("acme").Skill("Globex", "review"); sk != skill("review") {
			t.Fatalf("review after a restart = %+v", sk)
		}
	})
}

// A skill's name and description are ones a SKILL.md takes, so every skill
// declared here can be written out as a skill package saige reads.
func TestASkillIsOneASkillPackageTakes(t *testing.T) {
	for _, name := range []string{"review", "a", "q4-report", "a1-b2-c3", strings.Repeat("a", 64), strings.Repeat("a", 65), "Review", "5x", "-a", "a-", "a--b", "a_b", "a.b", "a b", ""} {
		if !projects.ValidSkill(name) {
			continue
		}
		md := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\nDo it.\n", name, strings.Repeat("d", projects.MaxSkillDescription))
		meta, body, err := skills.ParseSkill([]byte(md))
		if err != nil || meta.Name != name || strings.TrimSpace(body) != "Do it." {
			t.Errorf("%q is a skill here and saige reads %+v, %q, %v", name, meta, body, err)
		}
	}
}

// A change's record keeps each skill's name, description and hint, never its
// instructions: the project's own record holds them once, and history,
// which keeps every change, stays small.
func TestAChangeRecordsSkillsWithoutTheirInstructions(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	big := make([]projects.Skill, projects.MaxSkills)
	for i := range big {
		big[i] = projects.Skill{Name: fmt.Sprintf("s%d", i), Description: "Does it.", ArgumentHint: "[x]", Instructions: strings.Repeat("i", projects.MaxSkillInstructions)}
	}
	p, err := s.SetSkills("acme", "Field", 1, big, ada, t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Skills) != projects.MaxSkills || p.Skills[0].Instructions == "" {
		t.Fatalf("the project's record lost its skills' instructions: %+v", p.Skills[0])
	}
	if sk, _, _ := s.Tree("acme").Skill("Field", "s0"); sk.Instructions == "" {
		t.Fatal("the tree lost the instructions")
	}
	h, _ := s.History("acme", "Field", 1, 0)
	after := h[0].After
	if h[0].What != projects.SkillsChanged || after == nil || len(after.Skills) != projects.MaxSkills {
		t.Fatalf("the change = %+v", h[0])
	}
	for _, sk := range after.Skills {
		if sk.Instructions != "" || sk.Name == "" || sk.Description == "" || sk.ArgumentHint != "[x]" {
			t.Fatalf("a change records %+v", sk)
		}
	}
	if raw, _ := json.Marshal(h[0]); len(raw) > 16<<10 {
		t.Fatalf("a skills change is %d bytes", len(raw))
	}
}
