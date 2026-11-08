// Package githubinstall verifies native GitHub App installations. OAuth tokens
// are used only to prove the installing person's access and are never persisted.
package githubinstall

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/credref"
	githubapi "github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/state"
)

const namespace = "github_installations"
const lifetime = 10 * time.Minute

// Config is deployment configuration. RedirectURL must be the app's first
// callback URL, with user authorization during installation enabled in GitHub.
// The private key and webhook secret stay at their deployment references when
// a verified installation becomes a client. HTTP and endpoint overrides are
// trusted embedding configuration, never callback inputs.
type Config struct {
	AppID, AppSlug, ClientID, ClientSecret string
	PrivateKey, SigningSecret              string
	PrivateKeyRef, SigningSecretRef        string
	RedirectURL                            string
	GitHubURL, APIURL                      string
	HTTP                                   *http.Client
	Now                                    func() time.Time
}

// Installation is verified metadata, never a credential or a grant to agents.
type Installation struct {
	ID           string `json:"id"`
	AppID        string `json:"app_id"`
	AppSlug      string `json:"app_slug"`
	AccountID    string `json:"account_id"`
	AccountLogin string `json:"account_login"`
	UserID       string `json:"user_id"`
}

type pending struct {
	Hash    string        `json:"hash"`
	Binding string        `json:"binding"`
	Expires time.Time     `json:"expires"`
	Claimed bool          `json:"claimed"`
	Result  *Installation `json:"result,omitempty"`
}

// Manager has no local authorization cache. Every state transition uses the
// shared store, so restarts and concurrent callbacks retain the same boundary.
type Manager struct {
	store   state.Store
	cfg     Config
	key     *rsa.PrivateKey
	client  *http.Client
	now     func() time.Time
	binding string
}

