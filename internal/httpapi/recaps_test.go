package httpapi

import (
	"net/http"
	"slices"
	"testing"
	"time"
)

// signedAs is a machine credential in acme with every permission, for a
// subject a test names, so two people in one workspace can be told apart.
func (f *fixture) signedAs(t *testing.T, subject string) string {
	t.Helper()
	return f.iss.Sign(t, subject, audience, map[string]any{"org": "acme", "scp": everything})
}

// resultsFor returns the result messages a caller's inbox holds about a run.
func resultsFor(t *testing.T, f *fixture, tok, run string) []map[string]any {
	t.Helper()
	res, body := f.do(t, nil, "GET", "/api/inbox?kind=result&state=all", tok, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("inbox = %d %v", res.StatusCode, body)
	}
	var out []map[string]any
	for _, m := range body["messages"].([]any) {
		msg := m.(map[string]any)
		if msg["source"].(map[string]any)["run"] == run {
			out = append(out, msg)
		}
	}
	return out
}

func ended(t *testing.T, f *fixture, tok, id string) {
	t.Helper()
	for range 400 {
		_, run := f.do(t, nil, "GET", "/api/runs/"+id, tok, "")
		if s, _ := run["status"].(string); s == "completed" {
			return
		} else if s == "failed" || s == "stopped" {
			t.Fatalf("run %s %s", id, s)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run %s never ended", id)
}

// A schedule that delivers to the inbox sends each run's answer to the person
// who created it, whoever pressed run now, and to nobody else.
func TestAScheduledAnswerReachesItsCreator(t *testing.T) {
	f := newFixture(t)
	ada, bob := f.signedAs(t, "ada"), f.signedAs(t, "bob")
	f.do(t, nil, "POST", "/api/agents", ada, helperSpec)

	res, sc := f.do(t, nil, "POST", "/api/schedules", ada, `{"name":"digest","expression":"0 3 * * *","agent":"helper","text":"Report.","deliver":"inbox"}`)
	if res.StatusCode != http.StatusCreated || sc["deliver"] != "inbox" {
		t.Fatalf("create = %d %v", res.StatusCode, sc)
	}
	res, run := f.do(t, nil, "POST", "/api/schedules/digest/run", bob, "")
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("run now = %d %v", res.StatusCode, run)
	}
	id := run["id"].(string)
	ended(t, f, ada, id)

	var got []map[string]any
	for range 200 {
		if got = resultsFor(t, f, ada, id); len(got) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(got) != 1 || got[0]["to"] != "ada" || got[0]["subject"] == "" || got[0]["from"].(map[string]any)["name"] != "helper" ||
		len(got[0]["parts"].([]any)) != 1 {
		t.Fatalf("the creator's results = %v", got)
	}
	if other := resultsFor(t, f, bob, id); len(other) != 0 {
		t.Fatalf("someone else was sent the answer: %v", other)
	}

	// Editing the schedule keeps where its answers go only if the edit says
	// so: a field left out of a replace is cleared.
	res, edited := f.do(t, nil, "PUT", "/api/schedules/digest", ada, `{"expression":"0 4 * * *","agent":"helper","text":"Report."}`)
	if res.StatusCode != http.StatusOK || edited["deliver"] != nil {
		t.Fatalf("edit = %d %v", res.StatusCode, edited)
	}
	_, run = f.do(t, nil, "POST", "/api/schedules/digest/run", ada, "")
	quiet := run["id"].(string)
	ended(t, f, ada, quiet)
	time.Sleep(50 * time.Millisecond)
	if got := resultsFor(t, f, ada, quiet); len(got) != 0 {
		t.Fatalf("a schedule that delivers nowhere sent %v", got)
	}
}

func TestAScheduleDeliversOnlyWhereItCan(t *testing.T) {
	f := newFixture(t)
	tok := f.signedAs(t, "ada")
	f.do(t, nil, "POST", "/api/agents", tok, helperSpec)
	f.do(t, nil, "POST", "/api/projects", tok, `{"name":"Launch"}`)
	for name, body := range map[string]string{
		"somewhere else":      `{"name":"a","expression":"0 3 * * *","agent":"helper","text":"x","deliver":"email"}`,
		"a project with none": `{"name":"b","expression":"0 3 * * *","agent":"helper","text":"x","deliver":"project"}`,
	} {
		if res, out := f.do(t, nil, "POST", "/api/schedules", tok, body); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d %v", name, res.StatusCode, out)
		}
	}
	for name, body := range map[string]string{
		"a project":     `{"name":"c","expression":"0 3 * * *","agent":"helper","text":"x","deliver":"project","project":"Launch"}`,
		"the workspace": `{"name":"d","expression":"0 3 * * *","agent":"helper","text":"x","deliver":"project","project":"~"}`,
	} {
		if res, out := f.do(t, nil, "POST", "/api/schedules", tok, body); res.StatusCode != http.StatusCreated || out["deliver"] != "project" {
			t.Errorf("%s = %d %v", name, res.StatusCode, out)
		}
	}
}

// Scheduling in a project is a member's: a viewer may read its schedules
// and not write, pause or run them.
func TestOnlyAMemberSchedulesInAProject(t *testing.T) {
	f := newFixture(t)
	ada := f.signedAs(t, "ada")
	// Bob may do everything but govern projects, which owns them all.
	bob := f.iss.Sign(t, "bob", audience, map[string]any{"org": "acme", "scp": slices.DeleteFunc(slices.Clone(everything), func(s string) bool { return s == "humagen:projects:govern" })})
	f.do(t, nil, "POST", "/api/agents", ada, helperSpec)
	if res, out := f.do(t, nil, "POST", "/api/projects", ada, `{"name":"Field"}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("project = %d %v", res.StatusCode, out)
	}
	if res, out := f.do(t, nil, "PUT", "/api/projects/Field/members", ada,
		`{"version":1,"members":[{"who":"*","role":"viewer"},{"who":"ada","role":"owner"}]}`); res.StatusCode != http.StatusOK {
		t.Fatalf("members = %d %v", res.StatusCode, out)
	}
	recap := `{"name":"field-recap","expression":"41 8 * * 1","agent":"helper","text":"What changed?","project":"Field","deliver":"project"}`
	if res, _ := f.do(t, nil, "POST", "/api/schedules", bob, recap); res.StatusCode != http.StatusForbidden {
		t.Fatalf("a viewer scheduled in Field: %d", res.StatusCode)
	}
	if res, out := f.do(t, nil, "POST", "/api/schedules", ada, recap); res.StatusCode != http.StatusCreated {
		t.Fatalf("an owner's schedule = %d %v", res.StatusCode, out)
	}
	for _, try := range []struct{ method, path, body string }{
		{"POST", "/api/schedules/field-recap/run", ""},
		{"POST", "/api/schedules/field-recap/pause", ""},
		{"PUT", "/api/schedules/field-recap", recap},
		{"DELETE", "/api/schedules/field-recap", ""},
	} {
		if res, _ := f.do(t, nil, try.method, try.path, bob, try.body); res.StatusCode != http.StatusForbidden {
			t.Errorf("a viewer's %s %s = %d", try.method, try.path, res.StatusCode)
		}
	}
	if res, _ := f.do(t, nil, "GET", "/api/schedules", bob, ""); res.StatusCode != http.StatusOK {
		t.Errorf("a viewer cannot list schedules: %d", res.StatusCode)
	}
}
