package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/clients"
)

// spySpec is an agent that comes to live in Secret, a project only its
// owner reads.
const spySpec = `{"name":"spy","model":"offline","provider":"offline","system_prompt":"Spies."}`

// secretAgent fills acme with helper, which everyone reads, and spy, which
// lives in Secret, a members-only project of the owner's.
func secretAgent(t *testing.T, f *fixture, owner string) {
	t.Helper()
	want(t, f, owner, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/agents", spySpec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Secret"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Secret/members", `{"version":1,"members":[{"who":"svc-acme","role":"owner"}]}`, http.StatusOK)
	want(t, f, owner, "PUT", "/api/agents/spy/home", `{"project":"Secret"}`, http.StatusOK)
}

// asGhost sends a body naming spy, and the same body naming an agent that
// does not exist, and fails unless both answer alike: the same status and
// the same words, the name aside.
func asGhost(t *testing.T, f *fixture, tok, method, path, body string) int {
	t.Helper()
	code, out := send(t, f, tok, method, path, body)
	ghostCode, ghostOut := send(t, f, tok, method, path, strings.ReplaceAll(body, `"spy"`, `"spz"`))
	if code != ghostCode || strings.ReplaceAll(out, "spy", "spz") != ghostOut {
		t.Errorf("%s %s naming spy = %d %s, and naming an agent that does not exist %d %s", method, path, code, out, ghostCode, ghostOut)
	}
	return code
}

// read is one route's answer as a map.
func read(t *testing.T, f *fixture, tok, path string) (string, map[string]any) {
	t.Helper()
	code, raw := get(t, f, tok, path)
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, code, raw)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return raw, out
}

