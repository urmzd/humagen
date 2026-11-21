package platform

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/urmzd/humagen/internal/slackmanifest"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
)

// An agent's own Slack app (RFC 0012): a client whose default and only
// agent is the agent, and whose one bot is the agent's. The console builds
// its manifest from the agent's look, a Slack admin installs it, and linking
// reads who the bot is from its token, so nobody copies an id.

// ErrSlackRefused reports a bot token Slack would not say who it is for.
var ErrSlackRefused = errors.New("platform: slack refused the token")

// ErrAlreadyLinked reports a Slack app that is already one of the workspace's clients.
var ErrAlreadyLinked = errors.New("platform: that slack app is already connected")

// SlackBot is who a bot token is: its team, its app, and its bot user.
type SlackBot struct {
	Team    string `json:"team"`
	AppID   string `json:"app_id"`
	BotUser string `json:"bot_user"`
}

// AgentSlackApp is the manifest of an agent's own Slack app, the link that
// creates it in Slack with the manifest filled in, and the client it is
// linked as, when it is.
type AgentSlackApp struct {
	Manifest  slackmanifest.Manifest `json:"manifest"`
	CreateURL string                 `json:"create_url"`
	Client    string                 `json:"client,omitempty"`
	BotUser   string                 `json:"bot_user,omitempty"`
}

// linkedInstall reports the agent a client is the own app of, and its bot:
// a Slack client whose default is its only agent, with one bot standing for it.
func linkedInstall(c clients.Client) (agent, bot string, ok bool) {
	if c.Surface != clients.Slack || c.DefaultAgent == "" || len(c.Bots) != 1 {
		return "", "", false
	}
	for _, a := range c.Agents {
		if a != c.DefaultAgent {
			return "", "", false
		}
	}
	for user, a := range c.Bots {
		if a == c.DefaultAgent {
			return a, user, true
		}
	}
	return "", "", false
}

// AgentSlackApp builds an agent's own app's manifest from its look, and says
// which client it is linked as.
func (p *Platform) AgentSlackApp(ctx context.Context, tenant, agent string) (AgentSlackApp, error) {
	rev, err := p.Catalog().Get(ctx, tenant, agent, 0)
	if err != nil {
		return AgentSlackApp{}, err
	}
	looks, err := p.AgentLooks.Get(tenant, []string{agent})
	if err != nil {
		return AgentSlackApp{}, err
	}
	hues, err := p.AgentColors.Assign(tenant, []string{agent})
	if err != nil {
		return AgentSlackApp{}, err
	}
	look := looks[agent]
	var starters []string
	for _, s := range look.Starters {
		starters = append(starters, s.Title)
	}
	m := slackmanifest.ForAgent(slackmanifest.Agent{
		Name:        cmp.Or(look.DisplayName, agent),
		Description: rev.Spec.Description,
		Greeting:    look.Greeting,
		Starters:    starters,
		Hue:         hues[agent],
	})
	raw, err := json.Marshal(m)
	if err != nil {
		return AgentSlackApp{}, err
	}
	out := AgentSlackApp{Manifest: m, CreateURL: "https://api.slack.com/apps?new_app=1&manifest_json=" + url.QueryEscape(string(raw))}
	all, err := p.Clients.List(tenant)
	if err != nil {
		return AgentSlackApp{}, err
	}
	for _, c := range all {
		if a, bot, ok := linkedInstall(c); ok && a == agent && c.Enabled {
			out.Client, out.BotUser = c.ID, bot
			break
		}
	}
	return out, nil
}

// identifySlackBot asks Slack who a bot token is: auth.test for the team and
// the bot user, bots.info for the app the bot belongs to.
func (p *Platform) identifySlackBot(ctx context.Context, token string) (SlackBot, error) {
	base := strings.TrimRight(cmp.Or(p.cfg.SlackWebAPI, "https://slack.com/api"), "/")
	call := func(method string, form url.Values, out any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/"+method, strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := p.clientHTTP().Do(req)
		if err != nil {
			return fmt.Errorf("%w: %s could not be reached", ErrSlackRefused, method)
		}
		defer func() { _ = res.Body.Close() }()
		return json.NewDecoder(http.MaxBytesReader(nil, res.Body, 1<<20)).Decode(out)
	}
	var who struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		TeamID string `json:"team_id"`
		UserID string `json:"user_id"`
		BotID  string `json:"bot_id"`
	}
	if err := call("auth.test", url.Values{}, &who); err != nil {
		return SlackBot{}, err
	}
	if !who.OK || !clients.ValidSlackID(who.TeamID) || who.UserID == "" || who.BotID == "" {
		return SlackBot{}, fmt.Errorf("%w: %s", ErrSlackRefused, cmp.Or(who.Error, "it is not a bot token"))
	}
	var bot struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Bot   struct {
			AppID string `json:"app_id"`
		} `json:"bot"`
	}
	if err := call("bots.info", url.Values{"bot": {who.BotID}}, &bot); err != nil {
		return SlackBot{}, err
	}
	if !bot.OK || bot.Bot.AppID == "" {
		return SlackBot{}, fmt.Errorf("%w: %s", ErrSlackRefused, cmp.Or(bot.Error, "no app named"))
	}
	return SlackBot{Team: who.TeamID, AppID: bot.Bot.AppID, BotUser: who.UserID}, nil
}

