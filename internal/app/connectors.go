package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/credentials"
	"github.com/urmzd/warp/pkg/spec"
)

// The environment configures at most one client per surface: the default one,
// for a deployment that has a single Slack app or GitHub installation and
// wants no more ceremony than that. Further clients are created through the
// API, each with its own identity, secrets, agents and limits.
const (
	// Slack. A signing secret is what makes the trigger safe; a bot token is
	// what makes the sink able to write.
	EnvSlackToken   = "WARP_SLACK_TOKEN"
	EnvSlackSecret  = "WARP_SLACK_SIGNING_SECRET"
	EnvSlackTeam    = "WARP_SLACK_TEAM"
	EnvSlackApp     = "WARP_SLACK_APP"
	EnvSlackBots    = "WARP_SLACK_BOTS"
	EnvSlackDefault = "WARP_SLACK_DEFAULT_AGENT"
	// Sessions is a boolean; Commands is a JSON array of
	// {"name","description","argument_hint"} objects.
	EnvSlackSessions = "WARP_SLACK_SESSIONS"
	EnvSlackCommands = "WARP_SLACK_COMMANDS"

	// GitHub.
	EnvGitHubReadRepositories = "WARP_GITHUB_READ_REPOSITORIES"
	EnvGitHubToken            = "WARP_GITHUB_TOKEN"
	EnvGitHubSecret           = "WARP_GITHUB_WEBHOOK_SECRET"
	EnvGitHubOwners           = "WARP_GITHUB_OWNERS"
	EnvGitHubApp              = "WARP_GITHUB_APP"
	EnvGitHubInstallation     = "WARP_GITHUB_INSTALLATION"
	EnvGitHubBots             = "WARP_GITHUB_BOTS"
	EnvGitHubAutoLabels       = "WARP_GITHUB_AUTO_LABELS"
	EnvGitHubChecks           = "WARP_GITHUB_CHECKS"
	EnvGitHubGuides           = "WARP_GITHUB_GUIDES"
	EnvGitHubMutations        = "WARP_GITHUB_MUTATIONS"
	// EnvGitHubLogin is the app's own login. A comment that mentions it and
	// names no agent is answered by the client's default (EnvGitHubDefault),
	// else by the default chain of the client's home.
	EnvGitHubLogin   = "WARP_GITHUB_LOGIN"
	EnvGitHubDefault = "WARP_GITHUB_DEFAULT_AGENT"
)

// Ids of the clients the environment configures.
const (
	defaultSlackClient  = "default-slack"
	defaultGitHubClient = "default-github"
)

// defaultClients reads the environment into the clients it configures: none,
// one per surface, or an error for a half-configured or malformed one. Secrets
// are passed by reference, so the records name the variables and hold nothing.
func defaultClients(env func(string) string) ([]platform.ClientInput, error) {
	var out []platform.ClientInput
	for _, build := range []func(func(string) string) (*platform.ClientInput, error){githubClient, slackClient} {
		in, err := build(env)
		if err != nil {
			return nil, err
		}
		if in != nil {
			in.Enabled, in.Managed = true, clients.ManagedEnv
			// One installation used to answer for every agent in its tenant.
			in.Agents = []string{clients.AnyAgent}
			out = append(out, *in)
		}
	}
	return out, nil
}

// secretRefs is which environment variables each tenant's clients may
// reference. It is read from -secret-refs, written
// "tenant=NAME,NAME;tenant=NAME", and the server's own tenant is also given
// the variables its default clients are configured with. Nobody else is: the
// variables behind one tenant's default client are not another tenant's to
// name.
func secretRefs(raw, tenant string, env func(string) string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, entry := range strings.Split(raw, ";") {
		if entry = strings.TrimSpace(entry); entry == "" {
			continue
		}
		name, names, ok := strings.Cut(entry, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" || len(list(names)) == 0 {
			return nil, fmt.Errorf("-secret-refs: %q is not tenant=NAME,NAME", entry)
		}
		for _, v := range list(names) {
			// The sign-in secrets are the server's own, whatever the prefix.
			if _, err := clients.EnvRef("env:" + v); err != nil || strings.HasPrefix(v, "WARP_OIDC_") {
				return nil, fmt.Errorf("-secret-refs: %q cannot be referenced by a client", v)
			}
			out[name] = append(out[name], v)
		}
	}
	for _, pair := range [][2]string{{EnvSlackToken, EnvSlackSecret}, {EnvGitHubToken, EnvGitHubSecret}} {
		if env(pair[0]) != "" || env(pair[1]) != "" {
			out[tenant] = append(out[tenant], pair[0], pair[1])
		}
	}
	return out, nil
}

// personRefs is where each tenant's people may keep their own tool
// credentials: how the references they give may start, each with
// {subject} where the person's subject goes. It is read from -person-refs,
// written "tenant=START,START;tenant=START". A tenant not listed may
// reference nothing, and no two starts may share a place.
func personRefs(raw string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, entry := range strings.Split(raw, ";") {
		if entry = strings.TrimSpace(entry); entry == "" {
			continue
		}
		name, starts, ok := strings.Cut(entry, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" || len(list(starts)) == 0 {
			return nil, fmt.Errorf("-person-refs: %q is not tenant=START,START", entry)
		}
		for _, start := range list(starts) {
			if err := credentials.CheckStart(start); err != nil {
				return nil, fmt.Errorf("-person-refs: %w", err)
			}
			out[name] = append(out[name], start)
		}
	}
	if err := credentials.CheckStarts(out); err != nil {
		return nil, fmt.Errorf("-person-refs: %w", err)
	}
	return out, nil
}

