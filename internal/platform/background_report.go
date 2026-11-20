package platform

import (
	"context"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/inbox"
)

// Background work reports back where it was asked. A result that a watch, a
// goal or a task sent when it ended, or that notify sent, reaches a person a
// Slack client maps through that client's machine (SurfaceNoticesFor): as a
// reply in the client's own Slack thread the work was asked from, or in a
// direct message when it was asked anywhere else. No other result is
// forwarded.

// The keys the results background work and notify raise are under.
const (
	keyBackground = "background:"
	keyNotify     = "notify:"
)

// SurfaceNotice is one person a client's machine hears of an inbox message
// for, and, for a background result, the Slack thread to reply in.
type SurfaceNotice struct {
	For   ActAs
	Reply *SurfaceReply
}

// SurfaceReply is where a client's machine posts a background result for the
// person it maps: in its own Slack thread the work was asked from.
type SurfaceReply struct {
	Channel string `json:"channel"`
	TS      string `json:"ts"`
	// Session is the private session to open for the details: a watch's
	// checks or a task's run. Empty when the work ran in the conversation
	// that asked, as a goal's turns and notify do.
	Session string `json:"session,omitempty"`
}

// backgroundReport is what a background result is about: the conversation
// that asked for the work, the session its runs are in when that is another,
// and the runs whose words it may carry.
type backgroundReport struct {
	origin  string
	session string
	runs    []string
}

// backgroundReportOf reads what a result is about, when background work or
// notify raised it. Any other message, or one whose work is gone, is none.
func (p *Platform) backgroundReportOf(tenant string, m inbox.Message) (backgroundReport, bool) {
	if m.Kind != inbox.KindResult || m.To == "" {
		return backgroundReport{}, false
	}
	switch {
	case strings.HasPrefix(m.Key, keyBackground):
		it, err := p.Background.Get(tenant, strings.TrimPrefix(m.Key, keyBackground))
		if err != nil || it.Owner != m.To {
			return backgroundReport{}, false
		}
		r := backgroundReport{origin: it.Session, runs: []string{it.CreatedBy}}
		if it.Thread != it.Session {
			r.session = it.Thread
		}
		if it.Last != "" && it.Last != it.CreatedBy {
			r.runs = append(r.runs, it.Last)
		}
		return r, true
	case strings.HasPrefix(m.Key, keyNotify):
		run, err := p.Runs.Get(tenant, m.Source.Run)
		if err != nil || personOf(run.Actor) != m.To {
			return backgroundReport{}, false
		}
		r := backgroundReport{origin: run.Thread, runs: []string{run.ID}}
		// notify from one of the work's own runs reports where the work was
		// asked, when the run is in the work's session, for its person: a
		// tag that only names the work moves nothing.
		if id := wire.Meta(run.Tag())[metaBackground]; id != "" {
			if it, err := p.Background.Get(tenant, id); err == nil && it.Owner == m.To && it.Thread == run.Thread {
				r.origin = it.Session
				if it.Thread != it.Session {
					r.session = it.Thread
				}
				r.runs = append(r.runs, it.CreatedBy)
			}
		}
		return r, true
	}
	return backgroundReport{}, false
}

// reportsTo reads which client a background result's conversation is a
// Slack thread of, and the thread: empty when it was asked anywhere else.
func (p *Platform) reportsTo(tenant string, r backgroundReport) (client string, at SurfaceReply) {
	th, err := p.ThreadRecord(tenant, r.origin)
	if err != nil || th.Surface == nil {
		return "", SurfaceReply{}
	}
	c, err := p.Clients.Get(tenant, th.Surface.Client)
	if err != nil || c.Surface != clients.Slack || !c.Enabled {
		return "", SurfaceReply{}
	}
	return c.ID, SurfaceReply{Channel: th.Surface.Channel, TS: th.Surface.TS, Session: r.session}
}

// mayReplyIn reports whether a background result may be posted in the
// conversation that asked for it, for the person it is for: they may still
// read the conversation, and none of the runs whose words it carries read
// private context, as a schedule's answer is kept for its creator instead
// of posted (deliverToConnection). What cannot be read fails closed: the
// person is told in their direct messages instead.
func (p *Platform) mayReplyIn(ctx context.Context, person auth.Principal, r backgroundReport) bool {
	th, err := p.ThreadRecord(person.Tenant, r.origin)
	if err != nil || !p.Seer(person).Place(Place{Project: th.Project, Thread: th.ID}) {
		return false
	}
	for _, id := range r.runs {
		run, err := p.Runs.Get(person.Tenant, id)
		if err != nil || p.RunRequiresReviewedSummary(ctx, run, th.Project) {
			return false
		}
	}
	return true
}
