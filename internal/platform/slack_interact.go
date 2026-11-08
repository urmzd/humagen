package platform

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/runs"
	transport "github.com/urmzd/saige/flow/connectors/slack"
)

// The action ids the Slack surface's blocks carry: an approval's two
// buttons and an answer's feedback buttons, each with a #n suffix for its
// position.
const (
	SlackApproveAction  = "aui.approve"
	SlackDenyAction     = "aui.deny"
	SlackFeedbackAction = "aui.feedback"
)

// SlackActionValue is what an approval or feedback button carries: the run,
// and the question it answers or the rating (up or down) it gives.
type SlackActionValue struct {
	Run    string `json:"turnId"`
	Target string `json:"target"`
}

// slackInteract is what a Slack client does when a person presses one of
// its buttons. It acts as the person the client's
// team maps the Slack user to, through the same routes the console uses, and
// does nothing for someone it maps to nobody. Only a failure worth trying
// again is returned, so Slack delivers the press again.
func (p *Platform) slackInteract(c clients.Client) func(context.Context, transport.Interaction) error {
	return func(ctx context.Context, in transport.Interaction) error {
		current, enabled := p.currentInstallation(c)
		if !enabled {
			return nil
		}
		person := p.slackPersonFor(current, in.User)
		if person == "" {
			p.log.Info("platform: a Slack press from someone the client maps to nobody was ignored", "tenant", current.Tenant, "client", current.ID, "user", in.User, "action", cmp.Or(in.ActionID, in.CallbackID))
			return nil
		}
		if in.Kind != transport.InteractAction {
			return nil
		}
		name, _, _ := strings.Cut(in.ActionID, "#")
		var v SlackActionValue
		if json.Unmarshal([]byte(in.Value), &v) != nil || v.Run == "" || v.Target == "" {
			return nil
		}
		who := p.byPolicy(current.Tenant, person)
		var err error
		switch name {
		case SlackApproveAction:
			_, err = p.AnswerQuestion(ctx, who, v.Run, v.Target, QuestionAnswer{Action: ActApprove})
		case SlackDenyAction:
			_, err = p.AnswerQuestion(ctx, who, v.Run, v.Target, QuestionAnswer{Action: ActDeny})
		case SlackFeedbackAction:
			err = p.rateFromSlack(who.Tenant, person, v)
		}
		if err != nil && !refusal(err) {
			return err
		}
		if err != nil {
			p.log.Info("platform: a Slack press was refused", "tenant", current.Tenant, "client", current.ID, "person", person, "action", cmp.Or(in.ActionID, in.CallbackID), "why", err.Error())
		}
		return nil
	}
}

// rateFromSlack records a person's thumbs on a finished run they may read,
// as the console's feedback route does.
func (p *Platform) rateFromSlack(tenant, person string, v SlackActionValue) error {
	if v.Target != "up" && v.Target != "down" {
		return ErrForbidden
	}
	run, err := p.Runs.Get(tenant, v.Run)
	if err != nil {
		return err
	}
	who := p.byPolicy(tenant, person)
	if !p.Seer(who).Run(run) || !run.Status.Terminal() {
		return ErrForbidden
	}
	record, err := p.Feedback.Save(tenant, feedback.Record{Run: run.ID, Subject: person, Rating: v.Target})
	if err != nil {
		return err
	}
	return p.RecordFeedbackCandidate(tenant, record)
}

// refusal reports an error a press got because of what it asked, which Slack
// delivering it again would not change.
func refusal(err error) bool {
	for _, refused := range []error{ErrForbidden, ErrAction, inbox.ErrNotFound, inbox.ErrInvalid, runs.ErrNotFound} {
		if errors.Is(err, refused) {
			return true
		}
	}
	return false
}
