package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/urmzd/humagen/pkg/accounts"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/personctx"
)

// A person links their own GitHub or Slack account through the connection
// flow every person-scoped service uses (AuthorizeConnection, then
// CompleteConnection on the consent's return): the state is theirs alone,
// bound to their session, and spent once. The callback asks the surface who
// signed in (GitHub's GET /user, Slack's OpenID Connect userInfo) and keeps
// only the answer, in Accounts. The token is used for that one request and
// never written: nothing Humagen does needs to act as the person on the
// surface, so nothing holds the means to.

// The account-link connections, one per surface.
const (
	GitHubAccount = "github-account"
	SlackAccount  = "slack-account"
)

// accountKind is the kind of a preset that links a surface account.
const accountKind = "account_oauth"

// ErrAccountRefused reports a sign-in Humagen will not link: a Slack user
// from a team none of the workspace's Slack clients serves, or an identity
// the surface did not vouch for.
var ErrAccountRefused = errors.New("account refused")

// accountSurface is the surface an account-link connection links on.
func accountSurface(name string) (string, bool) {
	switch name {
	case GitHubAccount:
		return accounts.GitHub, true
	case SlackAccount:
		return accounts.Slack, true
	}
	return "", false
}

// accountProvider is an account link's fixed endpoints. GitHub's is the
// GitHub App's own user authorization; Slack's is Sign in with Slack.
func accountProvider(name string) connectionauth.Provider {
	switch name {
	case GitHubAccount:
		// A GitHub App's user authorization takes no scopes: what its token
		// may do is the app's permissions, and it is used only for GET /user.
		return connectionauth.Provider{Name: name, Resource: "https://api.github.com/", Issuer: "https://github.com", AuthorizationURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token"}
	case SlackAccount:
		return connectionauth.Provider{Name: name, Resource: "https://slack.com/api/", Issuer: "https://slack.com", AuthorizationURL: "https://slack.com/openid/connect/authorize", TokenURL: "https://slack.com/api/openid.connect.token", Scopes: []string{"openid", "profile"}}
	}
	return connectionauth.Provider{}
}

// identityPath is where each surface says who the token is for.
var identityPath = map[string]string{GitHubAccount: "user", SlackAccount: "openid.connect.userInfo"}

// accountPreset fills a person's account-link preset: whether they may link,
// and the account they linked.
func (p *Platform) accountPreset(who auth.Principal, c *ConnectionPreset) error {
	surface, _ := accountSurface(c.ID)
	switch {
	case who.Kind == auth.Machine:
		c.Reason = "Sign in as a person to connect your account."
	case !p.ConnectionAuth.Available(c.ID):
		c.Reason = "An administrator must give Humagen the " + c.Name + " app's OAuth client before people can connect " + c.Name + "."
	default:
		c.Available = true
	}
	if who.Kind == auth.Machine {
		return nil
	}
	held, err := p.Accounts.Of(who.Tenant, surface, who.Subject)
	if errors.Is(err, accounts.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	c.Connected, c.Account = true, &held
	return nil
}

// authorizeAccount starts a person's account link.
func (p *Platform) authorizeAccount(ctx context.Context, who auth.Principal, name string, in ConnectionAuthorizationInput) (connectionauth.Authorization, error) {
	if who.Kind != auth.User || who.Subject == "" {
		return connectionauth.Authorization{}, ErrForbidden
	}
	if (in.Scope != "" && in.Scope != "person") || in.Endpoint != "" || in.Registration != nil {
		return connectionauth.Authorization{}, personctx.ErrInvalid
	}
	if !p.ConnectionAuth.Available(name) {
		return connectionauth.Authorization{}, connectionauth.ErrUnavailable
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "connection.authorize", Resource: "connection", ID: name, Subject: who.Subject, Route: "POST /api/connections/{provider}/authorize", Rule: string(auth.RunsWrite)})
	if err != nil {
		return connectionauth.Authorization{}, err
	}
	out, err := p.ConnectionAuth.BeginFor(ctx, who.Tenant, who.Subject, who.Subject, name, accountProvider(name).Resource)
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return out, err
}

// completeAccount finishes a person's account link: their own state, spent
// once, then who the surface says signed in, linked to them. A user another
// person linked is refused (accounts.ErrClaimed).
func (p *Platform) completeAccount(ctx context.Context, who auth.Principal, name, nonce, code string) error {
	if who.Kind != auth.User || who.Subject == "" {
		return ErrForbidden
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "connection.connect", Resource: "connection", ID: name, Subject: who.Subject, Route: "POST /api/connections/{provider}/callback", Rule: string(auth.RunsWrite)})
	if err != nil {
		return err
	}
	err = p.linkAccount(ctx, who, name, nonce, code)
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return err
}

