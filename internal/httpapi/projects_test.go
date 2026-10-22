package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/state"
)

// operator is what the built-in operator role holds, as a machine's scopes:
// projects:read and projects:write, and not projects:govern.
var operator = []string{
	"warp:agents:read", "warp:runs:read", "warp:runs:write", "warp:schedules:read", "warp:schedules:write",
	"warp:alerts:read", "warp:alerts:write", "warp:billing:read", "warp:projects:read", "warp:projects:write",
}

func status(t *testing.T, f *fixture, tok, method, path, body string) (int, map[string]any) {
	t.Helper()
	res, out := f.do(t, nil, method, path, tok, body)
	return res.StatusCode, out
}

func want(t *testing.T, f *fixture, tok, method, path, body string, code int) map[string]any {
	t.Helper()
	got, out := status(t, f, tok, method, path, body)
	if got != code {
		t.Fatalf("%s %s = %d %v, want %d", method, path, got, out, code)
	}
	return out
}

func names(list any) []string {
	var out []string
	for _, v := range list.([]any) {
		out = append(out, v.(map[string]any)["name"].(string))
	}
	return out
}

// The projects routes check projects:* and nothing else, so a token that
// still holds only runs:* is refused, and one holding projects:read reads.
func TestProjectRoutesCheckProjectsPermissionsNotRuns(t *testing.T) {
	f := newFixture(t)
	runsOnly := f.machine(t, "acme", "warp:runs:read", "warp:runs:write")
	for _, tc := range []struct{ method, path, perm string }{
		{"GET", "/api/projects", "projects:read"},
		{"POST", "/api/projects", "projects:write"},
		{"GET", "/api/projects/~", "projects:read"},
		{"GET", "/api/projects/~/memory", "projects:read"},
		{"POST", "/api/projects/~/memory", "projects:write"},
		{"GET", "/api/projects/~/history", "projects:read"},
	} {
		if got, out := status(t, f, runsOnly, tc.method, tc.path, `{}`); got != http.StatusForbidden || out["error"] != "you need "+tc.perm {
			t.Errorf("%s %s with runs:* = %d %v, want 403 you need %s", tc.method, tc.path, got, out, tc.perm)
		}
	}
	reader := f.machine(t, "acme", "warp:projects:read")
	if out := want(t, f, reader, "GET", "/api/projects", "", http.StatusOK); len(out["projects"].([]any)) != 0 {
		t.Fatalf("an empty workspace lists %v", out)
	}
	want(t, f, reader, "POST", "/api/projects", `{"name":"Mine"}`, http.StatusForbidden)
}

