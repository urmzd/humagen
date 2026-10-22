package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/toolprovider"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/alerts"
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
// The sweep runs twice: as another workspace, which sees nothing of acme, and
// as a member of acme who is not a member of the hidden project, which is
// members only. A non-member still sees what acme holds outside any project
// (knowledge, clients, the directory), so that run hides only what lives in
// the project: its runs, threads, files, notes, schedules, rules, inbox
// messages, evals and the agent that lives there.

// marker is in every hidden name and text. It holds letters that are not hex,
// so no id or digest can contain it by chance.
const marker = "zqx"

// hidden is what one workspace holds that an outsider must not see.
type hidden struct {
	project, agent, schedule, rule, source, eval, client, variant string
	run, thread, message, artifact, agentNote, projectNote, doc   string
	handle, alert, channel, announcement, provider                string
	// open is, in a sweep inside the workspace, a Slack client the outsider
	// may read, with the hidden channel bound to it: its bindings must not
	// show that binding.
	open string
	// inside is true when the outsider is in the same workspace: what
	// lives outside the project is theirs to see, and is not hidden.
	inside bool
	// private is true when what is hidden lives in a private thread of a
	// project the outsider is a member of: only the thread's run, its
	// file, its alert and its inbox message are hidden, and everything
	// else is theirs to see.
	private bool
}

// confined is the fixture every sweep reads in: acme also offers a tool that
// acts as each person, so the routes naming one have something to hide from
// another workspace.
func confined(t *testing.T) *fixture {
	t.Helper()
	return newFixtureWith(t, platform.Config{
		Ext: platform.Extensions{ToolProviders: []platform.ToolProviderSeed{{Provider: toolprovider.Provider{
			Tenant: "acme", Name: canaryProvider, Transport: toolprovider.TransportHTTP,
			Endpoint: "https://issues.example.com/mcp", CredentialScope: toolprovider.ScopePerson,
		}}}},
		CredentialRefs: map[string][]string{"acme": {"op://People/{subject}/"}},
	})
}

// canaryProvider is the tool that acts as each person in acme. It is the
// workspace's, so everyone in acme sees it, and its name holds no marker.
const canaryProvider = "canary-issues"

// outsiders are who the sweep reads as: another workspace, and a member of
// acme who is not in the hidden project.
func outsiders(t *testing.T, f *fixture) map[string]struct {
	tok    string
	inside bool
} {
	t.Helper()
	nonmember := f.iss.Sign(t, "svc-nonmember", audience, map[string]any{
		"org": "acme", "scp": slices.DeleteFunc(slices.Clone(everything), func(s string) bool { return s == "warp:projects:govern" }),
	})
	return map[string]struct {
		tok    string
		inside bool
	}{
		"another workspace": {f.machine(t, "globex", everything...), false},
		"a non-member":      {nonmember, true},
	}
}

