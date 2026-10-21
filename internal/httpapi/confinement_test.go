package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/people"
)

// The confinement sweep reads every GET in the contract, and the event
// stream, as someone who must not see what a workspace holds, and checks that
// none of it reaches them:
//
//   - a read that names a hidden object answers exactly as one that names an
//     object that does not exist: the same status, 404 where the contract
//     documents it, and the same body;
//   - a list answers with nothing hidden anywhere in it, and a filter that
//     names a hidden object answers as one that names nothing;
//   - the stream carries no hidden run's events and no hidden inbox notice,
//     while what the reader may see does arrive, so the sweep cannot pass by
//     delivering nothing.
//
// Today the outsider is another workspace. When projects and threads can be
// hidden inside one, the same sweep runs as a member of the workspace who is
// not a member of the hidden project.

// marker is in every hidden name and text. It holds letters that are not hex,
// so no id or digest can contain it by chance.
const marker = "zqx"

// hidden is what one workspace holds that an outsider must not see.
type hidden struct {
	project, agent, schedule, rule, source, eval, client, variant string
	run, thread, message, artifact, agentNote, projectNote, doc   string
	handle                                                        string
}

// hide fills the acme workspace with one of everything, each named or
// written with the marker.
func hide(t *testing.T, f *fixture, owner string) hidden {
	t.Helper()
	h := hidden{
		project: "Canary" + marker, agent: "canary-" + marker, schedule: "canary-" + marker, rule: "canary-" + marker,
		source: "canary-" + marker, eval: "canary-" + marker, client: "canary-" + marker, variant: "lean-" + marker,
	}
	must := func(method, path, body string, want int) map[string]any {
		t.Helper()
		res, out := f.do(t, nil, method, path, owner, body)
		if res.StatusCode != want {
			t.Fatalf("%s %s = %d %v", method, path, res.StatusCode, out)
		}
		return out
	}
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return s }

	must("POST", "/api/agents", `{"name":"`+h.agent+`","model":"offline","provider":"offline","system_prompt":"Prompt `+marker+`."}`, http.StatusCreated)
	must("POST", "/api/projects", `{"name":"`+h.project+`","description":"Project `+marker+`."}`, http.StatusCreated)
	h.projectNote = str(must("POST", "/api/projects/"+h.project+"/memory", `{"text":"Project note `+marker+`."}`, http.StatusCreated), "id")
	h.agentNote = str(must("POST", "/api/agents/"+h.agent+"/memory", `{"text":"Agent note `+marker+`."}`, http.StatusCreated), "id")
	must("PUT", "/api/agents/"+h.agent+"/call-policy", `{"callers":"anyone","approval":"never"}`, http.StatusOK)

	run := must("POST", "/api/runs", `{"agent":"`+h.agent+`","text":"Run `+marker+`.","project":"`+h.project+`"}`, http.StatusAccepted)
	h.run, h.thread = str(run, "id"), str(run, "thread")
	ended(t, f, owner, h.run)

	if err := f.p.PutFile("acme", h.thread, "plan-"+marker+".txt", "Plan "+marker+"."); err != nil {
		t.Fatal(err)
	}
	published, err := f.p.Publish(t.Context(), platform.Publication{
		Tenant: "acme", Thread: h.thread, Run: h.run, Agent: h.agent, Project: h.project, By: "agent:" + h.agent, Path: "plan-" + marker + ".txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	h.artifact = published.ID

	must("POST", "/api/schedules", `{"name":"`+h.schedule+`","expression":"0 3 * * *","agent":"`+h.agent+`","text":"Schedule `+marker+`.","project":"`+h.project+`","deliver":"inbox"}`, http.StatusCreated)
	must("POST", "/api/alert-rules", `{"name":"`+h.rule+`","kind":"run_failed","severity":"critical","description":"Rule `+marker+`."}`, http.StatusCreated)
	must("POST", "/api/knowledge", `{"name":"`+h.source+`","description":"Source `+marker+`."}`, http.StatusCreated)
	h.doc = str(must("POST", "/api/knowledge/"+h.source+"/documents", `{"title":"Doc `+marker+`","text":"Words `+marker+` here."}`, http.StatusCreated), "id")
	must("POST", "/api/knowledge/"+h.source+"/variants", `{"name":"`+h.variant+`"}`, http.StatusCreated)
	must("POST", "/api/evals", `{"name":"`+h.eval+`","target":"agent","subject":"`+h.agent+`","cases":[{"id":"c-`+marker+`","input":"hi","checks":[{"kind":"contains","value":"`+marker+`"}]}]}`, http.StatusCreated)
	must("POST", "/api/clients", slackApp(h.client, "A01ZQX", "svc-acme"), http.StatusCreated)
	person, err := f.p.People.Seen("acme", people.Sighting{Subject: "okta|canary-" + marker, Name: "Canary " + marker, Source: people.SignIn}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	h.handle = person.Handle

	// A refusal at a limit raises an inbox message about the hidden agent.
	must("PUT", "/api/policies", `{"scope":{"kind":"agent","name":"`+h.agent+`"},"period":"day","max_runs":1}`, http.StatusOK)
	must("POST", "/api/runs", `{"agent":"`+h.agent+`","text":"Over `+marker+`."}`, http.StatusTooManyRequests)
	_, page := f.do(t, nil, "GET", "/api/inbox?state=all", owner, "")
	for _, m := range page["messages"].([]any) {
		if msg := m.(map[string]any); msg["source"].(map[string]any)["agent"] == h.agent {
			h.message = msg["id"].(string)
		}
	}
	if h.message == "" {
		t.Fatalf("the hidden agent raised no inbox message: %v", page)
	}
	return h
}

// value is the hidden object a path parameter names, in one operation's path.
func (h hidden) value(path, param string) (string, bool) {
	prefix := func(p string) bool { return strings.HasPrefix(path, p) }
	switch param {
	case "name":
		switch {
		case prefix("/api/agents/"):
			return h.agent, true
		case prefix("/api/projects/"):
			return h.project, true
		case prefix("/api/schedules/"):
			return h.schedule, true
		case prefix("/api/alert-rules/"):
			return h.rule, true
		case prefix("/api/knowledge/"):
			return h.source, true
		case prefix("/api/evals/"):
			return h.eval, true
		}
	case "id":
		switch {
		case prefix("/api/agents/{name}/memory/"):
			return h.agentNote, true
		case prefix("/api/projects/{name}/memory/"):
			return h.projectNote, true
		case prefix("/api/knowledge/{name}/documents/"):
			return h.doc, true
		case prefix("/api/runs/"):
			return h.run, true
		case prefix("/api/threads/"):
			return h.thread, true
		case prefix("/api/inbox/"):
			return h.message, true
		case prefix("/api/artifacts/"):
			return h.artifact, true
		case prefix("/api/clients/"):
			return h.client, true
		}
	case "handle":
		return h.handle, true
	case "variant":
		return h.variant, true
	case "client":
		return h.client, true
	case "part":
		return "p1", true
	case "question":
		return "q1", true
	}
	return "", false
}

// probes are the query parameters that name an object, and the hidden one
// each names.
func (h hidden) probes() map[string]string {
	return map[string]string{
		"project": h.project, "thread": h.thread, "run": h.run, "agent": h.agent,
		"source": h.source, "variant": h.variant, "q": "Canary" + marker,
	}
}

// ghost is a value shaped like v that names nothing: its last character
// changed.
func ghost(v string) string {
	if v == "" {
		return "ghost"
	}
	last := v[len(v)-1]
	switch {
	case last >= '0' && last < '9', last >= 'a' && last < 'z', last >= 'A' && last < 'Z':
		last++
	case last == '9':
		last = '0'
	case last == 'z':
		last = 'a'
	case last == 'Z':
		last = 'A'
	default:
		return v + "0"
	}
	return v[:len(v)-1] + string(last)
}

// queryParams are an operation's query parameters, with those it requires,
// following references into the shared parameters.
func queryParams(s *apiSpec, op specOp) (all []string, required []string) {
	for _, p := range sliceOf(op.raw["parameters"]) {
		param := obj(p)
		if ref, isRef := param["$ref"].(string); isRef {
			param = obj(obj(obj(s.doc["components"])["parameters"])[strings.TrimPrefix(ref, "#/components/parameters/")])
		}
		if param["in"] != "query" {
			continue
		}
		name, _ := param["name"].(string)
		all = append(all, name)
		if param["required"] == true {
			required = append(required, name)
		}
	}
	return all, required
}

// timestamps differ between two reads made a moment apart, and say nothing
// about what exists.
var timestamps = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`)

func get(t *testing.T, f *fixture, tok, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, f.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(raw)
}

// sameAsNothing fails unless reading path with the hidden value answers as
// reading it with the ghost.
func sameAsNothing(t *testing.T, f *fixture, tok, what, hiddenPath, ghostPath string, swaps map[string]string) (int, string) {
	t.Helper()
	code, body := get(t, f, tok, hiddenPath)
	ghostCode, ghostBody := get(t, f, tok, ghostPath)
	norm := func(s string) string {
		for real, g := range swaps {
			s = strings.ReplaceAll(s, real, g)
			s = strings.ReplaceAll(s, url.QueryEscape(real), url.QueryEscape(g))
		}
		return timestamps.ReplaceAllString(s, "T")
	}
	if code != ghostCode || norm(body) != norm(ghostBody) {
		t.Errorf("%s: a hidden object answers %d %s, and one that does not exist %d %s", what, code, body, ghostCode, ghostBody)
	}
	return code, body
}

func TestAnOutsiderReadsNothingHidden(t *testing.T) {
	f := newFixture(t)
	s := loadSpec(t)
	owner, outsider := f.machine(t, "acme", everything...), f.machine(t, "globex", everything...)
	h := hide(t, f, owner)
	defaults := map[string]string{"permission": "runs:read", "q": "hello"}

	var keys []string
	for key := range s.ops {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		op := s.ops[key]
		if op.method != http.MethodGet || !strings.HasPrefix(op.path, "/api/") || op.path == "/api/events" {
			continue
		}
		params := pathParams(op.path)
		all, required := queryParams(s, op)
		query := url.Values{}
		for _, name := range required {
			v, ok := defaults[name]
			if !ok {
				t.Errorf("%s requires %s, and the sweep has no value for it", key, name)
				continue
			}
			query.Set(name, v)
		}
		withQuery := func(path string, q url.Values) string {
			if len(q) == 0 {
				return path
			}
			return path + "?" + q.Encode()
		}

		if len(params) > 0 {
			if _, documents := op.schemas["404"]; !documents {
				t.Errorf("%s names an object and cannot answer 404 for one the caller may not see", key)
				continue
			}
			hiddenPath, ghostPath, swaps := op.path, op.path, map[string]string{}
			for _, name := range params {
				v, ok := h.value(op.path, name)
				if !ok {
					t.Errorf("%s: the sweep hides nothing for {%s}; add one to hide", key, name)
					continue
				}
				hiddenPath = strings.ReplaceAll(hiddenPath, "{"+name+"}", url.PathEscape(v))
				ghostPath = strings.ReplaceAll(ghostPath, "{"+name+"}", url.PathEscape(ghost(v)))
				swaps[v] = ghost(v)
			}
			if code, _ := sameAsNothing(t, f, outsider, key, withQuery(hiddenPath, query), withQuery(ghostPath, query), swaps); code != http.StatusNotFound {
				t.Errorf("%s answers an outsider %d for a hidden object, not 404", key, code)
			}
			continue
		}

		// A list holds nothing hidden.
		code, body := get(t, f, outsider, withQuery(op.path, query))
		if code >= 500 {
			t.Errorf("%s = %d %s", key, code, body)
		}
		if strings.Contains(body, marker) || strings.Contains(body, h.run) {
			t.Errorf("%s shows an outsider what is hidden: %s", key, body)
		}
		// And a filter naming something hidden answers as one naming nothing.
		for _, name := range all {
			v, probe := h.probes()[name]
			if !probe {
				continue
			}
			hq, gq := url.Values{}, url.Values{}
			for k, vs := range query {
				hq[k], gq[k] = vs, vs
			}
			hq.Set(name, v)
			gq.Set(name, ghost(v))
			sameAsNothing(t, f, outsider, key+"?"+name, withQuery(op.path, hq), withQuery(op.path, gq), map[string]string{v: ghost(v)})
		}
	}
}

// An outsider's stream carries what they may see and nothing hidden: no
// event of a hidden run and no notice of a hidden inbox message, while their
// own run's events and their own notice arrive.
func TestAnOutsiderStreamCarriesNothingHidden(t *testing.T) {
	f := newFixture(t)
	owner, outsider := f.machine(t, "acme", everything...), f.machine(t, "globex", everything...)
	h := hide(t, f, owner)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", f.srv.URL+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+outsider)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	// Hidden: a run of the hidden agent, and a file it published, which
	// raises an inbox message of its own kind.
	f.do(t, nil, "DELETE", "/api/policies?kind=agent&name="+h.agent, owner, "")
	_, run := f.do(t, nil, "POST", "/api/runs", owner, `{"agent":"`+h.agent+`","text":"Again `+marker+`.","project":"`+h.project+`"}`)
	hiddenRun, _ := run["id"].(string)
	if hiddenRun == "" {
		t.Fatalf("the hidden run did not start: %v", run)
	}
	if err := f.p.PutFile("acme", h.thread, "more-"+marker+".txt", "More "+marker+"."); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Publish(t.Context(), platform.Publication{
		Tenant: "acme", Thread: h.thread, Run: h.run, Agent: h.agent, Project: h.project, By: "agent:" + h.agent, Path: "more-" + marker + ".txt",
	}); err != nil {
		t.Fatal(err)
	}
	// Visible: the outsider's own run, which sends its answer to their inbox.
	f.do(t, nil, "POST", "/api/agents", outsider, helperSpec)
	if res, out := f.do(t, nil, "POST", "/api/schedules", outsider, `{"name":"control","expression":"0 3 * * *","agent":"helper","text":"Report.","deliver":"inbox"}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("control schedule = %d %v", res.StatusCode, out)
	}
	_, control := f.do(t, nil, "POST", "/api/schedules/control/run", outsider, "")
	controlRun, _ := control["id"].(string)

	var sawControl, sawNotice bool
	var grace <-chan time.Time
	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	name := ""
	for {
		var line string
		var open bool
		select {
		case line, open = <-lines:
		case <-grace:
			open = false
		}
		if !open {
			break
		}
		if v, isName := strings.CutPrefix(line, "event: "); isName {
			name = v
			continue
		}
		data, isData := strings.CutPrefix(line, "data: ")
		if !isData {
			if line == "" {
				name = ""
			}
			continue
		}
		if strings.Contains(data, marker) || strings.Contains(data, hiddenRun) || strings.Contains(data, h.run) {
			t.Errorf("the outsider's stream carried something hidden: %s", data)
		}
		var v map[string]any
		_ = json.Unmarshal([]byte(data), &v)
		switch name {
		case "inbox":
			// The hidden message is about a file; the outsider's is a result.
			if v["kind"] != "result" {
				t.Errorf("the outsider was told of a %v message", v["kind"])
			}
			sawNotice = true
		case "":
			if v["tenant"] != "globex" {
				t.Errorf("the outsider's stream carried an event of %v", v["tenant"])
			}
			if v["run_id"] == controlRun && v["kind"] == "lifecycle.completed" {
				sawControl = true
			}
		}
		if sawControl && sawNotice && grace == nil {
			// Anything hidden that is still on its way has a moment to
			// arrive before the window closes.
			grace = time.After(300 * time.Millisecond)
		}
	}
	if !sawControl || !sawNotice {
		t.Fatalf("the outsider's own run (%v) or notice (%v) never arrived, so the sweep proves nothing", sawControl, sawNotice)
	}
}
