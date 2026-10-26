package httpapi

import (
	"strings"
	"testing"
)

func TestSavedProfileNamesNewMessagesAndBriefsRealTurnsWithoutChangingIdentity(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	want(t, f, owner, "POST", "/api/agents", helperSpec, 201)
	person := f.person(t, "acme", "profile-person", "Identity Provider Name", "humagen-admins")
	project := "~"
	body := `{"display_name":"Nadia","theme":"system","accent":"purple","personal":{"briefing":"Start with the conclusion","notifications":{},"quiet_start":"","quiet_end":"","timezone":"UTC","remember":false,"use_personal_memory_in_projects":false}}`
	want(t, f, person, "PUT", "/api/preferences", body, 200)
	_, saved := f.do(t, nil, "GET", "/api/preferences", person, "")
	if saved["display_name"] != "Nadia" {
		t.Fatal("saved name did not survive reload")
	}
	_, identity := f.do(t, nil, "GET", "/api/me", person, "")
	principal := identity["principal"].(map[string]any)
	if principal["subject"] != "profile-person" || principal["name"] != "Identity Provider Name" {
		t.Fatalf("profile rewrote authoritative identity: %+v", principal)
	}
	response, posted := f.do(t, nil, "POST", "/api/threads", person, `{"project":"`+project+`","agents":["helper"],"text":"Hello"}`)
	if response.StatusCode != 201 {
		t.Fatalf("thread: %d %+v", response.StatusCode, posted)
	}
	message := posted["message"].(map[string]any)
	if message["name"] != "Nadia" {
		t.Fatalf("new message used old profile name: %+v", message)
	}
	started := posted["runs"].([]any)[0].(map[string]any)
	run, err := f.p.Runs.Get("acme", started["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if run.Actor.Display != "Nadia" || !strings.Contains(run.Tag().Text, "Start with the conclusion") {
		t.Fatalf("real turn ignored saved profile: %+v", run.Actor)
	}
	if run.Tag().Meta["_humagen.context.personal_memory"] != "" {
		t.Fatal("basic profile formatting classified as private memory")
	}
}
