package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/knowledge"
)

func TestExternalSourceMutationResponsesWithholdRetainedFileInventory(t *testing.T) {
	f := newFixture(t)
	token := f.machine(t, "acme", everything...)
	if res, _ := f.do(t, nil, "POST", "/api/knowledge", token, `{"name":"remote","configuration":{"mode":"indexed","provider":"drive","origin":"folder","project":"~"}}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("create source: %d", res.StatusCode)
	}
	at := time.Now()
	attempt, err := f.p.Knowledge.BeginSync("acme", "remote", at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Knowledge.CompleteSync("acme", "remote", attempt, []knowledge.ImportDocument{{ID: "cached-file-canary", Title: "Retained private title", Text: "retained text"}}, "collector", at); err != nil {
		t.Fatal(err)
	}
	// Seed a legacy saved variant through the internal store. Public mutations
	// cannot manufacture access to this retained provider inventory.
	if _, err := f.p.Knowledge.SetExcluded("acme", "remote", "cached-file-canary", true, at); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Knowledge.SaveVariant("acme", "remote", "legacy-view", at); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, path, body string }{
		{"PUT", "/api/knowledge/remote/configuration", `{"mode":"indexed","provider":"drive","origin":"folder","project":"~"}`},
		{"PUT", "/api/knowledge/remote/sync-settings", `{"every_seconds":0,"stop_after_seconds":600}`},
		{"POST", "/api/knowledge/remote/snapshot", ""},
		{"GET", "/api/knowledge", ""},
	} {
		res, body := f.do(t, nil, tc.method, tc.path, token, tc.body)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s %s: %d %+v", tc.method, tc.path, res.StatusCode, body)
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		raw := string(encoded)
		if strings.Contains(raw, "cached-file-canary") || strings.Contains(raw, "legacy-view") || strings.Contains(raw, "Retained private title") {
			t.Fatalf("%s returned cached file inventory: %s", tc.path, raw)
		}
	}
	if res, _ := f.do(t, nil, "GET", "/api/knowledge/remote/documents", token, ""); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("retained documents: %d", res.StatusCode)
	}
}
