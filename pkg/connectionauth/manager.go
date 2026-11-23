// Package connectionauth connects HTTP services through OAuth without exposing
// credentials to callers. State, client secrets and tokens are encrypted at rest.
package connectionauth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"golang.org/x/oauth2"
)

var (
	ErrUnavailable = errors.New("connection authorization is not configured")
	ErrInvalid     = errors.New("invalid connection authorization")
	ErrReconnect   = errors.New("reconnect this service to continue")
	// ErrConsentEnded reports a browser-bound consent that a callback which
	// did not match it ended: it completes nothing afterwards.
	ErrConsentEnded = fmt.Errorf("%w: this sign-in was ended; start again", ErrInvalid)
	ErrRejected     = errors.New("connection authorization was rejected")
	ErrBusy         = errors.New("connection refresh is in progress; try again")
)

// Provider is trusted deployment configuration, never taken from callback input.
// A pre-registered client is bound to Issuer; discovery must agree with it.
type Provider struct {
	Name         string
	Resource     string
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
	// Native endpoints are fixed deployment configuration. They do not use
	// MCP discovery or RFC 8707 resource parameters.
	AuthorizationURL string
	TokenURL         string
	Offline          bool
	SigningSecret    string
	// Prompt is the authorization request's prompt parameter, for a
	// provider that takes one: GitHub's select_account shows the account
	// choice even where the application was authorized before.
	Prompt string
}

// Config contains the encryption key and the one registered callback URL.
// Key must have 32 bytes and be kept outside the state database. HTTP, when set,
// must enforce the deployment's network policy; redirects are always refused.
type Config struct {
	Key         []byte
	RedirectURL string
	Providers   []Provider
	HTTP        *http.Client
	Now         func() time.Time
	// OnAuthFailure receives only an authenticated consenting actor and opaque provider.
	// It runs after releasing credential refresh leases.
	OnAuthFailure   func(context.Context, string, string, string) error
	OnAuthRecovered func(context.Context, string, string, string) error
}

type Manager struct {
	records   *state.Table[string]
	aead      cipher.AEAD
	cfg       Config
	providers map[string]Provider
	client    *http.Client
	now       func() time.Time
}

type transaction struct {
	Grant        string
	Native       bool
	Owner        string
	Provider     string
	Person       string
	Resource     string
	Issuer       string
	ClientID     string
	ClientSecret string
	TokenURL     string
	AuthStyle    oauth2.AuthStyle
	Verifier     string
	Expires      time.Time
	// Binding is a digest of a secret the browser that began the consent
	// holds (BeginBound). Its callback must present the same secret.
	Binding string `json:",omitempty"`
}

type account struct {
	Transaction   transaction
	Token         oauth2.Token
	LeaseUntil    time.Time
	Details       map[string]string
	GrantedScopes []string
	ScopesKnown   bool
	Rejected      bool
}

// Authorization is a redirect, not a credential. The caller opens URL only
// after its person explicitly chooses Connect.
type Authorization struct {
	Provider  string    `json:"provider,omitempty"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Status contains no token, secret, verifier, or authorization code.
type Status struct {
	ReconnectRequired bool       `json:"reconnect_required,omitempty"`
	Connected         bool       `json:"connected"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	GrantedScopes     *[]string  `json:"granted_scopes,omitempty"`
}

