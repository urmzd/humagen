package platform

import (
	"github.com/urmzd/humagen/pkg/connectionauth"
)

// NativeConnectionProvider keeps each native API's audience and least-privilege
// scopes separate from MCP credentials. Client secrets come from deployment refs.
func NativeConnectionProvider(name string) connectionauth.Provider {
	switch name {
	case GitHubAccount, SlackAccount:
		return accountProvider(name)
	case "drive":
		return connectionauth.Provider{Name: name, Resource: "https://www.googleapis.com/drive/v3/", Issuer: "https://accounts.google.com", AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", Offline: true, Scopes: []string{"https://www.googleapis.com/auth/drive.readonly"}}
	case "onedrive":
		// Microsoft identity platform common endpoints, so work, school and
		// personal Microsoft accounts can connect. A refresh token comes from
		// the offline_access scope rather than an access_type parameter. No ID
		// token is requested; the issuer names the authority only.
		return connectionauth.Provider{Name: name, Resource: "https://graph.microsoft.com/v1.0/", Issuer: "https://login.microsoftonline.com/common/v2.0", AuthorizationURL: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize", TokenURL: "https://login.microsoftonline.com/common/oauth2/v2.0/token", Scopes: []string{"Files.Read", "offline_access", "User.Read"}}
	case "bigquery":
		return connectionauth.Provider{Name: name, Resource: "https://bigquery.googleapis.com/bigquery/v2/", Issuer: "https://accounts.google.com", AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", Offline: true, Scopes: []string{"https://www.googleapis.com/auth/bigquery.readonly"}}
	case "slack":
		return connectionauth.Provider{Name: name, Resource: "https://slack.com/api/", Issuer: "https://slack.com", AuthorizationURL: "https://slack.com/oauth/v2/authorize", TokenURL: "https://slack.com/api/oauth.v2.access", Scopes: []string{"app_mentions:read", "chat:write", "channels:history", "groups:history", "im:history", "mpim:history", "files:read", "files:write", "reactions:write"}}
	}
	return connectionauth.Provider{}
}

// googleToolScopes are what each Google Workspace tool server is asked for:
// reading and changing that product's own content, and for the editors,
// finding their files in Drive. Each is one its server lists as supported.
// A tool that changes something still waits for its person's approval.
var googleToolScopes = map[string][]string{
	"gmail":           {"https://www.googleapis.com/auth/gmail.modify"},
	"google-calendar": {"https://www.googleapis.com/auth/calendar.events", "https://www.googleapis.com/auth/calendar.readonly"},
	"google-drive":    {"https://www.googleapis.com/auth/drive"},
	"google-docs":     {"https://www.googleapis.com/auth/documents", "https://www.googleapis.com/auth/drive.readonly"},
	"google-sheets":   {"https://www.googleapis.com/auth/spreadsheets", "https://www.googleapis.com/auth/drive.readonly"},
	"google-slides":   {"https://www.googleapis.com/auth/presentations", "https://www.googleapis.com/auth/drive.readonly"},
}

// GoogleToolProviders are the Google Workspace tool servers as connection
// providers, for a deployment that registered a Google application
// (connections.clients.google). Google's authorization server takes no client
// registration and its resource metadata names its issuer with a trailing
// slash its own metadata does not have, so the endpoints are fixed here, as
// they are for Drive and BigQuery, and each person consents per product.
func GoogleToolProviders() []connectionauth.Provider {
	out := []connectionauth.Provider{}
	for _, p := range connectionPresets {
		if p.client != GoogleClient {
			continue
		}
		out = append(out, connectionauth.Provider{
			Name: p.ID, Resource: p.Endpoint, Issuer: "https://accounts.google.com",
			AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token",
			Offline: true, Scopes: googleToolScopes[p.ID],
		})
	}
	return out
}
