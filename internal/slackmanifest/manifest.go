// Package slackmanifest builds the Slack app manifest Humagen's Slack app
// asks for: the one app a workspace installs, and the app of one agent
// that has its own install (RFC 0012).
package slackmanifest

import (
	"fmt"

	"github.com/urmzd/humagen/pkg/spec"
)

// Manifest is a Slack app manifest: what the Slack app asks Slack for.
// humagen writes it, so the scopes and events always match what the app it runs
// uses. apps/slack/manifest.json is this output, checked by a test, and the Slack
// CLI project in deploy/slack generates it from here when it pushes the app.
type Manifest struct {
	DisplayInformation struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		LongDescription string `json:"long_description"`
		BackgroundColor string `json:"background_color"`
	} `json:"display_information"`
	Features struct {
		AppHome struct {
			HomeTab             bool `json:"home_tab_enabled"`
			MessagesTab         bool `json:"messages_tab_enabled"`
			MessagesTabReadOnly bool `json:"messages_tab_read_only_enabled"`
			AgentTasks          bool `json:"agent_tasks_enabled"`
		} `json:"app_home"`
		BotUser struct {
			DisplayName  string `json:"display_name"`
			AlwaysOnline bool   `json:"always_online"`
		} `json:"bot_user"`
		Shortcuts     []Shortcut `json:"shortcuts"`
		SlashCommands []Command  `json:"slash_commands,omitempty"`
		AgentView     struct {
			Description      string   `json:"agent_description"`
			SuggestedPrompts []string `json:"suggested_prompts"`
		} `json:"agent_view"`
		// CodeChannels is present only when the Slack surface turns code
		// channels on, so the default manifest asks for nothing new.
		CodeChannels *CodeChannelsFeature `json:"code_channels,omitempty"`
	} `json:"features"`
	OAuth struct {
		Scopes struct {
			Bot []string `json:"bot"`
		} `json:"scopes"`
		PKCE bool `json:"pkce_enabled"`
	} `json:"oauth_config"`
	Settings struct {
		Events struct {
			Bot []string `json:"bot_events"`
		} `json:"event_subscriptions"`
		Interactivity struct {
			Enabled bool `json:"is_enabled"`
		} `json:"interactivity"`
		OrgDeploy        bool `json:"org_deploy_enabled"`
		SocketMode       bool `json:"socket_mode_enabled"`
		MCP              bool `json:"is_mcp_enabled"`
		TokenRotation    bool `json:"token_rotation_enabled"`
		AppTokenRotation bool `json:"app_level_token_rotation_enabled"`
	} `json:"settings"`
}

type Shortcut struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	CallbackID  string `json:"callback_id"`
	Description string `json:"description"`
}

type CodeChannelsFeature struct {
	Enabled bool `json:"enabled"`
}

// CodeChannelScopes are what opening a code channel adds to the bot's
// scopes: creating one, and reading a channel's record to tell a code channel
// and where it was opened from (apps/slack/src/codechannel-open.ts).
var CodeChannelScopes = []string{"code_channels:manage", "channels:read", "groups:read"}

type Command struct {
	Command      string `json:"command"`
	Description  string `json:"description"`
	UsageHint    string `json:"usage_hint"`
	ShouldEscape bool   `json:"should_escape"`
}

// Build builds the manifest for an app called name. Socket Mode is
// always on: the app dials out, so the platform needs no public endpoint.
// People mention the app (or DM it) and the entry agent brings in the agents
// that fit, so the manifest offers no slash commands and one message shortcut.
// Copy is what the manifest says about the app beyond its name: the Slack
// surface's own fields, each Humagen's when empty.
type Copy struct {
	Description     string
	BackgroundColor spec.Color
	// CodeChannels adds the code channels feature and its scopes. Off, the
	// manifest is the one every install already has.
	CodeChannels bool
}

