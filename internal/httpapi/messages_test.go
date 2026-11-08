package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
)

// The messages routes: a place's first message starts its root (201), the
// next one lands in it (200), a follow-up goes under a message, and a
// message reads and streams as its session.
func TestMessagesFindOrStartThePlacesRoot(t *testing.T) {
	f := newFixtureGranting(t, platform.Config{BuiltinProvider: "offline", BuiltinModel: "offline"}, `grant operator to subject "ana"`)
	c := &contract{t: t, f: f, spec: loadSpec(t), seen: map[string]bool{}}
	admin := f.machine(t, "acme", everything...)
	ana := f.person(t, "acme", "ana", "Ana", "humagen-operators")
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	pr := `"ref":{"type":"github","repo":"acme/web","kind":"pull","number":5}`
	first := c.call(ana, "POST", "/api/messages", "/api/messages", `{"messages":[{"role":"user","content":"review the change"}],`+pr+`}`, http.StatusCreated)
	root := first.field("root").(string)
	id := first.field("id").(string)
	if !strings.HasPrefix(id, root+":") || first.field("created") != true {
		t.Fatalf("first message = %s", first.raw)
	}
	again := c.call(ana, "POST", "/api/messages", "/api/messages", `{"messages":[{"role":"user","content":[{"type":"text","text":"and the tests"}]}],`+pr+`}`, http.StatusOK)
	if again.field("root") != root || again.field("created") != false {
		t.Fatalf("second message = %s", again.raw)
	}
	under := c.call(ana, "POST", "/api/messages/{id}", "/api/messages/"+id, `{"messages":[{"role":"user","content":"one more thing"}]}`, http.StatusOK)
	if under.field("root") != root {
		t.Fatalf("a follow-up landed in %v", under.field("root"))
	}
	read := c.call(ana, "GET", "/api/messages/{id}", "/api/messages/"+id, "", http.StatusOK)
	if read.field("id") != root || read.field("parent") != "github://acme/web/pull/5" {
		t.Fatalf("the message reads as %s", read.raw)
	}
	c.call(ana, "POST", "/api/messages", "/api/messages", `{"messages":[{"role":"assistant","content":"hi"}]}`, http.StatusBadRequest)
	bo := f.person(t, "acme", "bo", "Bo", "humagen-operators")
	c.call(bo, "GET", "/api/messages/{id}", "/api/messages/"+id, "", http.StatusNotFound)
}
