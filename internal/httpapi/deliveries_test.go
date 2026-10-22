package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/warp/internal/platform"
)

// slackApp is a Slack client whose machine is the credential named.
func slackApp(id, app, machine string) string {
	return `{"id":"` + id + `","name":"` + id + `","surface":"slack","identity":{"app_id":"` + app + `","team":"T01ABCDEF"},"default_agent":"helper",` +
		`"slack":{"machine":"` + machine + `"},"credentials":{"token":{"value":"` + botToken + `"},"signing_secret":{"value":"` + signingSecret + `"}}}`
}

// Only a client's own machine records where it posted a message, and only it
// is shown that receipt: another client's machine and a person see none.
func TestOnlyAClientsMachineRecordsAndSeesItsDeliveries(t *testing.T) {
	f := newFixture(t)
	admin, ada := f.machine(t, "acme", everything...), f.signedAs(t, "ada")
	one, two := f.signedAs(t, "svc-one"), f.signedAs(t, "svc-two")
	f.do(t, nil, "POST", "/api/agents", admin, helperSpec)
	for _, c := range []string{slackApp("one", "A01ONE", "svc-one"), slackApp("two", "A01TWO", "svc-two")} {
		if res, out := f.do(t, nil, "POST", "/api/clients", admin, c); res.StatusCode != http.StatusCreated {
			t.Fatalf("client = %d %v", res.StatusCode, out)
		}
	}
	// A refusal at a limit raises a message everyone may read.
	f.do(t, nil, "PUT", "/api/policies", admin, `{"scope":{"kind":"agent","name":"helper"},"period":"day","max_runs":1}`)
	f.do(t, nil, "POST", "/api/runs", admin, `{"agent":"helper","text":"one"}`)
	f.do(t, nil, "POST", "/api/runs", admin, `{"agent":"helper","text":"two"}`)
	var id string
	for range 200 {
		if _, page := f.do(t, nil, "GET", "/api/inbox?kind=budget", ada, ""); len(page["messages"].([]any)) > 0 {
			id = page["messages"].([]any)[0].(map[string]any)["id"].(string)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("no message to deliver")
	}
	put := func(tok, client, body string) int {
		t.Helper()
		res, _ := f.do(t, nil, "PUT", "/api/inbox/"+id+"/deliveries/"+client, tok, body)
		return res.StatusCode
	}
	post := `{"channel":"C0123ABC","ts":"1700000000.000100"}`
	for name, c := range map[string]struct {
		tok, client string
		want        int
	}{
		"its machine":         {one, "one", http.StatusNoContent},
		"the other machine":   {two, "two", http.StatusNoContent},
		"a person":            {ada, "one", http.StatusNotFound},
		"another's machine":   {two, "one", http.StatusNotFound},
		"a client not there":  {one, "ghost", http.StatusNotFound},
		"another workspace":   {f.machine(t, "globex", everything...), "one", http.StatusNotFound},
		"no permission to":    {f.iss.Sign(t, "svc-one", audience, map[string]any{"org": "acme", "scp": []string{"warp:runs:read"}}), "one", http.StatusForbidden},
		"a message not there": {one, "one", 0},
	} {
		if c.want == 0 {
			if res, _ := f.do(t, nil, "PUT", "/api/inbox/m-999/deliveries/one", c.tok, post); res.StatusCode != http.StatusNotFound {
				t.Errorf("%s = %d", name, res.StatusCode)
			}
			continue
		}
		if got := put(c.tok, c.client, post); got != c.want {
			t.Errorf("%s = %d, want %d", name, got, c.want)
		}
	}

	seen := func(tok string) map[string]any {
		t.Helper()
		_, out := f.do(t, nil, "GET", "/api/inbox/"+id, tok, "")
		d, _ := out["message"].(map[string]any)["deliveries"].(map[string]any)
		return d
	}
	if d := seen(one); len(d) != 1 || d["one"] == nil {
		t.Errorf("the first machine sees %v", d)
	}
	if d := seen(two); len(d) != 1 || d["two"] == nil {
		t.Errorf("the second machine sees %v", d)
	}
	if d := seen(ada); d != nil {
		t.Errorf("a person sees %v", d)
	}
	_, page := f.do(t, nil, "GET", "/api/inbox?kind=budget", one, "")
	if d, _ := page["messages"].([]any)[0].(map[string]any)["deliveries"].(map[string]any); len(d) != 1 || d["one"] == nil {
		t.Errorf("the first machine's page shows %v", d)
	}
}

// An announcement a Slack app posts to the channel its project is bound to
// keeps its receipt: the app's machine records where it posted it, as it
// reads it, and reads the receipt back, so an edit or a withdrawal updates
// that post rather than posting again.
func TestAnAnnouncementsSlackPostKeepsItsReceipt(t *testing.T) {
	f, ana, _, _ := acmeNews(t)
	admin, one := f.machine(t, "acme", everything...), f.signedAs(t, "svc-one")
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, admin, "POST", "/api/clients", slackApp("one", "A01ONE", "svc-one"), http.StatusCreated)
	want(t, f, admin, "PUT", "/api/projects/Field/channels/general", `{"version":0,"slack":{"client":"one","channel":"C01GEN"}}`, http.StatusOK)
	want(t, f, ana, "POST", "/api/projects/Field/announcements", `{"subject":"Field offsite","text":"Lisbon, in May."}`, http.StatusCreated)
	page := want(t, f, one, "GET", "/api/inbox?kind=announcement", "", http.StatusOK)["messages"].([]any)
	if len(page) != 1 {
		t.Fatalf("the app's machine reads %v", page)
	}
	id := page[0].(map[string]any)["id"].(string)
	want(t, f, one, "PUT", "/api/inbox/"+id+"/deliveries/one", `{"channel":"C01GEN","ts":"1700000000.000100"}`, http.StatusNoContent)
	got := want(t, f, one, "GET", "/api/inbox/"+id, "", http.StatusOK)["message"].(map[string]any)
	if d, _ := got["deliveries"].(map[string]any)["one"].(map[string]any); d["channel"] != "C01GEN" || d["ts"] != "1700000000.000100" {
		t.Fatalf("the receipt reads back as %v", got["deliveries"])
	}
}