// Until projects have roles, the route's permission decides, except that the
// workspace and company projects, their notes and a deleted name need
// projects:govern as well.
func TestTheWorkspaceAndCompanyProjectsNeedGovern(t *testing.T) {
	f := newFixture(t)
	admin, op := f.machine(t, "acme", everything...), f.machine(t, "acme", operator...)

	if out := want(t, f, op, "POST", "/api/projects", `{"name":"News","kind":"company"}`, http.StatusForbidden); !strings.Contains(out["error"].(string), "projects:govern") {
		t.Fatalf("the refusal does not say what is needed: %v", out)
	}
	want(t, f, admin, "POST", "/api/projects", `{"name":"News","kind":"company"}`, http.StatusCreated)
	want(t, f, op, "POST", "/api/projects", `{"name":"Platform"}`, http.StatusCreated)

	// The first project written writes the workspace's own record too.
	root := want(t, f, op, "GET", "/api/projects/~", "", http.StatusOK)["project"].(map[string]any)
	if root["name"] != "" || root["kind"] != "company" || root["version"].(float64) != 1 || root["children"].(float64) != 2 || root["depth"].(float64) != 0 {
		t.Fatalf("the workspace reads %v", root)
	}
	for _, tc := range []struct{ method, path, body string }{
		{"PUT", "/api/projects/~", `{"version":1,"instructions":"Answer plainly."}`},
		{"PUT", "/api/projects/News", `{"version":1,"title":"Company news"}`},
		{"DELETE", "/api/projects/News", ""},
		{"POST", "/api/projects/~/memory", `{"text":"We sell robots."}`},
		{"POST", "/api/projects/News/memory", `{"text":"Fridays are quiet."}`},
		{"POST", "/api/projects/News/memory/approve", `{"ids":[]}`},
		{"DELETE", "/api/projects/~/memory/m1", ""},
	} {
		if got, out := status(t, f, op, tc.method, tc.path, tc.body); got != http.StatusForbidden || !strings.Contains(out["error"].(string), "projects:govern") {
			t.Errorf("an operator %s %s = %d %v, want 403 naming projects:govern", tc.method, tc.path, got, out)
		}
	}
	// A team project is the operator's to change, notes and all.
	want(t, f, op, "PUT", "/api/projects/Platform", `{"version":1,"instructions":"Ship small."}`, http.StatusOK)
	want(t, f, op, "POST", "/api/projects/Platform/memory", `{"text":"Deploys go out on Tuesdays."}`, http.StatusCreated)
	// With govern, the workspace itself takes instructions and notes.
	if out := want(t, f, admin, "PUT", "/api/projects/~", `{"version":1,"instructions":"Answer plainly."}`, http.StatusOK); out["version"].(float64) != 2 || out["instructions"] != "Answer plainly." {
		t.Fatalf("the workspace after a change = %v", out)
	}
	want(t, f, admin, "POST", "/api/projects/~/memory", `{"text":"We sell robots."}`, http.StatusCreated)
	if out := want(t, f, op, "GET", "/api/projects/~", "", http.StatusOK)["project"].(map[string]any); out["notes"].(float64) != 1 {
		t.Fatalf("the workspace's notes are not counted: %v", out)
	}

	// A deleted name comes back only with govern, and says it adopts what
	// was filed under it.
	want(t, f, op, "DELETE", "/api/projects/Platform", "", http.StatusNoContent)
	want(t, f, op, "POST", "/api/projects", `{"name":"platform"}`, http.StatusForbidden)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Platform"}`, http.StatusCreated)
	changes := want(t, f, op, "GET", "/api/projects/Platform/history", "", http.StatusOK)["changes"].([]any)
	if latest := changes[0].(map[string]any); latest["what"] != "created" || latest["note"] != "adopts its old conversations" {
		t.Fatalf("the newest change = %v", latest)
	}
}