// asRead is a client as the console saves it: what the caller was shown,
// with no secrets, which keeps them.
func asRead(t *testing.T, c map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"name": c["name"], "surface": c["surface"], "identity": c["identity"], "default_agent": c["default_agent"],
		"agents": c["agents"], "bots": c["bots"], "slack": c["slack"], "github": c["github"], "home": c["home"], "enabled": c["enabled"],
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// A client names only the agents its reader may read. One that lives in a
// project they may not read is left out of its default, its list, its bots
// and its auto-labels, as if it were not there, except for the machine that
// hosts the client, which routes by them. A writer who saves the client as
// they read it keeps those agents, names one only as an agent that exists
// and that they may read, and learns nothing by naming one they may not.
func TestAClientNamesOnlyTheAgentsTheCallerMayRead(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	writer := f.machineAs(t, "svc-ops", "acme", "warp:clients:read", "warp:clients:write")
	machine := f.machineAs(t, "svc-app", "acme", "warp:clients:read")
	secretAgent(t, f, owner)

	app := func(fields string) string {
		return strings.Replace(slackApp("app", "A01APP", "svc-app"), `"default_agent":"helper"`, fields, 1)
	}
	want(t, f, owner, "POST", "/api/clients", app(`"default_agent":"spy","agents":["helper"],"bots":{"U0SPY":"spy","U0HELP":"helper"}`), http.StatusCreated)
	want(t, f, owner, "POST", "/api/clients", `{"id":"gh","name":"gh","surface":"github","identity":{"owners":["acme"]},"agents":["helper","spy"],`+
		`"github":{"auto_labels":{"acme/site":{"spy":"spy","help":"helper"},"acme/vault":{"spy":"spy"}}},`+
		`"credentials":{"token":{"value":"`+botToken+`"},"signing_secret":{"value":"`+signingSecret+`"}}}`, http.StatusCreated)

	for _, id := range []string{"app", "gh"} {
		if raw, _ := read(t, f, owner, "/api/clients/"+id); !strings.Contains(raw, `"spy"`) {
			t.Fatalf("the owner does not see spy on %s: %s", id, raw)
		}
		if raw, _ := read(t, f, writer, "/api/clients/"+id); strings.Contains(raw, "spy") {
			t.Errorf("a writer who may not read Secret reads spy on %s: %s", id, raw)
		}
	}
	if raw, _ := read(t, f, writer, "/api/clients"); strings.Contains(raw, "spy") || strings.Contains(raw, "acme/vault") {
		t.Errorf("the list shows spy: %s", raw)
	}
	_, shown := read(t, f, writer, "/api/clients/app")
	if shown["default_agent"] != nil || !slices.Equal(listed(shown["agents"]), []string{"helper"}) || len(shown["bots"].(map[string]any)) != 1 {
		t.Errorf("the writer reads app as %v", shown)
	}
	// The machine that hosts app routes by every agent it names.
	if raw, _ := read(t, f, machine, "/api/clients/app"); !strings.Contains(raw, `"default_agent":"spy"`) || !strings.Contains(raw, `"U0SPY":"spy"`) {
		t.Errorf("app's own machine does not read its default: %s", raw)
	}
	if raw, _ := read(t, f, machine, "/api/clients/gh"); strings.Contains(raw, "spy") {
		t.Errorf("app's machine reads spy on another client: %s", raw)
	}

	// Saved as the writer reads them, both keep spy.
	for _, id := range []string{"app", "gh"} {
		_, c := read(t, f, writer, "/api/clients/"+id)
		if out := want(t, f, writer, "PUT", "/api/clients/"+id, asRead(t, c), http.StatusOK); strings.Contains(asRead(t, out), "spy") {
			t.Errorf("the writer's save of %s answered with spy: %v", id, out)
		}
	}
	_, kept := read(t, f, owner, "/api/clients/app")
	if kept["default_agent"] != "spy" || kept["bots"].(map[string]any)["U0SPY"] != "spy" || !slices.Equal(listed(kept["agents"]), []string{"helper"}) {
		t.Errorf("after the writer's save, app is %v", kept)
	}
	if raw, _ := read(t, f, owner, "/api/clients/gh"); !strings.Contains(raw, `"acme/vault":{"spy":"spy"}`) || !strings.Contains(raw, `"spy":"spy"`) || !strings.Contains(raw, `"agents":["helper","spy"]`) {
		t.Errorf("after the writer's save, gh is %s", raw)
	}

	// Naming spy anywhere answers as naming an agent that does not exist.
	for _, fields := range []string{`"default_agent":"spy"`, `"agents":["spy"]`, `"default_agent":"helper","bots":{"U0NEW":"spy"}`} {
		if code := asGhost(t, f, writer, "PUT", "/api/clients/app", app(fields)); code != http.StatusBadRequest {
			t.Errorf("a save naming spy in %s = %d, want 400", fields, code)
		}
	}
	if code := asGhost(t, f, writer, "POST", "/api/clients", strings.Replace(app(`"default_agent":"spy"`), `"id":"app"`, `"id":"new"`, 1)); code != http.StatusBadRequest {
		t.Errorf("a create naming spy = %d, want 400", code)
	}
	// Another spelling of it is no way around.
	if code, out := send(t, f, writer, "PUT", "/api/clients/app", app(`"default_agent":"Spy"`)); code != http.StatusBadRequest {
		t.Errorf("a save naming Spy = %d %s, want 400", code, out)
	}
	if _, c := read(t, f, owner, "/api/clients/app"); c["version"].(float64) != 2 {
		t.Errorf("a refused save changed app: %v", c)
	}

	// A default the writer may read replaces spy as the default, and spy
	// stays one app routes to, so its bot still stands.
	_, c := read(t, f, writer, "/api/clients/app")
	c["default_agent"] = "helper"
	want(t, f, writer, "PUT", "/api/clients/app", asRead(t, c), http.StatusOK)
	_, kept = read(t, f, owner, "/api/clients/app")
	if kept["default_agent"] != "helper" || !slices.Equal(listed(kept["agents"]), []string{"helper", "spy"}) || kept["bots"].(map[string]any)["U0SPY"] != "spy" {
		t.Errorf("after the writer named helper, app is %v", kept)
	}
	// "*" reaches spy too, so the list holds it alone; narrowed again, the
	// list keeps spy for its bot.
	for _, list := range [][]string{{"*"}, {"helper"}} {
		_, c := read(t, f, writer, "/api/clients/app")
		c["agents"] = list
		want(t, f, writer, "PUT", "/api/clients/app", asRead(t, c), http.StatusOK)
	}
	if _, kept := read(t, f, owner, "/api/clients/app"); !slices.Equal(listed(kept["agents"]), []string{"helper", "spy"}) || kept["bots"].(map[string]any)["U0SPY"] != "spy" {
		t.Errorf("after the writer wrote * and then helper, app is %v", kept)
	}
}

// A client saved through the API names agents that exist. A name its record
// already holds is kept as it is, so a client saved before agents were
// checked still saves as it reads.
func TestAClientNamesAgentsThatExist(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	if code, out := send(t, f, admin, "POST", "/api/clients", slackApp("app", "A01APP", "svc-app")); code != http.StatusBadRequest || !strings.Contains(out, `there is no agent called \"helper\"`) {
		t.Fatalf("a client naming an agent that does not exist = %d %s", code, out)
	}
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, admin, "POST", "/api/clients", slackApp("app", "A01APP", "svc-app"), http.StatusCreated)
	// auto is no agent, and a client that reaches any agent may default to it.
	auto := strings.Replace(slackApp("any", "A01ANY", "svc-any"), `"default_agent":"helper"`, `"default_agent":"auto","agents":["*"]`, 1)
	want(t, f, admin, "POST", "/api/clients", auto, http.StatusCreated)

	in := platform.ClientInput{
		Client: clients.Client{
			ID: "old", Name: "old", Surface: clients.Slack, Enabled: true, Identity: clients.Identity{Team: "T01ABCDEF", AppID: "A01OLD"},
			DefaultAgent: "gone", Agents: []string{"helper"},
		},
		Token: clients.SecretInput{Value: botToken}, SigningSecret: clients.SecretInput{Value: signingSecret},
	}
	if _, err := f.p.CreateClient(context.Background(), "acme", "svc-acme", in); err != nil {
		t.Fatal(err)
	}
	_, c := read(t, f, admin, "/api/clients/old")
	c["enabled"] = false
	if out := want(t, f, admin, "PUT", "/api/clients/old", asRead(t, c), http.StatusOK); out["default_agent"] != "gone" || out["enabled"] != false {
		t.Fatalf("a save as read = %v", out)
	}
	c["agents"] = []string{"helper", "lost"}
	if code, out := send(t, f, admin, "PUT", "/api/clients/old", asRead(t, c)); code != http.StatusBadRequest || !strings.Contains(out, `\"lost\"`) {
		t.Fatalf("a save adding an agent that does not exist = %d %s", code, out)
	}
}

