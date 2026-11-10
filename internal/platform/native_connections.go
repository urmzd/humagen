package platform

import (
	"github.com/urmzd/humagen/pkg/connectionauth"
)

// NativeConnectionProvider keeps each native API's audience and least-privilege
// scopes separate from MCP credentials. Client secrets come from deployment refs.
func NativeConnectionProvider(name string) connectionauth.Provider {
	switch name {
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
		return connectionauth.Provider{Name: name, Resource: "https://slack.com/api/", Issuer: "https://slack.com", AuthorizationURL: "https://slack.com/oauth/v2/authorize", TokenURL: "https://slack.com/api/oauth.v2.access", Scopes: []string{"app_mentions:read", "chat:write", "channels:history", "groups:history", "im:history", "mpim:history", "files:read", "reactions:write"}}
	}
	return connectionauth.Provider{}
}
