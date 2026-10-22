package projects_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/projects"
)

func expert(who string, topics ...string) projects.Expert {
	return projects.Expert{Who: who, Topics: topics}
}

func setExperts(t *testing.T, s *projects.Store, name string, w projects.Writer, es ...projects.Expert) projects.Project {
	t.Helper()
	p, err := s.Get("acme", name)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.SetExperts("acme", name, p.Version, es, w, t0)
	if err != nil {
		t.Fatalf("experts of %q: %v", name, err)
	}
	return got
}

func TestTheNearestExpertWins(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Globex", Parent: "Field", Kind: projects.Account}, ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Initech", Parent: "Field", Kind: projects.Account}, ada)
	setExperts(t, s, projects.Root, admin, expert("role:security", "security"))
	setExperts(t, s, "Field", ada, expert("okta|bo", "billing"), expert("okta|cy", "billing", "renewals"))
	setExperts(t, s, "Globex", ada, expert("okta|ana", "billing"))
	tree := s.Tree("acme")
	for _, c := range []struct {
		project, topic string
		who            []string
		from           string
	}{
		{"Globex", "billing", []string{"okta|ana"}, "Globex"},
		{"Globex", "renewals", []string{"okta|cy"}, "Field"},
		{"Globex", "security", []string{"role:security"}, projects.Root},
		{"Initech", "billing", []string{"okta|bo", "okta|cy"}, "Field"},
		{"Initech", "", []string{"okta|bo", "okta|cy"}, "Field"},
		{"Globex", "", []string{"okta|ana"}, "Globex"},
		{projects.Root, "billing", nil, ""},
		{"Nowhere", "billing", nil, ""},
	} {
		who, from := tree.ExpertsFor(c.project, c.topic)
		if !reflect.DeepEqual(who, c.who) || from != c.from {
			t.Errorf("experts on %q in %q = %v from %q, want %v from %q", c.topic, c.project, who, from, c.who, c.from)
		}
	}
	if got := tree.ExpertTopics("Globex"); !reflect.DeepEqual(got, []string{"billing", "renewals", "security"}) {
		t.Fatalf("topics in Globex = %v", got)
	}
}

func TestTheExpertsLineNamesAtMostSixteenTopicsNearestFirst(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	var near, far []projects.Expert
	for i := range 3 {
		near = append(near, expert(fmt.Sprintf("near-%d", i), topics("n", i*6, 6)...))
	}
	for i := range 3 {
		far = append(far, expert(fmt.Sprintf("far-%d", i), topics("a", i*8, 8)...))
	}
	setExperts(t, s, "Field", ada, near...)
	setExperts(t, s, projects.Root, admin, far...)
	got := s.Tree("acme").ExpertTopics("Field")
	if len(got) != projects.MaxExpertTopics {
		t.Fatalf("%d topics, want %d", len(got), projects.MaxExpertTopics)
	}
	for _, topic := range got {
		if !strings.HasPrefix(topic, "n") {
			t.Fatalf("topics = %v; Field's own come before the root's", got)
		}
	}
	if line := projects.ExpertsLine([]string{"billing", "security"}); line != "Experts here: billing, security. Ask one with ask_person, asked_of sme:<topic>." {
		t.Fatalf("line = %q", line)
	}
	if projects.ExpertsLine(nil) != "" {
		t.Fatal("a project with no experts says something about them")
	}
}

func topics(prefix string, from, n int) []string {
	var out []string
	for i := range n {
		out = append(out, fmt.Sprintf("%s%02d", prefix, from+i))
	}
	return out
}

func TestExpertRules(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	many := make([]projects.Expert, projects.MaxExperts+1)
	for i := range many {
		many[i] = expert(fmt.Sprintf("okta|%d", i), "billing")
	}
	for _, c := range []struct {
		name    string
		experts []projects.Expert
	}{
		{"too many experts", many},
		{"no topics", []projects.Expert{expert("okta|ana")}},
		{"too many topics", []projects.Expert{expert("okta|ana", topics("t", 0, projects.MaxTopics+1)...)}},
		{"a topic in capitals", []projects.Expert{expert("okta|ana", "Billing")}},
		{"a topic twice", []projects.Expert{expert("okta|ana", "billing", "billing")}},
		{"someone twice", []projects.Expert{expert("okta|ana", "billing"), expert("okta|ana", "renewals")}},
		{"everyone", []projects.Expert{expert("*", "billing")}},
		{"no one", []projects.Expert{expert("", "billing")}},
		{"a role that is not one", []projects.Expert{expert("role:Field Eng", "billing")}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := s.SetExperts("acme", "Field", 1, c.experts, ada, t0); !errors.Is(err, projects.ErrInvalid) {
				t.Fatalf("SetExperts = %v, want ErrInvalid", err)
			}
		})
	}
	if _, err := s.SetExperts("acme", "Field", 7, []projects.Expert{expert("okta|ana", "billing")}, ada, t0); !errors.Is(err, projects.ErrVersion) {
		t.Fatalf("a stale version = %v", err)
	}
	if _, err := s.SetExperts("acme", projects.Root, 0, []projects.Expert{expert("okta|ana", "billing")}, ada, t0); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("the root's experts without govern = %v", err)
	}
	p := setExperts(t, s, "Field", ada, expert("okta|ana", "billing"), expert("role:security", "security"))
	if p.Version != 2 {
		t.Fatalf("version = %d", p.Version)
	}
	if h, _ := s.History("acme", "Field", 1, 0); h[0].What != projects.ExpertsChanged || len(h[0].After.Experts) != 2 {
		t.Fatalf("the change = %+v", h[0])
	}
	if same := setExperts(t, s, "Field", ada, expert("okta|ana", "billing"), expert("role:security", "security")); same.Version != 2 {
		t.Fatalf("the same experts again made version %d", same.Version)
	}
	if _, err := s.Update("acme", "Field", 2, ada, t0, func(p *projects.Project) error { p.Experts = nil; return nil }); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("changing experts through Update = %v, want ErrInvalid", err)
	}
	// A project nobody listed experts on still holds an empty list, never nil.
	mustCreate(t, s, "acme", team("Ops", ""), ada)
	if ops, _ := s.Get("acme", "Ops"); ops.Experts == nil {
		t.Fatal("a project's experts are nil")
	}
}

func TestExpertsTheFileSetsAreTheFiles(t *testing.T) {
	s := projects.NewStore()
	got, err := s.Upsert("acme", projects.Project{Name: "Globex", Kind: projects.Account, Experts: []projects.Expert{expert("okta|ana", "billing", "renewals")}},
		[]string{projects.FieldKind, projects.FieldExperts}, "file", t0)
	if err != nil || len(got.Experts) != 1 {
		t.Fatalf("Upsert = %+v, %v", got, err)
	}
	if _, err := s.SetExperts("acme", "Globex", got.Version, nil, ada, t0); !errors.Is(err, projects.ErrManaged) {
		t.Fatalf("changing the file's experts = %v, want ErrManaged", err)
	}
	if who, from := s.Tree("acme").ExpertsFor("Globex", "renewals"); from != "Globex" || len(who) != 1 {
		t.Fatalf("experts = %v from %q", who, from)
	}
}