// The workspace's notes and a company project's are reached only through the
// project routes, where projects:govern is asked for. An agent named
// workspace keeps notes of its own, and the agent memory routes answer 404
// for a name that is a project's owner, so agents:write is no way around
// govern.
func TestAnAgentNamedWorkspaceNeverReachesTheWorkspacesNotes(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	// A manager: agents:write and projects:write, not projects:govern.
	manager := f.machine(t, "acme", append([]string{"warp:agents:write"}, operator...)...)
	want(t, f, admin, "POST", "/api/projects", `{"name":"News","kind":"company"}`, http.StatusCreated)
	want(t, f, manager, "POST", "/api/projects/~/memory", `{"text":"Smuggled."}`, http.StatusForbidden)

	want(t, f, manager, "POST", "/api/agents", `{"name":"workspace","model":"offline","provider":"offline","system_prompt":"x"}`, http.StatusCreated)
	own := want(t, f, manager, "POST", "/api/agents/workspace/memory", `{"text":"Smuggled."}`, http.StatusCreated)
	everyone := want(t, f, admin, "POST", "/api/projects/~/memory", `{"text":"We sell robots."}`, http.StatusCreated)
	news := want(t, f, admin, "POST", "/api/projects/News/memory", `{"text":"Fridays are quiet."}`, http.StatusCreated)
	texts := func(path string) []string {
		t.Helper()
		var out []string
		for _, n := range want(t, f, admin, "GET", path, "", http.StatusOK)["notes"].([]any) {
			out = append(out, n.(map[string]any)["text"].(string))
		}
		return out
	}
	if got := texts("/api/projects/~/memory"); !slices.Equal(got, []string{"We sell robots."}) {
		t.Fatalf("the workspace's notes are %q", got)
	}
	if got := texts("/api/agents/workspace/memory"); !slices.Equal(got, []string{"Smuggled."}) {
		t.Fatalf("the agent named workspace holds %q", got)
	}
	if out := want(t, f, admin, "GET", "/api/projects/~", "", http.StatusOK)["project"].(map[string]any); out["notes"].(float64) != 1 {
		t.Fatalf("the workspace counts %v notes", out["notes"])
	}

	// No project's owner is an agent's name, whatever exists.
	for _, name := range []string{"project:", "project:News", "nobody"} {
		for _, tc := range []struct{ method, path, body string }{
			{"GET", "/api/agents/" + name + "/memory", ""},
			{"POST", "/api/agents/" + name + "/memory", `{"text":"Smuggled."}`},
			{"POST", "/api/agents/" + name + "/memory/approve", `{"ids":[]}`},
			{"DELETE", "/api/agents/" + name + "/memory/" + everyone["id"].(string), ""},
			{"DELETE", "/api/agents/" + name + "/memory/" + news["id"].(string), ""},
		} {
			if got, out := status(t, f, manager, tc.method, tc.path, tc.body); got != http.StatusNotFound {
				t.Errorf("%s %s = %d %v, want 404", tc.method, tc.path, got, out)
			}
		}
	}
	// The workspace's note is not the agent's to remove, and its own is.
	want(t, f, manager, "DELETE", "/api/agents/workspace/memory/"+everyone["id"].(string), "", http.StatusNotFound)
	want(t, f, manager, "DELETE", "/api/agents/workspace/memory/"+own["id"].(string), "", http.StatusNoContent)
	if got := texts("/api/projects/~/memory"); !slices.Equal(got, []string{"We sell robots."}) {
		t.Fatalf("after the agent routes the workspace's notes are %q", got)
	}
	if got := texts("/api/projects/News/memory"); !slices.Equal(got, []string{"Fridays are quiet."}) {
		t.Fatalf("after the agent routes News's notes are %q", got)
	}
}

// A conversation filed in a project that was deleted can be read, and its
// next turn is 409: it would count toward none of the limits above it.
func TestAConversationInADeletedProjectIsReadNotContinued(t *testing.T) {
	f := newFixture(t)
	op := f.machine(t, "acme", operator...)
	admin := f.machine(t, "acme", everything...)
	f.do(t, nil, "POST", "/api/agents", admin, helperSpec)
	want(t, f, op, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, op, "POST", "/api/projects", `{"name":"Sub","parent":"Field"}`, http.StatusCreated)
	want(t, f, admin, "PUT", "/api/policies", `{"scope":{"kind":"project","name":"Field"},"period":"day","max_runs":1}`, http.StatusOK)
	first := want(t, f, op, "POST", "/api/runs", `{"agent":"helper","text":"hi","project":"Sub"}`, http.StatusAccepted)
	if within, _ := first["within"].([]any); len(within) != 1 || within[0] != "Field" {
		t.Fatalf("the first turn is within %v", first["within"])
	}
	id := first["id"].(string)
	want(t, f, op, "DELETE", "/api/projects/Sub", "", http.StatusNoContent)
	for range 3 {
		out := want(t, f, op, "POST", "/api/runs", `{"agent":"helper","text":"more","after":"`+id+`"}`, http.StatusConflict)
		if msg := out["error"].(string); !strings.Contains(msg, `"Sub" was deleted`) {
			t.Fatalf("the refusal says %q", msg)
		}
	}
	thread := want(t, f, op, "GET", "/api/threads/"+first["thread"].(string), "", http.StatusOK)
	if turns := thread["turns"].([]any); len(turns) != 1 {
		t.Fatalf("the conversation holds %d turns", len(turns))
	}
}