var slug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,99}$`)

func New(st state.Store, cfg Config) (*Manager, error) {
	if st == nil || !number(cfg.AppID) || !slug.MatchString(cfg.AppSlug) || cfg.ClientID == "" || len(cfg.ClientID) > 200 || cfg.ClientSecret == "" || cfg.SigningSecret == "" || !safeURL(cfg.RedirectURL, false) {
		return nil, fmt.Errorf("%w: GitHub installation configuration is incomplete", connectionauth.ErrUnavailable)
	}
	for _, ref := range []string{cfg.PrivateKeyRef, cfg.SigningSecretRef} {
		if _, err := credref.Parse(ref); err != nil {
			return nil, fmt.Errorf("%w: GitHub credentials must use deployment references", connectionauth.ErrInvalid)
		}
	}
	key, err := githubapi.ParsePrivateKeyPEM(cfg.PrivateKey)
	if err != nil || key.N.BitLen() < 2048 {
		return nil, fmt.Errorf("%w: GitHub requires an RSA private key of at least 2048 bits", connectionauth.ErrInvalid)
	}
	if cfg.GitHubURL == "" {
		cfg.GitHubURL = "https://github.com"
	}
	if cfg.APIURL == "" {
		cfg.APIURL = "https://api.github.com"
	}
	if !safeURL(cfg.GitHubURL, true) || !safeURL(cfg.APIURL, true) {
		return nil, fmt.Errorf("%w: GitHub endpoints must use HTTPS or loopback HTTP", connectionauth.ErrInvalid)
	}
	cfg.GitHubURL, cfg.APIURL = strings.TrimRight(cfg.GitHubURL, "/"), strings.TrimRight(cfg.APIURL, "/")
	client := &http.Client{}
	if cfg.HTTP != nil {
		copy := *cfg.HTTP
		client = &copy
	}
	client.Timeout = 15 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	binding := hash(strings.Join([]string{cfg.AppID, cfg.AppSlug, cfg.ClientID, cfg.RedirectURL, cfg.GitHubURL, cfg.APIURL}, "\x00"))
	return &Manager{store: st, cfg: cfg, key: key, client: client, now: now, binding: binding}, nil
}

func number(s string) bool {
	n, err := strconv.ParseInt(s, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == s
}

func safeURL(raw string, base bool) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (!base || u.Path == "" || u.Path == "/" || strings.HasSuffix(u.Path, "/api/v3")) && (u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")))
}

func identity(tenant, person string) bool {
	return tenant != "" && person != "" && len(tenant) <= 200 && len(person) <= 200 && !strings.ContainsRune(tenant+person, 0)
}

func hash(s string) string {
	digest := sha256.Sum256([]byte(s))
	return hex.EncodeToString(digest[:])
}

func (m *Manager) Available() bool { return m != nil }

// Cancel revokes this person's pending or completed callback capability.
func (m *Manager) Cancel(ctx context.Context, tenant, person string) error {
	if m == nil {
		return connectionauth.ErrUnavailable
	}
	if !identity(tenant, person) {
		return connectionauth.ErrInvalid
	}
	// Keep a tombstone so a later Begin cannot reuse an earlier CAS version
	// while that earlier callback is still verifying with the provider.
	return m.store.Write(ctx, namespace, state.Put(tenant, hash(person), []byte(`{}`)))
}

// Begin replaces the one pending attempt for this actor and tenant. The nonce
// is unpredictable and only its digest is stored; starting again revokes it.
func (m *Manager) Begin(ctx context.Context, tenant, person string) (connectionauth.Authorization, error) {
	if m == nil {
		return connectionauth.Authorization{}, connectionauth.ErrUnavailable
	}
	if !identity(tenant, person) {
		return connectionauth.Authorization{}, connectionauth.ErrInvalid
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return connectionauth.Authorization{}, err
	}
	nonce := base64.RawURLEncoding.EncodeToString(random)
	p := pending{Hash: hash(nonce), Binding: m.binding, Expires: m.now().UTC().Add(lifetime)}
	raw, err := json.Marshal(p)
	if err != nil {
		return connectionauth.Authorization{}, err
	}
	if err := m.store.Write(ctx, namespace, state.Put(tenant, hash(person), raw)); err != nil {
		return connectionauth.Authorization{}, err
	}
	u := m.cfg.GitHubURL + "/apps/" + m.cfg.AppSlug + "/installations/new?" + url.Values{"state": {nonce}}.Encode()
	return connectionauth.Authorization{Provider: "github", URL: u, ExpiresAt: p.Expires}, nil
}

// Complete first consumes the actor-bound state, then independently proves
// user access and app ownership. A verified retry returns the same metadata
// without exchanging its one-use code again, including after a process restart.
func (m *Manager) Complete(ctx context.Context, tenant, person, nonce, code, installationID string) (Installation, error) {
	if m == nil {
		return Installation{}, connectionauth.ErrUnavailable
	}
	if !identity(tenant, person) || len(nonce) != 43 || code == "" || len(code) > 4096 || !number(installationID) {
		return Installation{}, connectionauth.ErrInvalid
	}
	rec, err := m.store.Lookup(ctx, namespace, tenant, hash(person))
	if errors.Is(err, state.ErrNotFound) {
		return Installation{}, connectionauth.ErrInvalid
	}
	if err != nil {
		return Installation{}, err
	}
	var p pending
	if json.Unmarshal(rec.Value, &p) != nil || p.Hash != hash(nonce) || p.Binding != m.binding || !m.now().Before(p.Expires) {
		return Installation{}, connectionauth.ErrInvalid
	}
	if p.Result != nil {
		if p.Result.ID != installationID {
			return Installation{}, connectionauth.ErrInvalid
		}
		return *p.Result, nil
	}
	if p.Claimed {
		return Installation{}, connectionauth.ErrBusy
	}
	p.Claimed = true
	raw, _ := json.Marshal(p)
	if err := m.store.Write(ctx, namespace, state.PutAt(tenant, rec.Key, raw, rec.Version)); err != nil {
		if errors.Is(err, state.ErrConflict) {
			return Installation{}, connectionauth.ErrBusy
		}
		return Installation{}, err
	}
	verified, err := m.verify(ctx, code, installationID)
	if err != nil {
		// A failed exchange may already have consumed the code. A fresh Begin
		// is required instead of allowing another replica to replay it.
		return Installation{}, err
	}
	if !m.now().Before(p.Expires) {
		return Installation{}, connectionauth.ErrInvalid
	}
	p.Result = &verified
	raw, _ = json.Marshal(p)
	if err := m.store.Write(ctx, namespace, state.PutAt(tenant, rec.Key, raw, rec.Version+1)); err != nil {
		if errors.Is(err, state.ErrConflict) {
			return Installation{}, connectionauth.ErrInvalid
		}
		return Installation{}, err
	}
	return verified, nil
}
