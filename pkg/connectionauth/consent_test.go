package connectionauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestCustomRegisteredClientIssuerScopesAndReconnect(t *testing.T) {
	m, st, _ := setup(t)
	original := m.client.Transport
	m.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			return response(`{"issuer":"https://auth.test","authorization_endpoint":"https://auth.test/authorize","token_endpoint":"https://auth.test/token","token_endpoint_auth_methods_supported":["client_secret_post"],"code_challenge_methods_supported":["S256"]}`), nil
		}
		if r.URL.Path == "/register" {
			t.Fatal("registered client attempted DCR")
		}
		if r.URL.Path == "/token" {
			return response(`{"access_token":"access-private","refresh_token":"refresh-private","token_type":"Bearer","expires_in":3600,"scope":"read files:read read"}`), nil
		}
		return original.RoundTrip(r)
	})
	ctx := context.Background()
	resource := "https://service.test/mcp"
	registration := &Registration{Issuer: "https://auth.test", ClientID: "customer-client", ClientSecret: "client-private"}
	a, err := m.BeginRegistered(ctx, "tenant", "alice", "alice", "other", resource, registration)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(a.URL)
	if u.Query().Get("client_id") != "customer-client" {
		t.Fatal("client ignored")
	}
	if err = m.Complete(ctx, "tenant", "alice", a.Provider, nonceOf(t, a), "code"); err != nil {
		t.Fatal(err)
	}
	status, err := m.Status("tenant", "alice", a.Provider)
	if err != nil || status.GrantedScopes == nil || strings.Join(*status.GrantedScopes, ",") != "read,files:read" {
		t.Fatal(status, err)
	}
	if _, err = m.BeginFor(ctx, "tenant", "alice", "alice", a.Provider, resource); err != nil {
		t.Fatal("reconnect lost registration", err)
	}
	bad := *registration
	bad.Issuer = "https://wrong.test"
	if _, err = m.BeginRegistered(ctx, "tenant", "alice", "alice", "other", resource, &bad); err == nil {
		t.Fatal("accepted mismatched issuer")
	}
	rows, _ := st.Load(ctx, "connection_oauth")
	for _, row := range rows {
		for _, secret := range []string{"client-private", "access-private", "refresh-private"} {
			if strings.Contains(string(row.Value), secret) {
				t.Fatal("plaintext stored")
			}
		}
	}
}

func TestConfirmedRefreshRejectionAlertsAfterLeaseRelease(t *testing.T) {
	for _, status := range []int{400, 401, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			m, _, _ := setup(t)
			ctx := context.Background()
			a, err := m.Begin(ctx, "tenant", "alice", "notes")
			if err != nil {
				t.Fatal(err)
			}
			if err = m.Complete(ctx, "tenant", "alice", "notes", nonceOf(t, a), "code"); err != nil {
				t.Fatal(err)
			}
			calls := 0
			m.cfg.OnAuthFailure = func(_ context.Context, tenant, person, provider string) error {
				calls++
				if tenant != "tenant" || person != "alice" || provider != "notes" {
					t.Fatal("wrong consenting actor")
				}
				row, _ := m.records.Lookup(tenant, recordKey("account", person, provider))
				var held account
				if m.open(tenant, row.Key, row.Value, &held) != nil || !held.LeaseUntil.IsZero() {
					t.Fatal("callback holds credential lease")
				}
				return errors.New("private callback failure")
			}
			m.client.Transport = transport(func(*http.Request) (*http.Response, error) {
				r := response(`{"error":"invalid_grant","error_description":"private token"}`)
				r.StatusCode = status
				if status == 500 {
					r.Body = response(`{"error":"server_error"}`).Body
				}
				return r, nil
			})
			_, err = m.Token(ctx, "tenant", "alice", "notes")
			if !errors.Is(err, ErrReconnect) || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
			state, stateErr := m.Status("tenant", "alice", "notes")
			if stateErr != nil || state.ReconnectRequired != (status != 500) {
				t.Fatal("rejection status not retained", state, stateErr)
			}
			if (calls == 1) != (status != 500) {
				t.Fatalf("alerts=%d status=%d", calls, status)
			}
		})
	}
}

func TestGrantedScopesNeverInventOrExposeCredentials(t *testing.T) {
	for _, raw := range []any{nil, []string{"read"}, "access-secret", "scope-access-secret", "bad\nscope"} {
		tok := (&oauth2.Token{AccessToken: "access-secret", Expiry: time.Now()}).WithExtra(map[string]any{"scope": raw})
		if _, known := grantedScopes(tok); known {
			t.Fatalf("unsafe scope accepted: %v", raw)
		}
	}
}
