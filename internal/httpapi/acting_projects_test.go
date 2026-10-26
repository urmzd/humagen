package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	slackflow "github.com/urmzd/humagen/pkg/integrations/slack"
	"github.com/urmzd/humagen/pkg/state/statetest"
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
	ana := f.person(t, "acme", "ana", "Ana", "humagen-admins")
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
	self := want(t, f, ana, "GET", "/api/me", "", http.StatusOK)
	personal := strings.TrimPrefix(self["personal_project"].(string), "~")
	expected := []string{"Field", "Renewals", personal}
	slices.Sort(expected)
	if got := sorted(hers); !slices.Equal(got, expected) {
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

// Home lists a person's own projects: the ones that name them, through an
// entry for their subject or a role they hold, there or as an owner above,
// which the list marks mine. Everyone's entry names nobody, nor does
// projects:govern, and a project hidden from the person is never listed,
// whatever entry in it names them.
func TestAPersonsOwnProjectsAreTheOnesThatNameThem(t *testing.T) {
	t.Run("memory", func(t *testing.T) { listsTheProjectsThatNameAPerson(t, platform.Backends{}) })
	t.Run("postgres", func(t *testing.T) {
		pool := statetest.Pool(t)
		b, release, err := platform.OpenPostgres(t.Context(), pool)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		listsTheProjectsThatNameAPerson(t, b)
	})
}

func listsTheProjectsThatNameAPerson(t *testing.T, b platform.Backends) {
	f := newFixtureGranting(t, platform.Config{
		Backends:       b,
		SlackAPI:       func(string) slackflow.API { return slackflow.NewFake() },
		ClientInterval: -1,
	}, `grant operator to subject "ana"
grant viewer to subject "ben"
grant admin to subject "gil"`)
	c := &contract{t: t, f: f, spec: loadSpec(t), seen: map[string]bool{}}
	app := f.machine(t, "acme", everything...)

	want(t, f, app, "POST", "/api/agents", helperSpec, http.StatusCreated)
	// Field is everyone's. Ana owns Launch by name, and so Docs below it;
	// ben is a member of Guild by the role he holds, though everyone owns
	// it, and of Secret by name, and a viewer of Inner. Gil is named in
	// Inner, under Secret, which only projects:govern lets him see.
	for _, body := range []string{
		`{"name":"Field"}`,
		`{"name":"Launch","members":[{"who":"*","role":"member"},{"who":"ana","role":"owner"}]}`,
		`{"name":"Docs","parent":"Launch"}`,
		`{"name":"Guild","members":[{"who":"*","role":"owner"},{"who":"role:viewer","role":"member"}]}`,
		`{"name":"Secret","members":[{"who":"ben","role":"member"},{"who":"svc-acme","role":"owner"}]}`,
		`{"name":"Inner","parent":"Secret","members":[{"who":"gil","role":"member"},{"who":"ben","role":"viewer"}]}`,
	} {
		want(t, f, app, "POST", "/api/projects", body, http.StatusCreated)
	}
	people := `"people":{"U0ANA":"ana","U0BEN":"ben","U0GIL":"gil"}`
	want(t, f, app, "POST", "/api/clients", strings.Replace(slackApp("app", "A01APP", "svc-acme"), `"machine":"svc-acme"`, `"machine":"svc-acme",`+people, 1), http.StatusCreated)

	// The projects a page lists in tree order, each with a * when it is
	// the reader's own.
	const list = "/api/projects"
	listed := func(path string) []string {
		var out []string
		for _, v := range sliceOf(c.call(app, "GET", list, path, "", http.StatusOK).field("projects")) {
			p := obj(v)
			name := p["name"].(string)
			if p["mine"] == true {
				name += "*"
			}
			out = append(out, name)
		}
		return out
	}
	as := func(user string) string { return list + "?act_as_client=app&act_as_user=" + user }
	for _, tc := range []struct {
		path string
		want []string
	}{
		{as("U0ANA"), []string{"Field", "Guild", "Launch*", "Docs*"}},
		{as("U0BEN"), []string{"Field", "Guild*", "Launch", "Docs", "Secret*", "Inner"}},
		{as("U0BEN") + "&within=Secret", []string{"Secret*", "Inner"}},
		// Gil sees everything, and none of it is his.
		{as("U0GIL"), []string{"Field", "Guild", "Launch", "Docs", "Secret", "Inner"}},
	} {
		if got := listed(tc.path); !slices.Equal(got, tc.want) {
			t.Errorf("%s lists %v, want %v", tc.path, got, tc.want)
		}
	}
	// The machine is named only where it is the owner it was made.
	if got := listed(list); !slices.Contains(got, "Secret*") || !slices.Contains(got, "Inner*") || !slices.Contains(got, "Launch") {
		t.Errorf("the machine's own list = %v", got)
	}

	// Read by ana herself, one project says the same, and so do the
	// projects above and below it.
	ana := f.person(t, "acme", "ana", "Ana")
	mine := func(v any) any { return obj(v)["mine"] }
	launch := c.call(ana, "GET", "/api/projects/{name}", "/api/projects/Launch", "", http.StatusOK)
	if mine(launch.field("project")) != true || mine(sliceOf(launch.field("ancestors"))[0]) != false || mine(sliceOf(launch.field("children"))[0]) != true {
		t.Errorf("ana's Launch = %s", launch.raw)
	}
	if field := c.call(ana, "GET", "/api/projects/{name}", "/api/projects/Field", "", http.StatusOK); mine(field.field("project")) != false {
		t.Errorf("ana's Field = %s", field.raw)
	}
}
