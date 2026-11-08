package platform

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/urmzd/orchagent/flow/address"
	"github.com/urmzd/orchagent/flow/event"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
)

// A schedule that delivers through a connection names which one and where.
// The server writes both into the schedule's meta, so every run it raises
// carries the place it was raised with.
const (
	// MetaDeliverClient is the connection a scheduled answer goes out
	// through. It is the one the schedule names, never another that happens
	// to serve the same place.
	MetaDeliverClient = "humagen.deliver_client"
	// MetaDeliverTo is where: a Slack channel id, or owner/repo#number for a
	// GitHub issue.
	MetaDeliverTo = "humagen.deliver_to"
)

var (
	// slackChannel is a Slack channel id: public (C) or private (G). A
	// schedule posts in channels, not in someone's direct messages.
	slackChannel = regexp.MustCompile(`^[CG][A-Z0-9]{2,31}$`)
	// githubIssue is owner/repo#number.
	githubIssue = regexp.MustCompile(`^([A-Za-z0-9-]{1,39})/([A-Za-z0-9._-]{1,100})#([1-9][0-9]{0,9})$`)
)

// ErrDeliveryTarget refuses a place a schedule cannot deliver to.
var ErrDeliveryTarget = errors.New("platform: delivery target")

// DeliveryTarget checks that a schedule may deliver through a tenant's
// connection to a place, and returns that place's address: the connection
// exists, is on the surface the delivery names, is switched on, and serves
// the place. It is checked when the schedule is written and again when each
// answer goes out.
func (p *Platform) DeliveryTarget(tenant, deliver, client, to string) (clients.Client, string, error) {
	c, err := p.Clients.Get(tenant, client)
	if err != nil {
		return clients.Client{}, "", fmt.Errorf("%w: no connection %q", ErrDeliveryTarget, client)
	}
	switch deliver {
	case DeliverSlack:
		if c.Surface != clients.Slack {
			return c, "", fmt.Errorf("%w: %q is not a Slack connection", ErrDeliveryTarget, client)
		}
		if !slackChannel.MatchString(to) {
			return c, "", fmt.Errorf("%w: a Slack channel id looks like C0123ABC", ErrDeliveryTarget)
		}
		if c.Identity.Team == "" {
			return c, "", fmt.Errorf("%w: the Slack connection %q has no workspace yet", ErrDeliveryTarget, client)
		}
		if !c.Enabled {
			return c, "", fmt.Errorf("%w: the Slack connection %q is off", ErrDeliveryTarget, client)
		}
		return c, "slack://" + c.Identity.Team + "/" + to, nil
	case DeliverGitHub:
		if c.Surface != clients.GitHub {
			return c, "", fmt.Errorf("%w: %q is not a GitHub connection", ErrDeliveryTarget, client)
		}
		m := githubIssue.FindStringSubmatch(to)
		if m == nil {
			return c, "", fmt.Errorf("%w: a GitHub issue looks like owner/repo#12", ErrDeliveryTarget)
		}
		if !slices.ContainsFunc(c.Identity.Owners, func(o string) bool { return strings.EqualFold(o, m[1]) }) {
			return c, "", fmt.Errorf("%w: the GitHub connection %q does not serve %s", ErrDeliveryTarget, client, m[1])
		}
		if !c.Enabled {
			return c, "", fmt.Errorf("%w: the GitHub connection %q is off", ErrDeliveryTarget, client)
		}
		return c, "github://" + m[1] + "/" + m[2] + "/issues/" + m[3], nil
	}
	return c, "", fmt.Errorf("%w: %q delivers through no connection", ErrDeliveryTarget, deliver)
}

// deliverToConnection posts a scheduled run's answer where its schedule said,
// through the connection it named and no other. An answer that must not leave
// as it is (the run read private context, or a person's own credentials) is
// kept for the schedule's creator instead, and so is one whose connection or
// place is gone, saying why, so no answer is lost.
func (p *Platform) deliverToConnection(ctx context.Context, run runs.Run, where string) error {
	if _, _, post := announcementOf(run.Answer); !post {
		return nil
	}
	meta := wire.Meta(run.Tag())
	creator := meta[metaCreatedBy]
	if p.RunRequiresReviewedSummary(ctx, run, p.RunPlace(run.Tenant, run).Project) {
		if creator == "" {
			return nil
		}
		return p.keptFor(run, creator, where)
	}
	c, to, err := p.DeliveryTarget(run.Tenant, where, meta[MetaDeliverClient], meta[MetaDeliverTo])
	if err == nil {
		if current, enabled := p.currentInstallation(c); !enabled || !current.AllowsOutcomes() {
			err = fmt.Errorf("%w: the connection %q may not post answers", ErrDeliveryTarget, c.ID)
		}
	}
	var st *seat
	if err == nil {
		if st = p.surfaces[string(c.Surface)].seat(run.Tenant, c.ID); st == nil {
			err = fmt.Errorf("%w: the connection %q is not running", ErrDeliveryTarget, c.ID)
		}
	}
	if err != nil {
		subject, body, _ := announcementOf(run.Answer)
		if body == "" {
			body = subject
		}
		return p.sendToCreator(run, subject, "Not delivered: "+strings.TrimPrefix(err.Error(), ErrDeliveryTarget.Error()+": ")+".\n\n"+body)
	}
	addr, err := address.Parse(to)
	if err != nil {
		return err
	}
	env := event.Envelope{
		ID: "schedule-answer-" + run.ID, Source: "humagen://schedule", Type: event.TypeCompleted,
		Channel: to, Time: p.now(), Tenant: run.Tenant, Run: run.ID, Agent: run.Agent, Session: run.Thread,
		Data: event.Completed{Text: run.Answer},
	}
	if env, err = p.reviewPublication(ctx, env); err != nil {
		return err
	}
	return st.peer.Deliver(ctx, addr, env)
}
