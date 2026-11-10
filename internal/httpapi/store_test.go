package httpapi

import (
	"net/http"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
)

// The store over HTTP: anyone who reads agents reads the store; installing
// and removing take agents:write. A new workspace lists only the entry and
// approval agents, an installed package's agents join the directory, and a
// removed one's leave it while its history still reads.
func TestTheStoreInstallsAndRemovesPackages(t *testing.T) {
	f := newFixtureWith(t, platform.Config{TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	admin := f.machine(t, "acme", everything...)
	reader := f.machine(t, "acme", "humagen:agents:read")
	names := func() map[string]bool {
		out := map[string]bool{}
		for _, item := range want(t, f, admin, "GET", "/api/agents", "", http.StatusOK)["agents"].([]any) {
			out[item.(map[string]any)["spec"].(map[string]any)["name"].(string)] = true
		}
		return out
	}
	if got := names(); len(got) != 2 || !got[platform.BuiltinEntry] || !got[platform.BuiltinApprover] {
		t.Fatalf("a new workspace lists %v", got)
	}
	store := want(t, f, reader, "GET", "/api/store/packages", "", http.StatusOK)["packages"].([]any)
	if len(store) != 2 || store[0].(map[string]any)["installed"] != false || store[1].(map[string]any)["title"] != "Review" {
		t.Fatalf("store = %v", store)
	}
	want(t, f, reader, "POST", "/api/store/packages/core/install", "", http.StatusForbidden)
	added := want(t, f, admin, "POST", "/api/store/packages/core/install", "", http.StatusOK)
	if pkg := added["package"].(map[string]any); pkg["installed"] != true || len(added["added"].([]any)) != 2 {
		t.Fatalf("install = %v", added)
	}
	if got := names(); !got[platform.BuiltinWriter] || !got[platform.BuiltinAnalyst] || len(got) != 4 {
		t.Fatalf("after install the directory lists %v", got)
	}
	want(t, f, reader, "DELETE", "/api/store/packages/core", "", http.StatusForbidden)
	removed := want(t, f, admin, "DELETE", "/api/store/packages/core", "", http.StatusOK)
	if len(removed["retired"].([]any)) != 2 || removed["package"].(map[string]any)["installed"] != false {
		t.Fatalf("remove = %v", removed)
	}
	if got := names(); got[platform.BuiltinWriter] || len(got) != 2 {
		t.Fatalf("after removal the directory lists %v", got)
	}
	want(t, f, admin, "POST", "/api/agents/humagen-writer/reset", `{"expected_rev":0}`, http.StatusNotFound)
	if history, err := f.p.Agents.History(t.Context(), "acme", platform.BuiltinWriter); err != nil || len(history) == 0 {
		t.Fatalf("history after removal: %v, %v", history, err)
	}
	other := f.machine(t, "globex", everything...)
	if pkg := want(t, f, other, "GET", "/api/store/packages", "", http.StatusOK)["packages"].([]any)[0].(map[string]any); pkg["installed"] != false {
		t.Fatalf("another workspace reads %v", pkg)
	}
}
