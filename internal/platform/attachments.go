package platform

import (
	"context"
	"fmt"

	"github.com/urmzd/fiber/pkg/address"
	"github.com/urmzd/fiber/pkg/attachment"
	"github.com/urmzd/fiber/pkg/thread"
	"github.com/urmzd/humagen/pkg/auth"
)

// surfaceFiles reads the actual Slack message through its client, so the host
// supplies neither content URLs nor digests. The message must be the poster's.
func (p *Platform) surfaceFiles(ctx context.Context, who auth.Principal, ref SurfaceRef, as *ActAs) ([]attachment.Attachment, error) {
	c, err := p.surfaceHost(who, ref, as)
	if err != nil {
		return nil, err
	}
	if as == nil {
		return nil, fmt.Errorf("%w: files need a mapped poster", ErrForbidden)
	}
	s := p.surfaces["slack"]
	s.mu.RLock()
	var reader thread.Lister
	for _, seat := range s.seats {
		if seat.client.Tenant == who.Tenant && seat.client.ID == c.ID {
			reader, _ = seat.peer.(thread.Lister)
			break
		}
	}
	s.mu.RUnlock()
	if reader == nil {
		return nil, fmt.Errorf("%w: this client cannot read files", ErrForbidden)
	}
	at := address.Address{Connector: "slack", Workspace: c.Identity.Team, Path: []string{ref.Channel}}.WithParam(thread.ParamThread, ref.TS)
	messages, err := reader.Messages(ctx, who.Tenant, at)
	if err != nil {
		return nil, err
	}
	for _, m := range messages {
		if m.ID != ref.message() {
			continue
		}
		if m.Actor.Bot || m.Actor.ID != as.User {
			return nil, fmt.Errorf("%w: files are not the poster's", ErrForbidden)
		}
		if m.Files == nil {
			return nil, nil
		}
		return m.Files(ctx)
	}
	return nil, fmt.Errorf("%w: Slack message not found", ErrForbidden)
}
