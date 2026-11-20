package httpapi

import (
	"net/http"
	"testing"
)

// A Slack app sets a conversation's approval mode for the person its
// client maps, with the person's own checks: the console's toggle and the
// app's control are one route and one rule.
func TestASurfaceSetsApprovalModeOnlyAsTheMappedPerson(t *testing.T) {
	f, _, _ := toolFixture(t)
	app := f.machine(t, "acme", "humagen:runs:read", "humagen:runs:write")
	run := f.ask(t, app, `{"agent":"worker","text":"Open bugs?","act_as":{"client":"slack-app","user":"U0ADA"}}`)
	thread, _ := run["thread"].(string)
	if thread == "" {
		t.Fatalf("ana's run is in no conversation: %v", run)
	}
	path := "/api/threads/" + thread + "/approval"

	// The app alone is no person, so it chooses for nobody.
	if res, out := f.do(t, nil, "PUT", path, app, `{"mode":"auto"}`); res.StatusCode != http.StatusForbidden {
		t.Fatalf("the app set the mode as itself: %d %v", res.StatusCode, out)
	}
	// Someone the client maps to nobody is refused.
	if res, out := f.do(t, nil, "PUT", path, app, `{"mode":"auto","act_as":{"client":"slack-app","user":"U0NOBODY"}}`); res.StatusCode != http.StatusForbidden {
		t.Fatalf("an unmapped person set the mode: %d %v", res.StatusCode, out)
	}
	// Run approves as ana, with the roles the workspace grants her, never
	// the app's.
	res, out := f.do(t, nil, "PUT", path, app, `{"mode":"auto","act_as":{"client":"slack-app","user":"U0ADA"}}`)
	approval, _ := out["approval"].(map[string]any)
	roles, _ := approval["roles"].([]any)
	if res.StatusCode != http.StatusOK || approval["mode"] != "auto" || approval["by"] != "ana" || len(roles) != 1 || roles[0] != "operator" {
		t.Fatalf("ana's choice through the app: %d %v", res.StatusCode, out)
	}
	// Another machine names no client of its own and sets nothing for her.
	other := f.machineAs(t, "another-machine", "acme", "humagen:runs:write")
	if res, out := f.do(t, nil, "PUT", path, other, `{"mode":"ask","act_as":{"client":"slack-app","user":"U0ADA"}}`); res.StatusCode != http.StatusForbidden {
		t.Fatalf("another machine set ana's mode: %d %v", res.StatusCode, out)
	}
	res, out = f.do(t, nil, "PUT", path, app, `{"mode":"ask","act_as":{"client":"slack-app","user":"U0ADA"}}`)
	if res.StatusCode != http.StatusOK || out["approval"] != nil {
		t.Fatalf("back to asking: %d %v", res.StatusCode, out)
	}
}