// LinkAgentSlack connects an agent's own Slack app, from its bot token and
// signing secret: Slack says who the bot is, and the client is made with the
// agent as its default and only agent and the bot as the agent's. It files
// what it starts where the team's other Slack client does, and acts for the
// people it does. The caller holds clients:write and owns the agent's home.
func (p *Platform) LinkAgentSlack(ctx context.Context, who auth.Principal, agent string, token, signing clients.SecretInput) (clients.Client, error) {
	if token.Value == "" {
		return clients.Client{}, fmt.Errorf("%w: the bot token is needed to ask Slack who the app is", clients.ErrInvalid)
	}
	if _, err := p.Catalog().Get(ctx, who.Tenant, agent, 0); err != nil {
		return clients.Client{}, err
	}
	bot, err := p.identifySlackBot(ctx, token.Value)
	if err != nil {
		return clients.Client{}, err
	}
	all, err := p.Clients.List(who.Tenant)
	if err != nil {
		return clients.Client{}, err
	}
	var main *clients.Client
	taken := map[string]bool{}
	for i, c := range all {
		taken[c.ID] = true
		if c.Surface != clients.Slack || c.Identity.Team != bot.Team {
			continue
		}
		if c.Identity.AppID == bot.AppID {
			return clients.Client{}, fmt.Errorf("%w as %s", ErrAlreadyLinked, c.ID)
		}
		if _, _, own := linkedInstall(c); !own && c.Enabled && main == nil {
			main = &all[i]
		}
	}
	id := "slack-" + agent
	for n := 2; taken[id]; n++ {
		id = fmt.Sprintf("slack-%s-%d", agent, n)
	}
	looks, _ := p.AgentLooks.Get(who.Tenant, []string{agent})
	c := clients.Client{
		ID: id, Name: cmp.Or(looks[agent].DisplayName, agent) + " in Slack", Surface: clients.Slack, Enabled: true,
		Identity:     clients.Identity{Team: bot.Team, AppID: bot.AppID},
		DefaultAgent: agent, Agents: []string{agent},
		Bots:  map[string]string{bot.BotUser: agent},
		Slack: &clients.SlackOptions{Sessions: true},
	}
	if main != nil {
		c.Home = main.Home
		if main.Slack != nil {
			c.Slack.Machine, c.Slack.People = main.Slack.Machine, main.Slack.People
		}
	}
	return p.CreateClient(ctx, who.Tenant, who.Subject, ClientInput{Client: c, Token: token, SigningSecret: signing})
}

// ownAppBots are the bots of agents' own apps in c's team, other than c.
func (p *Platform) ownAppBots(tenant string, c clients.Client) map[string]bool {
	out := map[string]bool{}
	if c.Surface != clients.Slack || c.Identity.Team == "" {
		return out
	}
	all, err := p.Clients.List(tenant)
	if err != nil {
		return out
	}
	for _, other := range all {
		if other.ID == c.ID || !other.Enabled || other.Identity.Team != c.Identity.Team {
			continue
		}
		if _, bot, ok := linkedInstall(other); ok {
			out[bot] = true
		}
	}
	return out
}

// deferredToOwnApp reports whether a Slack message names the bot of an agent's
// own app, other than the client it reached: that app answers it, so the
// client it reached posts it only, its mention a plain word (slackContext's
// Deferred), and one message never gets two answers.
func (p *Platform) deferredToOwnApp(tenant string, c clients.Client, text string) bool {
	if !strings.Contains(text, "<@") {
		return false
	}
	for bot := range p.ownAppBots(tenant, c) {
		if strings.Contains(text, "<@"+bot+">") || strings.Contains(text, "<@"+bot+"|") {
			return true
		}
	}
	return false
}

// teamBots is every enabled Slack client's bots in c's team, c's own first:
// a mention of an agent's own app's bot names the agent wherever it is
// written, as typing its handle does.
func (p *Platform) teamBots(tenant string, c clients.Client) map[string]string {
	bots := map[string]string{}
	for user, agent := range c.Bots {
		bots[user] = agent
	}
	if c.Surface != clients.Slack || c.Identity.Team == "" {
		return bots
	}
	all, err := p.Clients.List(tenant)
	if err != nil {
		return bots
	}
	for _, other := range all {
		if other.ID == c.ID || !other.Enabled || other.Surface != clients.Slack || other.Identity.Team != c.Identity.Team {
			continue
		}
		for user, agent := range other.Bots {
			if _, has := bots[user]; !has {
				bots[user] = agent
			}
		}
	}
	return bots
}