// The app posts the news of a project whose general channel is bound to it
// whatever its role there, so its machine reads and keeps the receipt of an
// announcement of Globex, where everyone is only a viewer.
func TestAnAnnouncementWhereTheAppOnlyViewsKeepsItsReceipt(t *testing.T) {
	f, ana, _, _ := acmeNews(t)
	admin, one := f.machine(t, "acme", everything...), f.signedAs(t, "svc-one")
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	want(t, f, admin, "POST", "/api/clients", slackApp("one", "A01ONE", "svc-one"), http.StatusCreated)
	want(t, f, admin, "PUT", "/api/projects/Globex/channels/general", `{"version":0,"slack":{"client":"one","channel":"C01GLX"}}`, http.StatusOK)
	id := want(t, f, ana, "POST", "/api/projects/Globex/announcements", `{"subject":"Renewal signed","text":"Three more years."}`, http.StatusCreated)["id"].(string)
	want(t, f, one, "GET", "/api/inbox/"+id, "", http.StatusOK)
	want(t, f, one, "PUT", "/api/inbox/"+id+"/deliveries/one", `{"channel":"C01GLX","ts":"1700000000.000200"}`, http.StatusNoContent)
	got := want(t, f, one, "GET", "/api/inbox/"+id, "", http.StatusOK)["message"].(map[string]any)
	if d, _ := got["deliveries"].(map[string]any)["one"].(map[string]any); d["ts"] != "1700000000.000200" {
		t.Fatalf("the receipt reads back as %v", got["deliveries"])
	}
}

