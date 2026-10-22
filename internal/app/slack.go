package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/urmzd/warp/pkg/credref"
	"github.com/urmzd/warp/pkg/spec"
)

// SlackManifest is a Slack app manifest: what the Slack app asks Slack for.
// warp writes it, so the scopes and events always match what the app it runs
// uses, and apps/slack/manifest.json is this output, checked by a test.
type SlackManifest struct {
	Metadata struct {
		Major int `json:"major_version"`
		Minor int `json:"minor_version"`
	} `json:"_metadata"`
	DisplayInformation struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		BackgroundColor string `json:"background_color"`
	} `json:"display_information"`
	Features struct {
		AppHome struct {
			HomeTab             bool `json:"home_tab_enabled"`
			MessagesTab         bool `json:"messages_tab_enabled"`
			MessagesTabReadOnly bool `json:"messages_tab_read_only_enabled"`
		} `json:"app_home"`
		BotUser struct {
			DisplayName  string `json:"display_name"`
			AlwaysOnline bool   `json:"always_online"`
		} `json:"bot_user"`
		Shortcuts     []slackShortcut `json:"shortcuts"`
		SlashCommands []slackCommand  `json:"slash_commands"`
		AgentView     struct {
			Description string `json:"agent_description"`
		} `json:"agent_view"`
	} `json:"features"`
	OAuth struct {
		Scopes struct {
			Bot []string `json:"bot"`
		} `json:"scopes"`
	} `json:"oauth_config"`
	Settings struct {
		Events struct {
			Bot []string `json:"bot_events"`
		} `json:"event_subscriptions"`
		Interactivity struct {
			Enabled bool `json:"is_enabled"`
		} `json:"interactivity"`
		OrgDeploy     bool `json:"org_deploy_enabled"`
		SocketMode    bool `json:"socket_mode_enabled"`
		TokenRotation bool `json:"token_rotation_enabled"`
	} `json:"settings"`
}

type slackShortcut struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	CallbackID  string `json:"callback_id"`
	Description string `json:"description"`
}

type slackCommand struct {
	Command      string `json:"command"`
	Description  string `json:"description"`
	UsageHint    string `json:"usage_hint"`
	ShouldEscape bool   `json:"should_escape"`
}

// slackManifest builds the manifest for an app called name. Socket Mode is
// always on: the app dials out, so the platform needs no public endpoint.
func slackManifest(name, command string) SlackManifest {
	var m SlackManifest
	m.Metadata.Major, m.Metadata.Minor = 2, 1
	m.DisplayInformation.Name = name
	m.DisplayInformation.Description = "Your workspace's agents in Slack. Ask a question, or review what one is waiting on."
	m.DisplayInformation.BackgroundColor = "#2b50c8"
	m.Features.AppHome.HomeTab, m.Features.AppHome.MessagesTab = true, true
	m.Features.BotUser.DisplayName, m.Features.BotUser.AlwaysOnline = strings.ToLower(name), true
	m.Features.Shortcuts = []slackShortcut{
		{Name: "Ask " + name, Type: "global", CallbackID: "aui_ask", Description: "Ask from anywhere. The answer arrives in the app's messages."},
		{Name: "Ask " + name + " about this", Type: "message", CallbackID: "aui_ask_about", Description: "Answer in this message's thread, using the message as the question."},
	}
	m.Features.SlashCommands = []slackCommand{{Command: command, Description: "Ask " + name + " in this channel", UsageHint: "[question] or help"}}
	// Slack delivers a channel message to the app only when it mentions the
	// app, so the agent is named after it.
	m.Features.AgentView.Description = "Your workspace's agents in Slack. Mention @" + name + " and the agent you want, such as @" + name + " @analyst, or message it."
	m.OAuth.Scopes.Bot = []string{
		"app_mentions:read", "assistant:write", "channels:history", "chat:write", "commands",
		"groups:history", "im:history", "mpim:history", "reactions:write", "users:read",
	}
	m.Settings.Events.Bot = []string{"agent_session_stopped", "app_home_opened", "app_mention", "assistant_thread_started", "message.im"}
	m.Settings.Interactivity.Enabled = true
	m.Settings.SocketMode = true
	return m
}

