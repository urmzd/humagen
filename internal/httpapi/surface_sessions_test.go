package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
)

// A chat service posts a Slack thread's messages to POST /api/sessions with
// the surface and the person: the first starts the place's root session
// (201), every later one in the thread lands in it (200).
func TestAChatServiceFindsOrStartsASlackThreadsRootSession(t *testing.T) {
	f := newFixtureGranting(t, platform.Config{}, `grant operator to subject "ana"`)
	c := &contract{t: t, f: f, spec: loadSpec(t), seen: map[string]bool{}}
	admin, app := f.machine(t, "acme", everything...), f.signedAs(t, "svc-one")
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	client := strings.Replace(slackApp("one", "A01ONE", "svc-one"), `"machine":"svc-one"`, `"machine":"svc-one","people":{"U01ANA":"ana"}`, 1)
	want(t, f, admin, "POST", "/api/clients", client, http.StatusCreated)
	post := func(message, text string) string {
		return `{"text":"` + text + `","act_as":{"client":"one","user":"U01ANA"},"surface":{"client":"one","channel":"D01ANA","ts":"1700000000.000100","message":"` + message + `","app_user":"UAPP"}}`
	}
	first := c.call(app, "POST", "/api/sessions", "/api/sessions", post("1700000000.000100", "what changed?"), http.StatusCreated)
	id := first.field("id").(string)
	if !strings.HasPrefix(first.field("parent").(string), "slack://") || first.field("agent") != "helper" {
		t.Fatalf("the root reads %s", first.raw)
	}
	again := c.call(app, "POST", "/api/sessions", "/api/sessions", post("1700000000.000200", "and why?"), http.StatusOK)
	if again.field("id") != id {
		t.Fatalf("a reply landed in %v, not the root %s", again.field("id"), id)
	}
	want(t, f, app, "POST", "/api/sessions", `{"text":"hi","project":"~","act_as":{"client":"one","user":"U01ANA"},"surface":{"client":"one","channel":"D01ANA","ts":"1700000000.000100"}}`, http.StatusBadRequest)
}