// A Slack app catches up on a bound project's announcements from the
// project's own inbox, newest first and withdrawn ones too. There, as
// wherever a message is read, a client's machine is shown its own client's
// receipt, and another client's machine and a person are shown none.
func TestAProjectsInboxShowsOnlyTheReadersOwnReceipts(t *testing.T) {
	f, ana, _, _ := acmeNews(t)
	admin := f.machine(t, "acme", everything...)
	one, two := f.signedAs(t, "svc-one"), f.signedAs(t, "svc-two")
	dana := f.person(t, "acme", "dana", "Dana", "warp-admins")
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	for _, c := range []string{slackApp("one", "A01ONE", "svc-one"), slackApp("two", "A01TWO", "svc-two")} {
		want(t, f, admin, "POST", "/api/clients", c, http.StatusCreated)
	}
	want(t, f, admin, "PUT", "/api/projects/Globex/channels/general", `{"version":0,"slack":{"client":"one","channel":"C01GLX"}}`, http.StatusOK)
	id := want(t, f, ana, "POST", "/api/projects/Globex/announcements", `{"subject":"Renewal signed","text":"Three more years."}`, http.StatusCreated)["id"].(string)
	want(t, f, one, "PUT", "/api/inbox/"+id+"/deliveries/one", `{"channel":"C01GLX","ts":"1700000000.000200"}`, http.StatusNoContent)
	want(t, f, two, "PUT", "/api/inbox/"+id+"/deliveries/two", `{"channel":"C02GLX","ts":"1700000000.000300"}`, http.StatusNoContent)
	want(t, f, ana, "DELETE", "/api/projects/Globex/announcements/"+id, "", http.StatusNoContent)

	receipts := func(tok string) map[string]any {
		t.Helper()
		page := want(t, f, tok, "GET", "/api/projects/Globex/inbox?kind=announcement&state=all", "", http.StatusOK)["messages"].([]any)
		if len(page) != 1 || page[0].(map[string]any)["id"] != id {
			t.Fatalf("Globex's inbox reads %v", page)
		}
		d, _ := page[0].(map[string]any)["deliveries"].(map[string]any)
		return d
	}
	if d := receipts(one); len(d) != 1 || d["one"].(map[string]any)["ts"] != "1700000000.000200" {
		t.Errorf("the first machine reads %v", d)
	}
	if d := receipts(two); len(d) != 1 || d["two"].(map[string]any)["ts"] != "1700000000.000300" {
		t.Errorf("the second machine reads %v", d)
	}
	for name, tok := range map[string]string{"its author": ana, "a person": dana} {
		if d := receipts(tok); d != nil {
			t.Errorf("%s reads %v", name, d)
		}
	}
	// Once the app has edited its post to say it was withdrawn, it records
	// the same post again, and the receipt says so from then on.
	before := receipts(one)["one"].(map[string]any)["at"]
	want(t, f, one, "PUT", "/api/inbox/"+id+"/deliveries/one", `{"channel":"C01GLX","ts":"1700000000.000200"}`, http.StatusNoContent)
	after := receipts(one)["one"].(map[string]any)
	if after["at"] == before || after["ts"] != "1700000000.000200" {
		t.Errorf("the post recorded again after the withdrawal reads %v, recorded first at %v", after, before)
	}
}

