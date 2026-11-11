package platform

import (
	"context"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/event"
)

type sessionReaderKey struct{}

func (p *Platform) sourceAlertSession(ctx context.Context, who auth.Principal, id string) (threads.Thread, error) {
	if !who.Can(auth.AlertsRead) {
		return threads.Thread{}, threads.ErrNotFound
	}
	a, err := p.Alerts.Alert(who.Tenant, id)
	if err != nil || a.Session == "" || !p.Seer(who).Alert(a) {
		return threads.Thread{}, threads.ErrNotFound
	}
	return p.SessionRecord(ctx, who, a.Session)
}
func (p *Platform) sessionDescendantOf(tenant, id, ancestor string) bool {
	for depth := 0; depth <= 64; depth++ {
		if id == ancestor {
			return true
		}
		th, err := p.Threads.Get(tenant, id)
		if err != nil || th.Session == nil {
			return false
		}
		id = th.Session.Parent
	}
	return false
}

// Source backlinks come from immutable session identity. Every admission
// rechecks the source audience and current project before pinning the run.
func (p *Platform) pinSessionSourceAlert(ctx context.Context, tag *event.Envelope) error {
	id := wire.Meta(*tag)[runs.MetaThread]
	if child, ok := ctx.Value(sessionCallKey{}).(admittedSessionCall); ok {
		id = child.call.Session.ID
	}
	th, err := p.Threads.Get(tag.Tenant, id)
	if err != nil || th.Session == nil || th.Session.SourceAlert == "" {
		return nil
	}
	actor := wire.ActorOf(*tag)
	subject := actor.User
	if subject == "" {
		subject = actor.ID
	}
	if child, ok := ctx.Value(sessionCallKey{}).(admittedSessionCall); ok {
		// Delegated calls identify the calling agent, not a person. Resolve
		// authority from the same-tenant admitted parent, never the child actor.
		parent, err := p.Runs.Get(tag.Tenant, child.parent)
		if err != nil || parent.Thread != th.Session.Parent {
			return threads.ErrNotFound
		}
		pinned, err := p.contextOfRun(parent)
		if err != nil {
			return err
		}
		subject = pinned.Person
	}
	if subject == "" {
		return threads.ErrNotFound
	}
	who := p.invitationReader(tag.Tenant, subject)
	if current, ok := ctx.Value(sessionReaderKey{}).(auth.Principal); ok && current.Tenant == tag.Tenant && current.Subject == subject {
		who = current
	}
	source, err := p.sourceAlertSession(ctx, who, th.Session.SourceAlert)
	if err != nil {
		return err
	}
	if source.Project != th.Project || !p.sessionDescendantOf(tag.Tenant, th.ID, source.ID) {
		return threads.ErrNotFound
	}
	meta := wire.Meta(*tag)
	meta[wire.MetaSourceAlert] = th.Session.SourceAlert
	wire.SetContext(tag, meta)
	return nil
}
