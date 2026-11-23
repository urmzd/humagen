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
	"github.com/urmzd/humagen/pkg/threads"
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
func (p *Platform) BridgeDeliveryAllowed(ctx context.Context, who auth.Principal, id, channel, ts, runID string) (bool, error) {
	c, err := p.Clients.Get(who.Tenant, id)
	if err != nil || !machineOf(c, who) {
		return false, clients.ErrNotFound
	}
	run, err := p.Runs.Get(who.Tenant, runID)
	if err != nil || !who.Can(auth.RunsRead) || !p.Seer(who).Run(run) {
		return false, clients.ErrNotFound
	}
	if len(channel) > 64 || channel == "" || len(ts) > 32 {
		return false, clients.ErrInvalid
	}
	return p.bridgeMayDeliver(ctx, c, channel, ts, run), nil
}

// bridgeMayDeliver reports whether client c may post run in a Slack channel,
// in the thread ts there ("" for none in particular).
func (p *Platform) bridgeMayDeliver(ctx context.Context, c clients.Client, channel, ts string, run runs.Run) bool {
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
		if p.privateThread(c.Tenant, run.Thread) && !p.startedIn(c, channel, ts, run.Thread) {
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

// startedIn reports a conversation that the Slack thread ts in channel
// started through client c: the session itself, or the root of the session
// tree it is in, carries that Slack thread as its surface. A private session
// of that kind answers in the Slack thread it was asked in, whatever kind of
// channel that is, because its people are that conversation's people. It
// answers in no other thread, and an empty ts names none.
//
// Only the client's own machine starts a session with a surface
// (StartSurfaceSession), so a session started anywhere else never matches:
// a private console session stays out of every channel, even one that names
// a Slack place as its parent.
func (p *Platform) startedIn(c clients.Client, channel, ts, thread string) bool {
	sf, ok := p.treeSurface(c.Tenant, thread)
	return ok && sf != nil && ts != "" && *sf == threads.Surface{Client: c.ID, Channel: channel, TS: ts}
}

// treeSurface is the surface of the session tree a conversation is in: its
// own, or else the first one on the way up through its session parents. Nil
// is a tree no surface started. ok is false when a conversation on the way
// could not be read or the tree nests too deep, which a caller treats as
// not knowing.
func (p *Platform) treeSurface(tenant, thread string) (*threads.Surface, bool) {
	root, ok := p.surfaceRoot(tenant, thread)
	return root.Surface, ok
}

// surfaceRoot is the conversation that carries the surface of the session
// tree thread is in (treeSurface), or the zero thread when no surface
// started the tree. A session in that tree reaches whom that surface
// reaches: it answers there.
func (p *Platform) surfaceRoot(tenant, thread string) (threads.Thread, bool) {
	id := thread
	for range 64 {
		th, err := p.Threads.Get(tenant, id)
		if err != nil {
			return threads.Thread{}, false
		}
		if th.Surface != nil {
			return th, true
		}
		if th.Session == nil || !threads.SessionParent(th.Session.Parent) {
			return threads.Thread{}, true
		}
		id = th.Session.Parent
	}
	return threads.Thread{}, false
}
