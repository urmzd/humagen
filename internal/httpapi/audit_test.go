package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/audit"
)

func TestChangeAuditIsBoundedAdminOnlyAndOwnerConfined(t *testing.T) {
	f := newFixture(t)
	machine := f.machine(t, "acme", everything...)
	want(t, f, machine, "GET", "/api/audit", "", 403)
	admin := f.signIn(t, "humagen-admins")
	now := time.Now().Add(-time.Second)
	for _, change := range []audit.Change{
		{Action: "skill.publish", Resource: "skill", ID: "public"},
		{Action: "memory.forget", Resource: "memory", ID: "secret", Subject: "someone-else", After: map[string]string{"text": "hidden text"}},
	} {
		if err := f.p.Audit.Record("acme", "ada", change, now); err != nil {
			t.Fatal(err)
		}
	}
	res, out := f.do(t, admin, http.MethodGet, "/api/audit?limit=1", "", "")
	if res.StatusCode != 200 {
		t.Fatalf("status %d: %#v", res.StatusCode, out)
	}
	rows := out["records"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["resource_id"] != "public" {
		t.Fatalf("hidden record affected result: %#v", rows)
	}
	for _, q := range []string{"?limit=501", "?limit=0", "?from=bad", "?from=2026-01-01T00:00:00Z&to=2026-03-01T00:00:00Z"} {
		res, _ := f.do(t, admin, "GET", "/api/audit"+q, "", "")
		if res.StatusCode != 400 {
			t.Fatalf("%s: %d", q, res.StatusCode)
		}
	}
}
