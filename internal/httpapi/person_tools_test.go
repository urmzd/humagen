package httpapi

import (
	"net/http"
	"testing"
)

func TestPersonToolReviewRequiresWriteAndOwnAllowedHost(t *testing.T) {
	f := newFixture(t)
	ana := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	other := f.person(t, "other", "ana", "Ana", "humagen-admins")
	machine := f.machine(t, "acme", "humagen:runs:write")
	readonly := f.machine(t, "acme", "humagen:runs:read")
	path := "/api/me/tool-servers/review"
	body := `{"name":"notes","url":"https://mcp.example.com"}`
	for _, token := range []string{ana, other, machine, readonly} {
		want(t, f, token, "POST", path, body, http.StatusForbidden)
	}
	want(t, f, ana, "POST", path, `{"name":"notes","url":"http://localhost"}`, http.StatusBadRequest)
	want(t, f, ana, "POST", path, `{"subject":"other","name":"notes","url":"https://mcp.example.com"}`, http.StatusBadRequest)
}
