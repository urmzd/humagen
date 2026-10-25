package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/usage"
)

var everything = []string{
	"humagen:agents:read", "humagen:agents:write", "humagen:runs:read", "humagen:runs:write",
	"humagen:schedules:read", "humagen:schedules:write", "humagen:alerts:read", "humagen:alerts:write",
	"humagen:knowledge:read", "humagen:knowledge:write", "humagen:evals:read", "humagen:evals:write",
	"humagen:billing:read", "humagen:billing:write", "humagen:artifacts:read", "humagen:artifacts:write",
	"humagen:clients:read", "humagen:clients:write", "humagen:projects:read", "humagen:projects:write", "humagen:projects:govern",
	"humagen:people:read",
}

func TestObservabilityIsConfinedToATenant(t *testing.T) {
	f := newFixture(t)
	acme, globex := f.machine(t, "acme", everything...), f.machine(t, "globex", everything...)

	f.do(t, nil, "POST", "/api/agents", acme, helperSpec)
	_, run := f.do(t, nil, "POST", "/api/runs", acme, `{"agent":"helper","text":"hi"}`)
	id := run["id"].(string)
	if res, _ := f.do(t, nil, "POST", "/api/alert-rules", acme, `{"name":"any","kind":"run_failed","severity":"critical"}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("create rule = %d", res.StatusCode)
	}
	if res, _ := f.do(t, nil, "POST", "/api/knowledge", acme, `{"name":"handbook"}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("create source = %d", res.StatusCode)
	}
	if res, _ := f.do(t, nil, "POST", "/api/knowledge/handbook/documents", acme, `{"title":"Releases","text":"The release bot tags every release."}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("add document = %d", res.StatusCode)
	}

	// The run's own tenant can see it all, once the run has had time to end.
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, out := f.do(t, nil, "GET", "/api/journal?kind=lifecycle.completed", acme, "")
		if len(out["entries"].([]any)) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("journal never showed the run completing: %v", out)
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, trace := f.do(t, nil, "GET", "/api/runs/"+id+"/trace", acme, "")
	if len(trace["spans"].([]any)) == 0 {
		t.Fatalf("trace has no spans: %v", trace)
	}
	_, stats := f.do(t, nil, "GET", "/api/analytics?window=1h", acme, "")
	if stats["total"].(map[string]any)["runs"].(float64) != 1 {
		t.Fatalf("analytics = %v", stats["total"])
	}
	_, hits := f.do(t, nil, "GET", "/api/knowledge/search?q=release+bot", acme, "")
	if len(hits["hits"].([]any)) != 1 {
		t.Fatalf("search = %v", hits)
	}

	// The other tenant sees none of it.
	for path, key := range map[string]string{
		"/api/journal":                    "entries",
		"/api/alert-rules":                "rules",
		"/api/alerts":                     "alerts",
		"/api/knowledge":                  "sources",
		"/api/knowledge/search?q=release": "hits",
	} {
		_, out := f.do(t, nil, "GET", path, globex, "")
		if got := out[key].([]any); len(got) != 0 {
			t.Errorf("other tenant GET %s returned %v", path, got)
		}
	}
	_, other := f.do(t, nil, "GET", "/api/analytics?window=1h", globex, "")
	if other["total"].(map[string]any)["runs"].(float64) != 0 {
		t.Errorf("other tenant's analytics counted the run: %v", other["total"])
	}
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/runs/" + id + "/trace"},
		{"GET", "/api/knowledge/handbook/documents"},
		{"GET", "/api/knowledge/search?q=release&source=handbook"},
		{"POST", "/api/knowledge/handbook/documents"},
		{"DELETE", "/api/knowledge/handbook"},
		{"DELETE", "/api/alert-rules/any"},
		{"POST", "/api/alerts/al-1/ack"},
	} {
		body := ""
		if tc.method == "POST" {
			body = `{"title":"x","text":"y"}`
		}
		if res, _ := f.do(t, nil, tc.method, tc.path, globex, body); res.StatusCode != http.StatusNotFound {
			t.Errorf("other tenant %s %s = %d, want 404", tc.method, tc.path, res.StatusCode)
		}
	}
}

func TestObservabilityPermissions(t *testing.T) {
	f := newFixture(t)
	viewer := f.signIn(t, "humagen-viewers")
	for _, tc := range []struct{ path, body string }{
		{"/api/alert-rules", `{"name":"any","kind":"run_failed","severity":"critical"}`},
		{"/api/knowledge", `{"name":"handbook"}`},
		{"/api/schedules/nightly/pause", ``},
		{"/api/schedules/nightly/run", ``},
	} {
		if res, _ := f.do(t, viewer, "POST", tc.path, "", tc.body); res.StatusCode != http.StatusForbidden {
			t.Errorf("viewer POST %s = %d, want 403", tc.path, res.StatusCode)
		}
	}
	for _, path := range []string{"/api/alerts", "/api/alert-rules", "/api/knowledge", "/api/analytics", "/api/journal"} {
		if res, _ := f.do(t, viewer, "GET", path, "", ""); res.StatusCode != http.StatusOK {
			t.Errorf("viewer GET %s = %d, want 200", path, res.StatusCode)
		}
	}
	if res, _ := f.do(t, viewer, "GET", "/api/analytics?window=9999h", "", ""); res.StatusCode != http.StatusBadRequest {
		t.Errorf("an unbounded window = %d, want 400", res.StatusCode)
	}
}

func TestScheduleLifecycle(t *testing.T) {
	f := newFixture(t)
	tok := f.machine(t, "acme", everything...)
	f.do(t, nil, "POST", "/api/agents", tok, helperSpec)
	res, sc := f.do(t, nil, "POST", "/api/schedules", tok, `{"name":"nightly","expression":"0 2 * * *","agent":"helper","text":"report"}`)
	if res.StatusCode != http.StatusCreated || len(sc["next"].([]any)) != 3 || sc["paused"] != false {
		t.Fatalf("create = %d %v", res.StatusCode, sc)
	}

	_, paused := f.do(t, nil, "POST", "/api/schedules/nightly/pause", tok, "")
	if paused["paused"] != true || len(paused["next"].([]any)) != 0 {
		t.Fatalf("a paused schedule still shows upcoming runs: %v", paused)
	}

	// Run now works while paused, and the run is tied back to the schedule.
	res, run := f.do(t, nil, "POST", "/api/schedules/nightly/run", tok, "")
	if res.StatusCode != http.StatusAccepted || run["schedule"] != "nightly" || run["origin"] != "manual" {
		t.Fatalf("run now = %d %v", res.StatusCode, run)
	}
	_, list := f.do(t, nil, "GET", "/api/schedules", tok, "")
	first := list["schedules"].([]any)[0].(map[string]any)
	if len(first["recent"].([]any)) != 1 {
		t.Fatalf("the schedule does not list its run: %v", first)
	}

	_, resumed := f.do(t, nil, "POST", "/api/schedules/nightly/resume", tok, "")
	if resumed["paused"] != false || len(resumed["next"].([]any)) != 3 {
		t.Fatalf("resume = %v", resumed)
	}
}

// lastRun polls a suite until its newest run has ended.
func (f *fixture) lastRun(t *testing.T, tok, suite string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		_, out := f.do(t, nil, "GET", "/api/evals/"+suite+"/runs", tok, "")
		if runs, _ := out["runs"].([]any); len(runs) > 0 {
			if run := runs[0].(map[string]any); run["status"] != "running" {
				return run
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("eval %s never finished: %v", suite, out)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAgentEval(t *testing.T) {
	f := newFixture(t)
	tok := f.machine(t, "acme", everything...)
	f.do(t, nil, "POST", "/api/agents", tok, helperSpec)
	f.do(t, nil, "POST", "/api/alert-rules", tok, `{"name":"quality","kind":"eval_below","agent":"helper","threshold":100,"severity":"warning"}`)

	// The offline provider names itself in its answer, so one case can pass
	// and one can fail without a model.
	suite := `{"name":"smoke","target":"agent","subject":"helper","cases":[
		{"id":"says-offline","input":"hello","checks":[{"kind":"contains","value":"offline provider"}]},
		{"id":"claims-a-model","input":"hello","checks":[{"kind":"contains","value":"I am a large language model"}]}
	]}`
	if res, out := f.do(t, nil, "POST", "/api/evals", tok, suite); res.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d %v", res.StatusCode, out)
	}
	res, started := f.do(t, nil, "POST", "/api/evals/smoke/run", tok, "")
	if res.StatusCode != http.StatusAccepted || started["rev"].(float64) != 1 {
		t.Fatalf("run = %d %v", res.StatusCode, started)
	}

	run := f.lastRun(t, tok, "smoke")
	if run["status"] != "failed" || run["passed"].(float64) != 1 || run["pass_rate"].(float64) != 50 {
		t.Fatalf("run = %v", run)
	}
	results := run["results"].([]any)
	first, second := results[0].(map[string]any), results[1].(map[string]any)
	if first["passed"] != true || second["passed"] != false || first["run_id"] == "" || first["output"] == "" {
		t.Fatalf("results = %v", results)
	}

	// The case's run is a real run, tagged as coming from an eval.
	_, agentRun := f.do(t, nil, "GET", "/api/runs/"+first["run_id"].(string), tok, "")
	if agentRun["origin"] != "eval" {
		t.Fatalf("the case's run has origin %v", agentRun["origin"])
	}
	// Falling under the threshold raised the alert.
	_, alerts := f.do(t, nil, "GET", "/api/alerts", tok, "")
	if list := alerts["alerts"].([]any); len(list) != 1 || list[0].(map[string]any)["suite"] != "smoke" {
		t.Fatalf("alerts = %v", alerts)
	}
}

func TestKnowledgeEval(t *testing.T) {
	f := newFixture(t)
	tok := f.machine(t, "acme", everything...)
	f.do(t, nil, "POST", "/api/knowledge", tok, `{"name":"handbook"}`)
	f.do(t, nil, "POST", "/api/knowledge/handbook/documents", tok, `{"title":"Releases","text":"The release bot tags every release."}`)
	f.do(t, nil, "POST", "/api/knowledge/handbook/documents", tok, `{"title":"On call","text":"The on-call engineer acknowledges a page in five minutes."}`)

	suite := `{"name":"retrieval","target":"knowledge","subject":"handbook","k":1,"cases":[
		{"id":"release","input":"who tags a release","expect":["Releases"]},
		{"id":"paging","input":"how fast must a page be acknowledged","expect":["On call"]},
		{"id":"wrong","input":"who tags a release","expect":["On call"]}
	]}`
	if res, out := f.do(t, nil, "POST", "/api/evals", tok, suite); res.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d %v", res.StatusCode, out)
	}
	f.do(t, nil, "POST", "/api/evals/retrieval/run", tok, "")
	run := f.lastRun(t, tok, "retrieval")
	if run["passed"].(float64) != 2 || run["total"].(float64) != 3 {
		t.Fatalf("run = %v", run)
	}
	m := run["metrics"].(map[string]any)
	if got := m["hit_rate"].(float64); got < 0.66 || got > 0.67 {
		t.Fatalf("hit rate = %v, want 2 of 3", got)
	}
	first := run["results"].([]any)[0].(map[string]any)
	if first["rank"].(float64) != 1 || len(first["hits"].([]any)) != 1 {
		t.Fatalf("first case = %v", first)
	}
}

func TestEvalsAreConfinedAndGuarded(t *testing.T) {
	f := newFixture(t)
	acme, globex := f.machine(t, "acme", everything...), f.machine(t, "globex", everything...)
	f.do(t, nil, "POST", "/api/agents", acme, helperSpec)
	suite := `{"name":"smoke","target":"agent","subject":"helper","cases":[{"id":"c","input":"hi","checks":[{"kind":"contains","value":"x"}]}]}`
	f.do(t, nil, "POST", "/api/evals", acme, suite)

	if _, out := f.do(t, nil, "GET", "/api/evals", globex, ""); len(out["suites"].([]any)) != 0 {
		t.Errorf("other tenant listed suites: %v", out)
	}
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/evals/smoke/runs", ""},
		{"POST", "/api/evals/smoke/run", ""},
		{"DELETE", "/api/evals/smoke", ""},
		{"PUT", "/api/evals/smoke", suite},
		// The other tenant has no agent called helper to point a suite at.
		{"POST", "/api/evals", suite},
	} {
		if res, _ := f.do(t, nil, tc.method, tc.path, globex, tc.body); res.StatusCode != http.StatusNotFound {
			t.Errorf("other tenant %s %s = %d, want 404", tc.method, tc.path, res.StatusCode)
		}
	}
	viewer := f.signIn(t, "humagen-viewers")
	if res, _ := f.do(t, viewer, "POST", "/api/evals/smoke/run", "", ""); res.StatusCode != http.StatusForbidden {
		t.Errorf("a viewer ran an eval: %d", res.StatusCode)
	}
	if res, _ := f.do(t, nil, "GET", "/api/agents/helper/history", globex, ""); res.StatusCode != http.StatusNotFound {
		t.Errorf("other tenant read the agent's history: %d", res.StatusCode)
	}
	_, hist := f.do(t, nil, "GET", "/api/agents/helper/history", acme, "")
	if len(hist["revisions"].([]any)) != 1 {
		t.Errorf("history = %v", hist)
	}
}

func TestConversationsBranchAndPick(t *testing.T) {
	f := newFixture(t)
	tok := f.machine(t, "acme", everything...)
	f.do(t, nil, "POST", "/api/agents", tok, helperSpec)
	f.do(t, nil, "POST", "/api/agents", tok, `{"name":"releaser","description":"Cuts releases and tags versions.","model":"offline","provider":"offline","system_prompt":"x"}`)

	// A new workspace's default is @auto. With no default that names an
	// agent, a message nothing matches goes nowhere: tag one.
	if res, out := f.do(t, nil, "POST", "/api/runs", tok, `{"text":"what is the weather"}`); res.StatusCode != http.StatusConflict || out["error"] != "no agent fits; tag one" {
		t.Fatalf("@auto with nothing to fall back to = %d %v, want 409", res.StatusCode, out)
	}
	if res, _ := f.do(t, nil, "PUT", "/api/projects/~/agents", tok, `{"version":0,"enable":["*"],"default":"ghost"}`); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a workspace default that is not an agent = %d, want 400", res.StatusCode)
	}
	// The workspace default names helper; a project below it uses @auto,
	// which falls back to helper.
	f.do(t, nil, "PUT", "/api/projects/~/agents", tok, `{"version":0,"enable":["*"],"default":"helper"}`)

	// The first message starts a thread and is auto-picked by description.
	_, first := f.do(t, nil, "POST", "/api/runs", tok, `{"agent":"auto","text":"tag the next release"}`)
	if first["agent"] != "releaser" || first["picked"] == "" || first["thread"] != first["id"] {
		t.Fatalf("first = %v", first)
	}
	_, weather := f.do(t, nil, "POST", "/api/runs", tok, `{"text":"what is the weather"}`)
	if weather["agent"] != "helper" || weather["picked"] != nil {
		t.Fatalf("an untagged message, so the workspace default should answer: %v", weather)
	}
	_, auto := f.do(t, nil, "POST", "/api/runs", tok, `{"agent":"auto","text":"what is the weather"}`)
	if auto["agent"] != "helper" || !strings.Contains(fmt.Sprint(auto["picked"]), "the default answered") {
		t.Fatalf("@auto with no match should hand the message to the default: %v", auto)
	}
	id := first["id"].(string)

	// A follow-up by a different agent stays in the thread, and the record
	// shows what the person typed, not the replayed conversation.
	_, second := f.do(t, nil, "POST", "/api/runs", tok, `{"agent":"helper","text":"and write the changelog","after":"`+id+`"}`)
	if second["thread"] != id || second["prev"] != id || second["text"] != "and write the changelog" {
		t.Fatalf("second = %v", second)
	}
	// Answering the same point again is a branch.
	_, branch := f.do(t, nil, "POST", "/api/runs", tok, `{"agent":"releaser","text":"and write the changelog","after":"`+id+`"}`)
	if branch["prev"] != id || branch["id"] == second["id"] {
		t.Fatalf("branch = %v", branch)
	}

	_, thread := f.do(t, nil, "GET", "/api/threads/"+id, tok, "")
	if len(thread["turns"].([]any)) != 3 {
		t.Fatalf("thread = %v", thread)
	}
	_, threads := f.do(t, nil, "GET", "/api/threads", tok, "")
	var summary map[string]any
	for _, s := range threads["threads"].([]any) {
		if s.(map[string]any)["id"] == id {
			summary = s.(map[string]any)
		}
	}
	if summary["turns"].(float64) != 3 || summary["branches"].(float64) != 1 || summary["title"] != "tag the next release" {
		t.Fatalf("summary = %v", summary)
	}

	// A project files a conversation, and a follow-up keeps it.
	if res, _ := f.do(t, nil, "POST", "/api/runs", tok, `{"agent":"helper","text":"hi","project":"Ghost"}`); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("an unknown project = %d, want 400", res.StatusCode)
	}
	if res, out := f.do(t, nil, "PUT", "/api/settings", tok, `{"history":"none","history_limit":5000}`); res.StatusCode != http.StatusOK {
		t.Fatalf("settings = %d %v", res.StatusCode, out)
	}
	if res, out := f.do(t, nil, "POST", "/api/projects", tok, `{"name":"Launch"}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("project = %d %v", res.StatusCode, out)
	}
	for _, bad := range []string{
		`{"history":"everything"}`, `{"history_limit":5}`, `{"projects":[{"name":"Launch"}]}`,
	} {
		if res, _ := f.do(t, nil, "PUT", "/api/settings", tok, bad); res.StatusCode != http.StatusBadRequest {
			t.Errorf("settings %s = %d, want 400", bad, res.StatusCode)
		}
	}
	_, filed := f.do(t, nil, "POST", "/api/runs", tok, `{"agent":"helper","text":"kickoff","project":"Launch"}`)
	_, next := f.do(t, nil, "POST", "/api/runs", tok, `{"agent":"helper","text":"then what","project":"Other","after":"`+filed["id"].(string)+`"}`)
	if filed["project"] != "Launch" || next["project"] != "Launch" {
		t.Fatalf("project: first %v, follow-up %v", filed["project"], next["project"])
	}

	_, choice := f.do(t, nil, "GET", "/api/pick?q=cut+a+release", tok, "")
	if choice["agent"] != "releaser" {
		t.Fatalf("pick = %v", choice)
	}

	// Another tenant cannot read the thread or continue it.
	globex := f.machine(t, "globex", everything...)
	if res, _ := f.do(t, nil, "GET", "/api/threads/"+id, globex, ""); res.StatusCode != http.StatusNotFound {
		t.Errorf("other tenant read the thread: %d", res.StatusCode)
	}
	if res, _ := f.do(t, nil, "POST", "/api/runs", globex, `{"agent":"helper","text":"hi","after":"`+id+`"}`); res.StatusCode != http.StatusNotFound {
		t.Errorf("other tenant continued the thread: %d", res.StatusCode)
	}
}