func (p *Platform) linkAccount(ctx context.Context, who auth.Principal, name, nonce, code string) error {
	raw, err := p.ConnectionAuth.CompleteIdentity(ctx, who.Tenant, who.Subject, name, nonce, code, identityPath[name])
	if err != nil {
		return err
	}
	var a accounts.Account
	switch name {
	case GitHubAccount:
		a, err = githubIdentity(raw)
	case SlackAccount:
		a, err = slackIdentity(raw)
		if err == nil && !p.servesSlackTeam(who.Tenant, a.Team) {
			err = fmt.Errorf("%w: that Slack account is in a workspace none of this workspace's Slack apps is installed in", ErrAccountRefused)
		}
	default:
		err = ErrProviderNotFound
	}
	if err != nil {
		return err
	}
	a.Subject, a.At = who.Subject, p.now()
	_, err = p.Accounts.Link(who.Tenant, a)
	return err
}

// disconnectAccount unlinks a person's own account, and drops any consent
// they began and did not finish.
func (p *Platform) disconnectAccount(who auth.Principal, name string) error {
	if who.Kind != auth.User || who.Subject == "" {
		return ErrForbidden
	}
	surface, _ := accountSurface(name)
	attempt, err := p.BeginAudit(who, audit.Change{Action: "connection.disconnect", Resource: "connection", ID: name, Subject: who.Subject, Route: "DELETE /api/connections/{provider}", Rule: string(auth.RunsWrite)})
	if err != nil {
		return err
	}
	err = p.Accounts.Unlink(who.Tenant, surface, who.Subject)
	if err == nil && p.ConnectionAuth != nil {
		err = p.ConnectionAuth.Disconnect(who.Tenant, who.Subject, name)
	}
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return err
}

// servesSlackTeam reports whether one of a workspace's Slack clients is
// installed in a Slack team.
func (p *Platform) servesSlackTeam(tenant, team string) bool {
	all, err := p.Clients.List(tenant)
	if err != nil || team == "" {
		return false
	}
	for _, c := range all {
		if c.Surface == clients.Slack && c.Identity.Team == team {
			return true
		}
	}
	return false
}

var (
	githubLogin = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	slackUser   = regexp.MustCompile(`^[UW][A-Z0-9]{2,30}$`)
	slackTeam   = regexp.MustCompile(`^[TE][A-Z0-9]{2,30}$`)
)

// githubIdentity reads GET /user: the numeric id is the key, the login is
// for display. Only a user signs in; a bot or an organization is refused.
func githubIdentity(raw []byte) (accounts.Account, error) {
	var u struct {
		ID     int64  `json:"id"`
		Login  string `json:"login"`
		Avatar string `json:"avatar_url"`
		Type   string `json:"type"`
	}
	if json.Unmarshal(raw, &u) != nil || u.ID <= 0 || !githubLogin.MatchString(u.Login) || (u.Type != "" && u.Type != "User") {
		return accounts.Account{}, fmt.Errorf("%w: GitHub did not say which user signed in", ErrAccountRefused)
	}
	return accounts.Account{Surface: accounts.GitHub, ID: strconv.FormatInt(u.ID, 10), Login: u.Login, Avatar: httpsURL(u.Avatar)}, nil
}

