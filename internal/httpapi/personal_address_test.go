package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestMePersonalProjectAddressIsDirectlyUsableForChat(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		f := newFixtureWith(t, platform.Config{BuiltinProvider: "offline", BuiltinModel: "offline", Backends: platform.Backends{State: open(t)}})
		ana := f.person(t, "acme", "ana", "Ana", "humagen-admins")
		want(t, f, ana, "GET", "/api/agents", "", http.StatusOK)
		self := want(t, f, ana, "GET", "/api/me", "", http.StatusOK)
		address := self["personal_project"].(string)
		// Use exactly what /me hands the composer, without repairing its value.
		want(t, f, ana, "GET", "/api/projects/"+address+"/agents", "", http.StatusOK)
		started := want(t, f, ana, "POST", "/api/threads", fmt.Sprintf(`{"project":%q,"visibility":"private","text":"@humagen-analyst Reply with PERSONAL_CHAT_VALIDATED"}`, address), http.StatusCreated)
		thread := started["thread"].(map[string]any)
		if thread["project"] != address || thread["visibility"] != "private" {
			t.Fatalf("personal chat placement = %v", thread)
		}
		if len(started["runs"].([]any)) != 1 {
			t.Fatalf("Company analyst could not answer personal chat: %v", started)
		}
		again := want(t, f, ana, "GET", "/api/me", "", http.StatusOK)
		if again["personal_project"] != address {
			t.Fatal("personal address changed")
		}
		bo := f.person(t, "acme", "bo", "Bo", "humagen-operators")
		want(t, f, bo, "GET", "/api/projects/"+address+"/agents", "", http.StatusNotFound)
		want(t, f, bo, "POST", "/api/threads", fmt.Sprintf(`{"project":%q,"visibility":"private","text":"hello"}`, address), http.StatusNotFound)
	})
}