// slackCmd is `warp slack manifest`. With -f, the name and command come from
// the spec: the workspace's Slack surface, else its display name, else the
// deployment's. Flags win.
func slackCmd(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "manifest" {
		return errors.New("usage: warp slack manifest [-f PATH] [-workspace NAME] [-name NAME] [-command /agent]")
	}
	fs := flag.NewFlagSet("slack manifest", flag.ContinueOnError)
	var paths files
	fs.Var(&paths, "f", "a spec file or directory to take the name and command from")
	workspace := fs.String("workspace", "", "with -f, which Platform document. Empty means the first with Slack")
	name := fs.String("name", "", `the app's name in Slack. Empty means the spec's, else "Warp"`)
	command := fs.String("command", "", `the slash command. Empty means the spec's, else "/agent"`)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	n, c := "Warp", "/agent"
	if len(paths) > 0 {
		cfg, err := spec.Load(paths...)
		if err != nil {
			return err
		}
		cfg.System.Defaults()
		n = cfg.System.Metadata.DisplayName
		for _, p := range cfg.Platforms {
			sl := p.Spec.Surfaces.Slack
			if (*workspace != "" && p.Metadata.Name != *workspace) || (*workspace == "" && sl == nil) {
				continue
			}
			if p.Metadata.DisplayName != "" {
				n = p.Metadata.DisplayName
			}
			if sl != nil && sl.AppName != "" {
				n = sl.AppName
			}
			if sl != nil && sl.Command != "" {
				c = sl.Command
			}
			break
		}
	}
	if *name == "" {
		*name = n
	}
	if *command == "" {
		*command = c
	}
	if !strings.HasPrefix(*command, "/") {
		return errors.New("-command starts with /")
	}
	b, err := json.MarshalIndent(slackManifest(*name, *command), "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(b))
	return err
}

// slackApp runs the Slack app (apps/slack) as a client of this server: it
// dials Slack over Socket Mode and calls warp's API like the console does, so
// there is no public endpoint and no second backend. It is started and
// stopped with the server; its tokens are read from where the choices say
// they are kept and handed to it in its environment, never written down.
type slackApp struct {
	tenant string
	choice spec.Slack
	keys   credref.Resolver
	// dir is apps/slack. Empty means not found.
	dir string
}

// slackDir finds apps/slack: WARP_SLACK_APP_DIR, else under the working
// directory or beside the binary.
func slackDir(getenv func(string) string) string {
	if d := strings.TrimSpace(getenv("WARP_SLACK_APP_DIR")); d != "" {
		return d
	}
	for _, r := range searchRoots() {
		for _, c := range []string{filepath.Join(r, "apps", "slack"), filepath.Join(r, "slack")} {
			if _, err := os.Stat(filepath.Join(c, "src", "main.ts")); err == nil {
				return c
			}
		}
	}
	return ""
}

// slackPassEnv are the variables of this process the Slack app is given when
// they are set: what Node needs to run, to trust a CA and to reach Slack
// through a proxy, and the app's own optional settings. Nothing else is
// passed, since warp's environment holds its database URL, its sign-in
// secret and its model keys, and the app runs npm dependencies.
var slackPassEnv = []string{
	"PATH", "HOME", "TMPDIR", "TZ", "XDG_STATE_HOME",
	"NODE_OPTIONS", "NODE_EXTRA_CA_CERTS", "NODE_USE_ENV_PROXY",
	"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy",
	"SLACK_BRANDING", "SLACK_SOCKET_IDLE_MS", "SLACK_SOCKET_MAX_ATTEMPTS",
}

// env is the Slack app's environment: the variables in slackPassEnv that
// lookup has, and its own, read from where the choices say they are kept.
func (s *slackApp) env(ctx context.Context, url, publicURL string, lookup func(string) (string, bool)) ([]string, error) {
	var env []string
	for _, k := range slackPassEnv {
		if v, ok := lookup(k); ok {
			env = append(env, k+"="+v)
		}
	}
	bot, err := s.keys.Resolve(ctx, s.choice.BotToken.Ref)
	if err != nil {
		return nil, fmt.Errorf("bot token: %w", err)
	}
	app, err := s.keys.Resolve(ctx, s.choice.AppToken.Ref)
	if err != nil {
		return nil, fmt.Errorf("app token: %w", err)
	}
	env = append(env,
		"SLACK_BOT_TOKEN="+bot, "SLACK_APP_TOKEN="+app, "SLACK_TEAM_ID="+s.choice.Team,
		"SLACK_OPERATORS="+strings.Join(s.choice.Operators, ","),
		"WARP_URL="+url, "WARP_TENANT="+s.tenant, "WARP_PUBLIC_URL="+publicURL,
	)
	// How the app signs in to warp's API (apps/slack/src/config.ts).
	if c := s.choice.WarpClient; c != nil {
		secret, err := s.keys.Resolve(ctx, c.ClientSecret.Ref)
		if err != nil {
			return nil, fmt.Errorf("warp_client.client_secret: %w", err)
		}
		env = append(env, "WARP_OIDC_TOKEN_URL="+c.TokenURL, "WARP_OIDC_CLIENT_ID="+c.ClientID,
			"WARP_OIDC_CLIENT_SECRET="+secret, "WARP_OIDC_SCOPES="+strings.Join(c.Scopes, " "))
		if c.Audience != "" {
			env = append(env, "WARP_OIDC_AUDIENCE="+c.Audience)
		}
	}
	if s.choice.WarpToken.Set() {
		token, err := s.keys.Resolve(ctx, s.choice.WarpToken.Ref)
		if err != nil {
			return nil, fmt.Errorf("warp_token: %w", err)
		}
		env = append(env, "WARP_TOKEN="+token)
	}
	return env, nil
}

// run keeps the Slack app running until ctx ends, restarting it with a
// growing pause when it exits. It returns an error only when the app cannot
// be started at all: no apps/slack, no Node.js, no dependencies, or a token
// that cannot be read. The caller logs it and leaves the surface off; the
// server it is a client of keeps running.
func (s *slackApp) run(ctx context.Context, url, publicURL string, log *slog.Logger) error {
	if s.dir == "" {
		return errors.New("apps/slack not found; set WARP_SLACK_APP_DIR or run from a checkout")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return errors.New("the Slack app needs Node.js on PATH")
	}
	if _, err := os.Stat(filepath.Join(s.dir, "node_modules")); err != nil {
		return fmt.Errorf("the Slack app's dependencies are not installed; run pnpm install at the repository root")
	}
	pause := time.Second
	for ctx.Err() == nil {
		env, err := s.env(ctx, url, publicURL, os.LookupEnv)
		if err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, node, "--import", "tsx", "src/main.ts")
		cmd.Dir, cmd.Env = s.dir, env
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
		cmd.WaitDelay = 5 * time.Second
		began := time.Now()
		log.Info("slack app starting", "team", s.choice.Team)
		err = cmd.Run()
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(began) > time.Minute {
			pause = time.Second
		}
		log.Warn("slack app exited; restarting", "error", err, "in", pause)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pause):
		}
		pause = min(pause*2, time.Minute)
	}
	return nil
}
