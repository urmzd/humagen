package githubinstall

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
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/state"
)

type providerFixture struct {
	cfg       Config
	key       *rsa.PrivateKey
	calls     atomic.Int64
	exchanges atomic.Int64
	appChecks atomic.Int64
	entered   chan struct{}
	requests  chan struct{}
	release   chan struct{}
	once      sync.Once
}

func fixture(t *testing.T, mode string) *providerFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &providerFixture{key: key, entered: make(chan struct{}), requests: make(chan struct{}, 4), release: make(chan struct{})}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		item := installation{ID: 42, AppID: 123, AppSlug: "humagen-app", Account: account{ID: 9, Login: "octo-org"}}
		switch r.URL.Path {
		case "/login/oauth/access_token":
			f.exchanges.Add(1)
			if r.Method != http.MethodPost || r.ParseForm() != nil || r.Form.Get("client_id") != f.cfg.ClientID || r.Form.Get("client_secret") != f.cfg.ClientSecret || r.Form.Get("code") != "one-use-code" || r.Form.Get("redirect_uri") != f.cfg.RedirectURL {
				t.Error("code was not exchanged using the registered app and callback")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if mode == "blocked" {
				f.requests <- struct{}{}
				f.once.Do(func() { close(f.entered) })
				<-f.release
			}
			if mode == "redirect" {
				w.Header().Set("Location", "/must-not-follow")
				w.WriteHeader(http.StatusFound)
				return
			}
			if mode == "failed" {
				_, _ = w.Write([]byte(`{"error":"bad_code","error_description":"private-provider-response"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "ghu_user_secret", "token_type": "bearer", "refresh_token": "ghr_refresh_secret"})
		case "/user":
			if r.Header.Get("Authorization") != "Bearer ghu_user_secret" {
				t.Error("user identity needs the exchanged user token")
			}
			_ = json.NewEncoder(w).Encode(account{ID: 5, Login: "octocat"})
		case "/user/installations":
			if r.Header.Get("Authorization") != "Bearer ghu_user_secret" || r.URL.Query().Get("per_page") != "100" {
				t.Error("user installation lookup must be authenticated and bounded")
			}
			items := []installation{item}
			if mode == "unauthorized" {
				items = nil
			}
			if mode == "unbounded" {
				item.ID = 99
				items = make([]installation, 100)
				for i := range items {
					items[i] = item
				}
				w.Header().Set("Link", `<https://attacker.invalid/steal>; rel="next"`)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"installations": items})
		case "/app/installations/42":
			f.appChecks.Add(1)
			parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
			if len(parts) != 3 {
				t.Error("app installation lookup requires an app JWT")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
			if rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig) != nil {
				t.Error("app JWT signature did not verify")
			}
			claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
			var payload struct {
				Iss string `json:"iss"`
				Exp int64  `json:"exp"`
			}
			_ = json.Unmarshal(claims, &payload)
			if payload.Iss != "123" || payload.Exp <= f.cfg.Now().Unix() || payload.Exp > f.cfg.Now().Add(10*time.Minute).Unix() {
				t.Error("app JWT is not bound to the configured app and bounded lifetime")
			}
			switch mode {
			case "other-app":
				item.AppID = 777
			case "other-account":
				item.Account.ID = 777
			case "other-slug":
				item.AppSlug = "attacker-app"
			case "suspended":
				now := f.cfg.Now()
				item.SuspendedAt = &now
			}
			_ = json.NewEncoder(w).Encode(item)
		default:
			t.Errorf("unexpected provider request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	f.cfg = Config{AppID: "123", AppSlug: "humagen-app", ClientID: "Iv.client", ClientSecret: "client-secret", PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), SigningSecret: "webhook-secret", PrivateKeyRef: "env:HUMAGEN_GITHUB_PRIVATE_KEY", SigningSecretRef: "env:HUMAGEN_GITHUB_WEBHOOK_SECRET", RedirectURL: "https://humagen.example/connections/callback", GitHubURL: server.URL, APIURL: server.URL, HTTP: server.Client(), Now: func() time.Time { return time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC) }}
	return f
}

func begin(t *testing.T, m *Manager) string {
	t.Helper()
	a, err := m.Begin(context.Background(), "acme", "alice")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(a.URL)
	if err != nil || u.Path != "/apps/humagen-app/installations/new" || a.Provider != "github" || len(u.Query().Get("state")) != 43 {
		t.Fatalf("invalid install redirect: %+v", a)
	}
	return u.Query().Get("state")
}

func TestInstallationVerificationSurvivesRestartWithoutPersistingCredentials(t *testing.T) {
	f := fixture(t, "")
	store := state.NewMemory()
	m, err := New(store, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	nonce := begin(t, m)
	m, err = New(store, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Complete(context.Background(), "acme", "alice", nonce, "one-use-code", "42")
	if err != nil || got.ID != "42" || got.AccountLogin != "octo-org" || got.UserID != "5" || got.AppID != "123" || f.appChecks.Load() != 1 {
		t.Fatalf("verified=%+v err=%v", got, err)
	}
	restarted, err := New(store, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	again, err := restarted.Complete(context.Background(), "acme", "alice", nonce, "one-use-code", "42")
	if err != nil || again != got || f.exchanges.Load() != 1 {
		t.Fatalf("idempotent completion=%+v err=%v exchanges=%d", again, err, f.exchanges.Load())
	}
	records, err := store.Load(context.Background(), namespace)
	if err != nil || len(records) != 1 {
		t.Fatalf("pending records=%d err=%v", len(records), err)
	}
	for _, sensitive := range []string{nonce, "one-use-code", "ghu_user_secret", "ghr_refresh_secret", f.cfg.ClientSecret, f.cfg.PrivateKey, f.cfg.SigningSecret} {
		if strings.Contains(string(records[0].Value), sensitive) {
			t.Fatal("stored installation contains authorization material")
		}
	}
}

func TestInstallationCallbackStateDeniesOtherActorsTenantsExpiryAndReplacement(t *testing.T) {
	f := fixture(t, "")
	now := f.cfg.Now()
	f.cfg.Now = func() time.Time { return now }
	m, err := New(state.NewMemory(), f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	nonce := begin(t, m)
	for _, tc := range []struct{ tenant, person, nonce, id string }{
		{"acme", "mallory", nonce, "42"}, {"other", "alice", nonce, "42"}, {"acme", "alice", strings.Repeat("x", 43), "42"}, {"acme", "alice", nonce, "42/../../user"},
	} {
		if _, err := m.Complete(context.Background(), tc.tenant, tc.person, tc.nonce, "one-use-code", tc.id); !errors.Is(err, connectionauth.ErrInvalid) {
			t.Fatalf("callback unexpectedly accepted: %+v err=%v", tc, err)
		}
	}
	newNonce := begin(t, m)
	if _, err := m.Complete(context.Background(), "acme", "alice", nonce, "one-use-code", "42"); !errors.Is(err, connectionauth.ErrInvalid) {
		t.Fatalf("replaced state accepted: %v", err)
	}
	now = now.Add(lifetime)
	if _, err := m.Complete(context.Background(), "acme", "alice", newNonce, "one-use-code", "42"); !errors.Is(err, connectionauth.ErrInvalid) {
		t.Fatalf("expired state accepted: %v", err)
	}
	if f.calls.Load() != 0 {
		t.Fatal("untrusted callback contacted GitHub")
	}
}

func TestInstallationCallbackClaimsOnceAcrossReplicas(t *testing.T) {
	f := fixture(t, "blocked")
	store := state.NewMemory()
	a, err := New(store, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(store, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	nonce := begin(t, a)
	done := make(chan error, 1)
	go func() {
		_, err := a.Complete(context.Background(), "acme", "alice", nonce, "one-use-code", "42")
		done <- err
	}()
	select {
	case <-f.entered:
	case <-time.After(5 * time.Second):
		close(f.release)
		t.Fatal("first exchange did not start")
	}
	_, second := b.Complete(context.Background(), "acme", "alice", nonce, "one-use-code", "42")
	close(f.release)
	first := <-done
	if first != nil || !errors.Is(second, connectionauth.ErrBusy) || f.exchanges.Load() != 1 {
		t.Fatalf("first=%v second=%v exchanges=%d", first, second, f.exchanges.Load())
	}
}

func TestInstallationDeniesUnverifiedOrSuspendedInstallationAndBoundsProviderRequests(t *testing.T) {
	for _, mode := range []string{"unauthorized", "other-app", "other-account", "other-slug", "suspended", "failed", "redirect", "unbounded"} {
		t.Run(mode, func(t *testing.T) {
			f := fixture(t, mode)
			m, err := New(state.NewMemory(), f.cfg)
			if err != nil {
				t.Fatal(err)
			}
			nonce := begin(t, m)
			_, err = m.Complete(context.Background(), "acme", "alice", nonce, "one-use-code", "42")
			if !errors.Is(err, connectionauth.ErrInvalid) || strings.Contains(err.Error(), "private-provider-response") {
				t.Fatalf("verification=%v", err)
			}
			if mode == "unauthorized" && f.appChecks.Load() != 0 {
				t.Fatal("app credentials were used before user access was established")
			}
			if mode == "unbounded" && f.calls.Load() != 12 {
				t.Fatalf("provider query not bounded at ten pages: %d", f.calls.Load())
			}
			if _, err := m.Complete(context.Background(), "acme", "alice", nonce, "one-use-code", "42"); !errors.Is(err, connectionauth.ErrBusy) || f.exchanges.Load() != 1 {
				t.Fatalf("failed exchange replayed: %v count=%d", err, f.exchanges.Load())
			}
		})
	}
}

func TestInstallationNewBeginRevokesAnInflightCallback(t *testing.T) {
	f := fixture(t, "blocked")
	m, err := New(state.NewMemory(), f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	nonce := begin(t, m)
	done := make(chan error, 1)
	go func() {
		_, err := m.Complete(context.Background(), "acme", "alice", nonce, "one-use-code", "42")
		done <- err
	}()
	select {
	case <-f.entered:
	case <-time.After(5 * time.Second):
		close(f.release)
		t.Fatal("first exchange did not start")
	}
	_ = begin(t, m)
	close(f.release)
	if err := <-done; !errors.Is(err, connectionauth.ErrInvalid) {
		t.Fatalf("revoked callback published installation: %v", err)
	}
}

func TestInstallationCancelCannotReuseInflightClaimVersion(t *testing.T) {
	f := fixture(t, "blocked")
	m, err := New(state.NewMemory(), f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var release sync.Once
	unblock := func() { release.Do(func() { close(f.release) }) }
	defer unblock()
	complete := func(nonce string) <-chan error {
		done := make(chan error, 1)
		go func() { _, err := m.Complete(ctx, "acme", "alice", nonce, "one-use-code", "42"); done <- err }()
		select {
		case <-f.requests:
		case <-ctx.Done():
			t.Fatal("provider exchange did not start")
		}
		return done
	}
	old := complete(begin(t, m))
	if err := m.Cancel(ctx, "acme", "alice"); err != nil {
		t.Fatal(err)
	}
	current := complete(begin(t, m))
	unblock()
	if err := <-old; !errors.Is(err, connectionauth.ErrInvalid) {
		t.Fatalf("canceled claim reused the next authorization's store version: %v", err)
	}
	if err := <-current; err != nil {
		t.Fatalf("new authorization did not survive canceled predecessor: %v", err)
	}
}
