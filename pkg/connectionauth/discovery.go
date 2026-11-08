package connectionauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type metadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	Challenges            []string `json:"code_challenge_methods_supported"`
	AuthMethods           []string `json:"token_endpoint_auth_methods_supported"`
}

func (m *Manager) providerMetadata(ctx context.Context, p Provider) (metadata, error) {
	if p.AuthorizationURL != "" {
		return metadata{Issuer: p.Issuer, AuthorizationEndpoint: p.AuthorizationURL, TokenEndpoint: p.TokenURL, Challenges: []string{"S256"}, AuthMethods: []string{"client_secret_post"}}, nil
	}
	return m.discover(ctx, p)
}

func safeURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 2048 {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && !publicIP(ip) {
		return false
	}
	return true
}
func publicIP(ip net.IP) bool {
	for _, cidr := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15"} {
		_, block, _ := net.ParseCIDR(cidr)
		if block.Contains(ip) {
			return false
		}
	}
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() && !ip.Equal(net.IPv4(169, 254, 169, 254))
}

// Resolve and dial the checked address, rather than resolving twice. Metadata
// cannot reach private services by redirect or DNS rebinding. Proxy environment
// variables are not used for this security boundary.
func publicClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 || len(ips) > 64 {
			return nil, fmt.Errorf("connection host could not be resolved")
		}
		for _, ip := range ips {
			if !publicIP(ip.IP) {
				return nil, fmt.Errorf("connection host is not public")
			}
		}
		dialer := net.Dialer{Timeout: 10 * time.Second}
		var last error
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second}
}
func wellKnown(raw, name string) string {
	u, _ := url.Parse(raw)
	u.Path = "/.well-known/" + name + strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	u.RawQuery = ""
	return u.String()
}
func (m *Manager) json(ctx context.Context, method, endpoint string, input, output any) error {
	if !safeURL(endpoint) {
		return fmt.Errorf("%w: service metadata contains an unsafe address", ErrInvalid)
	}
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return ErrInvalid
	}
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: service discovery could not be reached", ErrUnavailable)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return discoveryStatus(resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return fmt.Errorf("%w: service metadata is too large", ErrInvalid)
	}
	if json.Unmarshal(raw, output) != nil {
		return fmt.Errorf("%w: service metadata is malformed", ErrInvalid)
	}
	return nil
}

type discoveryStatus int

func (s discoveryStatus) Error() string { return fmt.Sprintf("service discovery returned HTTP %d", s) }
func (s discoveryStatus) Unwrap() error { return ErrUnavailable }
func unavailableMetadata(err error) bool {
	var status discoveryStatus
	return errors.As(err, &status) && (status == 404 || status == 405)
}

func (m *Manager) discover(ctx context.Context, p Provider) (metadata, error) {
	var resource struct {
		Resource string   `json:"resource"`
		Servers  []string `json:"authorization_servers"`
	}
	resourceEndpoint := wellKnown(p.Resource, "oauth-protected-resource")
	err := m.json(ctx, http.MethodGet, resourceEndpoint, nil, &resource)
	if err != nil && unavailableMetadata(err) {
		u, _ := url.Parse(p.Resource)
		u.Path = ""
		u.RawPath = ""
		u.RawQuery = ""
		root := wellKnown(u.String(), "oauth-protected-resource")
		if root != resourceEndpoint {
			err = m.json(ctx, http.MethodGet, root, nil, &resource)
		}
	}
	if err != nil {
		var status discoveryStatus
		if !errors.As(err, &status) || (status != 404 && status != 405 && status != 401) {
			return metadata{}, err
		}
		endpoint, challengeErr := m.challengeMetadata(ctx, p.Resource)
		if challengeErr != nil {
			return metadata{}, challengeErr
		}
		if err = m.json(ctx, http.MethodGet, endpoint, nil, &resource); err != nil {
			return metadata{}, err
		}
	}
	if resource.Resource != p.Resource || len(resource.Servers) == 0 || len(resource.Servers) > 8 {
		return metadata{}, fmt.Errorf("%w: resource metadata does not identify this service", ErrInvalid)
	}
	issuer := resource.Servers[0]
	if p.Issuer != "" {
		if !slices.Contains(resource.Servers, p.Issuer) {
			return metadata{}, fmt.Errorf("%w: service authorization server changed", ErrInvalid)
		}
		issuer = p.Issuer
	}
	if !safeURL(issuer) {
		return metadata{}, ErrInvalid
	}
	var md metadata
	issuerURL, _ := url.Parse(issuer)
	if issuerURL.RawQuery != "" {
		return metadata{}, ErrInvalid
	}
	if err := m.json(ctx, http.MethodGet, wellKnown(issuer, "oauth-authorization-server"), nil, &md); err != nil {
		if !unavailableMetadata(err) {
			return metadata{}, err
		}
		if err = m.json(ctx, http.MethodGet, strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", nil, &md); err != nil {
			return metadata{}, err
		}
	}
	if md.Issuer != issuer || !safeURL(md.AuthorizationEndpoint) || !safeURL(md.TokenEndpoint) || (md.RegistrationEndpoint != "" && !safeURL(md.RegistrationEndpoint)) {
		return metadata{}, fmt.Errorf("%w: authorization metadata does not match the issuer", ErrInvalid)
	}
	if !slices.Contains(md.Challenges, "S256") {
		return metadata{}, fmt.Errorf("%w: service does not support secure authorization (S256)", ErrUnavailable)
	}
	if len(md.AuthMethods) == 0 {
		md.AuthMethods = []string{"client_secret_basic"}
	}
	return md, nil
}

var bearerChallenge = regexp.MustCompile(`(?i)(?:^|,)\s*Bearer\s+`)
var metadataParameter = regexp.MustCompile(`(?i)(?:^|[,\s])resource_metadata\s*=\s*("(?:[^"\\]|\\.)*"|[^\s,]+)`)

// A protected resource may advertise its RFC 9728 metadata in a Bearer
// challenge. This probe sends no credential and follows no redirect.
func (m *Manager) challengeMetadata(ctx context.Context, resource string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource, nil)
	if err != nil || !safeURL(resource) {
		return "", ErrInvalid
	}
	req.Header.Set("Accept", "application/json")
	response, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: service authorization metadata could not be reached", ErrUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden {
		return "", fmt.Errorf("%w: service does not advertise authorization metadata", ErrUnavailable)
	}
	for _, header := range response.Header.Values("WWW-Authenticate") {
		if len(header) > 8192 {
			return "", ErrInvalid
		}
		scheme := bearerChallenge.FindStringIndex(header)
		if scheme == nil {
			continue
		}
		match := metadataParameter.FindStringSubmatch(header[scheme[1]:])
		if len(match) != 2 {
			continue
		}
		endpoint := match[1]
		if strings.HasPrefix(endpoint, `"`) {
			endpoint, err = strconv.Unquote(endpoint)
			if err != nil {
				return "", ErrInvalid
			}
		}
		if !safeURL(endpoint) {
			return "", fmt.Errorf("%w: service challenge contains an unsafe address", ErrInvalid)
		}
		return endpoint, nil
	}
	return "", fmt.Errorf("%w: service does not advertise authorization metadata", ErrUnavailable)
}