// personValues is the workspaces whose Platform document lets people store
// their own tool credentials with warp.
func personValues(cfg spec.Config) map[string]bool {
	out := map[string]bool{}
	for _, p := range cfg.Platforms {
		if pc := p.Spec.Workspace.PersonCredentials; pc != nil && pc.Values {
			out[p.Metadata.Name] = true
		}
	}
	return out
}

// seedClients creates the clients the environment configures, in one tenant.
func seedClients(ctx context.Context, p *platform.Platform, tenant string, env func(string) string) error {
	defaults, err := defaultClients(env)
	if err != nil {
		return err
	}
	for _, in := range defaults {
		if _, err := p.CreateClient(ctx, tenant, "environment", in); err != nil {
			return fmt.Errorf("cli: %s: %w", in.Surface, err)
		}
	}
	return nil
}

func boolEnv(env func(string) string, name string) (bool, error) {
	raw := env(name)
	if raw == "" {
		return false, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid $%s: %v", name, err)
	}
	return v, nil
}

func slackClient(env func(string) string) (*platform.ClientInput, error) {
	token, secret := env(EnvSlackToken), env(EnvSlackSecret)
	if token == "" && secret == "" {
		return nil, nil
	}
	if token == "" || secret == "" {
		return nil, fmt.Errorf("slack needs both $%s and $%s", EnvSlackToken, EnvSlackSecret)
	}
	if env(EnvSlackTeam) == "" {
		return nil, fmt.Errorf("slack installation needs $%s", EnvSlackTeam)
	}
	sessions, err := boolEnv(env, EnvSlackSessions)
	if err != nil {
		return nil, err
	}
	var commands []clients.Command
	if raw := env(EnvSlackCommands); raw != "" {
		if err := json.Unmarshal([]byte(raw), &commands); err != nil {
			return nil, fmt.Errorf("invalid $%s: %v", EnvSlackCommands, err)
		}
	}
	return &platform.ClientInput{
		Client: clients.Client{
			ID: defaultSlackClient, Name: "Slack", Surface: clients.Slack,
			Identity:     clients.Identity{Team: env(EnvSlackTeam), AppID: env(EnvSlackApp)},
			DefaultAgent: env(EnvSlackDefault), Bots: pairs(env(EnvSlackBots)),
			Slack: &clients.SlackOptions{Sessions: sessions, Commands: commands},
		},
		Token:         clients.SecretInput{Ref: "env:" + EnvSlackToken},
		SigningSecret: clients.SecretInput{Ref: "env:" + EnvSlackSecret},
	}, nil
}

func githubClient(env func(string) string) (*platform.ClientInput, error) {
	token, secret := env(EnvGitHubToken), env(EnvGitHubSecret)
	if token == "" && secret == "" {
		return nil, nil
	}
	if token == "" || secret == "" {
		return nil, fmt.Errorf("github needs both $%s and $%s", EnvGitHubToken, EnvGitHubSecret)
	}
	if len(list(env(EnvGitHubOwners))) == 0 {
		return nil, fmt.Errorf("GitHub installation needs $%s", EnvGitHubOwners)
	}
	opts := &clients.GitHubOptions{ReadRepositories: list(env(EnvGitHubReadRepositories)), Login: env(EnvGitHubLogin)}
	var err error
	if opts.Mutations, err = boolEnv(env, EnvGitHubMutations); err != nil {
		return nil, err
	}
	if opts.Guides, err = boolEnv(env, EnvGitHubGuides); err != nil {
		return nil, err
	}
	if opts.Checks, err = boolEnv(env, EnvGitHubChecks); err != nil {
		return nil, err
	}
	if raw := env(EnvGitHubAutoLabels); raw != "" {
		if err := json.Unmarshal([]byte(raw), &opts.AutoLabels); err != nil {
			return nil, fmt.Errorf("invalid $%s: %v", EnvGitHubAutoLabels, err)
		}
	}
	return &platform.ClientInput{
		Client: clients.Client{
			ID: defaultGitHubClient, Name: "GitHub", Surface: clients.GitHub,
			Identity: clients.Identity{
				Owners: list(env(EnvGitHubOwners)), AppID: env(EnvGitHubApp), Installation: env(EnvGitHubInstallation),
			},
			DefaultAgent: env(EnvGitHubDefault), Bots: pairs(env(EnvGitHubBots)), GitHub: opts,
		},
		Token:         clients.SecretInput{Ref: "env:" + EnvGitHubToken},
		SigningSecret: clients.SecretInput{Ref: "env:" + EnvGitHubSecret},
	}, nil
}

// pairs parses "k=v,k=v" into a map. Empty entries are skipped so that a
// trailing comma is not a configuration error.
func pairs(s string) map[string]string {
	out := map[string]string{}
	for _, entry := range list(s) {
		k, v, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// list splits a comma-separated environment value.
func list(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
