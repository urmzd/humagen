package spec

import (
	"fmt"
	"net/url"
)

// ConnectionAuthorization enables OAuth for personal service connections.
// The encryption key is a reference to 32 random bytes encoded as base64.
type ConnectionAuthorization struct {
	EncryptionKey *Ref                             `json:"encryption_key"`
	RedirectURL   string                           `json:"redirect_url"`
	Clients       map[string]ConnectionOAuthClient `json:"clients,omitempty"`
	GitHub        *GitHubInstallation              `json:"github,omitempty"`
}
type GitHubInstallation struct {
	AppID         string `json:"app_id"`
	AppSlug       string `json:"app_slug"`
	ClientID      string `json:"client_id"`
	ClientSecret  *Ref   `json:"client_secret"`
	PrivateKey    *Ref   `json:"private_key"`
	WebhookSecret *Ref   `json:"webhook_secret"`
}
type ConnectionOAuthClient struct {
	Issuer        string   `json:"issuer"`
	ClientID      string   `json:"client_id"`
	ClientSecret  *Ref     `json:"client_secret,omitempty"`
	SigningSecret *Ref     `json:"signing_secret,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
}

func (c *ConnectionAuthorization) check() error {
	if c == nil {
		return nil
	}
	if !c.EncryptionKey.Set() {
		return fmt.Errorf("connections: encryption_key is required")
	}
	if g := c.GitHub; g != nil {
		if g.AppID == "" || len(g.AppID) > 20 || g.AppSlug == "" || len(g.AppSlug) > 100 || g.ClientID == "" || len(g.ClientID) > 4096 || !g.ClientSecret.Set() || !g.PrivateKey.Set() || !g.WebhookSecret.Set() {
			return fmt.Errorf("connections: github needs app_id, app_slug, client_id and client_secret/private_key/webhook_secret references")
		}
		for _, r := range g.AppID {
			if r < '0' || r > '9' {
				return fmt.Errorf("connections: github app_id must be numeric")
			}
		}
		for _, r := range g.AppSlug {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return fmt.Errorf("connections: github app_slug must use lowercase letters, digits and dashes")
			}
		}
	}
	u, err := url.Parse(c.RedirectURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && (u.Scheme != "http" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1"))) {
		return fmt.Errorf("connections: redirect_url must be HTTPS or loopback HTTP")
	}
	for name, client := range c.Clients {
		if name != "notion" && name != "linear" && name != "granola" && name != "drive" && name != "slack" && name != "bigquery" {
			return fmt.Errorf("connections: unknown OAuth client %q", name)
		}
		issuer, err := url.Parse(client.Issuer)
		if name == "drive" || name == "slack" || name == "bigquery" {
			if name == "slack" && !client.SigningSecret.Set() {
				return fmt.Errorf("connections: slack needs signing_secret")
			}
			if client.ClientID == "" || len(client.ClientID) > 4096 || !client.ClientSecret.Set() || client.Issuer != "" || len(client.Scopes) > 0 {
				return fmt.Errorf("connections: %s needs client_id and client_secret; issuer and scopes are fixed", name)
			}
			continue
		}
		if client.ClientID == "" || len(client.ClientID) > 4096 || err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
			return fmt.Errorf("connections: %s needs a client_id and HTTPS issuer", name)
		}
		if len(client.Scopes) > 32 {
			return fmt.Errorf("connections: at most 32 scopes per client")
		}
	}
	return nil
}
