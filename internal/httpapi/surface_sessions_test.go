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

// A Slack thread that started as a conversation, in a channel bound to
// nothing or in a direct message, answers POST /api/sessions with a conflict
// that carries a code, and the same body is taken by POST /api/threads: the
// chat service tells this conflict from any other by the code, not the words.
func TestAConversationThreadRefusesASessionPostWithACode(t *testing.T) {
	f := newFixtureGranting(t, platform.Config{}, `grant operator to subject "ana"`)
	c := &contract{t: t, f: f, spec: loadSpec(t), seen: map[string]bool{}}
	admin, app := f.machine(t, "acme", everything...), f.signedAs(t, "svc-one")
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	client := strings.Replace(slackApp("one", "A01ONE", "svc-one"), `"machine":"svc-one"`, `"machine":"svc-one","people":{"U01ANA":"ana"}`, 1)
	want(t, f, admin, "POST", "/api/clients", client, http.StatusCreated)
	for _, channel := range []string{"C01OTHER", "D01ANA"} {
		post := func(message, text string) string {
			return `{"text":"` + text + `","act_as":{"client":"one","user":"U01ANA"},"surface":{"client":"one","channel":"` + channel + `","ts":"1700000000.000100","message":"` + message + `","app_user":"UAPP"}}`
		}
		first := c.call(app, "POST", "/api/threads", "/api/threads", post("1700000000.000100", "<@UAPP> what changed?"), http.StatusCreated)
		refused := c.call(app, "POST", "/api/sessions", "/api/sessions", post("1700000000.000200", "<@UAPP> and why?"), http.StatusConflict)
		if refused.field("code") != "thread_before_sessions" {
			t.Fatalf("%s: the conflict reads %s, want code thread_before_sessions", channel, refused.raw)
		}
		again := c.call(app, "POST", "/api/threads", "/api/threads", post("1700000000.000200", "<@UAPP> and why?"), http.StatusCreated)
		if id := again.field("thread").(map[string]any)["id"]; id != first.field("thread").(map[string]any)["id"] {
			t.Fatalf("%s: the reply landed in %v, not the conversation %s", channel, id, first.raw)
		}
	}
	// Any other conflict of the route carries no code.
	nowhere := `{"text":"anyone?","listen":true,"act_as":{"client":"one","user":"U01ANA"},"surface":{"client":"one","channel":"C01OTHER","ts":"1700000000.000900","message":"1700000000.000950","app_user":"UAPP"}}`
	if got := c.call(app, "POST", "/api/sessions", "/api/sessions", nowhere, http.StatusConflict); got.field("code") != nil {
		t.Fatalf("a reply where nobody listens reads %s, want no code", got.raw)
	}
}
