package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A run's read of its own repository through the App client, answered with
// a redirect the connector would allow, gives the read-redirected hint, and
// the location is not requested: the App client follows no redirect.
func TestAnAppReadAnsweredWithARedirectGivesTheRedirectHint(t *testing.T) {
	_, pemStr := generateTestRSAKeyPEM(t, false)
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	var moved atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/987654/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_installation", "expires_at": now.Add(time.Hour).Format(time.RFC3339)})
		case r.URL.Path == "/repos/acme/web/issues/100":
			w.Header().Set("Location", "https://api.github.example/repos/acme/web/issues/7")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMovedPermanently)
			_, _ = w.Write([]byte(`{"message":"Moved Permanently","url":"https://api.github.example/repos/acme/web/issues/7"}`))
		case strings.HasPrefix(r.URL.Path, "/repos/acme/web/issues/7"):
			moved.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "title": "moved", "state": "open"})
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
	c, err := New(Config{API: api, Owners: []string{"acme"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.PlaceReader().ReadContext(context.Background(), Ref{Owner: "acme", Repo: "web", Number: 100}, 1)
	if !errors.Is(err, ErrReadRedirected) {
		t.Fatalf("a redirected read = %v, want ErrReadRedirected", err)
	}
	if msg := err.Error(); strings.Contains(msg, "Moved Permanently") || strings.Contains(msg, "301") || strings.Contains(msg, "issues/7") {
		t.Errorf("a redirected read gives GitHub's answer: %v", err)
	}
	if n := moved.Load(); n != 0 {
		t.Errorf("the redirect's location was requested %d times", n)
	}
}
