package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/projects"
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
// an agent they may not read, and no count tells them of one. Settings that
// name the agent in another spelling keep its own name, and serve the same.
func TestAHousekeepingAgentThatLivesInAProjectServesOnlyItsTurns(t *testing.T) {
	for _, spelled := range []string{"spy", "Spy"} {
		t.Run(spelled, func(t *testing.T) { housekeepingServesOnlyItsTurns(t, spelled) })
	}
}

func housekeepingServesOnlyItsTurns(t *testing.T, spelled string) {
	f := newFixtureWith(t, platform.Config{OfflineHooks: true})
	owner := f.machine(t, "acme", everything...)
	reader := f.machineAs(t, "svc-ops", "acme", "warp:runs:read", "warp:runs:write", "warp:agents:read", "warp:projects:read")
	secretAgent(t, f, owner)
	_, st := read(t, f, owner, "/api/settings")
	st["title_agent"], st["memory_agent"] = spelled, spelled
	saved := want(t, f, owner, "PUT", "/api/settings", mustJSON(t, st), http.StatusOK)
	if saved["title_agent"] != "spy" || saved["memory_agent"] != "spy" {
		t.Errorf("the settings naming %s kept %v and %v", spelled, saved["title_agent"], saved["memory_agent"])
	}

	outside := want(t, f, reader, "POST", "/api/runs", `{"agent":"helper","text":"hello there"}`, http.StatusAccepted)["id"].(string)
	ended(t, f, reader, outside)
	counted := func() map[string]any {
		t.Helper()
		_, d := read(t, f, reader, "/api/projects/~")
		return obj(d["project"])
	}
	before := counted()
	if before["conversations"] != 1.0 {
		t.Fatalf("the workspace counts %v conversations for the reader, not their own one", before["conversations"])
	}
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
	_, all := read(t, f, owner, "/api/runs?limit=200")
	for _, r := range sliceOf(all["runs"]) {
		if run := obj(r); run["origin"] == "system" {
			ended(t, f, owner, run["id"].(string))
		}
	}
	// The turn outside it is not.
	if _, raw := get(t, f, owner, "/api/runs?limit=200"); strings.Contains(raw, "sys-title-"+outside) || strings.Contains(raw, "sys-memory-"+outside) {
		t.Errorf("spy was asked about a turn outside Secret: %s", raw)
	}
	for _, path := range []string{"/api/runs?limit=200", "/api/runs/" + outside, "/api/threads", "/api/analytics"} {
		if code, raw := get(t, f, reader, path); code != http.StatusOK || strings.Contains(strings.ToLower(raw), "spy") {
			t.Errorf("%s shows the reader spy: %d %s", path, code, raw)
		}
	}
	// The workspace's counts say nothing of the conversations spy's runs
	// made, which the reader is not shown.
	if after := counted(); after["conversations"] != before["conversations"] || after["active_at"] != before["active_at"] {
		t.Errorf("the workspace counted %v conversations, last active %v, for the reader after the turn in Secret, and %v, last active %v, before",
			after["conversations"], after["active_at"], before["conversations"], before["active_at"])
	}
	// The conversation of its own a housekeeping run makes is read where
	// the turn it serves is.
	_, all = read(t, f, owner, "/api/runs?limit=200")
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

// A thread names an agent only as its poster may read it. Tagging an agent
// they may not read tags no agent, as tagging one that does not exist, and
// naming it as a thread's default or a participant answers the same.
func TestAThreadNamesOnlyTheAgentsItsPosterMayRead(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	secretAgent(t, f, owner)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Open"}`, http.StatusCreated)
	ana := f.person(t, "acme", "00u-ana", "Ana", "warp-operators")
	if code, _ := get(t, f, ana, "/api/projects/Secret"); code != http.StatusNotFound {
		t.Fatalf("Ana reads Secret: %d", code)
	}

	// What a tag came to, in the thread it starts and in the answer.
	tagged := func(name string) string {
		t.Helper()
		out := want(t, f, ana, "POST", "/api/threads", `{"project":"Open","visibility":"shared","text":"@`+name+` hello"}`, http.StatusCreated)
		return mustJSON(t, map[string]any{"mentions": out["message"].(map[string]any)["mentions"], "refused": out["refused"], "runs": out["runs"]})
	}
	if spy, spz := tagged("spy"), tagged("spz"); strings.ReplaceAll(spy, "spy", "spz") != spz {
		t.Errorf("tagging spy came to %s, and tagging an agent that does not exist %s", spy, spz)
	}

	if code := asGhost(t, f, ana, "POST", "/api/threads", `{"project":"Open","visibility":"shared","text":"hello","default":"spy"}`); code != http.StatusBadRequest {
		t.Errorf("a thread whose default is spy = %d, want 400", code)
	}
	th := want(t, f, ana, "POST", "/api/threads", `{"project":"Open","visibility":"shared","text":"hello"}`, http.StatusCreated)["thread"].(map[string]any)
	id, version := th["id"].(string), int(th["version"].(float64))
	for _, tc := range []struct{ path, body string }{
		{"/api/threads/" + id, fmt.Sprintf(`{"version":%d,"title":"t","visibility":"shared","default":"spy"}`, version)},
		{"/api/threads/" + id + "/participants", fmt.Sprintf(`{"version":%d,"add":[{"kind":"agent","name":"spy"}]}`, version)},
	} {
		if code := asGhost(t, f, ana, "PUT", tc.path, tc.body); code != http.StatusBadRequest {
			t.Errorf("PUT %s naming spy = %d, want 400", tc.path, code)
		}
	}
}

// alike sends a body naming spy, and the same body naming an agent that does
// not exist, and fails unless both answer alike: the same status and the
// same words, the name aside. Unlike asGhost, it swaps spy inside a word, as
// in the tool tag_spy.
func alike(t *testing.T, f *fixture, tok, method, path string, body func(name string) string) int {
	t.Helper()
	code, out := send(t, f, tok, method, path, body("spy"))
	ghostCode, ghostOut := send(t, f, tok, method, path, body("spz"))
	if code != ghostCode || strings.ReplaceAll(out, "spy", "spz") != ghostOut {
		t.Errorf("%s %s naming spy = %d %s, and naming an agent that does not exist %d %s", method, path, code, out, ghostCode, ghostOut)
	}
	return code
}

// A spec and a call policy name only the agents their reader may read. One
// that lives in a project they may not read is left out of the agents a spec
// may call and the approvals of those calls, with a hash of what is shown,
// and of the callers a policy admits and trusts. A writer who saves either as
// they read it keeps that agent, names one only as an agent that exists and
// that they may read, and learns nothing by naming one they may not; nor by
// calling it from a run.
func TestASpecAndItsCallPolicyNameOnlyTheAgentsTheCallerMayRead(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	writer := f.machineAs(t, "svc-ops", "acme", "warp:agents:read", "warp:agents:write", "warp:runs:read", "warp:runs:write")
	secretAgent(t, f, owner)
	lead := func(spawn, approvals string) string {
		return `{"name":"lead","model":"offline","provider":"offline","system_prompt":"Leads.","access":{"spawn":` + spawn + `},"approvals":` + approvals + `}`
	}
	want(t, f, owner, "POST", "/api/agents", lead(`["helper","spy"]`, `["tag_helper","tag_spy"]`), http.StatusCreated)
	want(t, f, owner, "PUT", "/api/agents/helper/call-policy", `{"callers":"listed","allow":["lead","spy"],"approval":"unlisted","trusted":["lead","spy"]}`, http.StatusOK)

	ownerRaw, ownerList := read(t, f, owner, "/api/agents")
	if !strings.Contains(ownerRaw, `"spawn":["helper","spy"]`) || !strings.Contains(ownerRaw, `"tag_spy"`) {
		t.Fatalf("the owner does not see lead call spy: %s", ownerRaw)
	}
	revOf := func(list map[string]any) map[string]any {
		for _, r := range sliceOf(list["agents"]) {
			if obj(obj(r)["spec"])["name"] == "lead" {
				return obj(r)
			}
		}
		t.Fatalf("no lead in %v", list)
		return nil
	}
	for _, path := range []string{"/api/agents", "/api/agents/lead/history", "/api/agents/helper/call-policy"} {
		if raw, _ := read(t, f, writer, path); strings.Contains(raw, "spy") {
			t.Errorf("%s shows a writer who may not read Secret spy: %s", path, raw)
		}
	}
	_, list := read(t, f, writer, "/api/agents")
	shown := revOf(list)
	var spec agentspec.AgentSpec
	if err := json.Unmarshal([]byte(mustJSON(t, shown["spec"])), &spec); err != nil {
		t.Fatal(err)
	}
	if shown["hash"] == revOf(ownerList)["hash"] || shown["hash"] != spec.Normalize().Hash() {
		t.Errorf("the writer's lead hashes to %v, the owner's to %v, and what the writer is shown to %s", shown["hash"], revOf(ownerList)["hash"], spec.Normalize().Hash())
	}
	if _, pol := read(t, f, writer, "/api/agents/helper/call-policy"); !slices.Equal(listed(pol["allow"]), []string{"lead"}) || !slices.Equal(listed(pol["trusted"]), []string{"lead"}) {
		t.Errorf("the writer reads helper's call policy as %v", pol)
	}

	// Naming spy answers as naming an agent that does not exist.
	for _, tc := range []struct {
		method, path string
		body         func(string) string
		code         int
	}{
		{"POST", "/api/agents", func(a string) string {
			return `{"name":"probe","model":"offline","provider":"offline","system_prompt":"x","access":{"spawn":["` + a + `"]}}`
		}, http.StatusBadRequest},
		{"PUT", "/api/agents/lead", func(a string) string {
			return `{"spec":` + lead(`["helper","`+a+`"]`, `[]`) + `,"expected_rev":0}`
		}, http.StatusBadRequest},
		{"PUT", "/api/agents/lead", func(a string) string {
			return `{"spec":` + lead(`["helper"]`, `["tag_`+a+`"]`) + `,"expected_rev":0}`
		}, http.StatusBadRequest},
		{"PUT", "/api/agents/helper/call-policy", func(a string) string { return `{"callers":"listed","allow":["lead","` + a + `"]}` }, http.StatusBadRequest},
		{"PUT", "/api/agents/helper/call-policy", func(a string) string {
			return `{"callers":"anyone","approval":"unlisted","trusted":["` + a + `"]}`
		}, http.StatusBadRequest},
	} {
		if code := alike(t, f, writer, tc.method, tc.path, tc.body); code != tc.code {
			t.Errorf("%s %s naming spy = %d, want %d", tc.method, tc.path, code, tc.code)
		}
	}
	// And so does calling it from a run of lead, which may call it.
	run := want(t, f, owner, "POST", "/api/runs", `{"agent":"lead","text":"Lead."}`, http.StatusAccepted)["id"].(string)
	if code := alike(t, f, writer, "POST", "/api/runs/"+run+"/calls", func(a string) string { return `{"key":"probe","agent":"` + a + `","text":"probe"}` }); code != http.StatusForbidden {
		t.Errorf("calling spy from lead's run = %d, want 403", code)
	}

	// Saved as the writer reads them, both keep spy.
	_, list = read(t, f, writer, "/api/agents")
	shown = revOf(list)
	saved := want(t, f, writer, "PUT", "/api/agents/lead", mustJSON(t, map[string]any{"spec": shown["spec"], "expected_rev": shown["rev"]}), http.StatusOK)
	if strings.Contains(mustJSON(t, saved), "spy") {
		t.Errorf("the writer's save of lead answers %v", saved)
	}
	_, pol := read(t, f, writer, "/api/agents/helper/call-policy")
	want(t, f, writer, "PUT", "/api/agents/helper/call-policy", mustJSON(t, map[string]any{
		"callers": pol["callers"], "allow": pol["allow"], "approval": pol["approval"], "trusted": pol["trusted"],
		"approvers": pol["approvers"], "self_approval": pol["self_approval"], "expires_seconds": pol["expires_seconds"],
	}), http.StatusOK)
	// What the writer saved is what lead was, so it is not even a revision.
	if raw, _ := read(t, f, owner, "/api/agents/lead/history"); strings.Contains(raw, `"rev":2`) || !strings.Contains(raw, `"spawn":["helper","spy"]`) || !strings.Contains(raw, `"tag_spy"`) {
		t.Errorf("after the writer's save lead is %s", raw)
	}
	if _, pol := read(t, f, owner, "/api/agents/helper/call-policy"); !slices.Equal(listed(pol["allow"]), []string{"lead", "spy"}) || !slices.Equal(listed(pol["trusted"]), []string{"lead", "spy"}) {
		t.Errorf("after the writer's save helper's call policy is %v", pol)
	}
}

// A thread names only the agents its reader may read. An agent moved into a
// project they may not read is left out of its default and of its
// participants, since it never answered there, and who answers there reads
// exactly as in a thread with no default, as it is for them: the default is
// passed over for them. A tag of it reads as one of nobody, and what posting
// there did, or asking after a turn there, never names it and does what it
// does in a thread with no default. Saving the thread as they read it keeps
// it, and removing it answers as removing an agent that is not there.
func TestAThreadShowsOnlyTheAgentsItsReaderMayRead(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	want(t, f, owner, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/agents", spySpec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Secret"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Secret/members", `{"version":1,"members":[{"who":"svc-acme","role":"owner"}]}`, http.StatusOK)
	ana := f.person(t, "acme", "00u-ana", "Ana", "warp-operators")
	olu := f.person(t, "acme", "00u-olu", "Olu", "warp-admins")

	// While spy is a platform agent, Ana makes it the thread's default and
	// a participant; then it comes to live in Secret.
	started := want(t, f, ana, "POST", "/api/threads", `{"visibility":"shared","text":"Plans for the week.","post_only":true,"default":"spy"}`, http.StatusCreated)
	id := started["thread"].(map[string]any)["id"].(string)
	want(t, f, ana, "PUT", "/api/threads/"+id+"/participants", `{"version":1,"add":[{"kind":"agent","name":"spy"}]}`, http.StatusOK)
	want(t, f, owner, "PUT", "/api/agents/spy/home", `{"project":"Secret"}`, http.StatusOK)
	// Olu, who reads Secret, tags spy, and an agent that does not exist.
	for _, name := range []string{"spy", "spz"} {
		want(t, f, olu, "POST", "/api/threads/"+id+"/messages", `{"text":"@`+name+` have a look"}`, http.StatusCreated)
	}

	if raw, page := read(t, f, owner, "/api/threads/"+id); !strings.Contains(raw, `"default":"spy"`) || !strings.Contains(mustJSON(t, page["thread"]), `"name":"spy"`) ||
		obj(page["answers"])["agent"] != "spy" {
		t.Fatalf("the owner does not see spy in the thread: %s", raw)
	}
	// A thread beside it with no default is what Ana's reads as.
	plain := want(t, f, ana, "POST", "/api/threads", `{"visibility":"shared","text":"Plans for the month.","post_only":true}`, http.StatusCreated)
	plainID := plain["thread"].(map[string]any)["id"].(string)
	_, plainPage := read(t, f, ana, "/api/threads/"+plainID)
	raw, page := read(t, f, ana, "/api/threads/"+id)
	if record := mustJSON(t, page["thread"]); strings.Contains(record, "spy") || strings.Contains(record, `"default"`) ||
		mustJSON(t, page["answers"]) != mustJSON(t, plainPage["answers"]) {
		t.Errorf("Ana reads spy in the thread: %s, beside %s", raw, mustJSON(t, plainPage["answers"]))
	}
	var tags []string
	for _, turn := range sliceOf(page["turns"]) {
		if m := obj(obj(turn)["message"]); m != nil && strings.HasSuffix(fmt.Sprint(m["text"]), "have a look") {
			tags = append(tags, mustJSON(t, m["mentions"]))
		}
	}
	if len(tags) != 2 || strings.ReplaceAll(tags[0], "spy", "spz") != tags[1] {
		t.Errorf("to Ana, Olu's tag of spy reads %v, and of an agent that does not exist the second", tags)
	}
	if code, raw := get(t, f, ana, "/api/threads"); code != http.StatusOK || strings.Contains(raw, `"spy"`) {
		t.Errorf("Ana's threads = %d %s", code, raw)
	}

	// What posting there does names no agent Ana may not read.
	posted := want(t, f, ana, "POST", "/api/threads/"+id+"/messages", `{"text":"Any news?"}`, http.StatusCreated)
	plainPosted := want(t, f, ana, "POST", "/api/threads/"+plainID+"/messages", `{"text":"Any news?"}`, http.StatusCreated)
	if out := mustJSON(t, posted); strings.Contains(out, "spy") || posted["reason"] != plainPosted["reason"] || posted["rule"] != plainPosted["rule"] {
		t.Errorf("Ana's untagged message did %s, and in a thread with no default %s", out, mustJSON(t, plainPosted))
	}
	afterTurn := func(thread string) (int, string) {
		t.Helper()
		asked := want(t, f, ana, "POST", "/api/threads/"+thread+"/messages", `{"text":"@helper what's new?"}`, http.StatusCreated)
		turn := sliceOf(asked["runs"])[0].(map[string]any)["id"].(string)
		ended(t, f, ana, turn)
		return send(t, f, ana, "POST", "/api/runs", `{"text":"And then?","after":"`+turn+`"}`)
	}
	code, out := afterTurn(id)
	plainCode, plainOut := afterTurn(plainID)
	if strings.Contains(out, "spy") || code != plainCode || (code >= 400 && out != plainOut) {
		t.Errorf("Ana's untagged turn after one in the thread = %d %s, and in a thread with no default %d %s", code, out, plainCode, plainOut)
	}

	// Saved as Ana reads it, the thread keeps spy as its default; removing
	// spy answers as removing an agent that is not there.
	_, page = read(t, f, ana, "/api/threads/"+id)
	th := obj(page["thread"])
	version := int(th["version"].(float64))
	want(t, f, ana, "PUT", "/api/threads/"+id, fmt.Sprintf(`{"version":%d,"title":"Week","visibility":"shared"}`, version), http.StatusOK)
	if code := asGhost(t, f, ana, "PUT", "/api/threads/"+id+"/participants", fmt.Sprintf(`{"version":%d,"remove":["spy"]}`, version+1)); code != http.StatusOK {
		t.Errorf("Ana's removal of spy = %d", code)
	}
	if raw, page := read(t, f, owner, "/api/threads/"+id); !strings.Contains(raw, `"default":"spy"`) || !strings.Contains(mustJSON(t, page["thread"]), `"name":"spy"`) {
		t.Errorf("after Ana's saves the thread is %s", raw)
	}

	// helper answered there, so once it too comes to live in Secret its runs
	// still name it, and so do the participants and the conversation's
	// agents built from them.
	want(t, f, owner, "PUT", "/api/agents/helper/home", `{"project":"Secret"}`, http.StatusOK)
	raw, page = read(t, f, ana, "/api/threads/"+id)
	if !strings.Contains(mustJSON(t, page["thread"]), `"name":"helper"`) {
		t.Errorf("once helper lives in Secret, Ana reads the thread as %s", raw)
	}
	// Her tag of helper started a run, so it still reads as the tag of
	// helper that started it.
	tagged := 0
	for _, turn := range sliceOf(page["turns"]) {
		if m := obj(obj(turn)["message"]); m != nil && m["text"] == "@helper what's new?" {
			tagged++
			if got := mustJSON(t, m["mentions"]); !strings.Contains(got, `"kind":"agent","name":"helper"`) || !strings.Contains(got, `"run":`) || strings.Contains(got, "refused") {
				t.Errorf("once helper lives in Secret, Ana's tag of it reads %s", got)
			}
		}
	}
	if tagged != 1 {
		t.Errorf("Ana's thread holds %d tags of helper, want 1", tagged)
	}
	if _, list := read(t, f, ana, "/api/threads"); !strings.Contains(mustJSON(t, list), `"agents":["helper"]`) {
		t.Errorf("once helper lives in Secret, Ana's threads are %s", mustJSON(t, list))
	}
}

// A channel names only the agents its reader may read. Moving an agent into
// a project is refused while a channel's default would lose it, as for a
// project's or a client's default. A channel the platform file gives a
// default living where its reader may not read leaves it out of the channel
// and of the project's history, and who answers there reads exactly as in a
// channel with no default. Saving the channel as they read it keeps it, and
// naming it answers as naming an agent that does not exist.
func TestAChannelNamesNoDefaultItsReaderMayNotRead(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	want(t, f, owner, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/agents", spySpec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Secret"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Secret/members", `{"version":1,"members":[{"who":"svc-acme","role":"owner"}]}`, http.StatusOK)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Ops"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Ops/members", `{"version":1,"members":[{"who":"*","role":"member"},{"who":"00u-ana","role":"owner"}]}`, http.StatusOK)
	ana := f.person(t, "acme", "00u-ana", "Ana", "warp-operators")

	// While spy is a platform agent, it is #room's default, and moving it
	// into Secret is refused, as #room would lose it.
	want(t, f, ana, "POST", "/api/projects/Ops/channels", `{"name":"room","default":"spy"}`, http.StatusCreated)
	if code, out := send(t, f, owner, "PUT", "/api/agents/spy/home", `{"project":"Secret"}`); code != http.StatusConflict || !strings.Contains(out, "the default of #room in Ops") {
		t.Fatalf("moving spy into Secret while it is #room's default = %d %s", code, out)
	}
	_, list := read(t, f, ana, "/api/projects/Ops/channels")
	version := 0
	for _, c := range sliceOf(list["channels"]) {
		if obj(c)["name"] == "room" {
			version = int(obj(c)["version"].(float64))
		}
	}
	want(t, f, ana, "PUT", "/api/projects/Ops/channels/room", fmt.Sprintf(`{"version":%d,"title":"","purpose":"","default":""}`, version), http.StatusOK)
	want(t, f, owner, "PUT", "/api/agents/spy/home", `{"project":"Secret"}`, http.StatusOK)
	// The platform file sets a channel's default unchecked; so does this.
	if _, err := f.p.Projects.UpdateChannel("acme", "Ops", "room", version+1, projects.Writer{By: "file", Govern: true}, time.Now(), func(c *projects.Channel) error {
		c.Default = "spy"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	channels := func(tok string) (room, general map[string]any) {
		t.Helper()
		_, list := read(t, f, tok, "/api/projects/Ops/channels")
		for _, c := range sliceOf(list["channels"]) {
			switch obj(c)["name"] {
			case "room":
				room = obj(c)
			case "general":
				general = obj(c)
			}
		}
		return room, general
	}
	if room, _ := channels(owner); room["default"] != "spy" {
		t.Fatalf("the owner reads #room as %v", room)
	}
	room, general := channels(ana)
	if raw := mustJSON(t, room); strings.Contains(raw, "spy") || mustJSON(t, room["answers"]) != mustJSON(t, general["answers"]) {
		t.Errorf("Ana reads #room as %s, beside #general %s", raw, mustJSON(t, general))
	}
	if raw, _ := read(t, f, ana, "/api/projects/Ops/history"); strings.Contains(raw, "spy") {
		t.Errorf("Ana reads Ops's history as %s", raw)
	}
	if raw, _ := read(t, f, owner, "/api/projects/Ops/history"); !strings.Contains(raw, `"default":"spy"`) {
		t.Errorf("the owner reads Ops's history as %s", raw)
	}

	// Naming spy answers as naming an agent that does not exist; saving
	// #room as Ana reads it keeps spy.
	version = int(room["version"].(float64))
	if code := asGhost(t, f, ana, "PUT", "/api/projects/Ops/channels/room", fmt.Sprintf(`{"version":%d,"title":"","purpose":"","default":"spy"}`, version)); code != http.StatusBadRequest {
		t.Errorf("Ana naming spy #room's default = %d", code)
	}
	if code := asGhost(t, f, ana, "POST", "/api/projects/Ops/channels", `{"name":"den","default":"spy"}`); code != http.StatusBadRequest {
		t.Errorf("Ana naming spy a new channel's default = %d", code)
	}
	saved := want(t, f, ana, "PUT", "/api/projects/Ops/channels/room", fmt.Sprintf(`{"version":%d,"title":"Room","purpose":"","default":""}`, version), http.StatusOK)
	if strings.Contains(mustJSON(t, saved), "spy") {
		t.Errorf("Ana's save of #room answers %s", mustJSON(t, saved))
	}
	if room, _ := channels(owner); room["default"] != "spy" || room["title"] != "Room" {
		t.Errorf("after Ana's save the owner reads #room as %v", room)
	}
}

// A spec whose every approval is of a call to an agent its reader may not
// read reads as one with no approvals: who may approve is left out with
// them. Saving it as read keeps all three, and naming approvers with no
// approvals is refused as it is for a spec that has none.
func TestASpecWhoseEveryApprovalIsHiddenNamesNoApprovers(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	writer := f.machineAs(t, "svc-ops", "acme", "warp:agents:read", "warp:agents:write")
	secretAgent(t, f, owner)
	spec := func(name, spawn, approvals, approvers string, self bool) string {
		return fmt.Sprintf(`{"name":%q,"model":"offline","provider":"offline","system_prompt":"Leads.","access":{"spawn":%s},"approvals":%s,"approvers":%s,"self_approval":%t}`, name, spawn, approvals, approvers, self)
	}
	want(t, f, owner, "POST", "/api/agents", spec("lead", `["helper","spy"]`, `["tag_spy"]`, `["role:admin"]`, true), http.StatusCreated)
	want(t, f, owner, "POST", "/api/agents", spec("solo", `["helper"]`, `[]`, `[]`, false), http.StatusCreated)

	_, history := read(t, f, writer, "/api/agents/lead/history")
	page := obj(sliceOf(history["revisions"])[0])
	shown := obj(page["spec"])
	if raw := mustJSON(t, page); strings.Contains(raw, "spy") || strings.Contains(raw, "role:admin") || shown["self_approval"] == true {
		t.Fatalf("the writer reads lead as %s", raw)
	}
	var got agentspec.AgentSpec
	if err := json.Unmarshal([]byte(mustJSON(t, shown)), &got); err != nil {
		t.Fatal(err)
	}
	if err := got.Normalize().Validate(); err != nil {
		t.Errorf("lead as the writer reads it is not a valid spec: %v", err)
	}

	// Approvers with no approvals answer as they do on a spec with none.
	code, out := send(t, f, writer, "PUT", "/api/agents/lead", `{"spec":`+spec("lead", `["helper"]`, `[]`, `["role:ops"]`, false)+`,"expected_rev":0}`)
	soloCode, soloOut := send(t, f, writer, "PUT", "/api/agents/solo", `{"spec":`+spec("solo", `["helper"]`, `[]`, `["role:ops"]`, false)+`,"expected_rev":0}`)
	if code != http.StatusBadRequest || code != soloCode || strings.ReplaceAll(out, "lead", "solo") != soloOut {
		t.Errorf("approvers with no approvals on lead = %d %s, and on solo %d %s", code, out, soloCode, soloOut)
	}

	want(t, f, writer, "PUT", "/api/agents/lead", mustJSON(t, map[string]any{"spec": shown, "expected_rev": page["rev"]}), http.StatusOK)
	if raw, _ := read(t, f, owner, "/api/agents/lead/history"); strings.Contains(raw, `"rev":2`) || !strings.Contains(raw, `"approvers":["role:admin"]`) || !strings.Contains(raw, `"self_approval":true`) || !strings.Contains(raw, `"tag_spy"`) {
		t.Errorf("after the writer's save lead is %s", raw)
	}

	// Who may approve guards the hidden approval whatever the writer adds
	// beside it, having been shown none of it.
	latest := func() string {
		t.Helper()
		_, history := read(t, f, owner, "/api/agents/lead/history")
		return mustJSON(t, obj(sliceOf(history["revisions"])[0])["spec"])
	}
	want(t, f, writer, "PUT", "/api/agents/lead", `{"spec":`+spec("lead", `["helper"]`, `["tag_helper"]`, `[]`, false)+`,"expected_rev":0}`, http.StatusOK)
	if got := latest(); !strings.Contains(got, `"approvers":["role:admin"]`) || !strings.Contains(got, `"tag_spy"`) || !strings.Contains(got, `"tag_helper"`) {
		t.Errorf("after the writer adds an approval lead is %s", got)
	}
	// And once the writer removes every approval they were shown, and so
	// must clear who may approve, the hidden one keeps it.
	want(t, f, writer, "PUT", "/api/agents/lead", `{"spec":`+spec("lead", `["helper"]`, `[]`, `[]`, false)+`,"expected_rev":0}`, http.StatusOK)
	if got := latest(); !strings.Contains(got, `"approvers":["role:admin"]`) || !strings.Contains(got, `"approvals":["tag_spy"]`) {
		t.Errorf("after the writer removes the approvals they read lead is %s", got)
	}
}

// What a writer was not shown counts toward no limit they are told of: a
// call policy keeps the callers they were not shown beside the most they may
// list, and a thread the agents they were not shown beside the most
// participants they may add.
func TestWhatAWriterIsNotShownCountsTowardNoLimit(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	writer := f.machineAs(t, "svc-ops", "acme", "warp:agents:read", "warp:agents:write")
	want(t, f, owner, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/agents", spySpec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Secret"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Secret/members", `{"version":1,"members":[{"who":"svc-acme","role":"owner"}]}`, http.StatusOK)
	ana := f.person(t, "acme", "00u-ana", "Ana", "warp-operators")
	var names []string
	for i := range 100 {
		name := fmt.Sprintf("a%02d", i)
		names = append(names, name)
		want(t, f, owner, "POST", "/api/agents", fmt.Sprintf(`{"name":%q,"model":"offline","provider":"offline","system_prompt":"x"}`, name), http.StatusCreated)
	}
	want(t, f, owner, "PUT", "/api/agents/helper/call-policy", `{"callers":"listed","allow":["spy"],"approval":"never"}`, http.StatusOK)
	started := want(t, f, ana, "POST", "/api/threads", `{"visibility":"shared","text":"Plans.","post_only":true}`, http.StatusCreated)
	id := started["thread"].(map[string]any)["id"].(string)
	want(t, f, ana, "PUT", "/api/threads/"+id+"/participants", `{"version":1,"add":[{"kind":"agent","name":"spy"}]}`, http.StatusOK)
	want(t, f, owner, "PUT", "/api/agents/spy/home", `{"project":"Secret"}`, http.StatusOK)

	allow, _ := json.Marshal(names)
	want(t, f, writer, "PUT", "/api/agents/helper/call-policy", `{"callers":"listed","allow":`+string(allow)+`,"approval":"never"}`, http.StatusOK)
	if _, pol := read(t, f, owner, "/api/agents/helper/call-policy"); len(listed(pol["allow"])) != 101 || !slices.Contains(listed(pol["allow"]), "spy") {
		t.Errorf("after the writer's save helper's call policy allows %v", pol["allow"])
	}
	// The owner, shown all 101, saves the policy as they read it.
	_, pol := read(t, f, owner, "/api/agents/helper/call-policy")
	asRead, _ := json.Marshal(map[string]any{"callers": "listed", "allow": pol["allow"], "approval": "never"})
	want(t, f, owner, "PUT", "/api/agents/helper/call-policy", string(asRead), http.StatusOK)
	more, _ := json.Marshal(append(names, "helper"))
	if code, out := send(t, f, writer, "PUT", "/api/agents/helper/call-policy", `{"callers":"listed","allow":`+string(more)+`,"approval":"never"}`); code != http.StatusBadRequest || strings.Contains(out, "spy") {
		t.Errorf("101 callers the writer reads = %d %s", code, out)
	}

	// Ana reads herself alone in the thread, so she may add 49 more.
	add := func(from, to int) string {
		var ps []string
		for _, n := range names[from:to] {
			ps = append(ps, fmt.Sprintf(`{"kind":"agent","name":%q}`, n))
		}
		return "[" + strings.Join(ps, ",") + "]"
	}
	want(t, f, ana, "PUT", "/api/threads/"+id+"/participants", `{"version":2,"add":`+add(0, 49)+`}`, http.StatusOK)
	if code, out := send(t, f, ana, "PUT", "/api/threads/"+id+"/participants", `{"version":3,"add":`+add(49, 50)+`}`); code != http.StatusBadRequest || strings.Contains(out, "spy") {
		t.Errorf("a 51st participant Ana reads = %d %s", code, out)
	}
	if raw, page := read(t, f, owner, "/api/threads/"+id); len(sliceOf(obj(page["thread"])["participants"])) != 51 || !strings.Contains(raw, `"name":"spy"`) {
		t.Errorf("the owner reads the thread as %s", raw)
	}
}

// @auto, when nothing fits, falls back to the next named default the poster
// may read, past a nearer one they may not, as it would were that one not
// set.
func TestAutoFallsBackPastADefaultItsPosterMayNotRead(t *testing.T) {
	f := newFixture(t)
	owner := f.machine(t, "acme", everything...)
	want(t, f, owner, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/agents", spySpec, http.StatusCreated)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Secret"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Secret/members", `{"version":1,"members":[{"who":"svc-acme","role":"owner"}]}`, http.StatusOK)
	want(t, f, owner, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, owner, "PUT", "/api/projects/Field/members", `{"version":1,"members":[{"who":"*","role":"owner"}]}`, http.StatusOK)
	want(t, f, owner, "PUT", "/api/projects/Field/agents", `{"version":2,"default":"helper"}`, http.StatusOK)
	ana := f.person(t, "acme", "00u-ana", "Ana", "warp-operators")
	want(t, f, ana, "POST", "/api/projects/Field/channels", `{"name":"room","default":"auto"}`, http.StatusCreated)

	start := func(body string) string {
		t.Helper()
		started := want(t, f, ana, "POST", "/api/threads", body, http.StatusCreated)
		return started["thread"].(map[string]any)["id"].(string)
	}
	hidden := start(`{"project":"Field","channel":"room","visibility":"shared","text":"Plans.","post_only":true,"default":"spy"}`)
	plain := start(`{"project":"Field","channel":"room","visibility":"shared","text":"Plans.","post_only":true,"default":"auto"}`)
	want(t, f, owner, "PUT", "/api/agents/spy/home", `{"project":"Secret"}`, http.StatusOK)

	answeredBy := func(id string) string {
		t.Helper()
		posted := want(t, f, ana, "POST", "/api/threads/"+id+"/messages", `{"text":"@auto Anything?"}`, http.StatusCreated)
		if rs := sliceOf(posted["runs"]); len(rs) == 1 {
			return fmt.Sprint(obj(rs[0])["agent"])
		}
		return mustJSON(t, posted)
	}
	if got, without := answeredBy(hidden), answeredBy(plain); got != "helper" || without != "helper" {
		t.Errorf("Ana's @auto is answered by %s, and in a thread with auto as its default by %s", got, without)
	}
}