// hide fills the acme workspace with one of everything, each named or
// written with the marker.
func hide(t *testing.T, f *fixture, owner string, inside bool) hidden {
	t.Helper()
	h := hidden{
		inside:  inside,
		project: "Canary" + marker, agent: "canary-" + marker, schedule: "canary-" + marker, rule: "canary-" + marker,
		source: "canary-" + marker, eval: "canary-" + marker, client: "canary-" + marker, variant: "lean-" + marker,
		provider: canaryProvider,
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
	// The agent lives in the project, and the project is members only: its
	// creator, who holds projects:govern, is its one member.
	must("PUT", "/api/agents/"+h.agent+"/home", `{"project":"`+h.project+`"}`, http.StatusOK)
	must("PUT", "/api/projects/"+h.project+"/members", `{"version":1,"members":[{"who":"svc-acme","role":"owner"}]}`, http.StatusOK)
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
	if inside {
		// A schedule that ran in the project and was then moved out of it,
		// to the workspace, which the outsider sees. Its runs are found by
		// its name, so its view must not show the one it ran here.
		must("POST", "/api/agents", `{"name":"mover","model":"offline","provider":"offline","system_prompt":"Moves."}`, http.StatusCreated)
		must("POST", "/api/schedules", `{"name":"moved","expression":"0 3 * * *","agent":"mover","text":"Moved `+marker+`.","project":"`+h.project+`"}`, http.StatusCreated)
		ended(t, f, owner, str(must("POST", "/api/schedules/moved/run", ``, http.StatusAccepted), "id"))
		must("PUT", "/api/schedules/moved", `{"expression":"0 3 * * *","agent":"mover","text":"Moved."}`, http.StatusOK)
	}
	must("POST", "/api/alert-rules", `{"name":"`+h.rule+`","kind":"run_failed","severity":"critical","project":"`+h.project+`","description":"Rule `+marker+`."}`, http.StatusCreated)
	must("POST", "/api/evals", `{"name":"`+h.eval+`","target":"agent","subject":"`+h.agent+`","cases":[{"id":"c-`+marker+`","input":"hi","checks":[{"kind":"contains","value":"`+marker+`"}]}]}`, http.StatusCreated)
	// A channel and an announcement of the hidden project. The channel is
	// bound to a Slack channel of a client: the hidden one for another
	// workspace, and for a member of acme one they may read.
	h.channel = "canary-" + marker
	bound := h.client
	if inside {
		h.open = "slack-open"
		bound = h.open
		must("POST", "/api/clients", slackApp(h.open, "A01OPEN", "svc-acme"), http.StatusCreated)
	} else {
		must("POST", "/api/clients", slackApp(h.client, "A01ZQX", "svc-acme"), http.StatusCreated)
	}
	must("POST", "/api/projects/"+h.project+"/channels", `{"name":"`+h.channel+`","purpose":"Channel `+marker+`.","slack":{"client":"`+bound+`","channel":"C01CANARY"}}`, http.StatusCreated)
	if inside {
		// The open client's home is in the hidden project: the client is
		// the outsider's to read, its home is not.
		homed := strings.Replace(slackApp(h.open, "A01OPEN", "svc-acme"), `"slack":`, `"home":{"project":"`+h.project+`"},"slack":`, 1)
		if out := must("PUT", "/api/clients/"+h.open, homed, http.StatusOK); !strings.Contains(fmt.Sprint(out["home"]), h.project) {
			t.Fatalf("the owner does not see the open client's home: %v", out)
		}
	}
	if out := must("GET", "/api/clients/"+bound+"/bindings", "", http.StatusOK); !strings.Contains(fmt.Sprint(out), h.channel) {
		t.Fatalf("the owner does not see the hidden channel's binding: %v", out)
	}
	h.announcement = str(must("POST", "/api/projects/"+h.project+"/announcements", `{"subject":"News `+marker+`","text":"Words `+marker+`."}`, http.StatusCreated), "id")
	// An alert raised by the hidden run, in the hidden project.
	raised, _, err := f.p.Alerts.Raise("acme", "canary-alert-"+marker, alerts.Alert{
		Kind: alerts.RunFailed, Severity: alerts.Critical, Project: h.project, RunID: h.run, Title: "Alert " + marker, At: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	h.alert = raised.ID
	// What lives outside any project is hidden only from another workspace.
	if !inside {
		must("POST", "/api/knowledge", `{"name":"`+h.source+`","description":"Source `+marker+`."}`, http.StatusCreated)
		h.doc = str(must("POST", "/api/knowledge/"+h.source+"/documents", `{"title":"Doc `+marker+`","text":"Words `+marker+` here."}`, http.StatusCreated), "id")
		must("POST", "/api/knowledge/"+h.source+"/variants", `{"name":"`+h.variant+`"}`, http.StatusCreated)
		person, err := f.p.People.Seen("acme", people.Sighting{Subject: "okta|canary-" + marker, Name: "Canary " + marker, Source: people.SignIn}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		h.handle = person.Handle
	}

	// A refusal at a limit raises an inbox message about the hidden agent.
	must("PUT", "/api/policies", `{"scope":{"kind":"agent","name":"`+h.agent+`"},"period":"day","max_runs":1}`, http.StatusOK)
	must("POST", "/api/runs", `{"agent":"`+h.agent+`","text":"Over `+marker+`.","project":"`+h.project+`"}`, http.StatusTooManyRequests)
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

// hidePrivate fills acme with a private thread in Field, a project everyone
// in acme is a member of, started by Ana with the marker in its words: its
// run, a file it published, the alert a rule on Field raised for the run and
// the inbox message about that alert. Only what lives in the thread is
// hidden; Field, its agent and its rule are everyone's to see.
func hidePrivate(t *testing.T, f *fixture, owner string) hidden {
	t.Helper()
	h := hidden{inside: true, private: true, project: "Field", agent: "helper", rule: "slow", channel: "general"}
	want(t, f, owner, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	// Every run in Field is slow enough to raise an alert.
	want(t, f, owner, "POST", "/api/alert-rules", `{"name":"slow","kind":"slow_run","threshold":1e-9,"severity":"warning","project":"Field"}`, http.StatusCreated)
	ana := f.person(t, "acme", "00u-ana", "Ana", "warp-admins")
	started := want(t, f, ana, "POST", "/api/threads", `{"project":"Field","visibility":"private","text":"@helper the plan is `+marker+`."}`, http.StatusCreated)
	h.thread = started["thread"].(map[string]any)["id"].(string)
	h.run = started["runs"].([]any)[0].(map[string]any)["id"].(string)
	ended(t, f, ana, h.run)

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

	deadline := time.Now().Add(10 * time.Second)
	for h.alert == "" || h.message == "" {
		if time.Now().After(deadline) {
			t.Fatalf("the private run raised no alert (%q) or no message about it (%q)", h.alert, h.message)
		}
		for _, a := range f.p.Alerts.Alerts("acme") {
			if a.RunID == h.run {
				h.alert = a.ID
			}
		}
		_, page := f.do(t, nil, "GET", "/api/inbox?state=all", owner, "")
		for _, m := range page["messages"].([]any) {
			if msg := m.(map[string]any); msg["source"].(map[string]any)["run"] == h.run {
				h.message = msg["id"].(string)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return h
}

// visiblePath is an operation's path with every parameter naming what the
// outsider may see. In the private sweep that is Field, its agent, its rule
// and its general channel; in the sweep of a member of the workspace, the
// open Slack client the hidden channel is bound to. What it reads must hold
// nothing hidden.
func (h hidden) visiblePath(path string) (string, bool) {
	if h.inside && !h.private && h.open != "" && strings.HasPrefix(path, "/api/clients/{id}") {
		return strings.ReplaceAll(path, "{id}", url.PathEscape(h.open)), true
	}
	if !h.private {
		return "", false
	}
	out := path
	for _, param := range pathParams(path) {
		var v string
		switch {
		case param == "name" && strings.HasPrefix(path, "/api/agents/"):
			v = h.agent
		case param == "name" && strings.HasPrefix(path, "/api/projects/"):
			v = h.project
		case param == "name" && strings.HasPrefix(path, "/api/alert-rules/"):
			v = h.rule
		case param == "channel":
			v = h.channel
		default:
			return "", false
		}
		out = strings.ReplaceAll(out, "{"+param+"}", url.PathEscape(v))
	}
	return out, true
}

// holds reports whether a body shows anything hidden.
func (h hidden) holds(body string) bool {
	return strings.Contains(body, marker) || strings.Contains(body, h.run) || (h.thread != "" && strings.Contains(body, h.thread)) ||
		(!h.inside && h.provider != "" && strings.Contains(body, h.provider))
}

// unplaced are the path parameters that name something outside any project,
// which a member of the workspace may see.
func (h hidden) unplaced(path, param string) bool {
	if h.private {
		if param == "part" || param == "question" {
			return h.message == "" && strings.HasPrefix(path, "/api/inbox/")
		}
		switch {
		case param != "id":
			return true
		case strings.HasPrefix(path, "/api/inbox/"):
			return h.message == ""
		}
		return !slices.ContainsFunc([]string{"/api/runs/", "/api/threads/", "/api/artifacts/", "/api/alerts/"}, func(p string) bool { return strings.HasPrefix(path, p) })
	}
	if !h.inside {
		return false
	}
	switch {
	case param == "variant", param == "client", param == "handle", param == "provider":
		// A tool provider is the workspace's: everyone in it may connect to it.
		return true
	case strings.HasPrefix(path, "/api/knowledge/"), strings.HasPrefix(path, "/api/clients/"):
		return true
	}
	return false
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
		case prefix("/api/alerts/"):
			return h.alert, true
		case prefix("/api/projects/{name}/announcements/"):
			return h.announcement, true
		}
	case "channel":
		return h.channel, true
	case "handle":
		return h.handle, true
	case "variant":
		return h.variant, true
	case "client":
		return h.client, true
	case "provider":
		return h.provider, true
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
	if h.private {
		return map[string]string{"thread": h.thread, "run": h.run}
	}
	out := map[string]string{
		"project": h.project, "thread": h.thread, "run": h.run, "agent": h.agent, "q": "Canary" + marker,
	}
	if !h.inside {
		out["source"], out["variant"] = h.source, h.variant
	}
	return out
}

// ghost is a value shaped like v that names nothing: its last character
// changed, or for a counted id such as "al-1" the count 0, which no store
// issues, so the ghost is never the next one issued.
func ghost(v string) string {
	if v == "" {
		return "ghost"
	}
	if i := strings.LastIndexByte(v, '-'); i > 0 && i < len(v)-1 && strings.Trim(v[i+1:], "0123456789") == "" {
		return v[:i+1] + "0"
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
	f0 := newFixture(t)
	for who, o := range outsiders(t, f0) {
		t.Run(who, func(t *testing.T) {
			f := confined(t)
			sweepGets(t, f, f.machine(t, "acme", everything...), outsiderToken(t, f, who), o.inside)
		})
	}
}

// outsiderToken signs the outsider's credential against this fixture's
// issuer.
func outsiderToken(t *testing.T, f *fixture, who string) string {
	t.Helper()
	return outsiders(t, f)[who].tok
}

func sweepGets(t *testing.T, f *fixture, owner, outsider string, inside bool) {
	t.Helper()
	sweepGetsOf(t, f, outsider, hide(t, f, owner, inside))
}

func sweepGetsOf(t *testing.T, f *fixture, outsider string, h hidden) {
	t.Helper()
	s := loadSpec(t)
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

		// In a private sweep, what names Field or what lives openly in it
		// is the outsider's to read, and holds nothing of the thread.
		if visible, ok := h.visiblePath(op.path); ok && len(params) > 0 {
			code, body := get(t, f, outsider, withQuery(visible, query))
			if code >= 500 {
				t.Errorf("%s = %d %s", key, code, body)
			}
			if h.holds(body) {
				t.Errorf("%s shows an outsider what is hidden: %s", key, body)
			}
			continue
		}
		if len(params) > 0 {
			if _, documents := op.schemas["404"]; !documents {
				t.Errorf("%s names an object and cannot answer 404 for one the caller may not see", key)
				continue
			}
			hiddenPath, ghostPath, swaps := op.path, op.path, map[string]string{}
			if slices.ContainsFunc(params, func(name string) bool { return h.unplaced(op.path, name) }) {
				continue
			}
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
		if h.holds(body) {
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

// send makes one request as tok and answers its status and body.
func send(t *testing.T, f *fixture, tok, method, path, body string) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, f.srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(raw)
}

// An outsider changes nothing hidden, and learns nothing by trying: every
// operation in the contract that names an object and is not a read, sent
// against a hidden object, answers exactly as it does against one that does
// not exist, and so does one whose body names the hidden project. Afterwards
// every hidden object is as it was.
func TestAnOutsiderChangesNothingHidden(t *testing.T) {
	f0 := newFixture(t)
	for who, o := range outsiders(t, f0) {
		t.Run(who, func(t *testing.T) {
			f := confined(t)
			owner := f.machine(t, "acme", everything...)
			h := hide(t, f, owner, o.inside)
			sweepWrites(t, f, owner, outsiderToken(t, f, who), h)
		})
	}
}

func sweepWrites(t *testing.T, f *fixture, owner, outsider string, h hidden) {
	t.Helper()
	s := loadSpec(t)
	norm := func(swaps map[string]string, v string) string {
		for real, g := range swaps {
			v = strings.ReplaceAll(v, real, g)
		}
		return timestamps.ReplaceAllString(v, "T")
	}
	same := func(what string, code int, body string, ghostCode int, ghostBody string, swaps map[string]string) {
		t.Helper()
		if code != ghostCode || norm(swaps, body) != norm(swaps, ghostBody) {
			t.Errorf("%s: a hidden object answers %d %s, and one that does not exist %d %s", what, code, body, ghostCode, ghostBody)
		}
	}
	var keys []string
	for key, op := range s.ops {
		if op.method != http.MethodGet && strings.HasPrefix(op.path, "/api/") && len(pathParams(op.path)) > 0 {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		op := s.ops[key]
		params := pathParams(op.path)
		if slices.ContainsFunc(params, func(name string) bool { return h.unplaced(op.path, name) }) {
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
		body := "{}"
		if op.method == http.MethodDelete {
			body = ""
		}
		code, out := send(t, f, outsider, op.method, hiddenPath, body)
		ghostCode, ghostOut := send(t, f, outsider, op.method, ghostPath, body)
		same(key, code, out, ghostCode, ghostOut, swaps)
		if code >= 200 && code < 300 {
			t.Errorf("%s changed a hidden object for an outsider: %d %s", key, code, out)
		}
	}

	// A turn after the hidden run answers as one after a run that is not
	// there.
	{
		body := `{"agent":"helper","text":"probe","after":%q}`
		code, out := send(t, f, outsider, "POST", "/api/runs", fmt.Sprintf(body, h.run))
		ghostCode, ghostOut := send(t, f, outsider, "POST", "/api/runs", fmt.Sprintf(body, ghost(h.run)))
		same("POST /api/runs after the hidden run", code, out, ghostCode, ghostOut, map[string]string{h.run: ghost(h.run)})
	}
	if h.private {
		for _, path := range []string{"/api/runs/" + h.run, "/api/threads/" + h.thread, "/api/artifacts/" + h.artifact} {
			if code, out := get(t, f, owner, path); code != http.StatusOK {
				t.Errorf("after the sweep the owner reads %s as %d %s", path, code, out)
			}
		}
		_, th := get(t, f, owner, "/api/threads/"+h.thread)
		if !strings.Contains(th, `"visibility":"private"`) || strings.Contains(th, "svc-nonmember") {
			t.Errorf("after the sweep the private thread is %s", th)
		}
		for _, a := range f.p.Alerts.Alerts("acme") {
			if a.ID == h.alert && !a.Open() {
				t.Errorf("after the sweep the hidden alert was acknowledged by %s", a.AckedBy)
			}
		}
		return
	}

	// A body that names the hidden project answers as one naming a project
	// that is not there.
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/alert-rules", `{"name":"probe","kind":"run_failed","severity":"critical","project":%q}`},
		{"POST", "/api/runs", `{"agent":"helper","text":"probe","project":%q}`},
		{"POST", "/api/schedules", `{"name":"probe","expression":"0 3 * * *","agent":"helper","text":"probe","project":%q}`},
		{"POST", "/api/projects", `{"name":"probe","parent":%q}`},
		{"POST", "/api/threads", `{"project":%q,"visibility":"shared","text":"probe"}`},
	} {
		code, out := send(t, f, outsider, tc.method, tc.path, fmt.Sprintf(tc.body, h.project))
		ghostCode, ghostOut := send(t, f, outsider, tc.method, tc.path, fmt.Sprintf(tc.body, ghost(h.project)))
		same(tc.method+" "+tc.path+" naming the hidden project", code, out, ghostCode, ghostOut, map[string]string{h.project: ghost(h.project)})
	}

	// Every hidden object is as it was.
	for _, path := range []string{
		"/api/projects/" + h.project, "/api/agents/" + h.agent + "/history", "/api/evals/" + h.eval + "/runs",
		"/api/runs/" + h.run, "/api/threads/" + h.thread, "/api/artifacts/" + h.artifact,
		"/api/projects/" + h.project + "/channels",
	} {
		if code, out := get(t, f, owner, path); code != http.StatusOK {
			t.Errorf("after the sweep the owner reads %s as %d %s", path, code, out)
		}
	}
	for path, want := range map[string][]string{
		"/api/alert-rules":                         {`"name":"` + h.rule + `"`, "Rule " + marker},
		"/api/schedules":                           {`"name":"` + h.schedule + `"`, "Schedule " + marker},
		"/api/projects/" + h.project + "/channels": {`"name":"` + h.channel + `"`, "Channel " + marker},
		"/api/projects/" + h.project + "/members":  {`"who":"svc-acme"`},
		"/api/projects/" + h.project + "/inbox":    {"News " + marker},
	} {
		_, body := get(t, f, owner, path)
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("after the sweep %s lacks %s: %s", path, w, body)
			}
		}
	}
	for _, a := range f.p.Alerts.Alerts("acme") {
		if a.ID == h.alert && !a.Open() {
			t.Errorf("after the sweep the hidden alert was acknowledged by %s", a.AckedBy)
		}
	}
}

// An outsider's stream carries what they may see and nothing hidden: no
// event of a hidden run and no notice of a hidden inbox message, while their
// own run's events and their own notice arrive.
func TestAnOutsiderStreamCarriesNothingHidden(t *testing.T) {
	f0 := newFixture(t)
	for who, o := range outsiders(t, f0) {
		t.Run(who, func(t *testing.T) {
			f := confined(t)
			tenant := "globex"
			if o.inside {
				tenant = "acme"
			}
			sweepStream(t, f, f.machine(t, "acme", everything...), outsiderToken(t, f, who), o.inside, tenant)
		})
	}
}

// A private thread is its participants' alone. A member of its project who
// takes no part in it reads nothing of it (its runs, its file, the alert its
// run raised and the inbox message about that, by id or in any list,
// Field's own included), changes nothing of it, and their stream carries no
// notice of a message posted there and no event of the run it starts.
func TestAPrivateThreadIsItsParticipantsAlone(t *testing.T) {
	for _, part := range []string{"reads", "writes", "stream"} {
		t.Run(part, func(t *testing.T) {
			f := confined(t)
			owner := f.machine(t, "acme", everything...)
			h := hidePrivate(t, f, owner)
			member := outsiderToken(t, f, "a non-member")
			if code, out := get(t, f, member, "/api/projects/Field"); code != http.StatusOK || !strings.Contains(out, `"role":"member"`) {
				t.Fatalf("the outsider is not a member of Field: %d %s", code, out)
			}
			switch part {
			case "reads":
				sweepGetsOf(t, f, member, h)
			case "writes":
				sweepWrites(t, f, owner, member, h)
			case "stream":
				ana := f.person(t, "acme", "00u-ana", "Ana", "warp-admins")
				streamOf(t, f, owner, member, "acme", h, func() string {
					out := want(t, f, ana, "POST", "/api/threads/"+h.thread+"/messages", `{"text":"@helper and the budget is `+marker+`?"}`, http.StatusCreated)
					runs, _ := out["runs"].([]any)
					if len(runs) != 1 {
						t.Fatalf("the post in the private thread started %v", out)
					}
					return runs[0].(map[string]any)["id"].(string)
				})
			}
		})
	}
}

func sweepStream(t *testing.T, f *fixture, owner, outsider string, inside bool, tenant string) {
	t.Helper()
	h := hide(t, f, owner, inside)
	streamOf(t, f, owner, outsider, tenant, h, func() string {
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
		return hiddenRun
	})
}

// streamOf reads the outsider's stream while work does something hidden, and
// returns the hidden run it started; then the outsider's own run and notice
// must arrive, and nothing hidden with them.
func streamOf(t *testing.T, f *fixture, owner, outsider, tenant string, h hidden, work func() string) {
	t.Helper()
	inside := h.inside

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", f.srv.URL+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+outsider)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	hiddenRun := work()
	// Visible: the outsider's own run, which sends its answer to their inbox.
	if !inside {
		f.do(t, nil, "POST", "/api/agents", outsider, helperSpec)
	} else {
		f.do(t, nil, "POST", "/api/agents", owner, helperSpec)
	}
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
		if h.holds(data) || strings.Contains(data, hiddenRun) {
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
			if v["tenant"] != tenant {
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
