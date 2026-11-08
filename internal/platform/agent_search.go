package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/goodasks"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/threads"
)

// RefreshGoodAsk files a run's ask under its agent while anyone rates the
// whole answer up, and drops it once nobody does, so routing finds an agent
// for the work people were glad it did (pkg/join). A rating of one finding
// is about that finding, not the answer, and does not count. A run that read
// private context is never filed: routing must not learn from it. It is a
// hint for routing, so a failure is logged and never fails the rating.
func (p *Platform) RefreshGoodAsk(tenant, runID string) {
	if p.GoodAsks == nil || runID == "" {
		return
	}
	run, err := p.Runs.Get(tenant, runID)
	if err != nil || run.Agent == "" {
		return
	}
	up := false
	if !run.UsesPrivateContext() {
		var after int64
		for page := 0; page < 10 && !up; page++ {
			records, err := p.Feedback.List(tenant, runID, after)
			if err != nil {
				p.log.Warn("platform: a run's ratings could not be read for routing", "run", runID, "error", err)
				return
			}
			for _, r := range records {
				if r.Rating == "up" && r.FindingID == "" {
					up = true
					break
				}
			}
			if len(records) < feedback.MaxPage {
				break
			}
			after = records[len(records)-1].Seq
		}
	}
	if up {
		err = p.GoodAsks.Keep(tenant, goodasks.Ask{Agent: run.Agent, Run: run.ID, Text: run.Text, At: p.now()})
	} else {
		err = p.GoodAsks.Forget(tenant, run.Agent, run.ID)
	}
	if err != nil {
		p.log.Warn("platform: an ask for routing could not be kept", "run", runID, "error", err)
	}
}

// goodAsks is each agent's asks people rated up, for routing to search. A
// store that cannot be read routes on prompts alone.
func (p *Platform) goodAsks(tenant string) map[string][]string {
	if p.GoodAsks == nil {
		return nil
	}
	asks, err := p.GoodAsks.All(tenant)
	if err != nil {
		p.log.Warn("platform: asks for routing could not be read", "tenant", tenant, "error", err)
		return nil
	}
	return asks
}

// routePreview is what routing would do with a message, agent by agent and
// in the candidates' order: the best few that search finds are asked, and
// every other agent says search passed it over.
func routePreview(ctx context.Context, a join.Asker, cs []join.Candidate, message string) []join.Decision {
	found := join.Search(cs, message, join.Shortlist)
	asked := map[string]join.Decision{}
	for _, d := range join.Route(ctx, a, join.Candidates(found), message) {
		asked[d.Agent] = d
	}
	why := map[string]string{}
	for _, m := range found {
		why[m.Name] = strings.Join(m.Terms, ", ")
	}
	out := make([]join.Decision, len(cs))
	for i, c := range cs {
		d, ok := asked[c.Name]
		if !ok {
			d = join.Decision{Agent: c.Name, Reason: "Search did not find it for this message, so it was not asked."}
		}
		if !d.Join && ok && d.Reason == "" {
			d.Reason = "Found by search (" + why[c.Name] + ") and said no."
		}
		out[i] = d
	}
	return out
}

// search finds the agents in a project that keep passes and that are about
// message, best first, and asks the best few whether they take it, returning
// what it found and those that said yes. It is ErrNoTakers when search finds
// nobody, so no agent is asked about a message that is none of theirs.
func (p *Platform) search(ctx context.Context, tenant string, tree *projects.Tree, project, message string, keep func(string) bool) ([]join.Match, []join.Decision, error) {
	cs, err := p.candidates(ctx, tenant, tree, project, keep)
	if err != nil {
		return nil, nil, err
	}
	found := join.Search(cs, message, join.Shortlist)
	if len(found) == 0 {
		return nil, nil, fmt.Errorf("%w: no agent here is about it; it waits for a person to answer or tag an agent", ErrNoTakers)
	}
	return found, join.Joined(join.Route(ctx, p.asker(tenant), join.Candidates(found), message)), nil
}

// routeSession picks who answers a console message that names nobody where
// no agent is set to answer: the best match that takes it, or with none
// taking it the best match search found, so the message lands. The reason
// is shown with the answer, never before it. A message search finds nobody
// for is refused, saying to mention an agent.
func (p *Platform) routeSession(ctx context.Context, who auth.Principal, project, text string) (agent, picked string, err error) {
	sees := p.Seer(who).Agent
	found, joined, err := p.search(ctx, who.Tenant, p.Projects.Tree(who.Tenant), projects.NameOf(project), text, func(a string) bool { return sees(a) })
	if errors.Is(err, ErrNoTakers) {
		return "", "", fmt.Errorf("%w: no agent here is about this yet; mention one with @", threads.ErrInvalid)
	}
	if err != nil {
		return "", "", err
	}
	if len(joined) > 0 {
		return joined[0].Agent, joinedReason(joined[0]), nil
	}
	return found[0].Name, "Picked by search: matched " + strings.Join(found[0].Terms, ", ") + ".", nil
}

type pickedKey struct{}

// withPicked carries why routing picked the agent to the run it starts.
func withPicked(ctx context.Context, picked string) context.Context {
	if picked == "" {
		return ctx
	}
	return context.WithValue(ctx, pickedKey{}, picked)
}

func pickedOf(ctx context.Context) string {
	s, _ := ctx.Value(pickedKey{}).(string)
	return s
}