// A project's view counts what is filed in it and not in its sub-projects,
// and the conversation filters keep one project, or a project and everything
// below it.
func TestProjectsCountAndFilterWhatIsFiledInThem(t *testing.T) {
	f := newFixture(t)
	acme := f.machine(t, "acme", everything...)
	f.do(t, nil, "POST", "/api/agents", acme, helperSpec)
	want(t, f, acme, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, acme, "POST", "/api/projects", `{"name":"Globex","parent":"Field","kind":"account"}`, http.StatusCreated)
	want(t, f, acme, "POST", "/api/projects", `{"name":"Launch"}`, http.StatusCreated)

	for _, project := range []string{"", "~", "Field", "Globex", "Globex"} {
		body := `{"agent":"helper","text":"go"}`
		if project != "" {
			body = `{"agent":"helper","text":"go","project":"` + project + `"}`
		}
		want(t, f, acme, "POST", "/api/runs", body, http.StatusAccepted)
	}
	want(t, f, acme, "POST", "/api/schedules", `{"name":"recap","expression":"0 9 * * 1","agent":"helper","text":"recap","project":"Globex"}`, http.StatusCreated)

	views := map[string]map[string]any{}
	list := want(t, f, acme, "GET", "/api/projects", "", http.StatusOK)["projects"]
	for _, v := range list.([]any) {
		views[v.(map[string]any)["name"].(string)] = v.(map[string]any)
	}
	if got := names(list); strings.Join(got, ",") != "Field,Globex,Launch" {
		t.Fatalf("tree order = %v", got)
	}
	for name, n := range map[string]float64{"Field": 1, "Globex": 2, "Launch": 0} {
		if views[name]["conversations"].(float64) != n {
			t.Errorf("%s has %v conversations, want %v", name, views[name]["conversations"], n)
		}
	}
	if views["Globex"]["schedules"].(float64) != 1 || views["Globex"]["depth"].(float64) != 2 || views["Field"]["children"].(float64) != 1 {
		t.Errorf("Globex %v, Field %v", views["Globex"], views["Field"])
	}
	if _, has := views["Globex"]["active_at"]; !has {
		t.Error("a project with conversations has no active_at")
	}
	if _, has := views["Launch"]["active_at"]; has {
		t.Error("a project with nothing filed in it has an active_at")
	}
	if root := want(t, f, acme, "GET", "/api/projects/~", "", http.StatusOK)["project"].(map[string]any); root["conversations"].(float64) != 2 {
		t.Errorf("the workspace's own conversations = %v, want the two filed in no project", root["conversations"])
	}
	if got := names(want(t, f, acme, "GET", "/api/projects?parent=~", "", http.StatusOK)["projects"]); strings.Join(got, ",") != "Field,Launch" {
		t.Errorf("the top level = %v", got)
	}
	if got := names(want(t, f, acme, "GET", "/api/projects?within=Field", "", http.StatusOK)["projects"]); strings.Join(got, ",") != "Field,Globex" {
		t.Errorf("within Field = %v", got)
	}

	count := func(query string) int {
		t.Helper()
		return len(want(t, f, acme, "GET", "/api/threads?"+query, "", http.StatusOK)["threads"].([]any))
	}
	for query, n := range map[string]int{
		"project=Field": 1, "within=Field": 3, "project=Globex": 2, "project=~": 2, "within=~": 5,
		"project=Globex&within=Field": 2, "project=Launch&within=Field": 0, "within=Field&limit=1": 1,
	} {
		if got := count(query); got != n {
			t.Errorf("threads?%s = %d, want %d", query, got, n)
		}
	}
	for _, query := range []string{"project=Ghost", "within=Ghost"} {
		want(t, f, acme, "GET", "/api/threads?"+query, "", http.StatusNotFound)
	}
}

