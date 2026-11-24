package platform

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strconv"
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
	trusted, asker, askerID := p.trustedReviewRun(current, run, repo, number)
	authorID, _ := strconv.ParseInt(wire.Meta(tag)[wire.MetaGitHubAuthorID], 10, 64)
	grant := github.ReviewGrant{Policy: policy, Trusted: trusted, Asker: asker, AskerID: askerID, Author: wire.SurfaceOf(tag).GitHubAuthor, AuthorID: authorID}
	if email := current.GitHub.People[asker]; asker != "" && email != "" {
		grant.AskerEmails = []string{email}
	}
	return grant, nil
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
// and the login and GitHub user ID of the person who asked, if one did, as
// the signed event carried them. An automatic review is trusted unless a
// person the client does not map resumed it last. A comment and a review
// request are trusted when the person who asked (the commenter, or who
// requested the review) is one the client maps (its people, or its askers).
// A run another run called, a rule's run, and a stranger's are not trusted,
// whatever they say. ReviewEvent also needs the asker's ID.
//
// Only a run whose trigger this client's GitHub connector raised is
// trusted, and its user IDs believed: its origin is GitHub and the
// platform's ingress stamped it with this client (runs.ClientOf), which it
// writes over whatever the trigger said.
func (p *Platform) trustedReviewRun(c clients.Client, run runs.Run, repo string, number int) (bool, string, int64) {
	if run.Parent != "" || run.Origin != "github" || c.GitHub == nil {
		return false, "", 0
	}
	tag := run.Tag()
	if runs.ClientOf(tag) != c.ID || run.Client != c.ID {
		return false, "", 0
	}
	if strings.HasPrefix(run.TagID, "autoreview/"+url.PathEscape(c.ID)+"/") && run.Actor.ID == "" && wire.ShapeOf(tag) == wire.ShapeReview {
		state, ok := p.PullReviewOf(c.Tenant, c.ID, repo, number)
		if !ok {
			return false, "", 0
		}
		if state.ResumedBy == "" {
			return true, "", 0
		}
		return mappedAsker(c, state.ResumedBy), state.ResumedBy, state.ResumedByID
	}
	login := strings.TrimSpace(run.Actor.ID)
	if b, requested := tag.Data.(event.GitHubReviewRequested); requested && login == "" {
		login = strings.TrimSpace(b.Sender)
	}
	if run.Actor.Bot || login == "" {
		return false, "", 0
	}
	// The sender's ID is the asker's only when the sender is the asker.
	var id int64
	if sender := wire.Meta(tag)[metaGitHubSender]; strings.EqualFold(sender, login) {
		id, _ = strconv.ParseInt(wire.Meta(tag)[wire.MetaGitHubSenderID], 10, 64)
	}
	return mappedAsker(c, login), login, id
}

// metaGitHubSender is the trigger key saige writes a GitHub event's sender
// under.
const metaGitHubSender = "github_sender"

// mappedAsker reports whether login is a person the client maps: one in
// its people, or one of its askers.
func mappedAsker(c clients.Client, login string) bool {
	if c.GitHub == nil || login == "" {
		return false
	}
	return c.GitHub.People[login] != "" || slices.ContainsFunc(c.GitHub.Askers, func(a string) bool { return strings.EqualFold(a, login) })
}
