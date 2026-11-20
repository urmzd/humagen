package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/connectionauth"
)

const slackOAuthRef = "oauth:slack"
const slackSigningRef = "oauth:slack-signing"

// finishSlackInstall uses only the installation identity returned by Slack's
// authenticated exchange. New clients remain disabled until routing is chosen.
func (p *Platform) finishSlackInstall(ctx context.Context, who auth.Principal) (err error) {
	var recovered string
	defer func() {
		if err == nil && recovered != "" {
			p.ConnectionAuth.ReportRecovered(ctx, who.Tenant, connectionauth.WorkspaceOwner, recovered)
		}
	}()
	details, err := p.ConnectionAuth.Details(who.Tenant, connectionauth.WorkspaceOwner, "workspace-slack")
	if err != nil {
		return err
	}
	team, app := details["team"], details["app_id"]
	if !clients.ValidSlackID(team) || !strings.HasPrefix(team, "T") || !clients.ValidSlackID(app) || !strings.HasPrefix(app, "A") {
		return connectionauth.ErrReconnect
	}
	id := "slack-" + strings.ToLower(team)
	p.clientsMu.Lock()
	defer p.clientsMu.Unlock()
	old, err := p.Clients.Get(who.Tenant, id)
	if err != nil && !errors.Is(err, clients.ErrNotFound) {
		return err
	}
	if err == nil && (old.Surface != clients.Slack || old.Identity.Team != team || old.Identity.AppID != app || old.Credentials.Token.Ref != slackOAuthRef) {
		return clients.ErrExists
	}
	provider, err := p.ConnectionAuth.FinalizeSlack(who.Tenant, who.Subject, team, app)
	if err != nil {
		return err
	}
	if err = p.ConnectionAuth.Bind(who.Tenant, connectionauth.WorkspaceOwner, "client:"+id, provider); err != nil {
		return err
	}
	recovered = provider
	if old.ID != "" {
		_, err = p.replace(old, old, who.Subject, ClientInput{Client: old, nativeSlack: true})
		return err
	}
	in := ClientInput{Client: clients.Client{ID: id, Name: "Slack " + team, Surface: clients.Slack, Managed: clients.ManagedInstall, Identity: clients.Identity{Team: team, AppID: app}, Enabled: false, Capabilities: &clients.Capabilities{Events: []string{}, Actions: []string{}}}, nativeSlack: true}
	_, err = p.create(who.Tenant, who.Subject, in)
	return err
}

func (p *Platform) disableSlackInstall(tenant string) error {
	p.clientsMu.Lock()
	defer p.clientsMu.Unlock()
	all, err := p.Clients.List(tenant)
	if err != nil {
		return err
	}
	for _, c := range all {
		if c.Surface != clients.Slack || c.Managed != clients.ManagedInstall || c.Credentials.Token.Ref != slackOAuthRef {
			continue
		}
		c.Enabled = false
		saved, err := p.Clients.Update(tenant, c, p.now())
		if err != nil {
			return err
		}
		p.seat(saved, nil)
		if err = p.ConnectionAuth.Disconnect(tenant, connectionauth.WorkspaceOwner, slackProvider(c)); err != nil {
			return err
		}
	}
	p.touchClients()
	return nil
}

func slackProvider(c clients.Client) string {
	return "workspace-slack-" + strings.ToLower(c.Identity.Team)
}

func (p *Platform) slackClientToken(c clients.Client) (func(context.Context) (string, error), error) {
	provider := slackProvider(c)
	details, err := p.ConnectionAuth.Details(c.Tenant, connectionauth.WorkspaceOwner, provider)
	if err != nil || details["team"] != c.Identity.Team || details["app_id"] != c.Identity.AppID {
		return nil, connectionauth.ErrReconnect
	}
	return func(ctx context.Context) (string, error) {
		return p.ConnectionAuth.BoundToken(ctx, c.Tenant, connectionauth.WorkspaceOwner, "client:"+c.ID, provider)
	}, nil
}

// The installation token is fetched for every API request and sent only to the
// fixed Slack API origin. Failed refresh and redirects cannot use a stale token.
type slackOAuthTransport struct {
	token    func(context.Context) (string, error)
	revision func() string
	rejected func(context.Context, string, string)
	base     http.RoundTripper
}

func (t *slackOAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "slack.com" || !strings.HasPrefix(r.URL.Path, "/api/") {
		return nil, connectionauth.ErrInvalid
	}
	revision := ""
	if t.revision != nil {
		revision = t.revision()
	}
	token, err := t.token(r.Context())
	if err != nil {
		return nil, connectionauth.ErrReconnect
	}
	copy := r.Clone(r.Context())
	copy.Header = r.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+token)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	response, err := base.RoundTrip(copy)
	if err != nil {
		return nil, connectionauth.ErrReconnect
	}
	if t.rejected != nil && slackResponseRejected(response) {
		t.rejected(r.Context(), revision, token)
	}
	return response, nil
}

func (p *Platform) slackOAuthClient(c clients.Client, token func(context.Context) (string, error)) *http.Client {
	provider := slackProvider(c)
	return &http.Client{Timeout: 15 * time.Second, Transport: &slackOAuthTransport{token: token,
		revision: func() string { return p.ConnectionAuth.Revision(c.Tenant, connectionauth.WorkspaceOwner, provider) },
		rejected: func(ctx context.Context, revision, used string) {
			p.ConnectionAuth.ReportTokenRejected(ctx, c.Tenant, connectionauth.WorkspaceOwner, provider, revision, used)
		},
	}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// Slack reports credential rejection in HTTP 200 envelopes. Inspect a bounded
// prefix and restore every byte and the original closer for the API decoder.
func slackResponseRejected(response *http.Response) bool {
	if response.StatusCode == http.StatusUnauthorized {
		return true
	}
	if response.StatusCode != http.StatusOK || response.Body == nil {
		return false
	}
	const limit = 64 << 10
	body := response.Body
	raw, err := io.ReadAll(io.LimitReader(body, limit+1))
	response.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(raw), body), body}
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	return err == nil && len(raw) <= limit && json.Unmarshal(raw, &result) == nil && !result.OK && connectionauth.SlackAuthRejected(result.Error)
}
