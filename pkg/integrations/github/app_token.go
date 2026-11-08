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
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	transport "github.com/urmzd/saige/flow/connectors/github"
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
	// AuthRevision snapshots host consent before a mint attempt. Only a
	// confirmed HTTP 401 reports it back; network and permission failures do not.
	AuthRevision   func(context.Context) string
	OnAuthRejected func(context.Context, string)
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
	origin         *url.URL
	appID          string
	installationID string
	key            *rsa.PrivateKey
	httpClient     *http.Client
	clock          func() time.Time
	authRevision   func(context.Context) string
	onAuthRejected func(context.Context, string)

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// NewAppTokenSource validates cfg and returns a caching installation token source.
func NewAppTokenSource(base string, cfg AppConfig) (*AppTokenSource, error) {
	if strings.TrimSpace(cfg.AppID) == "" || len(cfg.AppID) > 200 || strings.ContainsAny(cfg.AppID, "\r\n") {
		return nil, fmt.Errorf("github: app authentication needs an app_id")
	}
	installation, err := strconv.ParseInt(cfg.InstallationID, 10, 64)
	if err != nil || installation <= 0 || strconv.FormatInt(installation, 10) != cfg.InstallationID {
		return nil, fmt.Errorf("github: app authentication needs a numeric installation_id")
	}
	key, err := ParsePrivateKeyPEM(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("github: app authentication requires a valid RSA private key")
	}
	if strings.TrimSpace(base) == "" {
		base = transport.GitHubBaseURL
	}
	origin, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.RawPath != "" || (origin.Path != "" && path.Clean(origin.Path) != origin.Path) || (origin.Scheme != "https" && (origin.Scheme != "http" || (origin.Hostname() != "127.0.0.1" && origin.Hostname() != "localhost" && origin.Hostname() != "::1"))) {
		return nil, fmt.Errorf("github: app API base must be HTTPS or loopback HTTP without credentials, query or fragment")
	}
	client := &http.Client{}
	if cfg.HTTPClient != nil {
		copy := *cfg.HTTPClient
		client = &copy
	}
	if client.Timeout <= 0 || client.Timeout > 30*time.Second {
		client.Timeout = 30 * time.Second
	}
	client.CheckRedirect = refuseAppRedirect
	return &AppTokenSource{
		base:           origin.String(),
		origin:         origin,
		appID:          strings.TrimSpace(cfg.AppID),
		installationID: strings.TrimSpace(cfg.InstallationID),
		key:            key,
		httpClient:     client,
		clock:          cfg.Clock,
		authRevision:   cfg.AuthRevision,
		onAuthRejected: cfg.OnAuthRejected,
	}, nil
}

func refuseAppRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// owns fences credentials before even minting a token. Escaped or cleaned-up
// paths cannot escape an enterprise API prefix or smuggle another authority.
func (s *AppTokenSource) owns(u *url.URL) bool {
	return u != nil && u.User == nil && u.Fragment == "" && u.Scheme == s.origin.Scheme && strings.EqualFold(u.Host, s.origin.Host) && path.Clean(u.Path) == u.Path && strings.HasPrefix(u.Path, s.origin.Path+"/")
}

// Token returns a valid installation access token, minting a new one when the
// cached token is missing or within 2 minutes of expiring.
func (s *AppTokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	rejected, revision := false, ""
	defer func() {
		s.mu.Unlock()
		if rejected && revision != "" && s.onAuthRejected != nil {
			s.onAuthRejected(ctx, revision)
		}
	}()

	now := time.Now().UTC()
	if s.clock != nil {
		now = s.clock().UTC()
	}
	if s.token != "" && now.Add(2*time.Minute).Before(s.expiresAt) {
		return s.token, nil
	}
	if s.authRevision != nil {
		revision = s.authRevision(ctx)
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
	resp, err := s.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("github: installation token request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rejected = resp.StatusCode == http.StatusUnauthorized
		return "", fmt.Errorf("github: installation token request returned status %d", resp.StatusCode)
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	const maxResponse = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(raw) > maxResponse || json.Unmarshal(raw, &out) != nil {
		return "", fmt.Errorf("github: installation token response was invalid")
	}
	if out.Token == "" || len(out.Token) > 4096 || strings.ContainsAny(out.Token, "\r\n\t ") {
		return "", fmt.Errorf("github: installation token response carried no valid token")
	}
	expiresAt := out.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = now.Add(55 * time.Minute)
	}
	if !expiresAt.After(now) {
		return "", fmt.Errorf("github: installation token response was already expired")
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
	if !rt.source.owns(req.URL) {
		return nil, fmt.Errorf("github: installation credentials are restricted to the configured API origin and path")
	}
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
	resp, err := base.RoundTrip(cloned)
	if err != nil {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		return nil, fmt.Errorf("github: installation API request failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// HTTPAPI includes provider response bodies in its errors. Keep the
		// status while preventing reflected tokens or private content escaping.
		_ = resp.Body.Close()
		body := `{"message":"GitHub installation request failed"}`
		resp.Body = io.NopCloser(strings.NewReader(body))
		resp.ContentLength = int64(len(body))
	}
	return resp, nil
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
	timeout := source.httpClient.Timeout
	if cfg.HTTPClient != nil {
		baseTransport = cfg.HTTPClient.Transport
	}
	authed := &http.Client{
		Timeout:       timeout,
		Transport:     &appRoundTripper{base: baseTransport, source: source},
		CheckRedirect: refuseAppRedirect,
	}
	base = source.base
	return &HTTPAPI{HTTPAPI: transport.NewHTTPAPIAt(base, "", authed), base: base, http: authed}, nil
}
