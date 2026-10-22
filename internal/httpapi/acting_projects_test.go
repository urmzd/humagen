package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/warp/internal/platform"
	slackflow "github.com/urmzd/warp/pkg/integrations/slack"
	"github.com/urmzd/warp/pkg/state/statetest"
)

// A client's machine lists one of its people's projects as that person sees
// them, as the Slack app's Home does: the projects they have a role in, each
// with their role, what they may do and what they may read in it, and none
// they cannot see. A filter naming a project hidden from them answers as one
// naming nothing. Someone the client maps to nobody, or who may not read
// projects, is refused, and another credential is told the client is not
// found.
func TestAClientsMachineListsAPersonsProjectsAsTheySeeThem(t *testing.T) {
	t.Run("memory", func(t *testing.T) { listsProjectsAsThePerson(t, platform.Backends{}) })
	t.Run("postgres", func(t *testing.T) {
		pool := statetest.Pool(t)
		b, release, err := platform.OpenPostgres(t.Context(), pool)
		if err != nil {
			t.Fatal(err)
		}
		// Registered before the platform starts, so it runs once the
		// platform has stopped and before the pool is closed.
		t.Cleanup(func() { _ = release() })
		listsProjectsAsThePerson(t, b)
	})
}

func listsProjectsAsThePerson(t *testing.T, b platform.Backends) {
	f := newFixtureGranting(t, platform.Config{
		Backends:       b,
		SlackAPI:       func(string) slackflow.API { return slackflow.NewFake() },
		ClientInterval: -1,
	}, `role runner { allow read on runs }
grant operator to subject "ana"
grant viewer to subject "ben"
grant runner to subject "dee"`)
	c := &contract{t: t, f: f, spec: loadSpec(t), seen: map[string]bool{}}
	app := f.machine(t, "acme", everything...)
	other := f.machineAs(t, "svc-other", "acme", everything...)

	want(t, f, app, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, app, "POST", "/api/projects", `{"name":"Field","title":"Field engineering"}`, http.StatusCreated)
	want(t, f, app, "POST", "/api/projects", `{"name":"Renewals","parent":"Field"}`, http.StatusCreated)
	// Secret is members only: ben is its one member.
	want(t, f, app, "POST", "/api/projects", `{"name":"Secret"}`, http.StatusCreated)
	want(t, f, app, "PUT", "/api/projects/Secret/members", `{"version":1,"members":[{"who":"ben","role":"member"}]}`, http.StatusOK)
	people := `"people":{"U0ANA":"ana","U0BEN":"ben","U0DEE":"dee"}`
	want(t, f, app, "POST", "/api/clients", strings.Replace(slackApp("app", "A01APP", "svc-acme"), `"machine":"svc-acme"`, `"machine":"svc-acme",`+people, 1), http.StatusCreated)
	// A private thread of ana's in Field: hers to count, not ben's.
	ana := f.person(t, "acme", "ana", "Ana", "warp-admins")
	started := want(t, f, ana, "POST", "/api/threads", `{"project":"Field","visibility":"private","text":"@helper plan the renewal."}`, http.StatusCreated)
	ended(t, f, ana, started["runs"].([]any)[0].(map[string]any)["id"].(string))

	const list = "/api/projects"
	as := func(user, query string) string {
		return list + "?act_as_client=app&act_as_user=" + user + query
	}
	byName := func(r reply) map[string]map[string]any {
		out := map[string]map[string]any{}
		for _, v := range sliceOf(r.field("projects")) {
			out[obj(v)["name"].(string)] = obj(v)
		}
		return out
	}
	sorted := func(m map[string]map[string]any) []string {
		var out []string
		for name := range m {
			out = append(out, name)
		}
		slices.Sort(out)
		return out
	}
	access := func(v map[string]any) string {
		a := obj(v["access"])
		return a["role"].(string) + " via " + a["via"].(string)
	}

	// The machine itself governs projects, so it sees Secret and owns Field.
	own := byName(c.call(app, "GET", list, list, "", http.StatusOK))
	if got := sorted(own); !slices.Equal(got, []string{"Field", "Renewals", "Secret"}) {
		t.Fatalf("the machine's own list = %v", got)
	}

	// Ana: everyone's projects, as a member, and her private thread counted.
	hers := byName(c.call(app, "GET", list, as("U0ANA", ""), "", http.StatusOK))
	if got := sorted(hers); !slices.Equal(got, []string{"Field", "Renewals"}) {
		t.Fatalf("ana's projects = %v", got)
	}
	field := hers["Field"]
	if access(field) != "member via *" || !slices.Equal(stringsOf(field["actions"]), []string{actCreate, actPropose}) {
		t.Errorf("ana's Field = %s, actions %v", access(field), field["actions"])
	}
	if fmt.Sprint(field["conversations"]) != "1" || field["active_at"] == nil {
		t.Errorf("ana's Field counts %v conversations, active at %v", field["conversations"], field["active_at"])
	}
	if path := sliceOf(hers["Renewals"]["path"]); len(path) != 2 || obj(path[0])["title"] != "Field engineering" {
		t.Errorf("Renewals sits at %v", path)
	}

	// Ben: Secret as its member, nothing he may change, and no private
	// thread of someone else's counted.
	his := byName(c.call(app, "GET", list, as("U0BEN", ""), "", http.StatusOK))
	if got := sorted(his); !slices.Equal(got, []string{"Field", "Renewals", "Secret"}) {
		t.Fatalf("ben's projects = %v", got)
	}
	if access(his["Secret"]) != "member via ben" || len(sliceOf(his["Secret"]["actions"])) != 0 {
		t.Errorf("ben's Secret = %s, actions %v", access(his["Secret"]), his["Secret"]["actions"])
	}
	if fmt.Sprint(his["Field"]["conversations"]) != "0" || his["Field"]["active_at"] != nil {
		t.Errorf("ben's Field counts %v conversations, active at %v", his["Field"]["conversations"], his["Field"]["active_at"])
	}

	// A filter naming Secret, which ana cannot see, answers as one naming
	// a project that is not there.
	for _, filter := range []string{"&parent=", "&within="} {
		_, hidden := get(t, f, app, as("U0ANA", filter+"Secret"))
		_, missing := get(t, f, app, as("U0ANA", filter+"Ghost"))
		c.call(app, "GET", list, as("U0ANA", filter+"Secret"), "", http.StatusNotFound)
		if hidden != missing {
			t.Errorf("%sSecret for ana = %s, for a project that is not there %s", filter, hidden, missing)
		}
	}
	c.call(app, "GET", list, as("U0BEN", "&parent=Secret"), "", http.StatusOK)

	// Dee reads runs and not projects: the inbox is hers to read through
	// the client, the projects are not.
	if refused := c.call(app, "GET", list, as("U0DEE", ""), "", http.StatusForbidden); refused.str("error") != "that person cannot read projects" {
		t.Errorf("dee's projects = %s", refused.raw)
	}
	c.call(app, "GET", "/api/inbox/count", "/api/inbox/count?act_as_client=app&act_as_user=U0DEE", "", http.StatusOK)
	c.call(app, "GET", list, as("U0NOBODY", ""), "", http.StatusForbidden)
	c.call(app, "GET", list, list+"?act_as_client=app", "", http.StatusBadRequest)
	// Only the credential the client names reads for its people.
	c.call(other, "GET", list, as("U0ANA", ""), "", http.StatusNotFound)
	c.call(app, "GET", list, list+"?act_as_client=ghost&act_as_user=U0ANA", "", http.StatusNotFound)
}

func stringsOf(v any) []string {
	var out []string
	for _, s := range sliceOf(v) {
		out = append(out, s.(string))
	}
	return out
}
