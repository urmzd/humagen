package app

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

func linkProvider(cfg *connectionauth.Config, name string) (connectionauth.Provider, bool) {
	for _, p := range cfg.Providers {
		if p.Name == name {
			return p, true
		}
	}
	return connectionauth.Provider{}, false
}

func TestAccountLinkClientsComeFromTheAppsOrTheEnvironment(t *testing.T) {
	vars := map[string]string{
		"KEY":                 base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))),
		EnvGitHubClientID:     "Iv1.env",
		EnvGitHubClientSecret: "env-secret",
		"SLACK_SECRET":        "slack-secret",
	}
	env := func(name string) string { return vars[name] }
	keys := credref.Resolver{LookupEnv: func(name string) (string, bool) { v, ok := vars[name]; return v, ok }}
	system := spec.System{Spec: spec.SystemSpec{Server: spec.Server{PublicURL: "https://humagen.example.com/"}, Connections: &spec.ConnectionAuthorization{EncryptionKey: &spec.Ref{Ref: "env:KEY"}}}}

	cfg, err := connectionAuthorization(t.Context(), system, keys, env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RedirectURL != "https://humagen.example.com/connections/callback" {
		t.Fatalf("redirect %q", cfg.RedirectURL)
	}
	gh, ok := linkProvider(cfg, platform.GitHubAccount)
	if !ok || gh.ClientID != "Iv1.env" || gh.ClientSecret != "env-secret" || gh.TokenURL != "https://github.com/login/oauth/access_token" {
		t.Fatalf("github from env: %+v", gh)
	}
	if _, ok := linkProvider(cfg, platform.SlackAccount); ok {
		t.Fatal("slack linking without a client")
	}

	// The Slack app's own client, declared for its install, signs people in.
	system.Spec.Connections.Clients = map[string]spec.ConnectionOAuthClient{"slack": {ClientID: "123.456", ClientSecret: &spec.Ref{Ref: "env:SLACK_SECRET"}}}
	system.Spec.Connections.RedirectURL = "https://other.example.com/connections/callback"
	if cfg, err = connectionAuthorization(t.Context(), system, keys, env); err != nil {
		t.Fatal(err)
	}
	sl, ok := linkProvider(cfg, platform.SlackAccount)
	if !ok || sl.ClientID != "123.456" || sl.ClientSecret != "slack-secret" || strings.Join(sl.Scopes, " ") != "openid profile" {
		t.Fatalf("slack: %+v", sl)
	}
	if cfg.RedirectURL != "https://other.example.com/connections/callback" {
		t.Fatalf("declared redirect %q", cfg.RedirectURL)
	}

	// A secret that cannot be read stops the server.
	keys.LookupEnv = func(name string) (string, bool) {
		if name == EnvGitHubClientSecret {
			return "", false
		}
		v, ok := vars[name]
		return v, ok
	}
	if _, err := connectionAuthorization(t.Context(), system, keys, env); err == nil {
		t.Fatal("an unreadable secret was accepted")
	}
}

// Linking is off until all of it is configured: with no connections block
// the environment's clients are not read, a client id with no secret stops
// the server, and each surface is on only with its own pair.
func TestAccountLinkingIsOffUntilAllOfItIsConfigured(t *testing.T) {
	vars := map[string]string{
		"KEY":                 base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))),
		EnvGitHubClientID:     "Iv1.env",
		EnvGitHubClientSecret: "env-secret",
		EnvSlackClientID:      "123.456",
		EnvSlackClientSecret:  "slack-secret",
	}
	env := func(name string) string { return vars[name] }
	keys := credref.Resolver{LookupEnv: func(name string) (string, bool) { v, ok := vars[name]; return v, ok }}
	system := spec.System{Spec: spec.SystemSpec{Server: spec.Server{PublicURL: "https://humagen.example.com"}}}

	if cfg, err := connectionAuthorization(t.Context(), system, keys, env); err != nil || cfg != nil {
		t.Fatalf("with no connections block: %+v %v", cfg, err)
	}
	system.Spec.Connections = &spec.ConnectionAuthorization{EncryptionKey: &spec.Ref{Ref: "env:KEY"}}
	cfg, err := connectionAuthorization(t.Context(), system, keys, env)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{platform.GitHubAccount, platform.SlackAccount} {
		if p, ok := linkProvider(cfg, name); !ok || p.ClientID == "" || p.ClientSecret == "" {
			t.Fatalf("%s with both set: %+v", name, p)
		}
	}
	// The Slack app's sign-in to Humagen has a secret of its own
	// (surfaces.slack.humagen_client), which is never read as Slack's.
	if EnvSlackClientSecret == "HUMAGEN_SLACK_CLIENT_SECRET" {
		t.Fatal("the Slack OAuth client secret shares a name with the Slack app's Humagen client secret")
	}
	for _, id := range []string{EnvGitHubClientID, EnvSlackClientID} {
		half := map[string]string{"KEY": vars["KEY"], id: "client-id"}
		_, err := connectionAuthorization(t.Context(), system, keys, func(name string) string { return half[name] })
		if err == nil || !strings.Contains(err.Error(), id) {
			t.Fatalf("%s with no secret: %v", id, err)
		}
	}
	// With neither, each surface is off and the rest of connections is on.
	cfg, err = connectionAuthorization(t.Context(), system, keys, func(name string) string { return "" })
	if err != nil || cfg == nil {
		t.Fatalf("with no clients: %+v %v", cfg, err)
	}
	for _, name := range []string{platform.GitHubAccount, platform.SlackAccount} {
		if _, ok := linkProvider(cfg, name); ok {
			t.Fatalf("%s is on with no client", name)
		}
	}
}
