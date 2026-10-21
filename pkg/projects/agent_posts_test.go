package projects_test

import (
	"errors"
	"testing"

	"github.com/urmzd/warp/pkg/projects"
)

// An agent's post waits for an owner where everyone reads it, the root and
// company projects, and is published at once in team and account projects,
// unless the project says otherwise.
func TestAgentPostsAreReviewedWhereEveryoneReads(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", projects.Project{Name: "Company news", Kind: projects.Company}, admin)
	mustCreate(t, s, "acme", team("Field", ""), ada)
	mustCreate(t, s, "acme", projects.Project{Name: "Globex", Kind: projects.Account, Parent: "Field"}, ada)
	tree := s.Tree("acme")
	for name, want := range map[string]string{
		projects.Root:  projects.AgentPostsReview,
		"Company news": projects.AgentPostsReview,
		"Field":        projects.AgentPostsPublish,
		"Globex":       projects.AgentPostsPublish,
		"Ghost":        projects.AgentPostsReview,
	} {
		if got := projects.EffectiveAgentPosts(tree, name); got != want {
			t.Errorf("%q = %s, want %s", name, got, want)
		}
	}

	// A project's own setting wins, and changing only it is a change.
	set := func(name string, version int, w projects.Writer, to string) (projects.Project, error) {
		return s.Update("acme", name, version, w, t0, func(p *projects.Project) error { p.AgentPosts = to; return nil })
	}
	if got, err := set("Field", 1, ada, projects.AgentPostsReview); err != nil || got.Version != 2 {
		t.Fatalf("review in Field = %+v, %v", got, err)
	}
	if _, err := set("Company news", 1, admin, projects.AgentPostsPublish); err != nil {
		t.Fatal(err)
	}
	tree = s.Tree("acme")
	if projects.EffectiveAgentPosts(tree, "Field") != projects.AgentPostsReview || projects.EffectiveAgentPosts(tree, "Company news") != projects.AgentPostsPublish {
		t.Fatal("a project's own setting did not win over its kind's")
	}
	if _, err := set("Globex", 1, ada, "sometimes"); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("an unknown setting = %v", err)
	}
}
