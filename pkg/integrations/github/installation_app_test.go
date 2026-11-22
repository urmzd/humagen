package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// An installation lists its repositories with its own installation token, at
// the App's API base.
func TestAnAppInstallationListsItsRepositoriesWithItsToken(t *testing.T) {
	_, pemStr := generateTestRSAKeyPEM(t, false)
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/987654/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_installation", "expires_at": now.Add(time.Hour).Format(time.RFC3339)})
		case r.Method == http.MethodGet && r.Host == "api.github.example" && r.URL.Path == "/installation/repositories":
			if got := r.Header.Get("Authorization"); got != "Bearer ghs_installation" {
				t.Errorf("Authorization = %q, want the installation token", got)
			}
			_, _ = w.Write([]byte(`{"total_count":2,"repositories":[{"full_name":"acme/api"},{"full_name":"acme/web"}]}`))
		default:
			http.NotFound(w, r)
		}
	})
	api, err := NewAppHTTPAPIAt("https://api.github.example", AppConfig{
		AppID: "12345", InstallationID: "987654", PrivateKey: pemStr, Clock: func() time.Time { return now },
		HTTPClient: &http.Client{Transport: appRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			return rec.Result(), nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	repos, err := api.InstallationRepositories(context.Background(), 0)
	if err != nil || len(repos) != 2 || repos[0] != "acme/api" || repos[1] != "acme/web" {
		t.Fatalf("repos = %v, %v", repos, err)
	}
}
