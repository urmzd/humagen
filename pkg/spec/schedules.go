package spec

import (
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/connectors/cron"
	"github.com/urmzd/humagen/internal/legacy"
)

// What becomes of what a scheduled agent posts in a project.
const (
	AgentPostsPublish = "publish"
	AgentPostsReview  = "review"
)

// DeliverProject is the one place a schedule in the file sends its answers
// besides keeping them on the run: the project it is filed in.
const DeliverProject = "project"

// Bounds on a schedule, as the platform holds them.
const (
	maxScheduleText        = 8000
	maxScheduleDescription = 500
)

// scheduleName is what a schedule's name may hold: it is part of the source
// address and the id of every run it raises.
var scheduleName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$`)

// checkSchedules holds the file's agent_posts and its projects' schedules to
// what the platform accepts, each mistake with its path (rule 7): a schedule's
// name is unique in the file, its cron parses, its agent is one the file or
// the platform has and is available in its project as far as the file can
// tell, and it delivers to its project or nowhere. A file has no person to
// send an answer to, so deliver: inbox is refused.
func checkSchedules(sp PlatformSpec) error {
	w := sp.Workspace
	if w.AgentPosts != nil && *w.AgentPosts != AgentPostsPublish && *w.AgentPosts != AgentPostsReview {
		return fmt.Errorf("workspace.agent_posts: %q is publish or review", *w.AgentPosts)
	}
	known := map[string]bool{}
	for _, b := range BuiltinAgents {
		known[b] = true
	}
	for old := range legacy.BuiltinAgents() {
		known[old] = true
	}
	blind := false
	homes := map[string]string{}
	for _, a := range sp.Agents {
		if a.Spec == nil {
			blind = true
			continue
		}
		name := agentspec.NormalizeName(a.Spec.Name)
		known[name] = true
		if a.Project != "" && a.Project != RootParent {
			homes[name] = a.Project
		}
	}
	parents := map[string]string{}
	lists := map[string]*Agents{"": w.Agents}
	for _, p := range w.Projects {
		parents[p.Name] = ParentOf(p)
		lists[p.Name] = p.Agents
	}
	names := map[string]string{}
	for i, p := range w.Projects {
		at := fmt.Sprintf("workspace.projects[%d]", i)
		if p.AgentPosts != "" && p.AgentPosts != AgentPostsPublish && p.AgentPosts != AgentPostsReview {
			return fmt.Errorf("%s.agent_posts: %q is publish or review", at, p.AgentPosts)
		}
		for j, sc := range p.Schedules {
			at := fmt.Sprintf("%s.schedules[%d]", at, j)
			switch {
			case !scheduleName.MatchString(sc.Name):
				return fmt.Errorf("%s.name: %q is letters, digits, dashes and underscores, at most 63", at, sc.Name)
			case names[sc.Name] != "":
				return fmt.Errorf("%s.name: %q is %s's name again; a schedule's name is unique", at, sc.Name, names[sc.Name])
			case sc.Text == "" || utf8.RuneCountInString(sc.Text) > maxScheduleText:
				return fmt.Errorf("%s.text: what the agent is asked, 1 to %d characters", at, maxScheduleText)
			case utf8.RuneCountInString(sc.Description) > maxScheduleDescription:
				return fmt.Errorf("%s.description: at most %d characters", at, maxScheduleDescription)
			case sc.Deliver == "inbox":
				return fmt.Errorf("%s.deliver: a schedule in the file has no creator to send its answers to; deliver to the project, or leave it out", at)
			case sc.Deliver != "" && sc.Deliver != DeliverProject:
				return fmt.Errorf("%s.deliver: %q is project, or left out to keep each answer on its run", at, sc.Deliver)
			}
			names[sc.Name] = at
			if _, err := cron.Parse(sc.Cron); err != nil {
				return fmt.Errorf("%s.cron: %w", at, err)
			}
			if err := checkAgentName(sc.Agent, known, blind); err != nil {
				return fmt.Errorf("%s.agent: %w", at, err)
			}
			if why := unavailable(sc.Agent, p.Name, parents, lists, homes); why != "" {
				return fmt.Errorf("%s.agent: %s is not available in %s: %s", at, sc.Agent, display(p.Name), why)
			}
		}
	}
	return nil
}
