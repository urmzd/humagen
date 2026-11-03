package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func generateTestRSAKeyPEM(t *testing.T, pkcs8 bool) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if pkcs8 {
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		block = &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	} else {
		der := x509.MarshalPKCS1PrivateKey(key)
		block = &pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}
	}
	return key, string(pem.EncodeToMemory(block))
}

type appRoundTripFunc func(*http.Request) (*http.Response, error)

func (f appRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAppTokenSourceMintsAndRefreshesInstallationTokens(t *testing.T) {
	key, pemStr := generateTestRSAKeyPEM(t, false)
	if !IsPrivateKeyPEM(pemStr) {
		t.Fatal("expected IsPrivateKeyPEM to be true for RSA PRIVATE KEY")
	}
	if _, pkcs8PEM := generateTestRSAKeyPEM(t, true); !IsPrivateKeyPEM(pkcs8PEM) {
		t.Fatal("expected IsPrivateKeyPEM to be true for PRIVATE KEY")
	} else if _, err := ParsePrivateKeyPEM(pkcs8PEM); err != nil {
		t.Fatalf("ParsePrivateKeyPEM(PKCS8) = %v", err)
	}

	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	var mintCalls atomic.Int32
	var commentCalls atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/987654/access_tokens":
			n := mintCalls.Add(1)
			auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			parts := strings.Split(auth, ".")
			if len(parts) != 3 {
				t.Errorf("invalid JWT format: %q", auth)
				http.Error(w, "bad jwt", http.StatusUnauthorized)
				return
			}
			sig, err := base64.RawURLEncoding.DecodeString(parts[2])
			if err != nil {
				t.Errorf("decode jwt sig: %v", err)
				http.Error(w, "bad sig", http.StatusUnauthorized)
				return
			}
			digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
				t.Errorf("verify RS256 signature: %v", err)
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
			payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
			if err != nil {
				t.Errorf("decode jwt claims: %v", err)
				http.Error(w, "bad claims", http.StatusUnauthorized)
				return
			}
			var claims struct {
				Iss string `json:"iss"`
				Iat int64  `json:"iat"`
				Exp int64  `json:"exp"`
			}
			if err := json.Unmarshal(payloadBytes, &claims); err != nil || claims.Iss != "12345" {
				t.Errorf("unexpected jwt claims: %+v (err=%v)", claims, err)
				http.Error(w, "bad iss", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      "ghs_install_token_" + string(rune('0'+n)),
				"expires_at": now.Add(time.Hour).Format(time.RFC3339),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/issues/42/comments":
			c := commentCalls.Add(1)
			wantAuth := "Bearer ghs_install_token_1"
			if c == 3 {
				wantAuth = "Bearer ghs_install_token_2"
			}
			if got := r.Header.Get("Authorization"); got != wantAuth {
				t.Errorf("comment call %d Authorization = %q, want %q", c, got, wantAuth)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":       int64(100 + c),
				"html_url": "https://github.com/acme/widgets/issues/42#issuecomment-101",
			})
		default:
			http.NotFound(w, r)
		}
	})

	httpClient := &http.Client{
		Transport: appRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			return rec.Result(), nil
		}),
	}

	api, err := NewAppHTTPAPIAt("https://api.github.example", AppConfig{
		AppID:          "12345",
		InstallationID: "987654",
		PrivateKey:     pemStr,
		HTTPClient:     httpClient,
		Clock:          func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewAppHTTPAPIAt = %v", err)
	}

	ref := Ref{Owner: "acme", Repo: "widgets", Number: 42}
	if id, err := api.CreateComment(context.Background(), ref, "First comment"); err != nil || id != 101 {
		t.Fatalf("first CreateComment = %d, %v", id, err)
	}
	// Second comment 30 minutes later should reuse cached token.
	now = now.Add(30 * time.Minute)
	if id, err := api.CreateComment(context.Background(), ref, "Second comment"); err != nil || id != 102 {
		t.Fatalf("second CreateComment = %d, %v", id, err)
	}
	if got := mintCalls.Load(); got != 1 {
		t.Fatalf("mintCalls after cached call = %d, want 1", got)
	}
	// Advance clock to 59 minutes after initial mint (within 2-minute refresh window).
	now = now.Add(29 * time.Minute)
	if id, err := api.CreateComment(context.Background(), ref, "Third comment"); err != nil || id != 103 {
		t.Fatalf("third CreateComment = %d, %v", id, err)
	}
	if got := mintCalls.Load(); got != 2 {
		t.Fatalf("mintCalls after expiry window = %d, want 2", got)
	}
}
