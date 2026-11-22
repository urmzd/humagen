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