func Build(name string, c Copy) Manifest {
	var m Manifest
	m.DisplayInformation.Name = name
	m.DisplayInformation.Description = "Mention @" + name + " in any channel, thread or DM. It brings in the right agents and answers where you asked."
	if c.Description != "" {
		m.DisplayInformation.Description = c.Description
	}
	// Slack shows the long description on the app's About page, and its App
	// Directory asks for 175 to 4,000 characters. It names only what the manifest
	// offers.
	m.DisplayInformation.LongDescription = name + " puts your workspace's agents in Slack. " +
		"Mention @" + name + " in a channel, a thread or a direct message. It brings in the agents that fit the question and answers in the thread where you asked.\n\n" +
		"Answers stream in as the agents work, and a Stop button ends a run early. Attach files, images or a voice clip and the agents read them. " +
		"Use the \"Ask about this\" shortcut on any message to get an answer in that message's thread.\n\n" +
		"The Home tab shows what is waiting on you and what is new in your projects. Like or dislike an answer to say how it did."
	m.DisplayInformation.BackgroundColor = "#2b50c8"
	if c.BackgroundColor != "" {
		m.DisplayInformation.BackgroundColor = c.BackgroundColor.Hex()
	}
	m.Features.AppHome.HomeTab, m.Features.AppHome.MessagesTab, m.Features.AppHome.AgentTasks = true, true, true
	m.Features.BotUser.DisplayName, m.Features.BotUser.AlwaysOnline = name, true
	m.Features.Shortcuts = []Shortcut{
		{Name: "Ask about this", Type: "message", CallbackID: "aui_ask_about", Description: "Answers in this message's thread, with the message as the question."},
	}
	m.Features.AgentView.Description = "Ask anything. " + name + " brings in the right agents and answers here."
	m.Features.AgentView.SuggestedPrompts = []string{}
	m.OAuth.Scopes.Bot = []string{
		"app_mentions:read", "assistant:write", "channels:history", "chat:write", "chat:write.customize", "commands",
		"groups:history", "im:history", "mpim:history", "reactions:write", "users:read",
		"files:read", "files:write", "canvases:write",
	}
	if c.CodeChannels {
		m.Features.CodeChannels = &CodeChannelsFeature{Enabled: true}
		m.OAuth.Scopes.Bot = append(m.OAuth.Scopes.Bot, CodeChannelScopes...)
	}
	m.Settings.Events.Bot = []string{"agent_session_stopped", "app_home_opened", "app_mention", "assistant_thread_started", "message.channels", "message.groups", "message.im"}
	m.Settings.Interactivity.Enabled = true
	m.Settings.SocketMode = true
	return m
}

// Agent is what an agent's own app says of itself: the name people call it,
// one line about it, a greeting in its voice, the jobs it offers, and its
// color as a hue (0 to 360), or a negative hue for none.
type Agent struct {
	Name        string
	Description string
	Greeting    string
	Starters    []string
	Hue         float64
}

// ForAgent builds the manifest of an agent's own app: the shared app's, so
// its scopes and events never drift, named and described as the agent. Slack
// takes no icon in a manifest; the console offers the agent's picture to
// upload.
func ForAgent(a Agent) Manifest {
	about := a.Description
	if about == "" {
		about = "One of your workspace's agents, in Slack."
	}
	m := Build(a.Name, Copy{Description: clip(about, 140)})
	m.DisplayInformation.LongDescription = clip(a.Name+" is one of your workspace's agents, with an app of its own in Slack. "+
		"Mention @"+a.Name+" in a channel or a thread, or send it a direct message, and it answers there, in its own name. "+
		about+" "+a.Greeting+"\n\n"+
		"Answers stream in as it works, and a Stop button ends a run early. Like or dislike an answer to say how it did.", 4000)
	if a.Hue >= 0 {
		m.DisplayInformation.BackgroundColor = hueHex(a.Hue)
	}
	m.Features.AgentView.Description = clip(firstOf(a.Greeting, about), 300)
	prompts := []string{}
	for _, s := range a.Starters {
		if s != "" && len(prompts) < 4 {
			prompts = append(prompts, clip(s, 75))
		}
	}
	m.Features.AgentView.SuggestedPrompts = prompts
	return m
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// hueHex is a hue as the darker shade Slack shows behind an app's name:
// saturation 55%, lightness 42%, so white text reads on it.
func hueHex(h float64) string {
	s, l := 0.55, 0.42
	c := (1 - abs(2*l-1)) * s
	hp := mod(h, 360) / 60
	x := c * (1 - abs(mod(hp, 2)-1))
	var r, g, b float64
	switch {
	case hp < 1:
		r, g = c, x
	case hp < 2:
		r, g = x, c
	case hp < 3:
		g, b = c, x
	case hp < 4:
		g, b = x, c
	case hp < 5:
		r, b = x, c
	default:
		r, b = c, x
	}
	m := l - c/2
	to := func(v float64) int { return int((v+m)*255 + 0.5) }
	return fmt.Sprintf("#%02x%02x%02x", to(r), to(g), to(b))
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func mod(v, m float64) float64 {
	for v >= m {
		v -= m
	}
	for v < 0 {
		v += m
	}
	return v
}
