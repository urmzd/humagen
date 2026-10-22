package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/warp/pkg/feedback"
	"github.com/urmzd/warp/pkg/people"
)

func TestFeedbackReferencesItsContextAndScopesTheRun(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	if res, out := f.do(t, nil, "POST", "/api/agents", admin, helperSpec); res.StatusCode != http.StatusCreated {
		t.Fatalf("agent: %d %v", res.StatusCode, out)
	}
	run := f.ask(t, admin, `{"agent":"helper","text":"Explain the change"}`)
	path := "/api/runs/" + run["id"].(string) + "/feedback"
	res, got := f.do(t, nil, "PUT", path, admin, `{"rating":"down","reason":"incomplete","comment":"Please include sources"}`)
	if res.StatusCode != http.StatusOK || got["run"] != run["id"] || got["comment"] != "Please include sources" {
		t.Fatalf("feedback: %d %v", res.StatusCode, got)
	}
	for _, field := range []string{"prompt", "answer"} {
		if _, exists := got[field]; exists {
			t.Fatalf("feedback duplicates %s", field)
		}
	}
	// Context comes from the referenced run, through its existing read checks.
	res, context := f.do(t, nil, "GET", "/api/runs/"+got["run"].(string), admin, "")
	if res.StatusCode != http.StatusOK || context["text"] != "Explain the change" || context["answer"] != run["answer"] {
		t.Fatalf("context: %d %v", res.StatusCode, context)
	}
	if res, out := f.do(t, nil, "GET", path, admin, ""); res.StatusCode != http.StatusOK || len(out["feedback"].([]any)) != 1 {
		t.Fatalf("collect: %d %v", res.StatusCode, out)
	}
	for _, method := range []string{"GET", "PUT"} {
		if res, _ := f.do(t, nil, method, path, f.machine(t, "globex", "warp:runs:read"), `{"rating":"up"}`); res.StatusCode != http.StatusNotFound {
			t.Fatalf("another tenant %s: %d", method, res.StatusCode)
		}
	}
	for _, body := range []string{`{"rating":"bad"}`, `{"rating":"up","prompt":"forged"}`, `{"rating":"up","subject":"someone-else"}`} {
		if res, _ := f.do(t, nil, "PUT", path, admin, body); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("accepted %s: %d", body, res.StatusCode)
		}
	}
}

func TestSlackConnectionControlsActOnlyForTheirMappedPerson(t *testing.T) {
	f, _, _ := toolFixture(t)
	app := f.machine(t, "acme", "warp:runs:read", "warp:runs:write")
	path := "/api/me/credentials/issues?act_as_client=slack-app&act_as_user=U0ADA"
	res, out := f.do(t, nil, "PUT", path, app, `{"ref":"env:WARP_PERSON_ANA_ISSUES"}`)
	if res.StatusCode != http.StatusOK || out["connected"] != true {
		t.Fatalf("connect as mapped person: %d %v", res.StatusCode, out)
	}
	if res, out := f.do(t, nil, "PUT", path, app, `{"ref":"env:WARP_PERSON_BEN_ISSUES"}`); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("accepted someone else's credential: %d %v", res.StatusCode, out)
	}
	if res, out := f.do(t, nil, "DELETE", path, app, ""); res.StatusCode != http.StatusNoContent {
		t.Fatalf("disconnect: %d %v", res.StatusCode, out)
	}
	if res, _ := f.do(t, nil, "PUT", path, f.machineAs(t, "another-machine", "acme", "warp:runs:write"), `{"ref":"env:WARP_PERSON_ANA_ISSUES"}`); res.StatusCode == http.StatusOK {
		t.Fatal("another machine changed a connection")
	}
}

// Anyone who may read a run reads its feedback, so a rater is named as the
// platform calls them, never by a subject that holds an address, and a page
// is found by a cursor that is not a subject.
func TestFeedbackNamesRatersAndNeverShowsAnAddress(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	if res, out := f.do(t, nil, "POST", "/api/agents", admin, helperSpec); res.StatusCode != http.StatusCreated {
		t.Fatalf("agent: %d %v", res.StatusCode, out)
	}
	run := f.ask(t, admin, `{"agent":"helper","text":"Explain the change"}`)
	path := "/api/runs/" + run["id"].(string) + "/feedback"
	const address = "ada@example.com"
	if _, err := f.p.People.Seen("acme", people.Sighting{Subject: address, Name: "Ada Lovelace", Source: people.SignIn}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Feedback.Save("acme", feedback.Record{Run: run["id"].(string), Subject: address, Rating: "up"}); err != nil {
		t.Fatal(err)
	}
	if res, out := f.do(t, nil, "PUT", path, admin, `{"rating":"down"}`); res.StatusCode != http.StatusOK || out["mine"] != true || out["subject"] != nil {
		t.Fatalf("own feedback: %d %v", res.StatusCode, out)
	}
	res, got := f.do(t, nil, "GET", path, admin, "")
	raw, _ := json.Marshal(got)
	out := string(raw)
	if res.StatusCode != http.StatusOK || strings.Contains(out, address) || strings.Contains(out, `"subject"`) {
		t.Fatalf("the list shows a subject: %d %s", res.StatusCode, out)
	}
	if !strings.Contains(out, `"by_name":"Ada Lovelace"`) || !strings.Contains(out, `"mine":false`) {
		t.Fatalf("the list does not name the rater: %s", out)
	}
	for _, bad := range []string{address, "0", "-1", "x"} {
		if res, _ := f.do(t, nil, "GET", path+"?after="+bad, admin, ""); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("after=%s: %d", bad, res.StatusCode)
		}
	}
}
