package platform

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/githubinstall"
	"github.com/urmzd/humagen/pkg/state"
)

func TestNativeGitHubInstallCreatesDisabledPinnedClientAndSurvivesRestart(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var exchanges atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		installation := map[string]any{"id": 42, "app_id": 123, "app_slug": "humagen-app", "account": map[string]any{"id": 9, "login": "octo-org"}}
		switch r.URL.Path {
		case "/login/oauth/access_token":
			exchanges.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "native-user-token", "token_type": "bearer"})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 5, "login": "octocat"})
		case "/user/installations":
			_ = json.NewEncoder(w).Encode(map[string]any{"installations": []any{installation}})
		case "/app/installations/42":
			_ = json.NewEncoder(w).Encode(installation)
		default:
			t.Errorf("unexpected native provider request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(provider.Close)
	store := state.NewMemory()
	cfg := &githubinstall.Config{AppID: "123", AppSlug: "humagen-app", ClientID: "client-id", ClientSecret: "client-secret", PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), SigningSecret: "webhook-secret", PrivateKeyRef: "env:HUMAGEN_GITHUB_PRIVATE_KEY", SigningSecretRef: "env:HUMAGEN_GITHUB_WEBHOOK_SECRET", RedirectURL: "https://humagen.example/connections/callback", GitHubURL: provider.URL, APIURL: provider.URL, HTTP: provider.Client()}
	newReplica := func() *Platform {
		t.Helper()
		p, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: store}, GitHubInstall: cfg})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		return p
	}
	p := newReplica()
	ctx := context.Background()
	who := auth.Principal{Tenant: "acme", Subject: "alice", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	for _, forbidden := range []auth.Principal{
		{Tenant: "acme", Subject: "member", Kind: auth.User},
		{Tenant: "acme", Subject: "bot", Kind: auth.Machine, Scopes: []auth.Permission{auth.ClientsWrite}},
	} {
		if _, err := p.BeginGitHubInstall(ctx, forbidden); !errors.Is(err, ErrForbidden) {
			t.Fatalf("native installation allowed forbidden caller: %v", err)
		}
		if err := p.CompleteGitHubInstall(ctx, forbidden, "state", "code", "42"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("callback allowed forbidden caller: %v", err)
		}
		if err := p.DisconnectGitHubInstall(ctx, forbidden); !errors.Is(err, ErrForbidden) {
			t.Fatalf("disconnect allowed forbidden caller: %v", err)
		}
	}
	a, err := p.BeginGitHubInstall(ctx, who)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(a.URL)
	nonce := u.Query().Get("state")
	if err := p.CompleteGitHubInstall(ctx, who, nonce, "one-use-code", "42"); err != nil {
		t.Fatal(err)
	}
	c, err := p.Clients.Get(who.Tenant, "github-42")
	if err != nil {
		t.Fatal(err)
	}
	if c.Enabled || len(c.Agents) != 0 || c.Capabilities == nil || len(c.Capabilities.Events) != 0 || len(c.Capabilities.Actions) != 0 || c.Capabilities.DeliverOutcomes || c.Managed != clients.ManagedInstall || c.Identity.Installation != "42" || c.Identity.AppID != "123" || c.Identity.Owners[0] != "octo-org" {
		t.Fatalf("installation acquired unintended grants: %+v", c)
	}
	if c.Credentials.Token.Ref != cfg.PrivateKeyRef || c.Credentials.SigningSecret.Ref != cfg.SigningSecretRef {
		t.Fatalf("installation did not preserve credential references: %+v", c.Credentials)
	}
	if c.GitHub == nil || c.GitHub.Login != "humagen-app[bot]" {
		t.Fatal("installation did not retain its verified app login")
	}
	for _, secret := range []string{clients.SecretToken, clients.SecretSigning} {
		if value, held := p.secrets.Get(who.Tenant, c.ID, secret); held && value != "" {
			t.Fatal("installation copied a deployment credential into client storage")
		}
	}
	for _, edit := range []ClientInput{
		{Client: c, Token: clients.SecretInput{Value: "replacement-token"}},
		{Client: c, SigningSecret: clients.SecretInput{Ref: "env:HUMAGEN_OTHER_SECRET"}},
	} {
		if _, err := p.UpdateClient(ctx, who.Tenant, c.ID, who.Subject, edit); !errors.Is(err, clients.ErrManaged) {
			t.Fatalf("installed credential replaced: %v", err)
		}
	}
	chosen := c
	chosen.Enabled = true
	chosen.Agents = []string{"helper"}
	chosen.Identity.AppID, chosen.Identity.Installation, chosen.Identity.Owners = "777", "777", []string{"attacker"}
	chosen.Credentials = clients.Credentials{}
	updated, err := p.UpdateClient(ctx, who.Tenant, c.ID, who.Subject, ClientInput{Client: chosen})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Enabled || updated.Agents[0] != "helper" || updated.Identity.AppID != "123" || updated.Identity.Installation != "42" || updated.Identity.Owners[0] != "octo-org" || updated.Credentials != c.Credentials || updated.Capabilities == nil {
		t.Fatalf("routing edit replaced verified identity or credentials: %+v", updated)
	}
	// A public caller cannot give a managed marker or use the deployment refs.
	forged := c
	forged.ID, forged.Name, forged.Managed = "forged-client", "Forged", ""
	if _, err := p.CreateClient(ctx, who.Tenant, who.Subject, ClientInput{Client: forged, Token: clients.SecretInput{Ref: cfg.PrivateKeyRef}, SigningSecret: clients.SecretInput{Ref: cfg.SigningSecretRef}}); !errors.Is(err, clients.ErrInvalid) {
		t.Fatalf("unverified client used app credentials: %v", err)
	}
	forged.Managed = clients.ManagedInstall
	if _, err := p.CreateClient(ctx, who.Tenant, who.Subject, ClientInput{Client: forged, Token: clients.SecretInput{Value: "token"}, SigningSecret: clients.SecretInput{Value: "secret"}}); !errors.Is(err, clients.ErrManaged) {
		t.Fatalf("unverified client forged installation marker: %v", err)
	}
	other := newReplica()
	if err := other.CompleteGitHubInstall(ctx, who, nonce, "one-use-code", "42"); err != nil {
		t.Fatal(err)
	}
	if exchanges.Load() != 1 {
		t.Fatalf("restart replayed authorization code: %d", exchanges.Load())
	}
	retained, _ := other.Clients.Get(who.Tenant, c.ID)
	if retained.Version != updated.Version || !retained.Enabled {
		t.Fatal("idempotent callback reset chosen client configuration")
	}
	assertGitHubAuthFailureOwnership(t, p, other, who, retained, cfg.PrivateKey)
	if err := other.DisconnectGitHubInstall(ctx, who); err != nil {
		t.Fatal(err)
	}
	retained, _ = other.Clients.Get(who.Tenant, c.ID)
	if retained.Enabled {
		t.Fatal("disconnect left installation enabled")
	}
	if err := other.CompleteGitHubInstall(ctx, who, nonce, "one-use-code", "42"); !errors.Is(err, connectionauth.ErrInvalid) {
		t.Fatalf("disconnect left old authorization usable: %v", err)
	}
}
