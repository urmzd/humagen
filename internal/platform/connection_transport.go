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
	revision func() string
	rejected func(*http.Request, string)
}

func (t connectionRejectionTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	revision := ""
	if t.revision != nil {
		revision = t.revision()
	}
	response, err := next.RoundTrip(r)
	if err == nil && response.StatusCode == http.StatusUnauthorized && t.rejected != nil {
		t.rejected(r, revision)
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
	// This client is built before its credential resolver runs. Keep that
	// consent revision for its lifetime, including delayed MCP responses.
	revision := p.ConnectionAuth.Revision(tenant, owner, server.OAuth)
	client.Transport = connectionRejectionTransport{next: client.Transport,
		revision: func() string { return revision },
		rejected: func(r *http.Request, revision string) {
			bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if ok {
				p.ConnectionAuth.ReportTokenRejected(r.Context(), tenant, owner, server.OAuth, revision, bearer)
			}
		},
	}
	return client
}
