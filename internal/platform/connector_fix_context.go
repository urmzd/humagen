package platform

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/orchagent/flow/connector"
	transport "github.com/urmzd/orchagent/flow/connectors/github"
)

func (p *Platform) fixPullContext(c clients.Client) func(context.Context, transport.Ref) (transport.FixPullContext, error) {
	return func(ctx context.Context, _ transport.Ref) (transport.FixPullContext, error) {
		out := transport.FixPullContext{}
		run, err := p.Runs.Get(c.Tenant, connector.RunID(ctx))
		current, enabled := p.currentInstallation(c)
		if err != nil || !enabled || run.Tenant != c.Tenant || !current.Routes(run.Agent) {
			return out, fmt.Errorf("github: fix context run is outside installation")
		}
		alertID := wire.Meta(run.Tag())[wire.MetaSourceAlert]
		who := p.invitationReader(c.Tenant, runs.AskedBy(run))
		th, err := p.SessionRecord(ctx, who, run.Thread)
		if err != nil {
			if alertID == "" {
				return out, nil
			}
			return out, err
		}
		if alertID != "" {
			a, err := p.Alerts.Alert(c.Tenant, alertID)
			if err != nil || a.Session == "" || !p.Seer(who).Alert(a) || !p.SeesSession(who, a.Session) || (a.Session != th.ID && (th.Session == nil || th.Session.SourceAlert != a.ID)) {
				return out, fmt.Errorf("github: source alert context is no longer authorized")
			}
			source, err := p.Threads.Get(c.Tenant, a.Session)
			if err != nil || source.Project != th.Project {
				return out, fmt.Errorf("github: source alert project changed")
			}
		}
		if p.cfg.PublicURL == "" {
			return out, nil
		}
		base, err := url.Parse(p.cfg.PublicURL)
		if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
			return out, fmt.Errorf("github: invalid canonical console URL")
		}
		base.Path = strings.TrimRight(base.Path, "/")
		sessionURL := *base
		sessionURL.Path += "/sessions/" + th.ID
		out.SessionURL = sessionURL.String()
		if alertID != "" {
			alertURL := *base
			alertURL.Path += "/alerts"
			alertURL.RawQuery = url.Values{"alert": {alertID}}.Encode()
			out.AlertURL = alertURL.String()
		}
		return out, nil
	}
}