func New(st state.Store, cfg Config) (*Manager, error) {
	if len(cfg.Key) != 32 || cfg.RedirectURL == "" {
		return nil, ErrUnavailable
	}
	u, err := url.Parse(cfg.RedirectURL)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || (u.Scheme != "https" && (u.Scheme != "http" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1"))) {
		return nil, fmt.Errorf("%w: callback must be HTTPS or loopback HTTP", ErrInvalid)
	}
	block, err := aes.NewCipher(cfg.Key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if st == nil {
		st = state.NewMemory()
	}
	client := publicClient()
	if cfg.HTTP != nil {
		copy := *cfg.HTTP
		client = &copy
	}
	client.Timeout = 15 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	m := &Manager{records: state.NewTable[string](st, "connection_oauth"), aead: aead, cfg: cfg, providers: map[string]Provider{}, client: client, now: cfg.Now}
	if m.now == nil {
		m.now = time.Now
	}
	for _, p := range cfg.Providers {
		if p.Name == "" || len(p.Name) > 63 || !safeURL(p.Resource) || strings.Contains(p.Name, "/") || (p.ClientID != "" && !safeURL(p.Issuer)) {
			return nil, fmt.Errorf("%w: invalid provider configuration", ErrInvalid)
		}
		if _, exists := m.providers[p.Name]; exists {
			return nil, fmt.Errorf("%w: repeated provider", ErrInvalid)
		}
		if (p.AuthorizationURL != "" || p.TokenURL != "") && (p.ClientID == "" || !safeURL(p.AuthorizationURL) || !safeURL(p.TokenURL)) {
			return nil, fmt.Errorf("%w: native provider requires registered endpoints and client", ErrInvalid)
		}
		m.providers[p.Name] = p
	}
	return m, nil
}

func (m *Manager) Available(provider string) bool {
	if m == nil {
		return false
	}
	provider = strings.TrimPrefix(provider, "workspace-")
	_, ok := m.providers[provider]
	return ok || provider == "other" || strings.HasPrefix(provider, "other-")
}
func ownerPrefix(kind, owner string) string {
	sum := sha256.Sum256([]byte(owner))
	return kind + "/" + hex.EncodeToString(sum[:]) + "/"
}
func recordKey(kind, person, name string) string {
	sum := sha256.Sum256([]byte(name))
	return ownerPrefix(kind, person) + hex.EncodeToString(sum[:])
}
func identity(tenant, person string) bool {
	return tenant != "" && person != "" && len(tenant) <= 200 && len(person) <= 200 && !strings.ContainsRune(tenant, 0) && !strings.ContainsRune(person, 0)
}
func (m *Manager) seal(tenant, key string, v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, m.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(m.aead.Seal(nonce, nonce, raw, []byte(tenant+"\x00"+key))), nil
}
func (m *Manager) open(tenant, key, encoded string, v any) error {
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(raw) < m.aead.NonceSize() {
		return ErrReconnect
	}
	n := m.aead.NonceSize()
	plain, err := m.aead.Open(nil, raw[:n], raw[n:], []byte(tenant+"\x00"+key))
	if err != nil {
		return ErrReconnect
	}
	if json.Unmarshal(plain, v) != nil {
		return ErrReconnect
	}
	return nil
}

// Begin creates one pending authorization per person/provider. Starting again
// invalidates its predecessor, bounding state and preventing callback mixups.
func (m *Manager) Begin(ctx context.Context, tenant, person, provider string) (Authorization, error) {
	return m.BeginFor(ctx, tenant, person, person, provider, "")
}
func (m *Manager) begin(ctx context.Context, tenant, person, owner, provider string, p Provider, binding string) (Authorization, error) {
	md, err := m.providerMetadata(ctx, p)
	if err != nil {
		return Authorization{}, err
	}
	clientID, clientSecret := p.ClientID, p.ClientSecret
	style := oauth2.AuthStyleInParams
	if clientID == "" {
		if md.RegistrationEndpoint == "" {
			return Authorization{}, fmt.Errorf("%w: this service needs a registered application; enter its issuer and client ID", ErrUnavailable)
		}
		var registered struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			Method       string `json:"token_endpoint_auth_method"`
		}
		body := map[string]any{"client_name": "Humagen", "redirect_uris": []string{m.cfg.RedirectURL}, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none"}
		if err := m.json(ctx, http.MethodPost, md.RegistrationEndpoint, body, &registered); err != nil {
			return Authorization{}, err
		}
		if registered.ClientID == "" || len(registered.ClientID) > 4096 || registered.Method != "" && registered.Method != "none" {
			return Authorization{}, fmt.Errorf("%w: unsupported client registration", ErrInvalid)
		}
		clientID, clientSecret = registered.ClientID, registered.ClientSecret
	} else if clientSecret != "" {
		if slices.Contains(md.AuthMethods, "client_secret_basic") {
			style = oauth2.AuthStyleInHeader
		} else if !slices.Contains(md.AuthMethods, "client_secret_post") {
			return Authorization{}, fmt.Errorf("%w: unsupported client authentication", ErrInvalid)
		}
	}
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		return Authorization{}, err
	}
	nonceText := base64.RawURLEncoding.EncodeToString(nonce)
	verifier := oauth2.GenerateVerifier()
	tx := transaction{Grant: nonceText, Native: p.AuthorizationURL != "", Owner: owner, Provider: provider, Person: person, Resource: p.Resource, Issuer: md.Issuer, ClientID: clientID, ClientSecret: clientSecret, TokenURL: md.TokenEndpoint, AuthStyle: style, Verifier: verifier, Expires: m.now().Add(10 * time.Minute), Binding: bindingDigest(binding)}
	// The state hash is stored with the encrypted transaction. It is not a key
	// supplied by the callback and cannot select another person's record.
	pending := struct {
		State       string
		Transaction transaction
	}{nonceText, tx}
	key := recordKey("pending", owner, provider)
	encrypted, err := m.seal(tenant, key, pending)
	if err != nil {
		return Authorization{}, err
	}
	batch := m.records.Batch()
	// A consent begun again replaces the one before it, and that one's state
	// index with it.
	if old, err := m.records.Lookup(tenant, key); err == nil {
		m.dropPending(batch, tenant, old)
	} else if !errors.Is(err, state.ErrNotFound) {
		return Authorization{}, err
	}
	batch.Put(tenant, key, encrypted)
	if tx.Binding != "" {
		index, err := m.seal(tenant, stateKey(nonceText), stateIndex{Pending: key})
		if err != nil {
			return Authorization{}, err
		}
		batch.Put(tenant, stateKey(nonceText), index)
	}
	if err = batch.Commit(); err != nil {
		return Authorization{}, err
	}
	config := oauth2.Config{ClientID: clientID, RedirectURL: m.cfg.RedirectURL, Endpoint: oauth2.Endpoint{AuthURL: md.AuthorizationEndpoint}, Scopes: p.Scopes}
	options := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier)}
	if !tx.Native {
		options = append(options, oauth2.SetAuthURLParam("resource", p.Resource))
	}
	if p.Offline {
		options = append(options, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	}
	if p.Name == "slack" {
		options = append(options, oauth2.SetAuthURLParam("scope", strings.Join(p.Scopes, ",")))
	}
	if p.Prompt != "" && !p.Offline {
		options = append(options, oauth2.SetAuthURLParam("prompt", p.Prompt))
	}
	return Authorization{Provider: provider, URL: config.AuthCodeURL(nonceText, options...), ExpiresAt: tx.Expires}, nil
}

