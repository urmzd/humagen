package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
)

// Every route that names a person by subject, the key the platform keeps
// them by, sends what they are called beside it: their name, else their
// handle, never a subject that holds an address. The subject stays, as the
// key a change sends back.

// pat signs in with a subject that holds an address and no name, as a
// Tailscale login can, so the directory calls them by their handle.
const patSubject = "tailscale:pat@example.com"

// namesFixture is a workspace with two admins: Ana, who has a name, and Pat,
// known by an address and their handle. Field engineering is Pat's, Globex
// under it Ana's. An agent on the "ollama" provider asks Pat a question.
func namesFixture(t *testing.T) (f *fixture, ana, pat string) {
	t.Helper()
	f = newFixtureWith(t, platform.Config{AgentOptions: []agentrt.Option{
		agentrt.WithProvider(agentrt.ProviderOllama, func(agentrt.Spec) (saigetypes.Provider, error) {
			return &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{
				agenttest.ToolCallResponse("call-ask", "ask_person", map[string]any{"question": "Which colour?", "options": []any{"red", "blue"}}),
				agenttest.TextResponse("Red."),
			}}, nil
		}),
	}})
	ana = f.person(t, "acme", "00u-ana", "Ana Lima", "humagen-admins")
	pat = f.person(t, "acme", patSubject, "", "humagen-admins")
	want(t, f, pat, "POST", "/api/projects", `{"name":"Field","title":"Field engineering"}`, http.StatusCreated)
	want(t, f, ana, "POST", "/api/projects", `{"name":"Globex","parent":"Field"}`, http.StatusCreated)
	return f, ana, pat
}

// noAddressIn fails when what a route calls anyone holds an address.
func noAddressIn(t *testing.T, what string, called ...any) {
	t.Helper()
	for _, c := range called {
		if s, _ := c.(string); strings.Contains(s, "example.com") {
			t.Errorf("%s calls someone %q", what, s)
		}
	}
}

