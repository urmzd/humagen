package app

import (
	"context"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

func TestGitHubInstallationResolvesOnlyConfiguredReferences(t *testing.T) {
	var system spec.System
	if got, err := githubInstallation(context.Background(), system, credref.Resolver{}); err != nil || got != nil {
		t.Fatalf("unconfigured installation=%+v err=%v", got, err)
	}
	system.Spec.Connections = &spec.ConnectionAuthorization{RedirectURL: "https://humagen.example/connections/callback", GitHub: &spec.GitHubInstallation{AppID: "123", AppSlug: "humagen-app", ClientID: "client-id", ClientSecret: &spec.Ref{Ref: "env:HUMAGEN_GITHUB_CLIENT_SECRET"}, PrivateKey: &spec.Ref{Ref: "env:HUMAGEN_GITHUB_PRIVATE_KEY"}, WebhookSecret: &spec.Ref{Ref: "env:HUMAGEN_GITHUB_WEBHOOK_SECRET"}}}
	values := map[string]string{"HUMAGEN_GITHUB_CLIENT_SECRET": "client-value", "HUMAGEN_GITHUB_PRIVATE_KEY": "private-value", "HUMAGEN_GITHUB_WEBHOOK_SECRET": "signing-value"}
	keys := credref.Resolver{LookupEnv: func(name string) (string, bool) { v, ok := values[name]; return v, ok }}
	got, err := githubInstallation(context.Background(), system, keys)
	if err != nil || got.AppID != "123" || got.AppSlug != "humagen-app" || got.ClientSecret != "client-value" || got.PrivateKey != "private-value" || got.SigningSecret != "signing-value" || got.PrivateKeyRef != "env:HUMAGEN_GITHUB_PRIVATE_KEY" || got.SigningSecretRef != "env:HUMAGEN_GITHUB_WEBHOOK_SECRET" || got.RedirectURL != system.Spec.Connections.RedirectURL {
		t.Fatalf("resolved installation=%v err=%v", got != nil, err)
	}
	delete(values, "HUMAGEN_GITHUB_PRIVATE_KEY")
	if _, err := githubInstallation(context.Background(), system, keys); err == nil || strings.Contains(err.Error(), "HUMAGEN_GITHUB") {
		t.Fatalf("unavailable key error exposed reference or was ignored: %v", err)
	}
	system.Spec.Connections.GitHub.PrivateKey = nil
	if _, err := githubInstallation(context.Background(), system, keys); err == nil {
		t.Fatal("missing private key reference accepted")
	}
}