// stateIndex finds a browser-bound pending consent by its state, whoever
// presents it, so a callback that does not match it can end it.
type stateIndex struct {
	Pending string
}

// pendingConsent is a pending record's sealed value.
type pendingConsent struct {
	State       string
	Transaction transaction
}

// stateKey is where a bound consent's state index is kept: a digest of the
// state, never the state.
func stateKey(nonce string) string {
	sum := sha256.Sum256([]byte("connectionauth state\x00" + nonce))
	return "state/" + hex.EncodeToString(sum[:])
}

// dropPending adds to b the deletion of a pending record and, for a bound
// consent, of its state index.
func (m *Manager) dropPending(b *state.Batch[string], tenant string, item state.Item[string]) {
	b.DeleteAt(tenant, item.Key, item.Version)
	var pending pendingConsent
	if m.open(tenant, item.Key, item.Value, &pending) == nil && pending.Transaction.Binding != "" && pending.State != "" {
		b.Delete(tenant, stateKey(pending.State))
	}
}

// EndBound ends the browser-bound consent whose state a callback presented,
// whoever presented it, and reports whether there was one. A consent ended
// completes nothing afterwards.
func (m *Manager) EndBound(tenant, nonce string) (bool, error) {
	if m == nil || tenant == "" || nonce == "" || len(nonce) > maxState {
		return false, nil
	}
	item, pending, found, err := m.boundByState(tenant, nonce)
	if err != nil || !found {
		return false, err
	}
	b := m.records.Batch()
	b.DeleteAt(tenant, item.Key, item.Version)
	b.Delete(tenant, stateKey(pending.State))
	return true, b.Commit()
}

