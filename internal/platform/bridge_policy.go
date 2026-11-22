package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

func (p *Platform) currentInstallation(c clients.Client) (clients.Client, bool) {
	if c.Managed == clients.ManagedStatic {
		return c, c.Enabled
	}
	current, err := p.Clients.Get(c.Tenant, c.ID)
	if err != nil || !current.Enabled {
		return clients.Client{}, false
	}
	return current, true
}
func (p *Platform) bridgeDirection(c clients.Client, channel string) (projects.BridgeDirection, bool) {
	if c.Surface != clients.Slack {
		return projects.BridgeTwoWay, false
	}
	ch, found := p.Projects.BoundTo(c.Tenant, c.ID, channel)
	if !found {
		return projects.BridgeTwoWay, false
	}
	return ch.Slack.Direction.Effective(), true
}
func webhookEvent(surface clients.Surface, r *http.Request, body []byte) string {
	if surface == clients.GitHub {
		return r.Header.Get("X-GitHub-Event")
	}
	if strings.HasSuffix(r.URL.Path, "/commands") {
		return "slash_command"
	}
	var input struct {
		Type  string `json:"type"`
		Event struct {
			Type string `json:"type"`
		} `json:"event"`
	}
	if json.Unmarshal(body, &input) != nil {
		return ""
	}
	if input.Type == "url_verification" {
		return ""
	}
	return input.Event.Type
}

// BridgeDeliveryAllowed is checked with the current authenticated machine and
// current placement before a native host publishes a run outside the console.
func (p *Platform) BridgeDeliveryAllowed(ctx context.Context, who auth.Principal, id, channel, runID string) (bool, error) {
	c, err := p.Clients.Get(who.Tenant, id)
	if err != nil || !machineOf(c, who) {
		return false, clients.ErrNotFound
	}
	run, err := p.Runs.Get(who.Tenant, runID)
	if err != nil || !who.Can(auth.RunsRead) || !p.Seer(who).Run(run) {
		return false, clients.ErrNotFound
	}
	if len(channel) > 64 || channel == "" {
		return false, clients.ErrInvalid
	}
	return p.bridgeMayDeliver(ctx, c, channel, run), nil
}
func (p *Platform) bridgeMayDeliver(ctx context.Context, c clients.Client, channel string, run runs.Run) bool {
	if !c.Enabled || !c.AllowsOutcomes() {
		return false
	}
	direction, bound := p.bridgeDirection(c, channel)
	if bound {
		if !direction.AllowsOutcomes() {
			return false
		}
		ch, ok := p.Projects.BoundTo(c.Tenant, c.ID, channel)
		if !ok || p.RunPlace(c.Tenant, run).Project != ch.Project {
			return false
		}
	}
	if !directMessage(channel) {
		if p.privateThread(c.Tenant, run.Thread) && !p.startedIn(c, channel, run.Thread) {
			return false
		}
		if p.RunRequiresReviewedSummary(ctx, run, p.RunPlace(c.Tenant, run).Project) {
			return false
		}
	}
	if bound {
		level := p.ActionDecision(c.Tenant, p.RunPlace(c.Tenant, run).Project, run.Agent, p.operationPerson(run), operations.ExternalMessages)
		if level == operations.Never || level == operations.Ask {
			return false
		}
	}
	return true
}

// startedIn reports a conversation that a Slack thread in channel started
// through client c: the session itself, or the root of the session tree it
// is in, carries that Slack thread as its surface. A private session of that
// kind answers in the Slack conversation it was asked in, whatever kind of
// channel that is, because its people are that conversation's people.
//
// Only the client's own machine starts a session with a surface
// (StartSurfaceSession), so a session started anywhere else never matches:
// a private console session stays out of every channel, even one that names
// a Slack place as its parent.
func (p *Platform) startedIn(c clients.Client, channel, thread string) bool {
	id := thread
	for range 64 {
		th, err := p.Threads.Get(c.Tenant, id)
		if err != nil {
			return false
		}
		if th.Surface != nil {
			return th.Surface.Client == c.ID && th.Surface.Channel == channel
		}
		if th.Session == nil || !strings.HasPrefix(th.Session.Parent, "s_") {
			return false
		}
		id = th.Session.Parent
	}
	return false
}
