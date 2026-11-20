package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/urmzd/humagen/pkg/auth"
)

// tailscaleAuth trusts Serve on this host. Local processes must be trusted:
// loopback prevents remote callers from forging identity headers, but does
// not authenticate the local process making the request. A process the server
// starts itself is the one caller that is not a person: it presents the token
// MintMachine made for it.
type tailscaleAuth struct {
	tenant string
	host   string
	policy *auth.Policy

	mu       sync.RWMutex
	machines []localMachine
}

// localMachine is a process this server started and the hash of the token it
// was handed. The token itself is not kept.
type localMachine struct {
	hash [sha256.Size]byte
	who  auth.Principal
}

// MintMachine makes a credential for a process this server starts itself,
// such as the Slack app, which cannot sign in through Tailscale Serve. It
// returns a new random token that authenticates as a machine with exactly
// these scopes and no role, for as long as this process lives. The token is
// accepted only from a loopback peer that did not come through Serve (see
// machine). The caller hands it to its child and writes it nowhere.
func (a *Authn) MintMachine(tenant, subject, name string, scopes []auth.Permission) (string, error) {
	t := a.tailscale
	if t == nil {
		return "", errors.New("httpapi: only Tailscale sign-in mints a credential for a process the server starts")
	}
	if tenant == "" || subject == "" {
		return "", errors.New("httpapi: a machine needs a tenant and a subject")
	}
	who := auth.Principal{Subject: subject, Tenant: tenant, Kind: auth.Machine, Name: name,
		Scopes: append([]auth.Permission(nil), scopes...)}.WithPolicy(t.policy)
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, m := range t.machines {
		if m.who.Tenant == tenant && m.who.Subject == subject {
			return "", fmt.Errorf("httpapi: machine %q of %q already has a credential", subject, tenant)
		}
	}
	tok := token()
	t.machines = append(t.machines, localMachine{hash: sha256.Sum256([]byte(tok)), who: who})
	return tok, nil
}

// NewTailscaleAuthn authenticates through Tailscale Serve identity headers.
// addr must listen on loopback and publicURL must be the HTTPS Serve origin.
func NewTailscaleAuthn(addr, publicURL, tenant string, policy *auth.Policy) (*Authn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || (host != "localhost" && !net.ParseIP(host).IsLoopback()) {
		return nil, fmt.Errorf("-auth=tailscale requires a loopback listener, not %q", addr)
	}
	u, err := url.Parse(publicURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("-auth=tailscale requires an HTTPS Serve origin as -public-url")
	}
	if tenant == "" {
		return nil, fmt.Errorf("-auth=tailscale requires a tenant")
	}
	return &Authn{Secure: true, tailscale: &tailscaleAuth{tenant: tenant, host: u.Host, policy: policy}}, nil
}

func (t *tailscaleAuth) authenticate(r *http.Request) (auth.Principal, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return auth.Principal{}, auth.ErrUnauthenticated
	}
	if r.Header.Get("Authorization") != "" {
		return t.machine(r)
	}
	if r.Host != t.host {
		return auth.Principal{}, auth.ErrUnauthenticated
	}
	login, err := identityHeader(r, "Tailscale-User-Login")
	if err != nil {
		return auth.Principal{}, err
	}
	name, err := identityHeader(r, "Tailscale-User-Name")
	if err != nil {
		return auth.Principal{}, err
	}
	return auth.TailscalePrincipal(t.tenant, login, name, t.policy)
}

// machine authenticates a process this server started, by the token
// MintMachine gave it. The peer is already known to be loopback. A request
// that came through Serve is refused whatever it carries: it has a Tailscale
// header, or it names the Serve host, where the child calls this server at its
// loopback address. Every other credential is refused as it always was.
func (t *tailscaleAuth) machine(r *http.Request) (auth.Principal, error) {
	for key := range r.Header {
		if strings.HasPrefix(strings.ToLower(key), "tailscale-") {
			return auth.Principal{}, auth.ErrUnauthenticated
		}
	}
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !loopbackHost(r.Host) {
		return auth.Principal{}, auth.ErrUnauthenticated
	}
	raw, ok := strings.CutPrefix(values[0], "Bearer ")
	if !ok || raw == "" {
		return auth.Principal{}, auth.ErrUnauthenticated
	}
	// Hashes are compared, each in constant time and all of them, so neither
	// a token's bytes nor which machine it is shows in how long this takes.
	sum := sha256.Sum256([]byte(raw))
	var who auth.Principal
	found := 0
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, m := range t.machines {
		if subtle.ConstantTimeCompare(sum[:], m.hash[:]) == 1 {
			who, found = m.who, 1
		}
	}
	if found != 1 {
		return auth.Principal{}, auth.ErrUnauthenticated
	}
	return who, nil
}

// loopbackHost reports whether a request's Host names this machine's loopback
// address, with or without a port.
func loopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
	}
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

func identityHeader(r *http.Request, key string) (string, error) {
	values := r.Header.Values(key)
	if len(values) == 0 {
		return "", nil
	}
	if len(values) != 1 {
		return "", auth.ErrUnauthenticated
	}
	s, err := new(mime.WordDecoder).DecodeHeader(values[0])
	if err != nil {
		return "", auth.ErrUnauthenticated
	}
	return s, nil
}

func (a *Authn) tailscaleLogin(w http.ResponseWriter, r *http.Request) {
	if _, err := a.Authenticate(r); err != nil {
		http.Error(w, "open Humagen through its Tailscale Serve URL from a user-owned tailnet device", http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, safeReturn(r.URL.Query().Get("return_to")), http.StatusFound)
}