// boundByState is the bound pending consent a state names. An index whose
// consent was replaced or spent is removed and names nothing.
func (m *Manager) boundByState(tenant, nonce string) (state.Item[string], pendingConsent, bool, error) {
	ikey := stateKey(nonce)
	idx, err := m.records.Lookup(tenant, ikey)
	if errors.Is(err, state.ErrNotFound) {
		return state.Item[string]{}, pendingConsent{}, false, nil
	}
	if err != nil {
		return state.Item[string]{}, pendingConsent{}, false, err
	}
	var ref stateIndex
	var pending pendingConsent
	item, lerr := state.Item[string]{}, error(nil)
	if m.open(tenant, ikey, idx.Value, &ref) == nil && strings.HasPrefix(ref.Pending, "pending/") {
		item, lerr = m.records.Lookup(tenant, ref.Pending)
	} else {
		lerr = state.ErrNotFound
	}
	if lerr != nil && !errors.Is(lerr, state.ErrNotFound) {
		return state.Item[string]{}, pendingConsent{}, false, lerr
	}
	if lerr != nil || m.open(tenant, item.Key, item.Value, &pending) != nil || pending.Transaction.Binding == "" ||
		subtle.ConstantTimeCompare([]byte(pending.State), []byte(nonce)) != 1 {
		b := m.records.Batch()
		b.DeleteAt(tenant, ikey, idx.Version)
		return state.Item[string]{}, pendingConsent{}, false, b.Commit()
	}
	return item, pending, true, nil
}

// bindingDigest is what a pending consent keeps of a browser's secret:
// nothing for none.
func bindingDigest(secret string) string {
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("connectionauth binding\x00" + secret))
	return hex.EncodeToString(sum[:])
}

// Complete consumes state once, before exchange. Callers bind tenant/person to
// their authenticated Principal. Errors never include remote bodies or tokens.
func (m *Manager) Complete(ctx context.Context, tenant, person, provider, nonce, code string) error {
	return m.CompleteFor(ctx, tenant, person, person, provider, nonce, code)
}
func (m *Manager) CompleteFor(ctx context.Context, tenant, person, owner, provider, nonce, code string) error {
	tx, tok, details, err := m.exchange(ctx, tenant, person, owner, provider, nonce, code, "")
	if err != nil {
		return err
	}
	key := recordKey("account", owner, provider)
	scopes, known := grantedScopes(tok)
	encrypted, err := m.seal(tenant, key, account{Transaction: tx, Token: *tok, Details: details, GrantedScopes: scopes, ScopesKnown: known})
	if err != nil {
		return err
	}
	if err = m.records.Put(tenant, key, encrypted); err != nil {
		return err
	}
	if provider != "workspace-slack" {
		m.authRecovered(ctx, tenant, tx)
	}
	return nil
}

// maxState bounds the state a callback presents; one made here is 43
// characters.
const maxState = 512

// MaxIdentity bounds the identity document CompleteIdentity reads.
const MaxIdentity = 1 << 20

// CompleteIdentity consumes a person's own state like Complete, then asks
// the provider who signed in: a GET of path under the provider's Resource
// with the new token. It returns that response's body and keeps nothing: the
// token is used once here and never written, so no account is connected.
// It is for a sign-in that proves an identity and needs no credential, and
// takes only a consent begun with BeginBound: binding must be the secret the
// browser that began it holds.
func (m *Manager) CompleteIdentity(ctx context.Context, tenant, person, provider, nonce, code, path, binding string) ([]byte, error) {
	if strings.Contains(path, "://") || strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
		return nil, ErrInvalid
	}
	if binding == "" {
		// A callback without the browser's secret ends the consent it names.
		if ended, err := m.EndBound(tenant, nonce); err != nil || !ended {
			return nil, ErrInvalid
		}
		return nil, ErrConsentEnded
	}
	tx, tok, _, err := m.exchange(ctx, tenant, person, person, provider, nonce, code, binding)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(tx.Resource, "/")+"/"+path, nil)
	if err != nil {
		return nil, ErrInvalid
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: the service could not be reached; connect again", ErrReconnect)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxIdentity+1))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || err != nil || len(raw) > MaxIdentity {
		return nil, fmt.Errorf("%w: the service did not say who signed in; connect again", ErrReconnect)
	}
	return raw, nil
}