// A reply in the Slack thread of an announcement the app posted is a thread
// whose turns start with the announcement, from the person who posted it,
// read as the spec says and under the thread's one read rule: in a
// members-only project someone who is not a member finds no thread. A
// withdrawn announcement then says only that, and a reply under one that was
// withdrawn before anyone replied starts nothing.
func TestAnAnnouncementsSlackThreadIsReadAsTheSpecSays(t *testing.T) {
	f := newFixtureGranting(t, platform.Config{}, `grant operator to subject "ana"`)
	c := &contract{t: t, f: f, spec: loadSpec(t), seen: map[string]bool{}}
	admin, app := f.machine(t, "acme", everything...), f.signedAs(t, "svc-one")
	ana, bo := f.person(t, "acme", "ana", "Ana", "warp-operators"), f.person(t, "acme", "bo", "Bo", "warp-operators")
	want(t, f, admin, "POST", "/api/agents", helperSpec, http.StatusCreated)
	client := strings.Replace(slackApp("one", "A01ONE", "svc-one"), `"machine":"svc-one"`, `"machine":"svc-one","people":{"U01ANA":"ana"}`, 1)
	want(t, f, admin, "POST", "/api/clients", client, http.StatusCreated)
	want(t, f, admin, "POST", "/api/projects", `{"name":"Vault","kind":"account","members":[{"who":"ana","role":"owner"},{"who":"svc-one","role":"viewer"}]}`, http.StatusCreated)
	want(t, f, admin, "PUT", "/api/projects/Vault/channels/general", `{"version":0,"slack":{"client":"one","channel":"C01VAULT"}}`, http.StatusOK)

	posted := func(subject, ts string) string {
		t.Helper()
		id := want(t, f, ana, "POST", "/api/projects/Vault/announcements", `{"subject":"`+subject+`","text":"Read it in the vault."}`, http.StatusCreated)["id"].(string)
		want(t, f, app, "PUT", "/api/inbox/"+id+"/deliveries/one", `{"channel":"C01VAULT","ts":"`+ts+`"}`, http.StatusNoContent)
		return id
	}
	reply := func(ts, message string) string {
		return `{"text":"<@UAPP> what does it mean?","act_as":{"client":"one","user":"U01ANA"},"surface":{"client":"one","channel":"C01VAULT","ts":"` + ts + `","message":"` + message + `","app_user":"UAPP"}}`
	}
	news := posted("Vault audit", "1700000000.000100")
	started := c.call(app, "POST", "/api/threads", "/api/threads", reply("1700000000.000100", "1700000000.000200"), http.StatusCreated)
	th := obj(started.field("thread"))
	if th["announcement"] != news || th["project"] != "Vault" || obj(started.field("message"))["by"] != "ana" {
		t.Fatalf("the reply started %s", started.raw)
	}
	path := "/api/threads/" + th["id"].(string)
	page := c.call(ana, "GET", "/api/threads/{id}", path, "", http.StatusOK)
	turns := sliceOf(page.field("turns"))
	if len(turns) < 2 || obj(turns[0])["kind"] != "announcement" || obj(turns[1])["kind"] != "message" {
		t.Fatalf("the thread reads %s", page.raw)
	}
	if a := obj(obj(turns[0])["announcement"]); a["id"] != news || a["kind"] != "person" || a["by"] != "ana" || a["name"] != "Ana" || a["subject"] != "Vault audit" {
		t.Fatalf("its first turn is %v", a)
	}
	c.call(app, "GET", "/api/threads/{id}", path, "", http.StatusOK)
	c.call(bo, "GET", "/api/threads/{id}", path, "", http.StatusNotFound)

	want(t, f, ana, "DELETE", "/api/projects/Vault/announcements/"+news, "", http.StatusNoContent)
	page = c.call(ana, "GET", "/api/threads/{id}", path, "", http.StatusOK)
	if a := obj(obj(sliceOf(page.field("turns"))[0])["announcement"]); a["withdrawn"] != true || a["subject"] != nil || a["text"] != nil {
		t.Fatalf("a withdrawn announcement reads %v", a)
	}
	gone := posted("Vault closed", "1700000000.000500")
	want(t, f, ana, "DELETE", "/api/projects/Vault/announcements/"+gone, "", http.StatusNoContent)
	c.call(app, "POST", "/api/threads", "/api/threads", reply("1700000000.000500", "1700000000.000600"), http.StatusGone)
}
