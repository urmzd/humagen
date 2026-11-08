package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAppTokenSourceRefusesRedirectsWithoutLeakingJWT(t *testing.T) {
	_, key := generateTestRSAKeyPEM(t, false)
	for _, sameOrigin := range []bool{false, true} {
		t.Run(fmt.Sprintf("same-origin-%t", sameOrigin), func(t *testing.T) {
			var reached, callerRedirect atomic.Int64
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1); w.WriteHeader(http.StatusOK) }))
			defer receiver.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/steal" {
					reached.Add(1)
					return
				}
				target := receiver.URL + "/steal"
				if sameOrigin {
					target = "/steal"
				}
				w.Header().Set("Location", target)
				w.WriteHeader(http.StatusTemporaryRedirect)
				_, _ = w.Write([]byte("private-provider-body " + r.Header.Get("Authorization")))
			}))
			defer origin.Close()
			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { callerRedirect.Add(1); return nil }}
			source, err := NewAppTokenSource(origin.URL, AppConfig{AppID: "123", InstallationID: "42", PrivateKey: key, HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			_, err = source.Token(context.Background())
			if err == nil || !strings.Contains(err.Error(), "307") || strings.Contains(err.Error(), "private-provider-body") || strings.Contains(err.Error(), "Bearer") || strings.Contains(err.Error(), "eyJ") {
				t.Fatalf("unsafe or missing mint error: %v", err)
			}
			if reached.Load() != 0 || callerRedirect.Load() != 0 || client.Timeout != 0 {
				t.Fatalf("redirect escaped or caller client changed: reached=%d redirect=%d timeout=%v", reached.Load(), callerRedirect.Load(), client.Timeout)
			}
		})
	}
}

func TestAppTokenSourceRedactsProviderAndTransportErrors(t *testing.T) {
	_, key := generateTestRSAKeyPEM(t, false)
	for _, mode := range []string{"http", "json", "transport", "expired", "header"} {
		t.Run(mode, func(t *testing.T) {
			client := &http.Client{Transport: appRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if mode == "transport" {
					return nil, fmt.Errorf("private-provider-body %s", r.Header.Get("Authorization"))
				}
				rec := httptest.NewRecorder()
				switch mode {
				case "http":
					rec.WriteHeader(http.StatusForbidden)
					_, _ = rec.WriteString("private-provider-body " + r.Header.Get("Authorization"))
				case "json":
					_, _ = rec.WriteString(`{"token":"ghs_secret","expires_at":"private-provider-body"}`)
				case "expired":
					_, _ = rec.WriteString(`{"token":"ghs_secret","expires_at":"2000-01-01T00:00:00Z"}`)
				case "header":
					_, _ = rec.WriteString(`{"token":"ghs_secret\r\nInjected: token"}`)
				}
				return rec.Result(), nil
			})}
			source, err := NewAppTokenSource("https://api.github.example", AppConfig{AppID: "123", InstallationID: "42", PrivateKey: key, HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			_, err = source.Token(context.Background())
			if err == nil || strings.Contains(err.Error(), "ghs_secret") || strings.Contains(err.Error(), "private-provider-body") || strings.Contains(err.Error(), "Bearer") || strings.Contains(err.Error(), "eyJ") {
				t.Fatalf("unsafe or missing mint error: %v", err)
			}
			if source.token != "" {
				t.Fatal("invalid token response was cached")
			}
		})
	}
}

func TestAppHTTPAPIRefusesRedirectsAndRedactsResponseBodies(t *testing.T) {
	_, key := generateTestRSAKeyPEM(t, false)
	for _, mode := range []string{"same-origin", "other-origin", "body", "transport"} {
		t.Run(mode, func(t *testing.T) {
			var reached atomic.Int64
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
			defer receiver.Close()
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/app/installations/42/access_tokens" {
					_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_private_installation_token", "expires_at": time.Now().Add(time.Hour).UTC()})
					return
				}
				if r.URL.Path == "/steal" {
					reached.Add(1)
					return
				}
				if mode == "body" {
					w.WriteHeader(http.StatusForbidden)
				} else {
					target := "/steal"
					if mode == "other-origin" {
						target = receiver.URL + "/steal"
					}
					w.Header().Set("Location", target)
					w.WriteHeader(http.StatusTemporaryRedirect)
				}
				_, _ = w.Write([]byte("private-provider-body " + r.Header.Get("Authorization")))
			}))
			defer provider.Close()
			client := provider.Client()
			if mode == "transport" {
				client = &http.Client{Transport: appRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/app/installations/42/access_tokens" {
						return http.DefaultTransport.RoundTrip(r)
					}
					return nil, fmt.Errorf("private-provider-body %s", r.Header.Get("Authorization"))
				})}
			}
			api, err := NewAppHTTPAPIAt(provider.URL, AppConfig{AppID: "123", InstallationID: "42", PrivateKey: key, HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			_, err = api.CreateComment(context.Background(), Ref{Owner: "acme", Repo: "widgets", Number: 42}, "hello")
			if err == nil || strings.Contains(err.Error(), "ghs_private_installation_token") || strings.Contains(err.Error(), "private-provider-body") || strings.Contains(err.Error(), "Bearer") {
				t.Fatalf("unsafe or missing API error: %v", err)
			}
			if reached.Load() != 0 {
				t.Fatal("installation token followed a redirect")
			}
		})
	}
}

func TestAppHTTPAPIOriginFenceRunsBeforeTokenMint(t *testing.T) {
	_, key := generateTestRSAKeyPEM(t, false)
	var requests atomic.Int64
	api, err := NewAppHTTPAPIAt("https://api.github.example/api/v3", AppConfig{AppID: "123", InstallationID: "42", PrivateKey: key, HTTPClient: &http.Client{Transport: appRoundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, fmt.Errorf("unexpected request")
	})}})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"https://other.example/api/v3/user",
		"http://api.github.example/api/v3/user",
		"https://api.github.example:444/api/v3/user",
		"https://api.github.example/outside",
		"https://api.github.example/api/v3/../outside",
		"https://api.github.example/api/v3/%2e%2e/outside",
		"https://user:secret@api.github.example/api/v3/user",
	} {
		response, err := api.http.Get(target)
		if response != nil {
			_ = response.Body.Close()
		}
		if err == nil {
			t.Fatalf("foreign origin/path was accepted: %s", target)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("origin failure minted a token or reached a provider: %d", requests.Load())
	}
	for _, base := range []string{"https://user:secret@api.github.example", "https://api.github.example?private=secret", "https://api.github.example/#private", "http://api.github.example", "https://api.github.example/api/../v3"} {
		if _, err := NewAppTokenSource(base, AppConfig{AppID: "123", InstallationID: "42", PrivateKey: key}); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe endpoint accepted or echoed: %v", err)
		}
	}
}
