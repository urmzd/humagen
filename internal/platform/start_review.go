package platform

import (
	"context"
	"errors"
	"fmt"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
)

// ToolStartReview turns a GitHub run that started as a reply into a review.
// The host decides a review only on signals that cannot be misread
// (githubShape); for anything else the agent reads what the person meant,
// and calls this when they asked for a review in their own words.
const ToolStartReview = "start_review"

// startReviewDone is what the tool says once the run is a review.
const startReviewDone = "This run is now a review. Return only the humagen.review.v2 report; Humagen pins the reviewed commit, renders the report and posts it as the pull request review."

// startReviewAlready is what a call says when the run is a review already:
// the host started it as one, or the run called start_review before.
const startReviewAlready = "start_review: this run is already a review, and a run becomes one once; return only the humagen.review.v2 report"

// startReviewTool is start_review for one turn. It works only for the run a
// person started on a pull request: not on an issue, not for a run an agent
// started for another, and not for one a rule started, whose shape is the
// rule's. It works once: the switch is an action on the run's record, which
// a retry, the native review and the format check read, so the run stays a
// review, and any later call is refused.
func (p *Platform) startReviewTool(req runtime.Request, action func(event.Action) error) saigetypes.Tool {
	return &saigetypes.ToolFunc{
		Def: saigetypes.ToolDef{
			Name:        ToolStartReview,
			Description: "Turn this answer into a review of the pull request. Call it once, before you review, when the person asks for a review in any words (\"have a look before I merge\", \"check this for security\"). Do not call it when they discuss, dispute or ask about an earlier review. After it, return only the humagen.review.v2 report. Only on a pull request, and only in the run the person started.",
			Parameters:  saigetypes.ParameterSchema{Type: "object", Properties: map[string]saigetypes.PropertyDef{}},
		},
		Fn: func(_ context.Context, _ map[string]any) (string, error) {
			if req.RunID == "" {
				return "", fmt.Errorf("start_review: only the run a person started on a pull request may start a review")
			}
			run, err := p.Runs.Get(req.Tenant, req.RunID)
			if err != nil {
				return "", fmt.Errorf("start_review: this run is not recorded")
			}
			if run.Parent != "" {
				return "", fmt.Errorf("start_review: only the run the person started may start a review; this one answers another agent, so answer in prose")
			}
			if run.Actor.ID == "" || run.Actor.Bot {
				return "", fmt.Errorf("start_review: only a run a person started may start a review; no person asked for this one, so answer in prose")
			}
			if _, kind, _, ok := channelRepository(run.Tag().Channel); !ok || kind != "pull" {
				return "", fmt.Errorf("start_review: a review needs a pull request; answer in prose")
			}
			shape, err := p.runShape(run)
			if err != nil {
				return "", fmt.Errorf("start_review: the run's record could not be read; try again")
			}
			if shape == wire.ShapeReview {
				return "", errors.New(startReviewAlready)
			}
			if action == nil {
				return "", fmt.Errorf("start_review: recording is unavailable")
			}
			key := run.Tenant + "\x00" + run.ID
			if _, done := p.startedReviews.LoadOrStore(key, true); done {
				return "", errors.New(startReviewAlready)
			}
			if err := action(event.Action{Name: ToolStartReview, Summary: "Started a review of the pull request"}); err != nil {
				p.startedReviews.Delete(key)
				return "", err
			}
			return startReviewDone, nil
		},
	}
}

// runShape is the shape a run answers with: the one the host saved on its
// tag when it started, or a review once the run called start_review. A run
// from before shapes were saved has none.
func (p *Platform) runShape(run runs.Run) (string, error) {
	shape := wire.ShapeOf(run.Tag())
	if shape != wire.ShapeReply {
		return shape, nil
	}
	events, err := p.Journal.Events(run.Tenant, []string{run.ID})
	if err != nil {
		return shape, err
	}
	for _, e := range events {
		if a, ok := e.Data.(event.Action); ok && a.Name == ToolStartReview && a.Error == "" {
			return wire.ShapeReview, nil
		}
	}
	return shape, nil
}
