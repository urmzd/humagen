package platform_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/agentlooks"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/clients"
)

// fakeSlackWeb answers auth.test and bots.info for one bot token, as Slack does.
func fakeSlackWeb(t *testing.T, token, team, botUser, botID, app string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid_auth"})
			return
		}
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/auth.test":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "team_id": team, "user_id": botUser, "bot_id": botID})
		case "/bots.info":
			if r.Form.Get("bot") != botID {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "bot_not_found"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "bot": map[string]any{"app_id": app}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// An agent's own Slack app is linked from its token alone: Slack says who
// the bot is, the client routes to the agent alone, and it files and acts as
// the team's other Slack client does. A mention of its bot through that
// other client names the agent, and is the own app's to answer.
func TestAnAgentsOwnSlackAppIsLinkedFromItsToken(t *testing.T) {
	slackWeb := fakeSlackWeb(t, "xoxb-barbara", "T0TEAM", "U0BARB", "B0BARB", "A0BARB")
	p, machine := slackRoomOn(t, startWith(t, platform.Config{Limits: call.Limits{}, SlackWebAPI: slackWeb.URL}), nil)
	if _, err := p.AgentLooks.Set(tenant, "helper", agentlooks.Look{DisplayName: "Barbara", Greeting: "I keep the reports current.", Starters: []agentlooks.Starter{{Title: "Summarize", Prompt: "Summarize."}}}, nil); err != nil {
		t.Fatal(err)
	}

	app, err := p.AgentSlackApp(t.Context(), tenant, "helper")
	if err != nil {
		t.Fatal(err)
	}
	if app.Manifest.DisplayInformation.Name != "Barbara" || app.Client != "" || !strings.Contains(app.CreateURL, "manifest_json=") {
		t.Fatalf("before linking the app is %+v", app)
	}
	if got := app.Manifest.Features.AgentView.SuggestedPrompts; len(got) != 1 || got[0] != "Summarize" {
		t.Fatalf("suggested prompts = %v", got)
	}

	admin := auth.Principal{Subject: "ada", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	if _, err := p.LinkAgentSlack(t.Context(), admin, "helper", clients.SecretInput{Value: "xoxb-wrong"}, clients.SecretInput{Value: "s"}); !errors.Is(err, platform.ErrSlackRefused) {
		t.Fatalf("a token Slack refuses: %v", err)
	}
	c, err := p.LinkAgentSlack(t.Context(), admin, "helper", clients.SecretInput{Value: "xoxb-barbara"}, clients.SecretInput{Value: "signing"})
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case c.Identity.Team != "T0TEAM" || c.Identity.AppID != "A0BARB":
		t.Fatalf("identity = %+v", c.Identity)
	case c.DefaultAgent != "helper" || len(c.Agents) != 1 || c.Bots["U0BARB"] != "helper":
		t.Fatalf("routing = %s %v %v", c.DefaultAgent, c.Agents, c.Bots)
	case c.Home == nil || c.Home.Project != "Initech" || c.Slack == nil || c.Slack.Machine != "svc-slack":
		t.Fatalf("it does not file or act as the team's other client: %+v %+v", c.Home, c.Slack)
	}
	if _, err := p.LinkAgentSlack(t.Context(), admin, "helper", clients.SecretInput{Value: "xoxb-barbara"}, clients.SecretInput{Value: "signing"}); !errors.Is(err, platform.ErrAlreadyLinked) {
		t.Fatalf("linking the same app twice: %v", err)
	}
	if app, _ := p.AgentSlackApp(t.Context(), tenant, "helper"); app.Client != c.ID || app.BotUser != "U0BARB" {
		t.Fatalf("after linking the app is %+v", app)
	}

	// Through the team's other client, the agent's bot is the agent's name,
	// and the message is posted only: the agent's own app answers it.
	posted, err := p.StartThread(t.Context(), machine, fromSlack("C09OPEN", "1700000000.000100", "", "<@UAPP> <@U0BARB> refresh the report"))
	if err != nil {
		t.Fatal(err)
	}
	if posted.Message.Text != "helper refresh the report" {
		t.Fatalf("the message reads %q", posted.Message.Text)
	}
	if len(posted.Runs) != 0 {
		t.Fatalf("the other client answered too: %+v", posted.Runs)
	}
}

// Through the agent's own app, its bot is the app's own mention, so the
// agent answers as the app's default.
func TestAnAgentsOwnSlackAppAnswersAsItsAgent(t *testing.T) {
	slackWeb := fakeSlackWeb(t, "xoxb-barbara", "T0TEAM", "U0BARB", "B0BARB", "A0BARB")
	p, machine := slackRoomOn(t, startWith(t, platform.Config{Limits: call.Limits{}, SlackWebAPI: slackWeb.URL}), nil)
	admin := auth.Principal{Subject: "ada", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	c, err := p.LinkAgentSlack(t.Context(), admin, "helper", clients.SecretInput{Value: "xoxb-barbara"}, clients.SecretInput{Value: "signing"})
	if err != nil {
		t.Fatal(err)
	}
	in := fromSlack("C09OPEN", "1700000000.000200", "", "<@U0BARB> refresh the report")
	in.ActAs.Client, in.Surface.Client, in.Surface.AppUser = c.ID, c.ID, "U0BARB"
	posted, err := p.StartThread(t.Context(), machine, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(posted.Runs) != 1 || posted.Runs[0].Agent != "helper" {
		t.Fatalf("the agent's own app started %+v (%s)", posted.Runs, posted.Rule)
	}
}