// exchange consumes a pending authorization once, before the code is
// exchanged, and returns the token without storing it. A consent bound to a
// browser is completed only with that browser's secret, and an unbound one
// only without one.
func (m *Manager) exchange(ctx context.Context, tenant, person, owner, provider, nonce, code, binding string) (transaction, *oauth2.Token, map[string]string, error) {
	if m == nil {
		return transaction{}, nil, nil, ErrUnavailable
	}
	if !identity(tenant, person) || (owner != person && owner != WorkspaceOwner) || nonce == "" || len(nonce) > maxState || code == "" || len(code) > 8192 {
		return transaction{}, nil, nil, ErrInvalid
	}
	// A browser-bound consent is found by its state, whoever presents it; any
	// other by the caller's own record.
	item, pending, bound, err := m.boundByState(tenant, nonce)
	if err != nil {
		return transaction{}, nil, nil, ErrInvalid
	}
	if !bound {
		key := recordKey("pending", owner, provider)
		if item, err = m.records.Lookup(tenant, key); err != nil || m.open(tenant, key, item.Value, &pending) != nil {
			return transaction{}, nil, nil, ErrInvalid
		}
		if subtle.ConstantTimeCompare([]byte(pending.State), []byte(nonce)) != 1 {
			return transaction{}, nil, nil, ErrInvalid
		}
	}
	match := pending.Transaction.Person == person && pending.Transaction.Owner == owner && pending.Transaction.Provider == provider && m.now().Before(pending.Transaction.Expires) &&
		subtle.ConstantTimeCompare([]byte(pending.Transaction.Binding), []byte(bindingDigest(binding))) == 1
	batch := m.records.Batch()
	m.dropPending(batch, tenant, item)
	if !match {
		// A bound consent ends at a callback that does not match it: another
		// person, another browser, no browser secret, another provider.
		if pending.Transaction.Binding != "" {
			if batch.Commit() != nil {
				return transaction{}, nil, nil, ErrInvalid
			}
			return transaction{}, nil, nil, ErrConsentEnded
		}
		return transaction{}, nil, nil, ErrInvalid
	}
	if err = batch.Commit(); err != nil {
		return transaction{}, nil, nil, ErrInvalid
	}
	tx := pending.Transaction
	config := m.oauth(tx)
	options := []oauth2.AuthCodeOption{oauth2.VerifierOption(tx.Verifier)}
	if !tx.Native {
		options = append(options, oauth2.SetAuthURLParam("resource", tx.Resource))
	}
	tok, err := config.Exchange(context.WithValue(ctx, oauth2.HTTPClient, m.client), code, options...)
	if err != nil {
		if rejectedToken(err) {
			m.authFailure(ctx, tenant, tx)
		}
		return transaction{}, nil, nil, fmt.Errorf("%w: authorization was refused; connect again", ErrReconnect)
	}
	details, valid := nativeToken(tx, tok)
	if !valid {
		return transaction{}, nil, nil, ErrReconnect
	}
	tx.Verifier = ""
	return tx, tok, details, nil
}
func (m *Manager) oauth(tx transaction) *oauth2.Config {
	return &oauth2.Config{ClientID: tx.ClientID, ClientSecret: tx.ClientSecret, RedirectURL: m.cfg.RedirectURL, Endpoint: oauth2.Endpoint{TokenURL: tx.TokenURL, AuthStyle: tx.AuthStyle}}
}
func (m *Manager) Status(tenant, person, provider string) (Status, error) {
	if m == nil {
		return Status{}, nil
	}
	item, err := m.records.Lookup(tenant, recordKey("account", person, provider))
	if errors.Is(err, state.ErrNotFound) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	var a account
	if err = m.open(tenant, item.Key, item.Value, &a); err != nil {
		return Status{}, err
	}
	out := Status{ReconnectRequired: a.Rejected || (!a.Token.Expiry.IsZero() && !m.now().Before(a.Token.Expiry) && a.Token.RefreshToken == ""), Connected: !a.Rejected && a.Token.AccessToken != "" && (a.Token.Expiry.IsZero() || m.now().Before(a.Token.Expiry) || a.Token.RefreshToken != "")}
	if !a.Token.Expiry.IsZero() {
		out.ExpiresAt = &a.Token.Expiry
	}
	if a.ScopesKnown {
		scopes := append([]string{}, a.GrantedScopes...)
		out.GrantedScopes = &scopes
	}
	return out, nil
}