func TestPreferencesAreTheCallersOwn(t *testing.T) {
	f := newFixture(t)
	ada := f.signIn(t, "humagen-viewers")
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 32)

	res, out := f.do(t, ada, "PUT", "/api/preferences", "", `{"display_name":"Ada","theme":"light","accent":"teal"}`)
	if res.StatusCode != http.StatusOK || out["theme"] != "light" {
		t.Fatalf("set = %d %v", res.StatusCode, out)
	}
	if res, _ := f.do(t, ada, "PUT", "/api/preferences", "", `{"display_name":"Ada","theme":"neon","accent":"teal"}`); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a bad theme = %d", res.StatusCode)
	}
	if res, _ := f.do(t, ada, "PUT", "/api/preferences/photo", "", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("an svg was accepted as a photo: %d", res.StatusCode)
	}
	if res, _ := f.do(t, ada, "PUT", "/api/preferences/photo", "", png); res.StatusCode != http.StatusOK {
		t.Fatalf("a png was refused: %d", res.StatusCode)
	}

	req, _ := http.NewRequest("GET", f.srv.URL+"/api/preferences/photo", nil)
	got, err := ada.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = got.Body.Close()
	if got.Header.Get("Content-Type") != "image/png" || got.Header.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(got.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("photo headers = %v", got.Header)
	}

	// A machine in the same tenant is a different caller and sees its own.
	svc := f.machine(t, "acme", everything...)
	if _, other := f.do(t, nil, "GET", "/api/preferences", svc, ""); other["display_name"] != "" || other["has_photo"] != false {
		t.Fatalf("another caller sees them: %v", other)
	}
	if res, _ := f.do(t, nil, "GET", "/api/preferences/photo", svc, ""); res.StatusCode != http.StatusNotFound {
		t.Fatalf("another caller read the photo: %d", res.StatusCode)
	}
}