func TestProjectsCallTheirPeopleByName(t *testing.T) {
	f, ana, pat := namesFixture(t)

	// Members and the owners from above: each by subject, and by name.
	members := want(t, f, ana, "GET", "/api/projects/Globex/members", "", http.StatusOK)
	called := map[string]string{}
	for _, m := range members["members"].([]any) {
		e := m.(map[string]any)
		name, _ := e["name"].(string)
		called[e["who"].(string)] = name
	}
	if called["*"] != "" || called["00u-ana"] != "Ana Lima" || len(called) != 2 {
		t.Errorf("Globex's members are called %v", called)
	}
	inherited := members["inherited"].([]any)
	if len(inherited) != 1 {
		t.Fatalf("Globex's owners from above are %v", inherited)
	}
	if o := inherited[0].(map[string]any); o["who"] != patSubject || o["name"] != "pat" || o["from"] != "Field" {
		t.Errorf("Globex's owner from above is %v", o)
	}
	// What was read is sent back without the names: a change names who
	// holds a role by subject alone.
	if got, out := status(t, f, ana, "PUT", "/api/projects/Globex/members",
		`{"version":1,"members":[{"who":"*","role":"member"},{"who":"00u-ana","role":"owner","name":"Ana Lima"}]}`); got != http.StatusBadRequest {
		t.Errorf("a change naming a member's name = %d %v, want 400", got, out)
	}

	// The project's record of changes.
	history := want(t, f, ana, "GET", "/api/projects/Globex/history", "", http.StatusOK)["changes"].([]any)
	if c := history[len(history)-1].(map[string]any); c["what"] != "created" || c["by"] != "00u-ana" || c["by_name"] != "Ana Lima" {
		t.Errorf("Globex was created by %v", c)
	}

	// A note, as it is added and as the project's notes list it.
	note := want(t, f, pat, "POST", "/api/projects/Globex/memory", `{"text":"Globex pays net 30."}`, http.StatusCreated)
	if note["by"] != patSubject || note["by_name"] != "pat" {
		t.Errorf("the note is by %v, called %v", note["by"], note["by_name"])
	}
	notes := want(t, f, ana, "GET", "/api/projects/Globex/memory", "", http.StatusOK)["notes"].([]any)
	if n := notes[0].(map[string]any); n["by_name"] != "pat" {
		t.Errorf("the project's notes call its writer %v", n["by_name"])
	}

	// A member's note is a proposal, and its owners are told who made it by
	// name.
	lee := f.person(t, "acme", "okta|lee", "Lee Wong", "humagen-operators")
	want(t, f, lee, "POST", "/api/projects/Globex/memory", `{"text":"Globex wants a site visit."}`, http.StatusCreated)
	proposals := want(t, f, ana, "GET", "/api/inbox?kind=memory", "", http.StatusOK)["messages"].([]any)
	if len(proposals) != 1 || proposals[0].(map[string]any)["subject"] != "Lee Wong proposed a note for Globex" {
		t.Errorf("Globex's owner is told %v", subjects(map[string]any{"messages": proposals}))
	}
	if from := proposals[0].(map[string]any)["from"].(map[string]any); from["name"] != "okta|lee" || from["display"] != "Lee Wong" {
		t.Errorf("the proposal is from %v", from)
	}

	// An announcement, as it is posted and as the project's inbox shows it.
	posted := want(t, f, pat, "POST", "/api/projects/Globex/announcements", `{"subject":"Renewal signed","text":"Three more years."}`, http.StatusCreated)
	if from := posted["from"].(map[string]any); from["name"] != patSubject || from["display"] != "pat" {
		t.Errorf("the announcement is from %v", from)
	}
	inbox := want(t, f, ana, "GET", "/api/projects/Globex/inbox", "", http.StatusOK)["messages"].([]any)
	if from := inbox[0].(map[string]any)["from"].(map[string]any); from["display"] != "pat" {
		t.Errorf("Globex's inbox shows the announcement from %v", from)
	}

	// A limit, as it is set and listed, and its change.
	pol := want(t, f, pat, "PUT", "/api/policies", `{"scope":{"kind":"project","name":"Globex"},"period":"day","max_runs":40}`, http.StatusOK)
	if pol["updated_by"] != patSubject || pol["updated_by_name"] != "pat" {
		t.Errorf("the limit was set by %v, called %v", pol["updated_by"], pol["updated_by_name"])
	}
	for _, p := range want(t, f, ana, "GET", "/api/policies", "", http.StatusOK)["policies"].([]any) {
		if p := p.(map[string]any); p["updated_by_name"] != "pat" {
			t.Errorf("the limits list calls its setter %v", p["updated_by_name"])
		}
	}
	for _, c := range want(t, f, ana, "GET", "/api/policies/history", "", http.StatusOK)["changes"].([]any) {
		if c := c.(map[string]any); c["by_name"] != "pat" {
			t.Errorf("the limit's change is by %v", c["by_name"])
		}
	}
	// A limit an owner above sets, on a project no admin has limited.
	want(t, f, ana, "POST", "/api/projects", `{"name":"Initech","parent":"Field"}`, http.StatusCreated)
	limit := want(t, f, pat, "PUT", "/api/projects/Initech/limits", `{"period":"day","max_runs":30}`, http.StatusOK)
	noAddressIn(t, "a project's limit", limit["updated_by_name"])
	if limit["updated_by_name"] != "pat" {
		t.Errorf("the project's limit was set by %v", limit["updated_by_name"])
	}
}