// Token refreshes a minute before expiry. A conditional lease serializes token
// rotation across processes. Contenders wait for the token or lease expiry,
// bounded by their context and 35 seconds. No plaintext is written to storage.
func (m *Manager) Token(ctx context.Context, tenant, person, provider string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	for {
		token, err := m.token(ctx, tenant, person, provider)
		if !errors.Is(err, ErrBusy) {
			return token, err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ErrBusy
		case <-timer.C:
		}
	}
}

func (m *Manager) token(ctx context.Context, tenant, person, provider string) (string, error) {
	if m == nil {
		return "", ErrUnavailable
	}
	if !identity(tenant, person) && (person != WorkspaceOwner || !identity(tenant, "workspace")) {
		return "", ErrInvalid
	}
	key := recordKey("account", person, provider)
	item, err := m.records.Lookup(tenant, key)
	if err != nil {
		return "", ErrReconnect
	}
	var a account
	if err = m.open(tenant, key, item.Value, &a); err != nil {
		return "", err
	}
	if a.Rejected {
		m.authFailure(ctx, tenant, a.Transaction)
		return "", ErrReconnect
	}
	if a.Token.AccessToken != "" && (a.Token.Expiry.IsZero() || a.Token.Expiry.After(m.now().Add(time.Minute))) {
		return a.Token.AccessToken, nil
	}
	if a.Token.RefreshToken == "" {
		if !a.Token.Expiry.IsZero() && !m.now().Before(a.Token.Expiry) {
			m.authFailure(ctx, tenant, a.Transaction)
		}
		return "", ErrReconnect
	}
	if a.LeaseUntil.After(m.now()) {
		return "", ErrBusy
	}
	a.LeaseUntil = m.now().Add(30 * time.Second)
	encoded, err := m.seal(tenant, key, a)
	if err != nil {
		return "", err
	}
	batch := m.records.Batch()
	batch.PutAt(tenant, key, encoded, item.Version)
	if err = batch.Commit(); err != nil {
		if errors.Is(err, state.ErrConflict) {
			return "", ErrBusy
		}
		return "", err
	}
	// Force the source to refresh under this lease, even within oauth2's own
	// shorter expiry margin. Preserve a rotated refresh token in the record.
	previous := a.Token
	previous.Expiry = time.Now().Add(-time.Hour)
	tok, refreshErr := m.oauth(a.Transaction).TokenSource(context.WithValue(ctx, oauth2.HTTPClient, m.client), &previous).Token()
	a.LeaseUntil = time.Time{}
	rejected := rejectedToken(refreshErr)
	if rejected {
		a.Rejected = true
	}
	valid := false
	if refreshErr == nil {
		_, valid = nativeToken(a.Transaction, tok)
	}
	if refreshErr == nil && valid {
		a.Token = *tok
		if scopes, known := grantedScopes(tok); known {
			a.GrantedScopes, a.ScopesKnown = scopes, true
		}
	} else {
		refreshErr = ErrReconnect
	}
	encoded, err = m.seal(tenant, key, a)
	if err != nil {
		return "", err
	}
	batch = m.records.Batch()
	batch.PutAt(tenant, key, encoded, item.Version+1)
	if err = batch.Commit(); err != nil {
		return "", ErrReconnect
	}
	if refreshErr != nil {
		if rejected {
			m.authFailure(ctx, tenant, a.Transaction)
		}
		return "", refreshErr
	}
	return a.Token.AccessToken, nil
}
func (m *Manager) Disconnect(tenant, person, provider string) error {
	if m == nil {
		return ErrUnavailable
	}
	if !identity(tenant, person) && (person != WorkspaceOwner || !identity(tenant, "workspace")) {
		return ErrInvalid
	}
	batch := m.records.Batch()
	batch.Delete(tenant, recordKey("account", person, provider))
	if old, err := m.records.Lookup(tenant, recordKey("pending", person, provider)); err == nil {
		m.dropPending(batch, tenant, old)
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	return batch.Commit()
}
