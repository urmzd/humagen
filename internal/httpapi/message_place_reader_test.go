package httpapi

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	slackflow "github.com/urmzd/saige/flow/connectors/slack"
)

// peopleIn are the people of a session as its reader is shown them, sorted.
func peopleIn(session map[string]any) []string {
	var out []string
	for _, v := range sliceOf(obj(session["thread"])["participants"]) {
		if pt := obj(v); pt["kind"] == "person" {
			out = append(out, pt["name"].(string))
		}
	}
	slices.Sort(out)
	return out
}

// A message sent to a place that already has a root is taken only from
// someone who reads that root. Anyone else is told there is none, and the
// root's people, version and audience stay as they were: a request's client
// and audience are read only from the client's own machine.
func TestAMessageToAPlaceIsTakenOnlyFromAReaderOfItsRoot(t *testing.T) {
	f := newFixtureGranting(t, platform.Config{
		SlackAPI:       func(string) slackflow.API { return slackflow.NewFake() },
		ClientInterval: -1,
	}, `grant operator to subject "ana"
grant operator to subject "ben"
grant operator to subject "vic"`)
	app := f.machine(t, "acme", everything...)
	want(t, f, app, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, app, "POST", "/api/projects", `{"name":"Field","title":"Field engineering"}`, http.StatusCreated)
	want(t, f, app, "PUT", "/api/projects/Field/members", `{"version":1,"members":[{"who":"ana","role":"member"},{"who":"ben","role":"member"},{"who":"vic","role":"member"}]}`, http.StatusOK)
	client := strings.Replace(slackApp("app", "A01APP", "svc-acme"), `"machine":"svc-acme"`, `"machine":"svc-acme","people":{"U0ANA":"ana","U0BEN":"ben","U0VIC":"vic"}`, 1)
	client = strings.Replace(client, `"default_agent":"helper",`, `"default_agent":"helper","home":{"project":"Field"},`, 1)
	want(t, f, app, "POST", "/api/clients", client, http.StatusCreated)
	ana := f.person(t, "acme", "ana", "Ana", "humagen-operators")
	vic := f.person(t, "acme", "vic", "Vic", "humagen-operators")
	elsewhere := f.person(t, "other", "vic", "Vic", "humagen-admins")

	place := func(ts string, members string) string {
		return `"ref":{"type":"slack","client":"app","team":"T01ABCDEF","channel":"G01SECRET","ts":"` + ts + `","thread_ts":"1700000000.000200","app_user":"UAPP","audience":{"type":"private_channel","members":[` + members + `]}}`
	}
	say := func(text, ref string) string {
		return `{"messages":[{"role":"user","content":"` + text + `"}],` + ref + `}`
	}
	forAna := func(text, ref string) string {
		return `{"messages":[{"role":"user","content":"` + text + `"}],` + ref + `,"act_as":{"client":"app","user":"U0ANA"}}`
	}
	first := want(t, f, app, "POST", "/api/messages", forAna("<@UAPP> status?", place("1700000000.000200", `"U0ANA","U0BEN"`)), http.StatusCreated)
	root := first["root"].(string)
	for _, r := range sliceOf(obj(first["posted"])["runs"]) {
		ended(t, f, app, obj(r)["id"].(string))
	}
	before := want(t, f, ana, "GET", "/api/sessions/"+root, "", http.StatusOK)
	if got := peopleIn(before); !slices.Equal(got, []string{"ana", "ben"}) {
		t.Fatalf("the root starts with %v", got)
	}
	audience := func() any {
		return want(t, f, ana, "GET", "/api/messages/"+root+"?depth=0", "", http.StatusOK)["audience"]
	}
	if kind := obj(audience())["kind"]; kind != "private" {
		t.Fatalf("the root's audience starts as %v", kind)
	}
	unchanged := func(when string) {
		t.Helper()
		now := want(t, f, ana, "GET", "/api/sessions/"+root, "", http.StatusOK)
		if !slices.Equal(peopleIn(now), peopleIn(before)) || obj(now["thread"])["version"] != obj(before["thread"])["version"] || len(sliceOf(now["messages"])) != len(sliceOf(before["messages"])) {
			t.Fatalf("%s the root has people %v at version %v with %d messages, want %v at %v with %d", when, peopleIn(now), obj(now["thread"])["version"], len(sliceOf(now["messages"])), peopleIn(before), obj(before["thread"])["version"], len(sliceOf(before["messages"])))
		}
		if kind := obj(audience())["kind"]; kind != "private" {
			t.Fatalf("%s the root's audience is %v", when, kind)
		}
	}

	// vic is a member of the project and takes no part in the root.
	want(t, f, vic, "POST", "/api/messages", say("@helper what was said here?", place("1700000000.000900", `"U0VIC"`)), http.StatusNotFound)
	unchanged("after a message from someone who does not read it")
	want(t, f, vic, "GET", "/api/sessions/"+root, "", http.StatusNotFound)
	want(t, f, vic, "GET", "/api/messages/"+root, "", http.StatusNotFound)
	want(t, f, vic, "POST", "/api/messages/"+root, say("@helper and now?", place("1700000000.000950", `"U0VIC"`)), http.StatusNotFound)
	if mine := want(t, f, vic, "GET", "/api/messages?roots=true", "", http.StatusOK); len(sliceOf(mine["messages"])) != 0 {
		t.Fatalf("vic lists roots: %v", mine)
	}

	// Another tenant's caller names the same place: the root is not theirs.
	if code, out := status(t, f, elsewhere, "POST", "/api/messages", say("@helper what was said here?", place("1700000000.000900", `"U0VIC"`))); out["root"] == root {
		t.Fatalf("another tenant's message = %d %v", code, out)
	}
	want(t, f, elsewhere, "GET", "/api/sessions/"+root, "", http.StatusNotFound)
	want(t, f, elsewhere, "POST", "/api/messages/"+root, say("@helper and now?", `"ref":{"type":"web"}`), http.StatusNotFound)
	unchanged("after another tenant's message")

	// A participant's message lands in the root, and the audience it names
	// is not read: ben stays.
	reply := want(t, f, ana, "POST", "/api/messages", say("@helper and the cost?", place("1700000000.000300", `"U0ANA"`)), http.StatusOK)
	if reply["root"] != root {
		t.Fatalf("a participant's message landed in %v, want %s", reply["root"], root)
	}
	for _, r := range sliceOf(obj(reply["posted"])["runs"]) {
		ended(t, f, app, obj(r)["id"].(string))
	}
	if now := want(t, f, ana, "GET", "/api/sessions/"+root, "", http.StatusOK); !slices.Equal(peopleIn(now), []string{"ana", "ben"}) {
		t.Fatalf("after a participant's message the root's people are %v", peopleIn(now))
	}

	// The client's machine reports the conversation's members, and the root
	// follows them: vic joined, ben left.
	next := want(t, f, app, "POST", "/api/messages", forAna("<@UAPP> and now?", place("1700000000.000400", `"U0ANA","U0VIC"`)), http.StatusOK)
	if next["root"] != root {
		t.Fatalf("the machine's message landed in %v, want %s", next["root"], root)
	}
	joined := want(t, f, vic, "GET", "/api/sessions/"+root, "", http.StatusOK)
	if !slices.Equal(peopleIn(joined), []string{"ana", "vic"}) {
		t.Fatalf("after the machine's message the root's people are %v", peopleIn(joined))
	}

	// A place someone named before its thread reached the client: the
	// request's client and audience are not read, the root is the caller's
	// own, and the thread's first message from the machine starts the
	// surface's root beside it.
	other := func(ts, members string) string {
		return strings.Replace(place(ts, members), `"thread_ts":"1700000000.000200"`, `"thread_ts":"1700000001.000100"`, 1)
	}
	ben := f.person(t, "acme", "ben", "Ben", "humagen-operators")
	named := want(t, f, ben, "POST", "/api/messages", say("@helper hello", other("1700000001.000100", `"U0ANA","U0VIC"`)), http.StatusCreated)
	own := named["root"].(string)
	for _, r := range sliceOf(obj(named["posted"])["runs"]) {
		ended(t, f, ben, obj(r)["id"].(string))
	}
	mine := want(t, f, ben, "GET", "/api/sessions/"+own, "", http.StatusOK)
	if !slices.Equal(peopleIn(mine), []string{"ben"}) || mine["project"] == "Field" || obj(mine["thread"])["visibility"] != "private" {
		t.Fatalf("the caller's root has people %v in %v, %v", peopleIn(mine), mine["project"], obj(mine["thread"])["visibility"])
	}
	want(t, f, ana, "GET", "/api/sessions/"+own, "", http.StatusNotFound)
	surfaced := want(t, f, app, "POST", "/api/messages", forAna("<@UAPP> status?", other("1700000001.000100", `"U0ANA","U0VIC"`)), http.StatusCreated)
	theirs := surfaced["root"].(string)
	if theirs == own {
		t.Fatalf("the machine's first message landed in the caller's root %s", own)
	}
	if now := want(t, f, ben, "GET", "/api/sessions/"+own, "", http.StatusOK); !slices.Equal(peopleIn(now), []string{"ben"}) || obj(now["thread"])["version"] != obj(mine["thread"])["version"] {
		t.Fatalf("the caller's root has people %v at version %v, want ben at %v", peopleIn(now), obj(now["thread"])["version"], obj(mine["thread"])["version"])
	}
	if kind := obj(want(t, f, ben, "GET", "/api/messages/"+own+"?depth=0", "", http.StatusOK)["audience"])["kind"]; kind != "direct" {
		t.Fatalf("the caller's root's audience is %v", kind)
	}
	want(t, f, ben, "GET", "/api/sessions/"+theirs, "", http.StatusNotFound)
	if in := want(t, f, ana, "GET", "/api/sessions/"+theirs, "", http.StatusOK); !slices.Equal(peopleIn(in), []string{"ana", "vic"}) {
		t.Fatalf("the surface's root has people %v", peopleIn(in))
	}
}