func TestAgentMemory(t *testing.T) {
	f := newFixture(t)
	acme, globex := f.machine(t, "acme", everything...), f.machine(t, "globex", everything...)
	f.do(t, nil, "POST", "/api/agents", acme, helperSpec)

	res, note := f.do(t, nil, "POST", "/api/agents/helper/memory", acme, `{"text":"Releases go out on Tuesdays."}`)
	if res.StatusCode != http.StatusCreated || note["source"] != "person" {
		t.Fatalf("add = %d %v", res.StatusCode, note)
	}
	if res, _ := f.do(t, nil, "POST", "/api/agents/helper/memory", acme, `{"text":"   "}`); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("an empty note = %d", res.StatusCode)
	}
	if res, _ := f.do(t, nil, "POST", "/api/agents/ghost/memory", acme, `{"text":"x"}`); res.StatusCode != http.StatusNotFound {
		t.Fatalf("a note for an agent that does not exist = %d", res.StatusCode)
	}
	// Another tenant has no such agent, so no such memory.
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/agents/helper/memory", ""},
		{"POST", "/api/agents/helper/memory", `{"text":"x"}`},
		{"DELETE", "/api/agents/helper/memory/" + note["id"].(string), ""},
	} {
		if res, _ := f.do(t, nil, tc.method, tc.path, globex, tc.body); res.StatusCode != http.StatusNotFound {
			t.Errorf("other tenant %s %s = %d, want 404", tc.method, tc.path, res.StatusCode)
		}
	}
	if res, out := f.do(t, nil, "POST", "/api/agents/helper/memory", acme, `{"text":"Reach Ada at ada@example.com."}`); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a note with an email address = %d %v", res.StatusCode, out)
	}
	if _, out := f.do(t, nil, "GET", "/api/memory/pending", globex, ""); len(out["proposals"].([]any)) != 0 {
		t.Errorf("other tenant sees proposals: %v", out)
	}
	if res, _ := f.do(t, nil, "POST", "/api/agents/helper/memory/approve", globex, `{"ids":["m1"]}`); res.StatusCode != http.StatusNotFound {
		t.Errorf("other tenant approved memory: %d", res.StatusCode)
	}
	reader := f.machine(t, "acme", "humagen:agents:read")
	if res, _ := f.do(t, nil, "POST", "/api/agents/helper/memory/approve", reader, `{"ids":["m1"]}`); res.StatusCode != http.StatusForbidden {
		t.Errorf("a reader approved memory: %d", res.StatusCode)
	}
	if res, _ := f.do(t, nil, "POST", "/api/agents/helper/memory", reader, `{"text":"x"}`); res.StatusCode != http.StatusForbidden {
		t.Errorf("a reader wrote memory: %d", res.StatusCode)
	}
	_, list := f.do(t, nil, "GET", "/api/agents/helper/memory", acme, "")
	if len(list["notes"].([]any)) != 1 {
		t.Fatalf("notes = %v", list)
	}
	if res, _ := f.do(t, nil, "DELETE", "/api/agents/helper/memory/"+note["id"].(string), acme, ""); res.StatusCode != http.StatusNoContent {
		t.Fatalf("remove = %d", res.StatusCode)
	}
}