func TestAgentsAndWhatWatchesThemCallTheirPeopleByName(t *testing.T) {
	f, ana, pat := namesFixture(t)

	created := want(t, f, pat, "POST", "/api/agents", helperSpec, http.StatusCreated)
	if created["created_by"] != patSubject || created["created_by_name"] != "pat" {
		t.Errorf("the agent was made by %v, called %v", created["created_by"], created["created_by_name"])
	}
	revised := want(t, f, ana, "PUT", "/api/agents/helper", `{"spec":{"name":"helper","model":"offline","provider":"offline","system_prompt":"Help more."},"expected_rev":1}`, http.StatusOK)
	if revised["created_by_name"] != "Ana Lima" {
		t.Errorf("the revision was saved by %v", revised["created_by_name"])
	}
	revs := want(t, f, ana, "GET", "/api/agents/helper/history", "", http.StatusOK)["revisions"].([]any)
	if len(revs) != 2 || revs[0].(map[string]any)["created_by_name"] != "Ana Lima" || revs[1].(map[string]any)["created_by_name"] != "pat" {
		t.Errorf("the agent's revisions are %v", revs)
	}
	for _, a := range want(t, f, ana, "GET", "/api/agents", "", http.StatusOK)["agents"].([]any) {
		if a := a.(map[string]any); a["spec"].(map[string]any)["name"] == "helper" && a["created_by_name"] != "Ana Lima" {
			t.Errorf("the agents list says helper was saved by %v", a["created_by_name"])
		}
	}

	pol := want(t, f, pat, "PUT", "/api/agents/helper/call-policy", `{"callers":"anyone","approval":"never"}`, http.StatusOK)
	if pol["updated_by_name"] != "pat" {
		t.Errorf("the call policy was saved by %v", pol["updated_by_name"])
	}
	if got := want(t, f, ana, "GET", "/api/agents/helper/call-policy", "", http.StatusOK); got["updated_by_name"] != "pat" {
		t.Errorf("the call policy reads as saved by %v", got["updated_by_name"])
	}

	sc := want(t, f, pat, "POST", "/api/schedules", `{"name":"nightly","expression":"0 3 * * *","agent":"helper","text":"Report."}`, http.StatusCreated)
	if sc["created_by"] != patSubject || sc["created_by_name"] != "pat" {
		t.Errorf("the schedule was made by %v, called %v", sc["created_by"], sc["created_by_name"])
	}

	// A rule, as it is made and listed, and sent back as it was read.
	rule := want(t, f, pat, "POST", "/api/alert-rules", `{"name":"any","kind":"run_failed","severity":"critical"}`, http.StatusCreated)
	if rule["created_by_name"] != "pat" {
		t.Errorf("the rule was made by %v", rule["created_by_name"])
	}
	rules := want(t, f, ana, "GET", "/api/alert-rules", "", http.StatusOK)["rules"].([]any)
	if len(rules) != 1 || rules[0].(map[string]any)["created_by_name"] != "pat" {
		t.Errorf("the rules are %v", rules)
	}
	back, err := json.Marshal(rules[0])
	if err != nil {
		t.Fatal(err)
	}
	if again := want(t, f, ana, "PUT", "/api/alert-rules/any", string(back), http.StatusOK); again["created_by_name"] != "pat" {
		t.Errorf("the rule sent back was made by %v", again["created_by_name"])
	}
}

// A question a person answers names them on the run's events and in the
// inbox, by subject and by name.
func TestWhoAnsweredAQuestionIsCalledByName(t *testing.T) {
	f, ana, pat := namesFixture(t)
	want(t, f, ana, "POST", "/api/agents", `{"name":"asker","model":"x","provider":"ollama","system_prompt":"Ask.","tools":["ask_person"],"answerers":["`+patSubject+`"]}`, http.StatusCreated)
	run := want(t, f, ana, "POST", "/api/runs", `{"agent":"asker","text":"Pick"}`, http.StatusAccepted)
	id := run["id"].(string)
	var question string
	for deadline := time.Now().Add(10 * time.Second); question == "" && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		now := want(t, f, ana, "GET", "/api/runs/"+id, "", http.StatusOK)
		q, _ := now["question"].(map[string]any)
		question, _ = q["id"].(string)
	}
	if question == "" {
		t.Fatal("the run never parked on its question")
	}
	answered := want(t, f, pat, "POST", "/api/runs/"+id+"/questions/"+question+"/answer", `{"action":"choose","choice":"red"}`, http.StatusOK)
	o := answered["parts"].([]any)[0].(map[string]any)["outcome"].(map[string]any)
	if o["by"] != patSubject || o["by_name"] != "pat" {
		t.Errorf("the question was answered by %v, called %v", o["by"], o["by_name"])
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if s := want(t, f, ana, "GET", "/api/runs/"+id, "", http.StatusOK)["status"]; s == "completed" || s == "failed" {
			break
		}
	}
	var resumed map[string]any
	for _, e := range want(t, f, ana, "GET", "/api/runs/"+id+"/events", "", http.StatusOK)["events"].([]any) {
		e := e.(map[string]any)
		if e["kind"] != "lifecycle.resumed" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(e["payload"].(string))
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		resumed, _ = body["answer"].(map[string]any)
	}
	if resumed == nil {
		t.Fatal("the run's events hold no answer")
	}
	if resumed["by"] != patSubject || resumed["by_name"] != "pat" || resumed["choice"] != "red" {
		t.Errorf("the run resumed with %v", resumed)
	}
}

