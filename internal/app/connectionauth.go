package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

// The GitHub App's and the Slack app's OAuth clients, for a person linking
// their account, when the spec does not declare the app (connections.github,
// connections.clients.slack). The secret is read through env:, as a
// credential reference, and never kept. Each pair is set together or not at
// all, and is read only where the spec has a connections block.
const (
	EnvGitHubClientID     = "HUMAGEN_GITHUB_CLIENT_ID"
	EnvGitHubClientSecret = "HUMAGEN_GITHUB_CLIENT_SECRET"
	EnvSlackClientID      = "HUMAGEN_SLACK_OAUTH_CLIENT_ID"
	EnvSlackClientSecret  = "HUMAGEN_SLACK_OAUTH_CLIENT_SECRET"
)

// RedirectURL is where every connection's consent returns: the spec's
// connections.redirect_url, else server.public_url's /connections/callback.
func RedirectURL(system spec.System) string {
	return system.Spec.Connections.Redirect(system.Spec.Server.PublicURL)
}

func connectionAuthorization(ctx context.Context, system spec.System, keys credref.Resolver, env func(string) string, log *slog.Logger) (*connectionauth.Config, error) {
	if env == nil {
		env = func(string) string { return "" }
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	settings := system.Spec.Connections
	if settings == nil {
		var unread []string
		for _, name := range []string{EnvGitHubClientID, EnvGitHubClientSecret, EnvSlackClientID, EnvSlackClientSecret} {
			if env(name) != "" {
				unread = append(unread, name)
			}
		}
		if len(unread) > 0 {
			log.Warn("connections: account linking is off: the spec has no connections block, so the environment's OAuth clients are not read", "variables", strings.Join(unread, ","))
		}
		return nil, nil
	}
	encoded, err := keys.Resolve(ctx, settings.EncryptionKey.Ref)
	if err != nil {
		return nil, errors.New("connection encryption key unavailable")
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, errors.New("connection encryption key must be 32 random bytes encoded as base64")
	}
	config := &connectionauth.Config{Key: key, RedirectURL: RedirectURL(system), Providers: platform.DefaultConnectionProviders()}
	for _, name := range []string{"drive", "onedrive", "slack", "bigquery"} {
		if _, ok := settings.Clients[name]; ok {
			config.Providers = append(config.Providers, platform.NativeConnectionProvider(name))
		}
	}
	// One Google application serves every Google Workspace tool server.
	google := map[string]bool{}
	if _, ok := settings.Clients[platform.GoogleClient]; ok {
		for _, provider := range platform.GoogleToolProviders() {
			google[provider.Name] = true
			config.Providers = append(config.Providers, provider)
		}
	}
	for i := range config.Providers {
		provider := &config.Providers[i]
		client, ok := settings.Clients[provider.Name]
		if !ok && google[provider.Name] {
			client, ok = settings.Clients[platform.GoogleClient]
		}
		if !ok {
			continue
		}
		provider.ClientID = client.ClientID
		if provider.AuthorizationURL == "" {
			provider.Issuer, provider.Scopes = client.Issuer, client.Scopes
		}
		if client.ClientSecret.Set() {
			provider.ClientSecret, err = keys.Resolve(ctx, client.ClientSecret.Ref)
			if err != nil {
				return nil, errors.New("connection client secret unavailable")
			}
		}
		if client.SigningSecret.Set() {
			provider.SigningSecret, err = keys.Resolve(ctx, client.SigningSecret.Ref)
			if err != nil {
				return nil, errors.New("connection signing secret unavailable")
			}
		}
	}
	// Account links sign in through the app the surface already is: its
	// own OAuth client, declared for its install, else in the environment.
	for _, link := range []struct {
		name, id, secretEnv, idEnv string
		ref                        *spec.Ref
	}{
		{name: platform.GitHubAccount, idEnv: EnvGitHubClientID, secretEnv: EnvGitHubClientSecret},
		{name: platform.SlackAccount, idEnv: EnvSlackClientID, secretEnv: EnvSlackClientSecret},
	} {
		declared := link.name == platform.GitHubAccount && settings.GitHub != nil || link.name == platform.SlackAccount && settings.Clients["slack"].ClientID != ""
		if declared {
			// The spec's app is used; a variable it does not itself name is
			// not read.
			var id, ref string
			if link.name == platform.GitHubAccount {
				id, ref = settings.GitHub.ClientID, refOf(settings.GitHub.ClientSecret)
			} else {
				id, ref = settings.Clients["slack"].ClientID, refOf(settings.Clients["slack"].ClientSecret)
			}
			if v := env(link.idEnv); v != "" && v != id {
				log.Warn("connections: the spec declares this app's OAuth client, so the variable is not read", "variable", link.idEnv)
			}
			if env(link.secretEnv) != "" && ref != "env:"+link.secretEnv {
				log.Warn("connections: the spec declares this app's OAuth client, so the variable is not read", "variable", link.secretEnv)
			}
		} else if env(link.idEnv) == "" && env(link.secretEnv) != "" {
			log.Warn("connections: account linking is off for this surface: the client secret is set without its client id", "secret", link.secretEnv, "missing", link.idEnv)
		}
		switch {
		case link.name == platform.GitHubAccount && settings.GitHub != nil:
			link.id, link.ref = settings.GitHub.ClientID, settings.GitHub.ClientSecret
		case link.name == platform.SlackAccount && settings.Clients["slack"].ClientID != "":
			link.id, link.ref = settings.Clients["slack"].ClientID, settings.Clients["slack"].ClientSecret
		case env(link.idEnv) != "" && env(link.secretEnv) != "":
			link.id, link.ref = env(link.idEnv), &spec.Ref{Ref: "env:" + link.secretEnv}
		case env(link.idEnv) != "":
			// The id alone names the pair, so half of it is said at start
			// rather than left as a link that is off for no stated reason.
			return nil, fmt.Errorf("%s is set without %s: set both to let people link their account, or neither", link.idEnv, link.secretEnv)
		default:
			continue
		}
		if link.id == "" {
			return nil, errors.New("account link client id is empty")
		}
		provider := platform.NativeConnectionProvider(link.name)
		provider.ClientID = link.id
		if !link.ref.Set() {
			return nil, errors.New("account link client secret must be a reference")
		}
		if provider.ClientSecret, err = keys.Resolve(ctx, link.ref.Ref); err != nil {
			return nil, errors.New("account link client secret unavailable")
		}
		config.Providers = append(config.Providers, provider)
	}
	return config, nil
}

// refOf is a reference's text, empty for none.
func refOf(r *spec.Ref) string {
	if r == nil {
		return ""
	}
	return r.Ref
}
