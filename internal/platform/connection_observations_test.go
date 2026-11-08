package platform

import (
	"net/http"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/state"
)

func TestConnectionObservationsSurviveRestartAndFollowConsentOwner(t *testing.T) {
	store := state.NewMemory()
	oauth := nativeConfig("drive")
	oauth.HTTP.Transport = nativeHTTP(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/drive/v3/about" {
			return nativeResponse(`{"kind":"drive#about"}`), nil
		}
		return nativeResponse(`{"access_token":"private-access","refresh_token":"private-refresh","expires_in":3600,"token_type":"Bearer","scope":"https://www.googleapis.com/auth/drive.readonly"}`), nil
	})
	cfg := Config{Workspace: t.TempDir(), Backends: Backends{State: store}, ConnectionAuth: oauth, Now: func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }}
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	who := auth.Principal{Tenant: "tenant", Subject: "alice", Kind: auth.User, Scopes: []auth.Permission{auth.RunsRead, auth.KnowledgeRead}}
	// Imported local sources have no external lifecycle and are not OAuth uses.
	if _, err = p.Knowledge.Create(who.Tenant, "local", "", who.Subject, p.now()); err != nil {
		t.Fatal(err)
	}
	authorizeNative(t, p, who, "drive", "person")
	observed, err := p.CheckConnection(t.Context(), who, "drive")
	if err != nil || !observed.Healthy {
		t.Fatal(observed, err)
	}
	cfg.Workspace = t.TempDir()
	q, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })
	detail, err := q.GetConnection(t.Context(), who, "drive")
	if err != nil || detail.Connection.Health == nil || !detail.Connection.Health.Healthy || !detail.Connection.Health.CheckedAt.Equal(observed.CheckedAt) {
		t.Fatal(detail, err)
	}
	if detail.Connection.GrantedScopes == nil || len(*detail.Connection.GrantedScopes) != 1 {
		t.Fatal("granted scope lost on restart")
	}
	for _, other := range []auth.Principal{{Tenant: "tenant", Subject: "bob", Kind: auth.User}, {Tenant: "other", Subject: "alice", Kind: auth.User}} {
		got, err := q.GetConnection(t.Context(), other, "drive")
		if err != nil || got.Connection.Health != nil || got.Connection.GrantedScopes != nil || got.Connection.Connected {
			t.Fatal("private consent leaked", got, err)
		}
	}
	authorizeNative(t, q, who, "drive", "person")
	detail, err = q.GetConnection(t.Context(), who, "drive")
	if err != nil || detail.Connection.Health != nil {
		t.Fatal("reconnect retained obsolete health", detail, err)
	}
}

func TestNativeBigQueryConsentCannotBorrowDriveAudience(t *testing.T) {
	drive, bq := NativeConnectionProvider("drive"), NativeConnectionProvider("bigquery")
	if drive.Resource == bq.Resource || len(bq.Scopes) != 1 || bq.Scopes[0] != "https://www.googleapis.com/auth/bigquery.readonly" || !bq.Offline {
		t.Fatal("BigQuery consent broadened or reused Drive")
	}
}

func TestConnectionHTTP401RequiresReconnectButOutageDoesNot(t *testing.T) {
	for _, code := range []int{401, 403, 500} {
		p, err := New(Config{Workspace: t.TempDir(), ConnectionAuth: nativeConfig("drive"), PersonalHTTP: &http.Client{Transport: nativeHTTP(func(*http.Request) (*http.Response, error) {
			r := nativeResponse(`{"error":"private diagnostic"}`)
			r.StatusCode = code
			return r, nil
		})}})
		if err != nil {
			t.Fatal(err)
		}
		who := auth.Principal{Tenant: "tenant", Subject: "alice", Kind: auth.User}
		authorizeNative(t, p, who, "drive", "person")
		client := p.personOAuthHTTPClient(who.Tenant, who.Subject, personctx.ToolServer{OAuth: "drive"})
		response, err := client.Get("https://service.example/mcp")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		status, err := p.ConnectionAuth.Status(who.Tenant, who.Subject, "drive")
		if err != nil || status.ReconnectRequired != (code == 401) {
			t.Fatal(code, status, err)
		}
		_ = p.Close()
	}
}

func TestConnectionDetailShowsConfiguredReplyDestinations(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir(), ConnectionAuth: nativeConfig("slack")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	who := auth.Principal{Tenant: "tenant", Subject: "alice", Kind: auth.User, Scopes: []auth.Permission{auth.ClientsRead, auth.ClientsWrite}}
	authorizeNative(t, p, who, "slack", "workspace")
	c, err := p.Clients.Get(who.Tenant, "slack-t12345")
	if err != nil {
		t.Fatal(err)
	}
	c.Slack = &clients.SlackOptions{ActionChannels: []string{"C12345"}}
	if _, err = p.Clients.Update(who.Tenant, c, p.now()); err != nil {
		t.Fatal(err)
	}
	detail, err := p.GetConnection(t.Context(), who, "slack")
	if err != nil || len(detail.Mappings) != 1 || len(detail.Mappings[0].Places) != 1 || detail.Mappings[0].Places[0] != "Slack action destination C12345" {
		t.Fatal(detail, err)
	}
	who.Scopes = nil
	hidden, err := p.GetConnection(t.Context(), who, "slack")
	if err != nil || len(hidden.Mappings) != 0 {
		t.Fatal("client permission bypass", hidden, err)
	}
}