// A message is called what its poster is called now: one who had no name
// when they posted, and gave one since, reads by it, as the rest of the
// console shows them.
func TestAMessageIsCalledWhatItsPosterIsCalledNow(t *testing.T) {
	f := newFixture(t)
	kim := f.person(t, "acme", "okta|kim", "", "humagen-admins")
	posted := want(t, f, kim, "POST", "/api/threads", `{"visibility":"shared","text":"hello","post_only":true}`, http.StatusCreated)
	if m := posted["message"].(map[string]any); m["name"] != "kim" {
		t.Fatalf("a poster with no name is called %v", m["name"])
	}
	id := posted["thread"].(map[string]any)["id"].(string)
	kim = f.person(t, "acme", "okta|kim", "Kim Park", "humagen-admins")
	page := want(t, f, kim, "GET", "/api/threads/"+id, "", http.StatusOK)
	if m := page["turns"].([]any)[0].(map[string]any)["message"].(map[string]any); m["name"] != "Kim Park" || m["by"] != "kim-park" {
		t.Errorf("the message is by %v, called %v", m["by"], m["name"])
	}
}

// A person who chose a name for themselves is called by it wherever they
// are named, as the Directory and the console's header call them.
func TestAPersonIsCalledTheNameTheyChose(t *testing.T) {
	f, ana, pat := namesFixture(t)
	want(t, f, pat, "PUT", "/api/preferences", `{"display_name":"Pat Quinn","theme":"system","accent":"purple"}`, http.StatusOK)
	members := want(t, f, ana, "GET", "/api/projects/Globex/members", "", http.StatusOK)
	if o := members["inherited"].([]any)[0].(map[string]any); o["name"] != "Pat Quinn" {
		t.Errorf("Globex's owner from above is called %v", o["name"])
	}
	posted := want(t, f, pat, "POST", "/api/threads", `{"visibility":"shared","text":"hello","post_only":true}`, http.StatusCreated)
	if m := posted["message"].(map[string]any); m["name"] != "Pat Quinn" {
		t.Errorf("Pat's message is called %v", m["name"])
	}
}

// A mention is from its poster as the thread calls them now. What it keeps
// of them is a name, which is no subject: read as one, it could name someone
// else. A poster who gives a name later reads by it, in the mention's
// subject too.
func TestAMentionIsFromItsPosterAsTheThreadCallsThem(t *testing.T) {
	f := newFixture(t)
	// Lee Wong signs in as "lee" and goes by lee-wong; the second person's
	// subject makes the handle lee, which is Lee Wong's subject.
	f.person(t, "acme", "lee", "Lee Wong", "humagen-admins")
	other := f.person(t, "acme", "okta|lee", "", "humagen-admins")
	zed := f.person(t, "acme", "okta|zed", "Zed Park", "humagen-admins")
	want(t, f, other, "POST", "/api/threads", `{"visibility":"shared","text":"@zed-park look","post_only":true}`, http.StatusCreated)
	from := func() (map[string]any, map[string]any) {
		t.Helper()
		got := want(t, f, zed, "GET", "/api/inbox?kind=mention", "", http.StatusOK)["messages"].([]any)
		if len(got) != 1 {
			t.Fatalf("Zed's mentions are %v", got)
		}
		m := got[0].(map[string]any)
		return m, m["from"].(map[string]any)
	}
	m, sender := from()
	if sender["kind"] != "person" || sender["display"] != "lee" || m["subject"] != "lee mentioned you in #general" {
		t.Errorf("the mention is %q, from %v", m["subject"], sender)
	}
	if by := m["parts"].([]any)[0].(map[string]any)["mention"].(map[string]any)["by"]; by != "lee" {
		t.Errorf("the mention's poster goes by %v", by)
	}

	// The poster gives a name, and their handle is made again from it.
	f.person(t, "acme", "okta|lee", "Lee Ross", "humagen-admins")
	m, sender = from()
	if sender["display"] != "Lee Ross" || m["subject"] != "Lee Ross mentioned you in #general" {
		t.Errorf("the mention is %q, from %v", m["subject"], sender)
	}
	if by := m["parts"].([]any)[0].(map[string]any)["mention"].(map[string]any)["by"]; by != "lee-ross" {
		t.Errorf("the mention's poster goes by %v", by)
	}
	noAddressIn(t, "a mention", sender["name"], sender["display"])
}
