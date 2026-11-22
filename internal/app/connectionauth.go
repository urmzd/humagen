package app

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

// The GitHub App's and the Slack app's OAuth clients, for a person linking
// their account, when the spec does not declare the app (connections.github,
// connections.clients.slack). The secret is read through env:, as a
// credential reference, and never kept.
const (
	EnvGitHubClientID     = "HUMAGEN_GITHUB_CLIENT_ID"
	EnvGitHubClientSecret = "HUMAGEN_GITHUB_CLIENT_SECRET"
	EnvSlackClientID      = "HUMAGEN_SLACK_CLIENT_ID"
	EnvSlackClientSecret  = "HUMAGEN_SLACK_CLIENT_SECRET"
)

// RedirectURL is where every connection's consent returns: the spec's
// connections.redirect_url, else server.public_url's /connections/callback.
func RedirectURL(system spec.System) string {
	if u := system.Spec.Connections.RedirectURL; u != "" {
		return u
	}
	return strings.TrimRight(cmp.Or(system.Spec.Server.PublicURL, "http://localhost:8080"), "/") + "/connections/callback"
}

func connectionAuthorization(ctx context.Context, system spec.System, keys credref.Resolver, env func(string) string) (*connectionauth.Config, error) {
	settings := system.Spec.Connections
	if settings == nil {
		return nil, nil
	}
	if env == nil {
		env = func(string) string { return "" }
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
		switch {
		case link.name == platform.GitHubAccount && settings.GitHub != nil:
			link.id, link.ref = settings.GitHub.ClientID, settings.GitHub.ClientSecret
		case link.name == platform.SlackAccount && settings.Clients["slack"].ClientID != "":
			link.id, link.ref = settings.Clients["slack"].ClientID, settings.Clients["slack"].ClientSecret
		case env(link.idEnv) != "" && env(link.secretEnv) != "":
			link.id, link.ref = env(link.idEnv), &spec.Ref{Ref: "env:" + link.secretEnv}
		default:
			continue
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
