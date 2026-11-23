package platform_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/signature"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/integrations/slack"
	"github.com/urmzd/humagen/pkg/runs"
)

// slackAt signs a body with a secret the way Slack does and serves it on a
// path of the ingress, answering the whole response.
func slackAt(h http.Handler, secret, path, body string) *httptest.ResponseRecorder {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":" + body))
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set(signature.HeaderSlackTimestamp, ts)
	r.Header.Set(signature.HeaderSlackSignature, "v0="+hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// answeredIn reports whether a client's answer in a Slack thread, or the
// answer with that ts, is on record, so a reply or a reaction there is its.
func answeredIn(p *platform.Platform, client, channel, ts string) bool {
	return p.SurfaceAnswered(tenant, client, address.Address{Connector: "slack", Workspace: team, Path: []string{channel}}.WithParam("thread", ts))
}

func slackCommand(teamID, user, trigger string) string {
	form := url.Values{"channel_id": {"C0CODE"}, "user_id": {user}, "command": {"/run-tests"}, "text": {"now"}, "trigger_id": {trigger}, "api_app_id": {"A0DOCS"}}
	if teamID != "" {
		form.Set("team_id", teamID)
	}
	return form.Encode()
}

// Each way a Slack delivery asks the webhook connector for work starts or
// continues a run for a person the client maps, writing from its team, and
// the run acts for that person.
func TestASlackConnectorStartsRunsForMappedPeople(t *testing.T) {
	f := &fakes{}
	var says [][]saigetypes.Delta
	for range 6 {
		says = append(says, say("Docs here."))
	}
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"docs": says}}, f.config())
	in := slackClient("docs-app", "A0DOCS", team, "docs")
	in.Slack.Commands = []clients.Command{{Name: "run-tests", Description: "Run the test suite"}}
	create(t, p, tenant, in)
	ingress := p.IngressHandler()
	event := func(id, inner string) string {
		return `{"type":"event_callback","api_app_id":"A0DOCS","team_id":"` + team + `","event_id":"` + id + `","event":` + inner + `}`
	}
	post := func(path, body string) {
		t.Helper()
		if w := slackAt(ingress, "secret-docs-app", path, body); w.Code != http.StatusOK || w.Body.Len() != 0 {
			t.Fatalf("answered %d %q", w.Code, w.Body.String())
		}
	}
	mine := func(r runs.Run) {
		t.Helper()
		if r.Actor.ID != "U0HUMAN1" || r.Actor.User != "human-one" || r.Client != "docs-app" {
			t.Fatalf("the run acts for %+v through %q, want the mapped person", r.Actor, r.Client)
		}
	}

	post("/connectors/slack", event("Ev1", `{"type":"app_mention","user":"U0HUMAN1","text":"<@U08BOTID> go","ts":"1700000000.000100","channel":"C01"}`))
	mine(await(t, p, runtime.RunID(tenant, "Ev1"), runs.Completed))
	eventually(t, "the answer is in the thread", func() bool { return answeredIn(p, "docs-app", "C01", "1700000000.000100") })

	// A reply in the thread the client answered in continues it.
	post("/connectors/slack", event("Ev2", `{"type":"message","channel_type":"channel","user":"U0HUMAN1","text":"and then?","ts":"1700000000.000200","thread_ts":"1700000000.000100","channel":"C01"}`))
	mine(await(t, p, runtime.RunID(tenant, "Ev2"), runs.Completed))

	post("/connectors/slack", event("Ev3", `{"type":"message","channel_type":"im","user":"U0HUMAN1","text":"hello","ts":"1700000000.000300","channel":"D01"}`))
	mine(await(t, p, runtime.RunID(tenant, "Ev3"), runs.Completed))

	// The sender's own team, where Slack names it, is the client's.
	post("/connectors/slack", event("Ev4", `{"type":"app_mention","user":"U0HUMAN1","user_team":"`+team+`","text":"<@U08BOTID> again","ts":"1700000000.000400","channel":"C01"}`))
	mine(await(t, p, runtime.RunID(tenant, "Ev4"), runs.Completed))

	post("/connectors/slack/commands", slackCommand(team, "U0HUMAN1", "trig1"))
	mine(runWith(t, p, "/run-tests now", "C0CODE"))
}

