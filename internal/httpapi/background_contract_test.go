package httpapi

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/background"
)

// exerciseBackgroundContract lists and stops a person's own background work,
// and finds another person's not found.
func exerciseBackgroundContract(t *testing.T, c *contract) {
	t.Helper()
	owner := c.f.person(t, "acme", "bg-owner", "Background owner", "humagen-admins")
	other := c.f.person(t, "acme", "bg-other", "Someone else", "humagen-admins")
	it, err := c.f.p.Background.Create("acme", background.Spec{Kind: background.Watch, Owner: "bg-owner", Agent: "worker", Session: "s-contract", Project: "Field", Text: "the deploy is green"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if page := c.call(owner, "GET", "/api/me/background", "/api/me/background", "", 200); !strings.Contains(string(page.raw), it.ID) {
		t.Errorf("the owner's list leaves out their watch: %s", page.raw)
	}
	if page := c.call(other, "GET", "/api/me/background", "/api/me/background", "", 200); strings.Contains(string(page.raw), it.ID) {
		t.Errorf("another person sees the watch: %s", page.raw)
	}
	c.call(other, "POST", "/api/me/background/{id}/stop", "/api/me/background/"+it.ID+"/stop", "", 404)
	if v := c.call(owner, "POST", "/api/me/background/{id}/stop", "/api/me/background/"+it.ID+"/stop", "", 200); v.str("state") != "stopped" {
		t.Errorf("stop: %s", v.raw)
	}
}
