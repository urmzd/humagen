package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	transport "github.com/urmzd/orchagent/flow/connectors/github"
)

// AppConfig configures a GitHub App installation token source.
//
// Instead of a static personal access token, a GitHub App signs a short-lived
// RS256 JWT with its RSA private key and exchanges it at
// POST /app/installations/{installation_id}/access_tokens for a 1-hour
// installation token, refreshing automatically before expiry.
type AppConfig struct {
	AppID          string
	InstallationID string
	PrivateKey     string
	HTTPClient     *http.Client
	Clock          func() time.Time
}

// IsPrivateKeyPEM reports whether s looks like a PEM-encoded private key
// rather than a static token.
func IsPrivateKeyPEM(s string) bool {
	trimmed := strings.TrimSpace(s)
	return strings.HasPrefix(trimmed, "-----BEGIN ") && strings.Contains(trimmed, "PRIVATE KEY-----")
}

// ParsePrivateKeyPEM parses a PKCS#1 or PKCS#8 PEM-encoded RSA private key.
func ParsePrivateKeyPEM(raw string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(raw)))
	if block == nil {
		return nil, fmt.Errorf("github: private key is not valid PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("github: private key must be RSA PKCS#1 or PKCS#8: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("github: private key is not an RSA key")
	}
	return key, nil
}

// SignAppJWT signs a short-lived RS256 JWT for a GitHub App.
func SignAppJWT(appID string, key *rsa.PrivateKey, now time.Time) (string, error) {
	if strings.TrimSpace(appID) == "" {
		return "", fmt.Errorf("github: app JWT needs an app ID")
	}
	if key == nil {
		return "", fmt.Errorf("github: app JWT needs an RSA private key")
	}
	headerJSON, err := json.Marshal(map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	})
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": appID,
	})
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("github: sign app JWT: %w", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// AppTokenSource mints and caches GitHub App installation access tokens.
type AppTokenSource struct {
	base           string
	appID          string
	installationID string
	key            *rsa.PrivateKey
	httpClient     *http.Client
	clock          func() time.Time

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// NewAppTokenSource validates cfg and returns a caching installation token source.
func NewAppTokenSource(base string, cfg AppConfig) (*AppTokenSource, error) {
	if strings.TrimSpace(cfg.AppID) == "" {
		return nil, fmt.Errorf("github: app authentication needs an app_id")
	}
	if strings.TrimSpace(cfg.InstallationID) == "" {
		return nil, fmt.Errorf("github: app authentication needs an installation_id")
	}
	key, err := ParsePrivateKeyPEM(cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(base) == "" {
		base = transport.GitHubBaseURL
	}
	return &AppTokenSource{
		base:           strings.TrimRight(base, "/"),
		appID:          strings.TrimSpace(cfg.AppID),
		installationID: strings.TrimSpace(cfg.InstallationID),
		key:            key,
		httpClient:     cfg.HTTPClient,
		clock:          cfg.Clock,
	}, nil
}

// Token returns a valid installation access token, minting a new one when the
// cached token is missing or within 2 minutes of expiring.
func (s *AppTokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if s.clock != nil {
		now = s.clock().UTC()
	}
	if s.token != "" && now.Add(2*time.Minute).Before(s.expiresAt) {
		return s.token, nil
	}

	jwtToken, err := SignAppJWT(s.appID, s.key, now)
	if err != nil {
		return "", err
	}

	path := fmt.Sprintf("%s/app/installations/%s/access_tokens", s.base, url.PathEscape(s.installationID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, bytes.NewReader(nil))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: mint installation token for installation %s: %w", s.installationID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("github: mint installation token for installation %s: status %d: %s", s.installationID, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("github: mint installation token for installation %s: %w", s.installationID, err)
	}
	if out.Token == "" {
		return "", fmt.Errorf("github: installation token response for %s carried no token", s.installationID)
	}
	expiresAt := out.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = now.Add(55 * time.Minute)
	}
	s.token = out.Token
	s.expiresAt = expiresAt
	return s.token, nil
}

type appRoundTripper struct {
	base   http.RoundTripper
	source *AppTokenSource
}

func (rt *appRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := rt.source.Token(req.Context())
	if err != nil {
		return nil, err
	}
	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", "Bearer "+token)
	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(cloned)
}

// NewAppHTTPAPI returns an HTTPAPI that authenticates as a GitHub App
// installation against GitHubBaseURL.
func NewAppHTTPAPI(cfg AppConfig) (*HTTPAPI, error) {
	return NewAppHTTPAPIAt(transport.GitHubBaseURL, cfg)
}

// NewAppHTTPAPIAt returns an HTTPAPI that authenticates as a GitHub App
// installation against base. It is orchagent's HTTPAPI with a transport that
// replaces the Authorization header with a current installation token.
func NewAppHTTPAPIAt(base string, cfg AppConfig) (*HTTPAPI, error) {
	source, err := NewAppTokenSource(base, cfg)
	if err != nil {
		return nil, err
	}
	var baseTransport http.RoundTripper
	timeout := 30 * time.Second
	if cfg.HTTPClient != nil {
		baseTransport = cfg.HTTPClient.Transport
		if cfg.HTTPClient.Timeout > 0 {
			timeout = cfg.HTTPClient.Timeout
		}
	}
	authed := &http.Client{
		Timeout:   timeout,
		Transport: &appRoundTripper{base: baseTransport, source: source},
	}
	base = strings.TrimRight(base, "/")
	return &HTTPAPI{HTTPAPI: transport.NewHTTPAPIAt(base, "", authed), base: base, http: authed}, nil
}
