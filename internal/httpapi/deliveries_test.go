package httpapi

import (
	"net/http"
	"testing"
	"time"
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
