package platform

import (
	"net/http"
	"strings"

	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/personctx"
)

// The existing tool client retains its origin and credential guards. This
// observer records only a confirmed authentication rejection, never a remote
// error body, transient outage or ordinary permissions refusal.
type connectionRejectionTransport struct {
	next     http.RoundTripper
	rejected func(*http.Request)
}

func (t connectionRejectionTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	response, err := next.RoundTrip(r)
	if err == nil && response.StatusCode == http.StatusUnauthorized {
		t.rejected(r)
	}
	return response, err
}
func (p *Platform) personOAuthHTTPClient(tenant, person string, server personctx.ToolServer) *http.Client {
	client := p.personHTTPClient()
	if server.OAuth == "" {
		return client
	}
	owner := person
	if strings.HasPrefix(server.OAuth, "workspace-") {
		owner = connectionauth.WorkspaceOwner
	}
	client.Transport = connectionRejectionTransport{next: client.Transport, rejected: func(r *http.Request) { p.ConnectionAuth.ReportRejected(r.Context(), tenant, owner, server.OAuth) }}
	return client
}
