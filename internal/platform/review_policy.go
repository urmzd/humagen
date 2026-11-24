package platform

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/runs"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/event"
)

// reviewGrant is what the host says of the run a GitHub review answers for,
// read from the run's record and the current settings, never from the
// model: the answering agent's review policy, narrowed by its current
// revision and the repository's settings; whether a signal that may approve
// started the run; and the pull request's author as the signed event
// carried it. Anything it cannot read approves nothing.
func (p *Platform) reviewGrant(ctx context.Context, c clients.Client, v transport.View) (github.ReviewGrant, error) {
	run, err := p.Runs.Get(c.Tenant, v.RunID)
	if err != nil || v.Tenant != c.Tenant {
		return github.ReviewGrant{}, errors.New("review context is not available")
	}
	current, enabled := p.currentInstallation(c)
	if !enabled || !current.Routes(run.Agent) {
		return github.ReviewGrant{}, nil
	}
	tag := run.Tag()
	repo, kind, number, ok := channelRepository(tag.Channel)
	if at, atKind, atNumber, atOK := channelRepository(v.Channel); !ok || kind != "pull" || !atOK || !strings.EqualFold(at, repo) || atKind != kind || atNumber != number {
		return github.ReviewGrant{}, nil
	}
	// An agent no longer readable, such as one deleted while its run
	// worked, approves nothing; its review still posts.
	pinned, err := p.Catalog().Get(ctx, run.Tenant, run.Agent, run.Rev)
	if err != nil {
		return github.ReviewGrant{}, nil
	}
	policy := reviewPolicy(pinned.Spec.Review)
	// An agent changed since the run started is held to the stricter of
	// the two revisions; one no longer readable approves nothing.
	if latest, err := p.Catalog().Get(ctx, run.Tenant, run.Agent, 0); err != nil {
		policy.Approve = wire.ReviewApproveNever
	} else {
		policy = stricterReview(policy, reviewPolicy(latest.Spec.Review))
	}
	if settings, read := p.repoSettingsOf(current, repo); !read || settings.NeverApproves() {
		policy.Approve = wire.ReviewApproveNever
	}
	trusted, asker := p.trustedReviewRun(current, run, repo, number)
	return github.ReviewGrant{Policy: policy, Trusted: trusted, Asker: asker, Author: wire.SurfaceOf(tag).GitHubAuthor}, nil
}

// reviewPolicy is an agent's review policy as the presentation reads it.
func reviewPolicy(r wire.Review) presentation.ReviewPolicy {
	r = wire.AgentSpec{Review: r}.Normalize().Review
	return presentation.ReviewPolicy{Approve: r.Approve, BlockOn: r.BlockOn}
}

// stricterReview approves only when both approve, and blocks on the lesser
// severity of the two.
func stricterReview(a, b presentation.ReviewPolicy) presentation.ReviewPolicy {
	if a.Approve != presentation.ApproveWhenClean || b.Approve != presentation.ApproveWhenClean {
		a.Approve = wire.ReviewApproveNever
	}
	rank := func(s string) int {
		if s == "" {
			s = presentation.DefaultBlockOn
		}
		if i := slices.Index(wire.ReviewSeverities, s); i >= 0 {
			return i
		}
		return len(wire.ReviewSeverities)
	}
	if rank(b.BlockOn) > rank(a.BlockOn) {
		a.BlockOn = b.BlockOn
	}
	return a
}

// trustedReviewRun reports whether a signal that may approve started run,
// and the login of the person who asked, if one did. An automatic review is
// trusted unless a person the client does not map resumed it last. A
// comment and a review request are trusted when the person who asked is
// one the client maps (its people, or its askers). A run another run
// called, a rule's run, and a stranger's are not trusted, whatever they
// say. ReviewEvent also refuses an asker who wrote the pull request.
func (p *Platform) trustedReviewRun(c clients.Client, run runs.Run, repo string, number int) (bool, string) {
	if run.Parent != "" || run.Origin != "github" || c.GitHub == nil {
		return false, ""
	}
	tag := run.Tag()
	if strings.HasPrefix(run.TagID, "autoreview/"+url.PathEscape(c.ID)+"/") && run.Actor.ID == "" && wire.ShapeOf(tag) == wire.ShapeReview {
		state, ok := p.PullReviewOf(c.Tenant, c.ID, repo, number)
		if !ok {
			return false, ""
		}
		if state.ResumedBy == "" {
			return true, ""
		}
		return mappedAsker(c, state.ResumedBy), state.ResumedBy
	}
	login := strings.TrimSpace(run.Actor.ID)
	if b, requested := tag.Data.(event.GitHubReviewRequested); requested && login == "" {
		login = strings.TrimSpace(b.Sender)
	}
	if run.Actor.Bot || login == "" {
		return false, ""
	}
	return mappedAsker(c, login), login
}

// mappedAsker reports whether login is a person the client maps: one in
// its people, or one of its askers.
func mappedAsker(c clients.Client, login string) bool {
	if c.GitHub == nil || login == "" {
		return false
	}
	return c.GitHub.People[login] != "" || slices.ContainsFunc(c.GitHub.Askers, func(a string) bool { return strings.EqualFold(a, login) })
}