// Another workspace's project does not exist, whatever is asked of it, and
// each workspace reads its own root.
func TestATenantSeesOnlyItsOwnTree(t *testing.T) {
	f := newFixture(t)
	acme, globex := f.machine(t, "acme", everything...), f.machine(t, "globex", everything...)
	// The workspace reads as version 0 until it is first written.
	want(t, f, acme, "PUT", "/api/projects/~", `{"version":0,"instructions":"Acme only."}`, http.StatusOK)
	if out := want(t, f, acme, "PUT", "/api/projects/~", `{"version":0,"instructions":"Twice."}`, http.StatusConflict); !strings.Contains(out["error"].(string), "the workspace") {
		t.Fatalf("a stale write to the workspace = %v", out)
	}
	want(t, f, acme, "POST", "/api/projects", `{"name":"Field","instructions":"Never promise dates."}`, http.StatusCreated)

	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/projects/Field", ""}, {"PUT", "/api/projects/Field", `{"version":1}`}, {"DELETE", "/api/projects/Field", ""},
		{"GET", "/api/projects/Field/history", ""}, {"GET", "/api/projects/Field/memory", ""},
		{"GET", "/api/projects?within=Field", ""}, {"GET", "/api/projects?parent=Field", ""},
		{"GET", "/api/threads?project=Field", ""}, {"GET", "/api/threads?within=Field", ""},
		{"POST", "/api/projects", `{"name":"Sub","parent":"Field"}`},
		{"PUT", "/api/policies", `{"scope":{"kind":"project","name":"Field"},"period":"day","max_runs":1}`},
	} {
		if got, _ := status(t, f, globex, tc.method, tc.path, tc.body); got != http.StatusNotFound {
			t.Errorf("globex %s %s = %d, want 404", tc.method, tc.path, got)
		}
	}
	root := want(t, f, globex, "GET", "/api/projects/~", "", http.StatusOK)
	if p := root["project"].(map[string]any); p["instructions"] != nil || p["children"].(float64) != 0 {
		t.Fatalf("globex reads acme's workspace: %v", root)
	}
	if out := want(t, f, globex, "GET", "/api/projects", "", http.StatusOK); len(out["projects"].([]any)) != 0 {
		t.Fatalf("globex lists %v", out)
	}
	// The same name is free in another workspace.
	want(t, f, globex, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
}

// A project's detail names every limit that holds it, its own and those of
// the projects above it, and the workspace's own are the tenant scope.
func TestAProjectsDetailNamesTheLimitsAlongItsChain(t *testing.T) {
	f := newFixture(t)
	acme := f.machine(t, "acme", everything...)
	want(t, f, acme, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, acme, "POST", "/api/projects", `{"name":"Globex","parent":"Field","kind":"account"}`, http.StatusCreated)
	want(t, f, acme, "PUT", "/api/policies", `{"scope":{"kind":"tenant"},"period":"day","max_runs":100}`, http.StatusOK)
	want(t, f, acme, "PUT", "/api/policies", `{"scope":{"kind":"project","name":"Field"},"period":"day","max_runs":7}`, http.StatusOK)
	for _, name := range []string{"~", ""} {
		want(t, f, acme, "PUT", "/api/policies", `{"scope":{"kind":"project","name":"`+name+`"},"period":"day","max_runs":7}`, http.StatusBadRequest)
	}
	var from []string
	for _, l := range want(t, f, acme, "GET", "/api/projects/Globex", "", http.StatusOK)["limits"].([]any) {
		from = append(from, l.(map[string]any)["from"].(string))
	}
	if strings.Join(from, ",") != "Field," {
		t.Fatalf("Globex is held by %q, want Field's limit then the workspace's", from)
	}
}