// The same deliveries from anyone else are acknowledged and start nothing:
// someone the client maps to nobody, a mapped user id from another team, a
// delivery that names no team, an app's bot user, and a bot's post. A slash
// command is answered to its sender alone.
func TestASlackConnectorStartsNothingForAnyoneElse(t *testing.T) {
	f := &fakes{}
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"docs": {say("Docs here."), say("Never.")}}}, f.config())
	in := slackClient("docs-app", "A0DOCS", team, "docs")
	in.Slack.Commands = []clients.Command{{Name: "run-tests", Description: "Run the test suite"}}
	create(t, p, tenant, in)
	ingress := p.IngressHandler()
	event := func(teamField, id, inner string) string {
		return `{"type":"event_callback","api_app_id":"A0DOCS",` + teamField + `"event_id":"` + id + `","event":` + inner + `}`
	}
	here := `"team_id":"` + team + `",`

	// A mapped person's mention gives the client a thread it answered in.
	if w := slackAt(ingress, "secret-docs-app", "/connectors/slack", event(here, "Ev1", `{"type":"app_mention","user":"U0HUMAN1","text":"<@U08BOTID> go","ts":"1700000000.000100","channel":"C01"}`)); w.Code != http.StatusOK {
		t.Fatalf("answered %d", w.Code)
	}
	await(t, p, runtime.RunID(tenant, "Ev1"), runs.Completed)
	fake := f.slackFor("token-docs-app")
	eventually(t, "the answer is in the thread", func() bool {
		return said(fake, "Docs here.") && answeredIn(p, "docs-app", "C01", "1700000000.000100")
	})
	posted := len(fake.Messages())

	for name, body := range map[string]string{
		"a mention from someone unmapped":        event(here, "Ev10", `{"type":"app_mention","user":"U0STRANGER","text":"<@U08BOTID> go","ts":"1700000001.000100","channel":"C01"}`),
		"a direct message from someone unmapped": event(here, "Ev11", `{"type":"message","channel_type":"im","user":"U0STRANGER","text":"hello","ts":"1700000001.000200","channel":"D09"}`),
		"a thread reply from someone unmapped":   event(here, "Ev12", `{"type":"message","channel_type":"channel","user":"U0STRANGER","text":"and then?","ts":"1700000001.000300","thread_ts":"1700000000.000100","channel":"C01"}`),
		"a file from someone unmapped":           event(here, "Ev13", `{"type":"message","subtype":"file_share","channel_type":"im","user":"U0STRANGER","text":"read","ts":"1700000001.000400","channel":"D09","files":[{"id":"F01"}]}`),
		"a mapped user id from another team":     event(here, "Ev14", `{"type":"app_mention","user":"U0HUMAN1","user_team":"T09OTHER","text":"<@U08BOTID> go","ts":"1700000001.000500","channel":"C01"}`),
		"a delivery that names no team":          event("", "Ev16", `{"type":"app_mention","user":"U0HUMAN1","text":"<@U08BOTID> go","ts":"1700000001.000700","channel":"C01"}`),
		"an app's bot user":                      event(here, "Ev17", `{"type":"app_mention","user":"U08BOTID","text":"<@U08BOTID> go","ts":"1700000001.000800","channel":"C01"}`),
		"a bot's post":                           event(here, "Ev18", `{"type":"message","channel_type":"im","bot_id":"B01","user":"U0HUMAN1","text":"hello","ts":"1700000001.000900","channel":"D01"}`),
	} {
		if w := slackAt(ingress, "secret-docs-app", "/connectors/slack", body); w.Code != http.StatusOK || w.Body.Len() != 0 {
			t.Errorf("%s answered %d %q, want an empty 200", name, w.Code, w.Body.String())
		}
	}
	for name, form := range map[string]string{
		"a command from someone unmapped":  slackCommand(team, "U0STRANGER", "trig10"),
		"a command that names no team":     slackCommand("", "U0HUMAN1", "trig12"),
		"a command from an app's bot user": slackCommand(team, "U08BOTID", "trig13"),
	} {
		w := slackAt(ingress, "secret-docs-app", "/connectors/slack/commands", form)
		var reply struct {
			Type string `json:"response_type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || w.Code != http.StatusOK || reply.Type != "ephemeral" || reply.Text != slack.NotConnected {
			t.Errorf("%s answered %d %q, want the ephemeral not-connected reply", name, w.Code, w.Body.String())
		}
	}
	// A delivery for a team no client of the app serves finds no client.
	for name, w := range map[string]*httptest.ResponseRecorder{
		"a mention for another team": slackAt(ingress, "secret-docs-app", "/connectors/slack", event(`"team_id":"T09OTHER",`, "Ev15", `{"type":"app_mention","user":"U0HUMAN1","text":"<@U08BOTID> go","ts":"1700000001.000600","channel":"C01"}`)),
		"a command for another team": slackAt(ingress, "secret-docs-app", "/connectors/slack/commands", slackCommand("T09OTHER", "U0HUMAN1", "trig11")),
	} {
		if w.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", name, w.Code)
		}
	}
	// The connector answers before anything is raised, so nothing is pending.
	if all := p.Runs.List(tenant, 50); len(all) != 1 {
		t.Fatalf("%d runs, want the mapped person's one", len(all))
	}
	if n := len(fake.Messages()); n != posted {
		t.Fatalf("%d messages were posted for people the client does not map", n-posted)
	}
}

// A connector an embedding program passes as a Connection has no client
// record, so no team and no people: the platform starts nothing for what it
// raises, whoever wrote it.
func TestAConnectionWithoutAClientStartsNothingOnSlack(t *testing.T) {
	const secret = "8f742231b10e8888abcd99yyyzzz85a5"
	fake := slack.NewFake()
	peer, err := slack.New(slack.Config{
		API: fake, Verifier: signature.NewSlack(secret), Team: team,
		Bots: map[string]string{"U08BOTID": "lead"}, Sessions: true, Interval: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	heard := &logs{}
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"lead": {say("Never.")}}}, platform.Config{
		Policy: slackPeoplePolicy, Log: slog.New(slog.NewTextHandler(heard, nil)),
		Connections: func(wire.AgentStore) ([]platform.Connection, error) {
			return []platform.Connection{{Tenant: tenant, Peer: peer}}, nil
		},
	})
	ingress := p.IngressHandler()
	eventually(t, "the slack trigger is ingesting", peer.Ingesting)
	if w := slackAt(ingress, secret, "/connectors/slack", mentionOf("A0LEAD", team, "Ev1", "C01", "1700000000.000100")); w.Code != http.StatusOK {
		t.Fatalf("mention answered %d", w.Code)
	}
	eventually(t, "the mention was refused", func() bool {
		return strings.Contains(heard.String(), "a client's message was refused")
	})
	if all := p.Runs.List(tenant, 10); len(all) != 0 || len(fake.Messages()) != 0 {
		t.Fatalf("%d runs and %d messages, want none", len(all), len(fake.Messages()))
	}
}