// slackIdentity reads OpenID Connect userInfo: the user and team claims
// are the key, the name is for display.
func slackIdentity(raw []byte) (accounts.Account, error) {
	var u struct {
		OK      bool   `json:"ok"`
		User    string `json:"https://slack.com/user_id"`
		Team    string `json:"https://slack.com/team_id"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if json.Unmarshal(raw, &u) != nil || !u.OK || !slackUser.MatchString(u.User) || !slackTeam.MatchString(u.Team) {
		return accounts.Account{}, fmt.Errorf("%w: Slack did not say which user signed in", ErrAccountRefused)
	}
	name := strings.TrimSpace(u.Name)
	if len(name) > 200 {
		name = name[:200]
	}
	return accounts.Account{Surface: accounts.Slack, ID: u.User, Team: u.Team, Login: name, Avatar: httpsURL(u.Picture)}, nil
}

// httpsURL keeps an avatar only when it is an HTTPS address.
func httpsURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(raw) > 2048 {
		return ""
	}
	return raw
}

// LinkedAccounts is the accounts people in a workspace linked on a client's
// surface, for its administrators. Another tenant's client is not found.
func (p *Platform) LinkedAccounts(who auth.Principal, client string) ([]accounts.Account, error) {
	c, err := p.Clients.Get(who.Tenant, client)
	if err != nil {
		return nil, err
	}
	surface := ""
	switch c.Surface {
	case clients.GitHub:
		surface = accounts.GitHub
	case clients.Slack:
		surface = accounts.Slack
	default:
		return []accounts.Account{}, nil
	}
	all, err := p.Accounts.List(who.Tenant, surface, accounts.MaxList)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, a := range all {
		// A Slack client lists the people of its own team.
		if surface == accounts.Slack && a.Team != c.Identity.Team {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// linkedGitHub is the person a GitHub user id is linked to in a workspace.
func (p *Platform) linkedGitHub(tenant string, id int64) string {
	if id <= 0 || p.Accounts == nil {
		return ""
	}
	a, err := p.Accounts.Get(tenant, accounts.GitHub, strconv.FormatInt(id, 10))
	if err != nil {
		if !errors.Is(err, accounts.ErrNotFound) {
			p.log.Warn("platform: linked GitHub accounts could not be read", "tenant", tenant, "error", err)
		}
		return ""
	}
	return a.Subject
}

// linkedSlack is the person a Slack user of a team is linked to in a
// workspace.
func (p *Platform) linkedSlack(tenant, team, user string) string {
	if user == "" || team == "" || p.Accounts == nil {
		return ""
	}
	a, err := p.Accounts.Get(tenant, accounts.Slack, user)
	if err != nil {
		if !errors.Is(err, accounts.ErrNotFound) {
			p.log.Warn("platform: linked Slack accounts could not be read", "tenant", tenant, "error", err)
		}
		return ""
	}
	if a.Team != team {
		return ""
	}
	return a.Subject
}

// slackPeople is who a Slack client's users are: the people who linked an
// account in its team, then the client's own map for anyone who did not.
// Nil when there is neither.
func (p *Platform) slackPeople(tenant string, c clients.Client) map[string]string {
	var out map[string]string
	if c.Slack != nil && len(c.Slack.People) > 0 {
		out = make(map[string]string, len(c.Slack.People))
		maps.Copy(out, c.Slack.People)
	}
	if p.Accounts == nil || c.Surface != clients.Slack || c.Identity.Team == "" {
		return out
	}
	linked, err := p.Accounts.List(tenant, accounts.Slack, accounts.MaxList)
	if err != nil {
		p.log.Warn("platform: linked Slack accounts could not be read", "tenant", tenant, "error", err)
		return out
	}
	for _, a := range linked {
		if a.Team != c.Identity.Team {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[a.ID] = a.Subject
	}
	return out
}

// actsFor is the person a Slack app's own credential (machine) asks for when
// it acts for a Slack user of its client: the person who linked that user
// in the client's team, else the client's people map (clients.ActsFor).
// Only the credential the client names may ask, as before.
func (p *Platform) actsFor(c clients.Client, machine, user string) (string, bool) {
	if c.Surface != clients.Slack || !c.Enabled || c.Slack == nil || c.Slack.Machine == "" || machine != c.Slack.Machine {
		return "", false
	}
	if who := p.linkedSlack(c.Tenant, c.Identity.Team, user); who != "" {
		return who, true
	}
	return c.ActsFor(machine, user)
}

// githubSender is who sent a GitHub delivery, by the id GitHub gives every
// user for good and the login they go by now. The connector reads the
// login alone; the hand-off to it carries both on the request's context.
type githubSender struct {
	ID    int64
	Login string
}

type githubSenderKey struct{}

// withGitHubSender reads a signature-verified delivery's sender onto ctx.
func withGitHubSender(ctx context.Context, body []byte) context.Context {
	var h struct {
		Sender struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
		} `json:"sender"`
	}
	if json.Unmarshal(body, &h) != nil || h.Sender.ID <= 0 || h.Sender.Login == "" {
		return ctx
	}
	return context.WithValue(ctx, githubSenderKey{}, githubSender{ID: h.Sender.ID, Login: h.Sender.Login})
}

// githubSenderOf is the delivery's sender when its login is login.
func githubSenderOf(ctx context.Context, login string) (int64, bool) {
	s, ok := ctx.Value(githubSenderKey{}).(githubSender)
	if !ok || login == "" || !strings.EqualFold(s.Login, login) {
		return 0, false
	}
	return s.ID, true
}