// A project's lists of agents name only the agents its reader may read. An
// owner who may not read Secret is not shown spy in a list that names it,
// keeps it in the allow list when they save, and names it only as an agent
// that does not exist.
func TestAProjectsListsNameOnlyTheAgentsTheCallerMayRead(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	writer := f.machineAs(t, "svc-ops", "acme", "warp:projects:read", "warp:projects:write", "warp:agents:read")
	secretAgent(t, f, owner)
	want(t, f, writer, "POST", "/api/projects", `{"name":"Open"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Open/agents", `{"version":1,"enable":["helper"],"allow":["helper","spy"]}`, http.StatusOK)

	if raw, _ := read(t, f, owner, "/api/projects/Open/agents"); !strings.Contains(raw, `"allow":["helper","spy"]`) {
		t.Fatalf("the owner reads Open's agents as %s", raw)
	}
	for _, path := range []string{"/api/projects", "/api/projects/Open", "/api/projects/Open/agents", "/api/projects/Open/history", "/api/projects/~/agents"} {
		if raw, _ := read(t, f, writer, path); strings.Contains(raw, "spy") {
			t.Errorf("%s shows the writer spy: %s", path, raw)
		}
	}
	_, v := read(t, f, writer, "/api/projects/Open/agents")
	if !slices.Equal(listed(v["allow"]), []string{"helper"}) {
		t.Errorf("the writer reads Open's allow list as %v", v["allow"])
	}

	// Saved as they read it, the allow list keeps spy.
	want(t, f, writer, "PUT", "/api/projects/Open/agents", `{"version":2,"enable":["helper"],"allow":["helper"],"default":"helper"}`, http.StatusOK)
	if raw, _ := read(t, f, owner, "/api/projects/Open/agents"); !strings.Contains(raw, `"allow":["helper","spy"]`) || !strings.Contains(raw, `"default":"helper"`) {
		t.Errorf("after the writer's save, Open's agents are %s", raw)
	}
	// Naming spy in any list answers as naming an agent that does not
	// exist, not as one that lives somewhere.
	for _, body := range []string{
		`{"version":3,"enable":["helper"],"allow":["helper","spy"]}`,
		`{"version":3,"enable":["helper","spy"]}`,
		`{"version":3,"enable":["helper"],"default":"spy"}`,
	} {
		if code := asGhost(t, f, writer, "PUT", "/api/projects/Open/agents", body); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", body, code)
		}
	}

	// An allow list whose every name is one the reader may not read reads
	// as an empty list, and saving it keeps the list.
	want(t, f, owner, "PUT", "/api/projects/Open/agents", `{"version":3,"enable":["helper"],"allow":["spy"]}`, http.StatusOK)
	_, v = read(t, f, writer, "/api/projects/Open/agents")
	if allow, set := v["allow"].([]any); !set || len(allow) != 0 {
		t.Errorf("the writer reads an allow list of spy alone as %v", v["allow"])
	}
	if _, p := read(t, f, writer, "/api/projects/Open"); !strings.Contains(mustJSON(t, p), `"allow":[]`) {
		t.Errorf("the writer reads Open as %v", p)
	}
	want(t, f, writer, "PUT", "/api/projects/Open/agents", `{"version":4,"enable":[],"allow":[]}`, http.StatusOK)
	if raw, _ := read(t, f, owner, "/api/projects/Open/agents"); !strings.Contains(raw, `"allow":["spy"]`) || !strings.Contains(raw, `"enable":[]`) {
		t.Errorf("after the writer saved an empty allow list, Open's agents are %s", raw)
	}
}

// The workspace's title and memory agents are shown only to who may read
// them. A save that names none keeps one the writer is not shown, and naming
// one they may not read answers as naming an agent that does not exist.
func TestTheSettingsNameOnlyTheAgentsTheCallerMayRead(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	writer := f.machineAs(t, "svc-ops", "acme", "warp:agents:read", "warp:agents:write")
	secretAgent(t, f, owner)
	settings := func(tok, title, memory string) string {
		t.Helper()
		_, st := read(t, f, tok, "/api/settings")
		st["title_agent"], st["memory_agent"], st["history_limit"] = title, memory, 9000
		return mustJSON(t, st)
	}
	want(t, f, owner, "PUT", "/api/settings", settings(owner, "spy", "spy"), http.StatusOK)

	if _, st := read(t, f, writer, "/api/settings"); st["title_agent"] != "" || st["memory_agent"] != "" {
		t.Fatalf("the writer reads the settings as %v", st)
	}
	if out := want(t, f, writer, "PUT", "/api/settings", settings(writer, "", ""), http.StatusOK); out["title_agent"] != "" {
		t.Errorf("the writer's save answered with %v", out)
	}
	if _, st := read(t, f, owner, "/api/settings"); st["title_agent"] != "spy" || st["memory_agent"] != "spy" || st["history_limit"].(float64) != 9000 {
		t.Errorf("after the writer's save, the settings are %v", st)
	}
	for _, body := range []string{settings(writer, "spy", ""), settings(writer, "", "spy")} {
		if code := asGhost(t, f, writer, "PUT", "/api/settings", body); code != http.StatusBadRequest {
			t.Errorf("a save naming spy = %d, want 400", code)
		}
	}
	want(t, f, writer, "PUT", "/api/settings", settings(writer, "helper", ""), http.StatusOK)
	if _, st := read(t, f, owner, "/api/settings"); st["title_agent"] != "helper" || st["memory_agent"] != "spy" {
		t.Errorf("after the writer named helper, the settings are %v", st)
	}
}

// listed is a JSON list of strings.
func listed(v any) []string {
	var out []string
	for _, s := range sliceOf(v) {
		str, _ := s.(string)
		out = append(out, str)
	}
	return out
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// A title or memory agent that lives in a project is asked only about the
// turns there and below, whose readers all read it. A turn anywhere else
// gets no housekeeping run, so nobody who reads that turn is shown a run of
// an agent they may not read.
func TestAHousekeepingAgentThatLivesInAProjectServesOnlyItsTurns(t *testing.T) {
	f := newFixtureWith(t, platform.Config{OfflineHooks: true})
	owner := f.machine(t, "acme", everything...)
	reader := f.machineAs(t, "svc-ops", "acme", "warp:runs:read", "warp:runs:write", "warp:agents:read")
	secretAgent(t, f, owner)
	_, st := read(t, f, owner, "/api/settings")
	st["title_agent"], st["memory_agent"] = "spy", "spy"
	want(t, f, owner, "PUT", "/api/settings", mustJSON(t, st), http.StatusOK)

	outside := want(t, f, reader, "POST", "/api/runs", `{"agent":"helper","text":"hello there"}`, http.StatusAccepted)["id"].(string)
	ended(t, f, reader, outside)
	inside := want(t, f, owner, "POST", "/api/runs", `{"agent":"helper","text":"plan the vault","project":"Secret"}`, http.StatusAccepted)["id"].(string)
	ended(t, f, owner, inside)

	// The turn in Secret is titled and taught from, by spy.
	hooks := func() (title, memory bool) {
		_, raw := get(t, f, owner, "/api/runs?limit=200")
		return strings.Contains(raw, `"sys-title-`+inside+`"`), strings.Contains(raw, `"sys-memory-`+inside+`"`)
	}
	for i := 0; ; i++ {
		if title, memory := hooks(); title && memory {
			break
		}
		if i == 400 {
			_, raw := get(t, f, owner, "/api/runs?limit=200")
			t.Fatalf("the turn in Secret had no housekeeping: %s", raw)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The turn outside it is not.
	if _, raw := get(t, f, owner, "/api/runs?limit=200"); strings.Contains(raw, "sys-title-"+outside) || strings.Contains(raw, "sys-memory-"+outside) {
		t.Errorf("spy was asked about a turn outside Secret: %s", raw)
	}
	for _, path := range []string{"/api/runs?limit=200", "/api/runs/" + outside, "/api/threads", "/api/analytics"} {
		if code, raw := get(t, f, reader, path); code != http.StatusOK || strings.Contains(raw, "spy") {
			t.Errorf("%s shows the reader spy: %d %s", path, code, raw)
		}
	}
	// The conversation of its own a housekeeping run makes is read where
	// the turn it serves is.
	_, all := read(t, f, owner, "/api/runs?limit=200")
	for _, r := range sliceOf(all["runs"]) {
		if run := obj(r); run["origin"] == "system" {
			path := "/api/threads/" + run["thread"].(string)
			if code, raw := get(t, f, reader, path); code != http.StatusNotFound {
				t.Errorf("%s, a housekeeping run's conversation in Secret, reads to the reader as %d %s", path, code, raw)
			}
			if code, _ := get(t, f, owner, path); code != http.StatusOK {
				t.Errorf("%s reads to the owner as %d", path, code)
			}
		}
	}
}

// A model policy an agent still runs on is not removed, and the refusal names
// only the agents its writer may read.
func TestAModelPolicyInUseNamesOnlyTheAgentsTheCallerMayRead(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	writer := f.machineAs(t, "svc-ops", "acme", "warp:agents:read", "warp:agents:write")
	secretAgent(t, f, owner)
	_, st := read(t, f, owner, "/api/settings")
	st["model_policies"] = []any{map[string]any{"name": "steady", "models": []any{map[string]any{"provider": "offline", "model": "offline"}}}}
	want(t, f, owner, "PUT", "/api/settings", mustJSON(t, st), http.StatusOK)
	for _, spec := range []string{
		`{"name":"mole","provider":"policy","model":"steady","system_prompt":"Digs."}`,
		`{"name":"plain","provider":"policy","model":"steady","system_prompt":"Plain."}`,
	} {
		want(t, f, owner, "POST", "/api/agents", spec, http.StatusCreated)
	}
	want(t, f, owner, "PUT", "/api/agents/mole/home", `{"project":"Secret"}`, http.StatusOK)

	clear := func(tok string) (int, string) {
		t.Helper()
		_, st := read(t, f, tok, "/api/settings")
		st["model_policies"] = []any{}
		return send(t, f, tok, "PUT", "/api/settings", mustJSON(t, st))
	}
	if code, out := clear(owner); code != http.StatusConflict || !strings.Contains(out, "mole") || !strings.Contains(out, "plain") {
		t.Errorf("the owner's removal of a policy in use = %d %s", code, out)
	}
	if code, out := clear(writer); code != http.StatusConflict || strings.Contains(out, "mole") || !strings.Contains(out, "plain") {
		t.Errorf("the writer's removal of a policy in use = %d %s", code, out)
	}
	want(t, f, owner, "PUT", "/api/agents/plain", `{"spec":{"name":"plain","provider":"offline","model":"offline","system_prompt":"Plain."},"expected_rev":1}`, http.StatusOK)
	if code, out := clear(writer); code != http.StatusConflict || strings.Contains(out, "mole") {
		t.Errorf("the writer's removal of a policy only mole runs on = %d %s", code, out)
	}
	if raw, _ := read(t, f, owner, "/api/settings"); !strings.Contains(raw, `"steady"`) {
		t.Errorf("a refused removal lost the policy: %s", raw)
	}
}
