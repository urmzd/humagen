package platform

import (
	"strconv"
	"strings"

	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/state"
)

// AutoReviewPushes is how many pushes to one pull request are reviewed
// automatically before review pauses there. A person mentioning
// the app or an agent on the pull request resumes it.
const AutoReviewPushes = 5

// PullReview is what automatic review keeps for one pull request.
type PullReview struct {
	// Reviewed is the last commit reviewed automatically, and From the commit
	// that review started after: empty for a review of the whole pull request.
	Reviewed string `json:"reviewed,omitempty"`
	From     string `json:"from,omitempty"`
	// Pushes counts the pushes reviewed since review started or resumed.
	Pushes int `json:"pushes"`
	// Paused is set by the push that found the count spent.
	Paused bool `json:"paused,omitempty"`
}

func pullReviewKey(client, repo string, number int) string {
	return client + "/" + strings.ToLower(repo) + "#" + strconv.Itoa(number)
}

// PullReviewOf reports what automatic review keeps for a pull request, such
// as whether it is paused, for what the pull request shows.
func (p *Platform) PullReviewOf(tenant, client, repo string, number int) (PullReview, bool) {
	item, err := p.pullReviews.Lookup(tenant, pullReviewKey(client, repo, number))
	return item.Value, err == nil
}

// planAutoReview decides whether a pull request event is reviewed, and from
// which commit. Opening, reopening or leaving draft reviews the whole pull
// request and starts the count afresh. Each push reviews the commits after the
// last one reviewed, until AutoReviewPushes pushes; the next pauses review. A
// redelivery of a reviewed commit plans the same review again, so it joins the
// run it already started.
func (p *Platform) planAutoReview(c clients.Client, req clients.AutoReviewRequest) (from string, review, paused bool, err error) {
	_, err = p.pullReviews.Update(c.Tenant, pullReviewKey(c.ID, req.Repository, req.Number), func(v PullReview, ok bool) (PullReview, error) {
		from, review, paused = "", false, false
		switch {
		case ok && v.Reviewed == req.SHA:
			from, review = v.From, true
			return v, state.ErrUnchanged
		case !req.Push:
			review = true
			return PullReview{Reviewed: req.SHA}, nil
		case v.Paused:
			return v, state.ErrUnchanged
		case v.Pushes >= AutoReviewPushes:
			v.Paused, paused = true, true
			return v, nil
		}
		v.Pushes++
		v.From, v.Reviewed = v.Reviewed, req.SHA
		from, review = v.From, true
		return v, nil
	})
	return from, review, paused, err
}

// resumeAutoReview starts the count afresh after a person addressed the app
// or an agent on the pull request; the next push is reviewed.
func (p *Platform) resumeAutoReview(c clients.Client, repo string, number int) error {
	_, err := p.pullReviews.Update(c.Tenant, pullReviewKey(c.ID, repo, number), func(v PullReview, ok bool) (PullReview, error) {
		if !ok || !v.Paused && v.Pushes == 0 {
			return v, state.ErrUnchanged
		}
		v.Pushes, v.Paused = 0, false
		return v, nil
	})
	return err
}