func TestProjects(t *testing.T) {
	f := newFixture(t)
	acme, globex := f.machine(t, "acme", everything...), f.machine(t, "globex", everything...)
	f.do(t, nil, "POST", "/api/agents", acme, helperSpec)

	if res, out := f.do(t, nil, "POST", "/api/projects", acme, `{"name":"Launch","description":"Q4"}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d %v", res.StatusCode, out)
	}
	if res, _ := f.do(t, nil, "POST", "/api/projects", acme, `{"name":"launch"}`); res.StatusCode != http.StatusConflict {
		t.Fatalf("a duplicate, differing only in case = %d", res.StatusCode)
	}
	if res, _ := f.do(t, nil, "POST", "/api/projects", acme, `{"name":"<b>"}`); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a bad name = %d", res.StatusCode)
	}

	// Several conversations, a schedule and a shared note, all in the project.
	_, one := f.do(t, nil, "POST", "/api/runs", acme, `{"agent":"helper","text":"kickoff","project":"Launch"}`)
	f.do(t, nil, "POST", "/api/runs", acme, `{"agent":"helper","text":"risks","project":"Launch"}`)
	res, sc := f.do(t, nil, "POST", "/api/schedules", acme, `{"name":"standup","expression":"0 9 * * *","agent":"helper","text":"status","project":"Launch"}`)
	if res.StatusCode != http.StatusCreated || sc["project"] != "Launch" {
		t.Fatalf("schedule = %d %v", res.StatusCode, sc)
	}
	if res, _ := f.do(t, nil, "POST", "/api/schedules", acme, `{"name":"x","expression":"0 9 * * *","agent":"helper","text":"s","project":"Ghost"}`); res.StatusCode != http.StatusNotFound {
		t.Fatalf("a schedule in a project that does not exist = %d", res.StatusCode)
	}
	// A run the schedule raises is filed under its project.
	_, fired := f.do(t, nil, "POST", "/api/schedules/standup/run", acme, "")
	if fired["project"] != "Launch" {
		t.Fatalf("the schedule's run has project %v", fired["project"])
	}
	if res, note := f.do(t, nil, "POST", "/api/projects/Launch/memory", acme, `{"text":"The launch date is the first Tuesday of November."}`); res.StatusCode != http.StatusCreated || note["status"] != "approved" {
		t.Fatalf("note = %d %v", res.StatusCode, note)
	}

	_, list := f.do(t, nil, "GET", "/api/projects", acme, "")
	p := list["projects"].([]any)[0].(map[string]any)
	if p["conversations"].(float64) != 3 || p["schedules"].(float64) != 1 || p["notes"].(float64) != 1 {
		t.Fatalf("project = %v", p)
	}

	// The next turn in the project is sent what the project knows. A turn
	// outside it is not.
	_, inside := f.do(t, nil, "POST", "/api/runs", acme, `{"agent":"helper","text":"when is it","after":"`+one["id"].(string)+`"}`)
	_, outside := f.do(t, nil, "POST", "/api/runs", acme, `{"agent":"helper","text":"when is it"}`)
	sent := func(id any) string {
		r, err := f.p.Runs.Get("acme", id.(string))
		if err != nil {
			t.Fatal(err)
		}
		return r.Tag().Text
	}
	if !strings.Contains(sent(inside["id"]), "first Tuesday of November") {
		t.Fatalf("a turn in the project was not sent its shared memory: %q", sent(inside["id"]))
	}
	if strings.Contains(sent(outside["id"]), "November") {
		t.Fatal("a turn outside the project was sent its memory")
	}

	// Another tenant has no such project.
	if _, other := f.do(t, nil, "GET", "/api/projects", globex, ""); len(other["projects"].([]any)) != 0 {
		t.Errorf("other tenant lists projects: %v", other)
	}
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/projects/Launch/memory", ""},
		{"POST", "/api/projects/Launch/memory", `{"text":"x"}`},
		{"POST", "/api/projects/Launch/memory/approve", `{"ids":["m1"]}`},
		{"DELETE", "/api/projects/Launch", ""},
	} {
		if res, _ := f.do(t, nil, tc.method, tc.path, globex, tc.body); res.StatusCode != http.StatusNotFound {
			t.Errorf("other tenant %s %s = %d, want 404", tc.method, tc.path, res.StatusCode)
		}
	}
	viewer := f.signIn(t, "humagen-viewers")
	if res, _ := f.do(t, viewer, "POST", "/api/projects", "", `{"name":"Mine"}`); res.StatusCode != http.StatusForbidden {
		t.Errorf("a viewer made a project: %d", res.StatusCode)
	}

	// A project a schedule files its runs in is kept until the schedule
	// goes. Deleting it then drops its shared memory and keeps its
	// conversations.
	if res, out := f.do(t, nil, "DELETE", "/api/projects/Launch", acme, ""); res.StatusCode != http.StatusConflict || !strings.Contains(out["error"].(string), "the schedule standup") {
		t.Fatalf("delete with a schedule in it = %d %v", res.StatusCode, out)
	}
	f.do(t, nil, "DELETE", "/api/schedules/standup", acme, "")
	if res, _ := f.do(t, nil, "DELETE", "/api/projects/Launch", acme, ""); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d", res.StatusCode)
	}
	if got := f.p.Memory.List("acme", "project:Launch"); len(got) != 0 {
		t.Fatalf("the project's memory outlived it: %v", got)
	}
	if res, _ := f.do(t, nil, "GET", "/api/threads/"+one["id"].(string), acme, ""); res.StatusCode != http.StatusOK {
		t.Fatalf("a conversation was lost with its project: %d", res.StatusCode)
	}
}

func TestThreadsPage(t *testing.T) {
	f := newFixture(t)
	acme := f.machine(t, "acme", everything...)
	f.do(t, nil, "POST", "/api/agents", acme, helperSpec)
	for _, text := range []string{"one", "two", "three"} {
		f.do(t, nil, "POST", "/api/runs", acme, `{"agent":"helper","text":"`+text+`"}`)
		time.Sleep(5 * time.Millisecond)
	}

	// With no parameters the old shape still works, and next is present.
	_, all := f.do(t, nil, "GET", "/api/threads", acme, "")
	if len(all["threads"].([]any)) != 3 {
		t.Fatalf("threads = %v", all)
	}
	if next, present := all["next"]; !present || next != nil {
		t.Fatalf("next = %v (present %v), want null", next, present)
	}

	_, first := f.do(t, nil, "GET", "/api/threads?limit=2&origin=api", acme, "")
	cursor, _ := first["next"].(string)
	if len(first["threads"].([]any)) != 2 || cursor == "" {
		t.Fatalf("first page = %v", first)
	}
	_, second := f.do(t, nil, "GET", "/api/threads?limit=2&origin=api&before="+cursor, acme, "")
	if len(second["threads"].([]any)) != 1 || second["next"] != nil {
		t.Fatalf("second page = %v", second)
	}
	if _, none := f.do(t, nil, "GET", "/api/threads?origin=cron", acme, ""); len(none["threads"].([]any)) != 0 {
		t.Fatalf("origin filter = %v", none)
	}
	for _, bad := range []string{"limit=0", "limit=many", "standing=sleepy", "before=not-a-cursor!"} {
		if res, _ := f.do(t, nil, "GET", "/api/threads?"+bad, acme, ""); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", bad, res.StatusCode)
		}
	}
	// A cursor carries no access: another tenant using it sees only its own.
	globex := f.machine(t, "globex", everything...)
	if _, other := f.do(t, nil, "GET", "/api/threads?before="+cursor, globex, ""); len(other["threads"].([]any)) != 0 {
		t.Fatalf("another tenant paged into these threads: %v", other)
	}
}

func TestBillingPolicies(t *testing.T) {
	f := newFixture(t)
	acme, globex := f.machine(t, "acme", everything...), f.machine(t, "globex", everything...)
	f.do(t, nil, "POST", "/api/agents", acme, helperSpec)
	f.do(t, nil, "POST", "/api/projects", acme, `{"name":"Launch"}`)

	res, tenantPolicy := f.do(t, nil, "PUT", "/api/policies", acme, `{"scope":{"kind":"tenant"},"period":"day","max_runs":100}`)
	if res.StatusCode != http.StatusOK || tenantPolicy["version"].(float64) != 1 || tenantPolicy["action"] != "block" {
		t.Fatalf("tenant policy = %d %v", res.StatusCode, tenantPolicy)
	}
	for name, body := range map[string]string{
		"more than the tenant allows":   `{"scope":{"kind":"project","name":"Launch"},"period":"day","max_runs":500}`,
		"a project that does not exist": `{"scope":{"kind":"project","name":"Ghost"},"period":"day","max_runs":5}`,
		"an agent that does not exist":  `{"scope":{"kind":"agent","name":"ghost"},"period":"day","max_runs":5}`,
		"no limits":                     `{"scope":{"kind":"tenant"},"period":"day"}`,
		"approval, which is not built":  `{"scope":{"kind":"tenant"},"period":"day","max_runs":5,"action":"approve"}`,
	} {
		if res, _ := f.do(t, nil, "PUT", "/api/policies", acme, body); res.StatusCode != http.StatusBadRequest && res.StatusCode != http.StatusNotFound {
			t.Errorf("%s = %d", name, res.StatusCode)
		}
	}
	if res, _ := f.do(t, nil, "PUT", "/api/policies", acme, `{"scope":{"kind":"project","name":"Launch"},"period":"day","max_runs":2}`); res.StatusCode != http.StatusOK {
		t.Fatalf("project policy = %d", res.StatusCode)
	}

	// Two runs fit in the project. The third is refused with the reason, the
	// policy and its version, and when the count starts again.
	for range 2 {
		if res, out := f.do(t, nil, "POST", "/api/runs", acme, `{"agent":"helper","text":"go","project":"Launch"}`); res.StatusCode != http.StatusAccepted {
			t.Fatalf("a run under the limit = %d %v", res.StatusCode, out)
		}
	}
	res, refused := f.do(t, nil, "POST", "/api/runs", acme, `{"agent":"helper","text":"go","project":"Launch"}`)
	if res.StatusCode != http.StatusTooManyRequests || refused["limit"] != "runs" || refused["remaining"].(float64) != 0 || refused["resets_at"] == nil {
		t.Fatalf("refusal = %d %v", res.StatusCode, refused)
	}
	pol := refused["policy"].(map[string]any)
	if pol["version"].(float64) != 1 || pol["scope"].(map[string]any)["name"] != "Launch" {
		t.Fatalf("the refusal names the wrong policy: %v", pol)
	}
	// Outside the project the run is held only to the tenant's limit.
	if res, _ := f.do(t, nil, "POST", "/api/runs", acme, `{"agent":"helper","text":"go"}`); res.StatusCode != http.StatusAccepted {
		t.Fatalf("a run outside the project = %d", res.StatusCode)
	}

	_, usage := f.do(t, nil, "GET", "/api/usage", acme, "")
	var launch map[string]any
	for _, s := range usage["scopes"].([]any) {
		if s.(map[string]any)["scope"].(map[string]any)["kind"] == "project" {
			launch = s.(map[string]any)
		}
	}
	if launch["state"] != "over" || launch["runs"].(map[string]any)["used"].(float64) != 2 || launch["spend"].(map[string]any)["metered"] != true {
		t.Fatalf("usage = %v", launch)
	}
	_, notices := f.do(t, nil, "GET", "/api/usage/notices", acme, "")
	if first := notices["notices"].([]any)[0].(map[string]any); first["kind"] != "refused" || first["version"].(float64) != 1 {
		t.Fatalf("notices = %v", notices)
	}
	_, alerts := f.do(t, nil, "GET", "/api/alerts", acme, "")
	found := false
	for _, a := range alerts["alerts"].([]any) {
		found = found || a.(map[string]any)["kind"] == "budget"
	}
	if !found {
		t.Fatalf("no budget alert was raised: %v", alerts)
	}

	// Every change is on record with who, before and after.
	f.do(t, nil, "PUT", "/api/policies", acme, `{"scope":{"kind":"project","name":"Launch"},"period":"day","max_runs":5}`)
	_, history := f.do(t, nil, "GET", "/api/policies/history", acme, "")
	latest := history["changes"].([]any)[0].(map[string]any)
	if latest["by"] != "svc-acme" || latest["before"].(map[string]any)["max_runs"].(float64) != 2 || latest["after"].(map[string]any)["version"].(float64) != 2 {
		t.Fatalf("history = %v", latest)
	}
	// Raising the limit lets the next run in.
	if res, _ := f.do(t, nil, "POST", "/api/runs", acme, `{"agent":"helper","text":"go","project":"Launch"}`); res.StatusCode != http.StatusAccepted {
		t.Fatalf("after raising the limit = %d", res.StatusCode)
	}

	// Another tenant sees none of it, and only an admin may change a policy.
	for path, key := range map[string]string{"/api/policies": "policies", "/api/policies/history": "changes", "/api/usage": "scopes", "/api/usage/notices": "notices"} {
		if _, out := f.do(t, nil, "GET", path, globex, ""); len(out[key].([]any)) != 0 {
			t.Errorf("other tenant GET %s = %v", path, out)
		}
	}
	if res, _ := f.do(t, nil, "DELETE", "/api/policies?kind=project&name=Launch", globex, ""); res.StatusCode != http.StatusNotFound {
		t.Errorf("other tenant deleted the policy: %d", res.StatusCode)
	}
	viewer := f.signIn(t, "humagen-viewers")
	if res, _ := f.do(t, viewer, "GET", "/api/usage", "", ""); res.StatusCode != http.StatusOK {
		t.Errorf("a viewer cannot read usage: %d", res.StatusCode)
	}
	if res, _ := f.do(t, viewer, "PUT", "/api/policies", "", `{"scope":{"kind":"tenant"},"period":"day","max_runs":1}`); res.StatusCode != http.StatusForbidden {
		t.Errorf("a viewer changed a policy: %d", res.StatusCode)
	}
	if res, _ := f.do(t, nil, "DELETE", "/api/policies?kind=project&name=Launch", acme, ""); res.StatusCode != http.StatusNoContent {
		t.Errorf("delete = %d", res.StatusCode)
	}
}

func TestOnboardingFlags(t *testing.T) {
	f := newFixture(t)
	admin, viewer := f.signIn(t, "humagen-admins"), f.signIn(t, "humagen-viewers")

	_, me := f.do(t, admin, "GET", "/api/me", "", "")
	if me["first_admin"] != true {
		t.Fatalf("an admin in a workspace nobody set up: first_admin = %v", me["first_admin"])
	}
	if _, v := f.do(t, viewer, "GET", "/api/me", "", ""); v["first_admin"] != false {
		t.Fatalf("a viewer was called the first admin")
	}
	if _, prefs := f.do(t, admin, "GET", "/api/preferences", "", ""); prefs["onboarded_at"] != nil {
		t.Fatalf("onboarded before onboarding: %v", prefs)
	}

	// Only an admin names the workspace, and the server says who and when.
	if res, _ := f.do(t, viewer, "PUT", "/api/settings", "", `{"workspace_name":"Mine"}`); res.StatusCode != http.StatusForbidden {
		t.Fatalf("a viewer set the workspace up: %d", res.StatusCode)
	}
	res, saved := f.do(t, admin, "PUT", "/api/settings", "", `{"workspace_name":" Acme Robotics ","setup_by":"someone-else","setup_at":"2001-01-01T00:00:00Z"}`)
	if res.StatusCode != http.StatusOK || saved["workspace_name"] != "Acme Robotics" || saved["setup_by"] != "00u-ada" {
		t.Fatalf("setup = %d %v", res.StatusCode, saved)
	}
	first := saved["setup_at"]
	if first == nil || strings.HasPrefix(first.(string), "2001") {
		t.Fatalf("setup_at came from the client: %v", first)
	}
	if res, _ := f.do(t, admin, "PUT", "/api/settings", "", `{"workspace_name":"<b>x</b>"}`); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("markup in a workspace name = %d", res.StatusCode)
	}
	// A later write does not move it, and nobody is the first admin any more.
	_, again := f.do(t, admin, "PUT", "/api/settings", "", `{"workspace_name":"Acme"}`)
	if again["setup_at"] != first || again["setup_by"] != "00u-ada" {
		t.Fatalf("setup moved: %v", again)
	}
	if _, me := f.do(t, admin, "GET", "/api/me", "", ""); me["first_admin"] != false {
		t.Fatal("still the first admin after setup")
	}
	if _, read := f.do(t, viewer, "GET", "/api/settings", "", ""); read["workspace_name"] != "Acme" {
		t.Fatalf("a viewer cannot read the workspace name: %v", read)
	}

	// Onboarding is per person, set once, with the server's time.
	_, done := f.do(t, admin, "PUT", "/api/preferences", "", `{"display_name":"Ada","theme":"dark","accent":"purple","onboarded":true}`)
	at := done["onboarded_at"]
	if at == nil || done["display_name"] != "Ada" {
		t.Fatalf("onboarding = %v", done)
	}
	_, later := f.do(t, admin, "PUT", "/api/preferences", "", `{"display_name":"Ada L","theme":"dark","accent":"purple"}`)
	if later["onboarded_at"] != at {
		t.Fatalf("a later save moved or cleared onboarded_at: %v", later)
	}
	// Both sign-ins in this fixture are the same person, so the other caller
	// here is a machine in the same workspace.
	if _, other := f.do(t, nil, "GET", "/api/preferences", f.machine(t, "acme", everything...), ""); other["onboarded_at"] != nil {
		t.Fatal("one caller finishing onboarding marked another")
	}
}

// A workspace whose spend for the period has reached its limit refuses a new
// run with 402, naming the limit, the policy and its version, and when the
// count resets. Another workspace's spending never shows in this one's usage
// or touches its limits, and a run's usage is on its record.
func TestASpendRefusalNamesTheLimitPolicyAndReset(t *testing.T) {
	f := newFixture(t)
	acme, globex := f.machine(t, "acme", everything...), f.machine(t, "globex", everything...)
	f.do(t, nil, "POST", "/api/agents", acme, helperSpec)
	f.do(t, nil, "POST", "/api/agents", globex, helperSpec)
	f.do(t, nil, "PUT", "/api/policies", acme, `{"scope":{"kind":"tenant"},"period":"month","spend_limit":5000,"max_tokens":100000}`)
	f.do(t, nil, "PUT", "/api/policies", globex, `{"scope":{"kind":"tenant"},"period":"month","spend_limit":5000}`)

	// globex spends past its limit; acme is untouched by it.
	if _, err := f.p.Usage.Append(usage.Entry{Kind: usage.Spent, Tenant: "globex", Agent: "helper", RunID: "r-g", Key: "r-g/a1/1",
		At: time.Now(), Spend: &usage.Spend{Micros: 9000, ModelCalls: 1, Tokens: 500}}); err != nil {
		t.Fatal(err)
	}
	res, run := f.do(t, nil, "POST", "/api/runs", acme, `{"agent":"helper","text":"go"}`)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("acme was refused for globex's spending: %d %v", res.StatusCode, run)
	}
	_, acmeUsage := f.do(t, nil, "GET", "/api/usage", acme, "")
	spend := acmeUsage["scopes"].([]any)[0].(map[string]any)["spend"].(map[string]any)
	if spend["used"].(float64) != 0 || spend["metered"] != true {
		t.Fatalf("acme's spend shows another workspace's: %v", spend)
	}
	if res, _ := f.do(t, nil, "POST", "/api/runs", globex, `{"agent":"helper","text":"go"}`); res.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("globex past its spend limit = %d", res.StatusCode)
	}

	// The run's usage is on its record once it has ended.
	id := run["id"].(string)
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, got := f.do(t, nil, "GET", "/api/runs/"+id, acme, "")
		if u, ok := got["usage"].(map[string]any); ok && got["status"] == "completed" {
			if u["total"].(map[string]any)["model_calls"].(float64) != 1 {
				t.Fatalf("usage = %v", u)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run's usage never arrived: %v", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if res, _ := f.do(t, nil, "GET", "/api/runs/"+id, globex, ""); res.StatusCode != http.StatusNotFound {
		t.Fatalf("another workspace reading the run = %d", res.StatusCode)
	}

	// acme spends to its limit.
	if _, err := f.p.Usage.Append(usage.Entry{Kind: usage.Spent, Tenant: "acme", Agent: "helper", RunID: "r-a", Key: "r-a/a1/1",
		At: time.Now(), Spend: &usage.Spend{Micros: 5000, ModelCalls: 1, Tokens: 500}}); err != nil {
		t.Fatal(err)
	}
	res, refused := f.do(t, nil, "POST", "/api/runs", acme, `{"agent":"helper","text":"again"}`)
	if res.StatusCode != http.StatusPaymentRequired || refused["limit"] != "spend" || refused["remaining"].(float64) != 0 {
		t.Fatalf("refusal = %d %v", res.StatusCode, refused)
	}
	pol := refused["policy"].(map[string]any)
	if pol["version"].(float64) != 1 || pol["period"] != "month" || pol["scope"].(map[string]any)["kind"] != "tenant" {
		t.Fatalf("the refusal names the wrong policy: %v", pol)
	}
	resets, err := time.Parse(time.RFC3339, refused["resets_at"].(string))
	if err != nil || !resets.Equal(policy.PeriodEnd(policy.Month, time.Now())) {
		t.Fatalf("resets_at = %v (%v)", refused["resets_at"], err)
	}
}

// alertsFor polls until a rule has raised an alert for a run, and returns
// every alert raised for that run by then.
func (f *fixture) alertsFor(t *testing.T, tok, runID, rule string) map[string]bool {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		_, out := f.do(t, nil, "GET", "/api/alerts", tok, "")
		raised := map[string]bool{}
		for _, a := range out["alerts"].([]any) {
			if a := a.(map[string]any); a["run_id"] == runID {
				raised[a["rule"].(string)] = true
			}
		}
		if raised[rule] {
			return raised
		}
		if time.Now().After(deadline) {
			t.Fatalf("rule %s never raised an alert for %s: %v", rule, runID, out)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAlertRuleConfiguration(t *testing.T) {
	f := newFixture(t)
	acme, globex := f.machine(t, "acme", everything...), f.machine(t, "globex", everything...)
	f.do(t, nil, "POST", "/api/agents", acme, helperSpec)
	f.do(t, nil, "POST", "/api/projects", acme, `{"name":"Launch"}`)

	// Every run is slower than a nanosecond, so "control" speaks for every
	// run, and a rule that stays quiet beside it is known to have been asked.
	if res, _ := f.do(t, nil, "POST", "/api/alert-rules", acme, `{"name":"control","kind":"slow_run","threshold":1e-9,"severity":"warning"}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("control = %d", res.StatusCode)
	}
	res, rule := f.do(t, nil, "POST", "/api/alert-rules", acme, `{"name":"slow","kind":"slow_run","threshold":1e-9,"severity":"warning",
		"description":"Anything slow in the launch","project":"Launch","agent":"helper","paused":true,"cooldown_seconds":3600,
		"notify":["role:operator"," ada "],"created_by":"mallory","updated_by":"mallory"}`)
	if res.StatusCode != http.StatusCreated || rule["created_by"] == "mallory" || rule["created_at"] == nil || rule["updated_by"] != nil ||
		rule["paused"] != true || rule["project"] != "Launch" || rule["notify"].([]any)[1] != "ada" {
		t.Fatalf("create = %d %v", res.StatusCode, rule)
	}
	creator := rule["created_by"]

	run := func(body string) string {
		t.Helper()
		_, r := f.do(t, nil, "POST", "/api/runs", acme, body)
		return r["id"].(string)
	}

	// Paused, the rule says nothing.
	id := run(`{"agent":"helper","text":"hi","project":"Launch"}`)
	if raised := f.alertsFor(t, acme, id, "control"); raised["slow"] {
		t.Fatalf("a paused rule fired: %v", raised)
	}

	// Refusals: who may change a rule, which rule, and what it may become.
	viewer := f.signIn(t, "humagen-viewers")
	enabled := `{"kind":"slow_run","threshold":1e-9,"severity":"critical","project":"Launch","cooldown_seconds":3600}`
	if res, _ := f.do(t, viewer, "PUT", "/api/alert-rules/slow", "", enabled); res.StatusCode != http.StatusForbidden {
		t.Errorf("viewer PUT = %d, want 403", res.StatusCode)
	}
	for _, tc := range []struct {
		tok, path, body string
		want            int
	}{
		{globex, "/api/alert-rules/slow", enabled, http.StatusNotFound},
		{acme, "/api/alert-rules/ghost", enabled, http.StatusNotFound},
		{acme, "/api/alert-rules/slow", `{"name":"renamed","kind":"run_failed","severity":"critical"}`, http.StatusBadRequest},
		{acme, "/api/alert-rules/slow", `{"kind":"spend","window_seconds":60,"severity":"critical"}`, http.StatusBadRequest},
		{acme, "/api/alert-rules/slow", `{"kind":"run_failed","severity":"critical","cooldown_seconds":99999999}`, http.StatusBadRequest},
		{acme, "/api/alert-rules/slow", `{"kind":"run_failed","severity":"critical","notify":["role:"]}`, http.StatusBadRequest},
		{acme, "/api/alert-rules/slow", `{"kind":"run_failed","severity":"critical","agent":"ghost"}`, http.StatusNotFound},
		{acme, "/api/alert-rules/slow", `{"kind":"run_failed","severity":"critical","project":"Ghost"}`, http.StatusNotFound},
		{acme, "/api/alert-rules", `{"name":"lost","kind":"run_failed","severity":"critical","project":"Ghost"}`, http.StatusNotFound},
	} {
		method := "PUT"
		if tc.path == "/api/alert-rules" {
			method = "POST"
		}
		if res, out := f.do(t, nil, method, tc.path, tc.tok, tc.body); res.StatusCode != tc.want {
			t.Errorf("%s %s %s = %d %v, want %d", method, tc.path, tc.body, res.StatusCode, out, tc.want)
		}
	}
	_, list := f.do(t, nil, "GET", "/api/alert-rules", acme, "")
	for _, r := range list["rules"].([]any) {
		if r := r.(map[string]any); r["name"] == "slow" && (r["paused"] != true || r["agent"] != "helper") {
			t.Fatalf("a refused edit changed the rule: %v", r)
		}
	}

	// Enabled and widened to every agent in the project.
	res, rule = f.do(t, nil, "PUT", "/api/alert-rules/slow", acme, enabled)
	if res.StatusCode != http.StatusOK || rule["paused"] != nil || rule["severity"] != "critical" || rule["agent"] != nil ||
		rule["created_by"] != creator || rule["updated_by"] == nil || rule["updated_at"] == nil || rule["description"] != nil {
		t.Fatalf("update = %d %v", res.StatusCode, rule)
	}

	// Outside the project, the rule says nothing.
	id = run(`{"agent":"helper","text":"hi"}`)
	if raised := f.alertsFor(t, acme, id, "control"); raised["slow"] {
		t.Fatalf("a run outside the project fired the project's rule: %v", raised)
	}
	// In it, the rule fires once, and its cooldown holds the next.
	id = run(`{"agent":"helper","text":"hi","project":"Launch"}`)
	f.alertsFor(t, acme, id, "slow")
	id = run(`{"agent":"helper","text":"hi again","project":"Launch"}`)
	if raised := f.alertsFor(t, acme, id, "control"); raised["slow"] {
		t.Fatalf("a rule cooling down fired: %v", raised)
	}
}

