package httpapi

import (
	"net/http"
	"testing"
)

// A Slack app answers anyone who can message it, so the server, not only the
// app, holds its credential to starting runs for a person its client maps:
// as them (act_as), or for nobody at their asking (asked_by). An old or
// forged app that asks as itself starts nothing.
func TestASlackAppsCredentialStartsNoRunForNobody(t *testing.T) {
	f, _, _ := toolFixture(t)
	app := f.machine(t, "acme", "humagen:runs:read", "humagen:runs:write")

	for name, body := range map[string]string{
		"as itself":                         `{"agent":"worker","text":"Open bugs?"}`,
		"at the asking of someone unmapped": `{"agent":"worker","text":"Open bugs?","asked_by":{"client":"slack-app","user":"U0NOBODY"}}`,
		"as someone unmapped":               `{"agent":"worker","text":"Open bugs?","act_as":{"client":"slack-app","user":"U0NOBODY"}}`,
		"at the asking of a reader":         `{"agent":"worker","text":"Open bugs?","asked_by":{"client":"slack-app","user":"U0BEN"}}`,
	} {
		if res, out := f.do(t, nil, "POST", "/api/runs", app, body); res.StatusCode != http.StatusForbidden {
			t.Errorf("the app started a run %s: %d %v", name, res.StatusCode, out)
		}
	}
	both := `{"agent":"worker","text":"Open bugs?","act_as":{"client":"slack-app","user":"U0ADA"},"asked_by":{"client":"slack-app","user":"U0ADA"}}`
	if res, out := f.do(t, nil, "POST", "/api/runs", app, both); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("act_as and asked_by together: %d %v", res.StatusCode, out)
	}

	// For a person the client maps, as them or at their asking, it starts.
	if run := f.ask(t, app, `{"agent":"worker","text":"Open bugs?","act_as":{"client":"slack-app","user":"U0ADA"}}`); run["actor"].(map[string]any)["user"] != "ana" {
		t.Fatalf("ana's run acts for %v", run["actor"])
	}
	run := f.ask(t, app, `{"agent":"worker","text":"What did ben say?","asked_by":{"client":"slack-app","user":"U0ADA"}}`)
	if actor, _ := run["actor"].(map[string]any); actor["user"] != nil && actor["user"] != "" {
		t.Fatalf("a run at ana's asking acts for %v, want nobody", actor)
	}
	// The run says ana asked for it, though it acts for nobody.
	if run["requested_by"] != "ana" {
		t.Fatalf("a run at ana's asking was requested by %v", run["requested_by"])
	}

	// Asked again through the inbox or as a call, the credential names
	// nobody, so it starts nothing there either.
	id, _ := run["id"].(string)
	if res, out := f.do(t, nil, "POST", "/api/runs/"+id+"/calls", app, `{"agent":"worker","text":"and more"}`); res.StatusCode != http.StatusForbidden {
		t.Fatalf("the app made a call as itself: %d %v", res.StatusCode, out)
	}
	if res, out := f.do(t, nil, "POST", "/api/assignments/any/run", app, ""); res.StatusCode != http.StatusForbidden {
		t.Fatalf("the app ran an assignment as itself: %d %v", res.StatusCode, out)
	}

	// Any other credential is someone the workspace let in, and asks as itself.
	other := f.machineAs(t, "ci-bot", "acme", "humagen:runs:read", "humagen:runs:write")
	if res, out := f.do(t, nil, "POST", "/api/runs", other, `{"agent":"worker","text":"Open bugs?"}`); res.StatusCode != http.StatusAccepted {
		t.Fatalf("another machine asked as itself: %d %v", res.StatusCode, out)
	}
	// Only a Slack client's machine says who asked: anyone else's asked_by is refused.
	if res, out := f.do(t, nil, "POST", "/api/runs", other, `{"agent":"worker","text":"Open bugs?","asked_by":{"client":"slack-app","user":"U0ADA"}}`); res.StatusCode != http.StatusForbidden {
		t.Fatalf("another machine named ana as its asker: %d %v", res.StatusCode, out)
	}
}