// Filing something under a project and deleting it race, and exactly one of
// them wins: a delete never leaves a schedule, an alert rule or a limit
// naming a project that is gone.
func TestFilingUnderAProjectAndDeletingItCannotBothWin(t *testing.T) {
	f := newFixture(t)
	acme := f.machine(t, "acme", everything...)
	f.do(t, nil, "POST", "/api/agents", acme, helperSpec)
	files := map[string]func(project string, i int) (string, string){
		"schedule": func(p string, i int) (string, string) {
			return "/api/schedules", fmt.Sprintf(`{"name":"s%d","expression":"0 9 * * *","agent":"helper","text":"go","project":%q}`, i, p)
		},
		"alert rule": func(p string, i int) (string, string) {
			return "/api/alert-rules", fmt.Sprintf(`{"name":"a%d","kind":"run_failed","severity":"warning","project":%q}`, i, p)
		},
	}
	for what, file := range files {
		for i := range 20 {
			project := fmt.Sprintf("P %s %d", strings.ReplaceAll(what, " ", "-"), i)
			want(t, f, acme, "POST", "/api/projects", `{"name":"`+project+`"}`, http.StatusCreated)
			path, body := file(project, i)
			var filed, deleted int
			var wg sync.WaitGroup
			wg.Go(func() { filed, _ = status(t, f, acme, "POST", path, body) })
			wg.Go(func() { deleted, _ = status(t, f, acme, "DELETE", "/api/projects/"+project, "") })
			wg.Wait()
			switch {
			case filed == http.StatusCreated && deleted == http.StatusConflict:
			case filed == http.StatusNotFound && deleted == http.StatusNoContent:
			default:
				t.Fatalf("%s %d: filing = %d, deleting = %d", what, i, filed, deleted)
			}
		}
	}
	// Policies race the same way.
	for i := range 20 {
		project := fmt.Sprintf("P limit %d", i)
		want(t, f, acme, "POST", "/api/projects", `{"name":"`+project+`"}`, http.StatusCreated)
		var put, deleted int
		var wg sync.WaitGroup
		wg.Go(func() {
			put, _ = status(t, f, acme, "PUT", "/api/policies", `{"scope":{"kind":"project","name":"`+project+`"},"period":"day","max_runs":3}`)
		})
		wg.Go(func() { deleted, _ = status(t, f, acme, "DELETE", "/api/projects/"+project, "") })
		wg.Wait()
		switch {
		case put == http.StatusOK && deleted == http.StatusConflict:
		case put == http.StatusNotFound && deleted == http.StatusNoContent:
		default:
			t.Fatalf("limit %d: putting = %d, deleting = %d", i, put, deleted)
		}
	}
}

// TestAProjectWrittenBeforeProjectsHadAgentsIsServedAsTheSpecSays seeds the
// records a workspace kept before projects had agents, the root, a project
// and their history with no enable list, and reads them as the console does.
func TestAProjectWrittenBeforeProjectsHadAgentsIsServedAsTheSpecSays(t *testing.T) {
	st := state.NewMemory()
	at := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	root := map[string]any{"name": "", "kind": "company", "version": 1, "created_by": "migration", "created_at": at, "updated_at": at}
	field := map[string]any{"name": "Field", "kind": "team", "version": 1, "created_by": "ada", "created_at": at, "updated_at": at}
	var ops []state.Op
	for i, rec := range []struct {
		key string
		v   any
	}{
		{"p:", root},
		{"p:Field", field},
		{"h:" + state.Seq(1), map[string]any{"seq": 1, "at": at, "by": "migration", "project": "", "what": "created", "after": root}},
		{"h:" + state.Seq(2), map[string]any{"seq": 2, "at": at, "by": "ada", "project": "Field", "what": "created", "after": field}},
	} {
		raw, err := json.Marshal(rec.v)
		if err != nil {
			t.Fatal(i, err)
		}
		ops = append(ops, state.Put("acme", rec.key, raw))
	}
	if err := st.Write(t.Context(), "projects", ops...); err != nil {
		t.Fatal(err)
	}
	f := newFixtureWith(t, platform.Config{Backends: platform.Backends{State: st}})
	c := &contract{t: t, f: f, spec: loadSpec(t), seen: map[string]bool{}}
	tok := f.machine(t, "acme", everything...)
	c.call(tok, "GET", "/api/projects", "/api/projects", "", http.StatusOK)
	c.call(tok, "GET", "/api/projects/{name}", "/api/projects/Field", "", http.StatusOK)
	c.call(tok, "GET", "/api/projects/{name}", "/api/projects/~", "", http.StatusOK)
	c.call(tok, "GET", "/api/projects/{name}/history", "/api/projects/Field/history", "", http.StatusOK)
	// The upgrade's own change to the root records the root as it read.
	h := c.call(tok, "GET", "/api/projects/{name}/history", "/api/projects/~/history", "", http.StatusOK)
	if changes, _ := h.field("changes").([]any); len(changes) != 2 {
		t.Fatalf("the root's history = %s, want the upgrade's change and the creation", h.raw)
	}
}