func TestScheduleEdit(t *testing.T) {
	f := newFixture(t)
	acme, globex := f.machine(t, "acme", everything...), f.machine(t, "globex", everything...)
	f.do(t, nil, "POST", "/api/agents", acme, helperSpec)
	f.do(t, nil, "POST", "/api/projects", acme, `{"name":"Launch"}`)
	res, sc := f.do(t, nil, "POST", "/api/schedules", acme, `{"name":"nightly","expression":"0 2 * * *","agent":"helper","text":"report","description":"The nightly report"}`)
	if res.StatusCode != http.StatusCreated || sc["description"] != "The nightly report" || sc["updated_by"] != nil {
		t.Fatalf("create = %d %v", res.StatusCode, sc)
	}
	creator := sc["created_by"]
	f.do(t, nil, "POST", "/api/schedules/nightly/pause", acme, "")
	if res, _ := f.do(t, nil, "POST", "/api/schedules/nightly/run", acme, ""); res.StatusCode != http.StatusAccepted {
		t.Fatalf("run now = %d", res.StatusCode)
	}

	edit := `{"expression":"30 6 * * 1-5","agent":"helper","text":"weekday report","project":"Launch","description":"Weekdays"}`
	viewer := f.signIn(t, "humagen-viewers")
	if res, _ := f.do(t, viewer, "PUT", "/api/schedules/nightly", "", edit); res.StatusCode != http.StatusForbidden {
		t.Errorf("viewer PUT = %d, want 403", res.StatusCode)
	}
	for _, tc := range []struct {
		tok, path, body string
		want            int
	}{
		{globex, "/api/schedules/nightly", edit, http.StatusNotFound},
		{acme, "/api/schedules/ghost", edit, http.StatusNotFound},
		{acme, "/api/schedules/nightly", `{"name":"daily","expression":"0 2 * * *","agent":"helper","text":"r"}`, http.StatusBadRequest},
		{acme, "/api/schedules/nightly", `{"expression":"whenever","agent":"helper","text":"r"}`, http.StatusBadRequest},
		{acme, "/api/schedules/nightly", `{"expression":"0 2 * * *","agent":"helper","text":"r","description":"` + strings.Repeat("a", 501) + `"}`, http.StatusBadRequest},
		{acme, "/api/schedules/nightly", `{"expression":"0 2 * * *","agent":"ghost","text":"r"}`, http.StatusNotFound},
		{acme, "/api/schedules/nightly", `{"expression":"0 2 * * *","agent":"helper","text":"r","project":"Ghost"}`, http.StatusNotFound},
	} {
		if res, out := f.do(t, nil, "PUT", tc.path, tc.tok, tc.body); res.StatusCode != tc.want {
			t.Errorf("PUT %s %s = %d %v, want %d", tc.path, tc.body, res.StatusCode, out, tc.want)
		}
	}
	_, list := f.do(t, nil, "GET", "/api/schedules", acme, "")
	if got := list["schedules"].([]any)[0].(map[string]any); got["expression"] != "0 2 * * *" || got["text"] != "report" || got["description"] != "The nightly report" {
		t.Fatalf("a refused edit changed the schedule: %v", got)
	}

	res, sc = f.do(t, nil, "PUT", "/api/schedules/nightly", acme, edit)
	if res.StatusCode != http.StatusOK || sc["expression"] != "30 6 * * 1-5" || sc["text"] != "weekday report" ||
		sc["project"] != "Launch" || sc["description"] != "Weekdays" || sc["paused"] != true || len(sc["next"].([]any)) != 0 ||
		sc["created_by"] != creator || sc["updated_by"] == nil || sc["updated_at"] == nil || len(sc["recent"].([]any)) != 1 {
		t.Fatalf("edit = %d %v", res.StatusCode, sc)
	}
	_, resumed := f.do(t, nil, "POST", "/api/schedules/nightly/resume", acme, "")
	next := resumed["next"].([]any)
	if len(next) != 3 || !strings.HasSuffix(next[0].(string), "T06:30:00Z") {
		t.Fatalf("resumed = %v", resumed)
	}
	// Leaving the project out takes the schedule out of it.
	_, sc = f.do(t, nil, "PUT", "/api/schedules/nightly", acme, `{"name":"nightly","expression":"0 2 * * *","agent":"helper","text":"report"}`)
	if sc["project"] != nil || sc["description"] != nil || sc["paused"] != false {
		t.Fatalf("edit back = %v", sc)
	}
}
