package slack_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/integrations/slack"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/signature"
)

const (
	senderSecret = "8f742231b10e8888abcd99yyyzzz85a5"
	senderTeam   = "T01ABCDEF"
	knownThread  = "1700000000.000100"
)

// gate is a connector whose client maps one Slack user, U0ANA, in its own
// team. It records who the connector asked about and what it raised.
type gate struct {
	t    *testing.T
	c    *slack.Connector
	fake *slack.Fake

	mu     sync.Mutex
	asked  []string
	raised []event.Envelope
	// unasked is what was raised without its sender having been asked
	// about and found to be the client's.
	unasked []string
}

// reset forgets what was asked and raised so far.
func (g *gate) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked, g.raised = nil, nil
}

func newGate(t *testing.T, with ...func(*slack.Config)) *gate {
	t.Helper()
	g := &gate{t: t, fake: slack.NewFake()}
	cfg := slack.Config{
		API: g.fake, Verifier: signature.NewSlack(senderSecret), Team: senderTeam, Tenant: "acme",
		Bots: map[string]string{"U08BOTID": "lead"}, Sessions: true, Interval: -1,
		Commands:       []slack.Command{{Name: "run-tests", Description: "Run the test suite"}},
		ContinueThread: func(_, _, thread string) bool { return thread == knownThread },
		Sender: func(team, user string) bool {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.asked = append(g.asked, team+"/"+user)
			return team == senderTeam && user == "U0ANA"
		},
	}
	for _, fn := range with {
		fn(&cfg)
	}
	c, err := slack.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	g.c = c
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Ingest(ctx, func(_ context.Context, e event.Envelope) error {
			g.mu.Lock()
			defer g.mu.Unlock()
			// Whatever is raised was asked about first, and is the
			// client's to act for.
			if who := wire.ActorOf(e).ID; !slices.Contains(g.asked, senderTeam+"/"+who) || who != "U0ANA" {
				g.unasked = append(g.unasked, e.ID+" from "+who)
			}
			g.raised = append(g.raised, e)
			return nil
		})
	}()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(10 * time.Second)
	for !c.Ingesting() {
		if time.Now().After(deadline) {
			t.Fatal("the trigger never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return g
}

// post signs a body the way Slack does and serves it on the connector.
func (g *gate) post(path, body string) *httptest.ResponseRecorder {
	g.t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(senderSecret))
	mac.Write([]byte("v0:" + ts + ":" + body))
	r := httptest.NewRequest(http.MethodPost, "/connectors/slack"+path, strings.NewReader(body))
	r.Header.Set(signature.HeaderSlackTimestamp, ts)
	r.Header.Set(signature.HeaderSlackSignature, "v0="+hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	g.c.ServeHTTP(w, r)
	return w
}

func (g *gate) counts() (asked []string, raised int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.asked...), len(g.raised)
}

var eventSeq int

// eventBody is an Events API delivery. team is the delivery's team_id; ""
// leaves it out.
func eventBody(team, inner string) string {
	eventSeq++
	head := `{"type":"event_callback",`
	if team != "" {
		head += `"team_id":"` + team + `",`
	}
	return head + `"event_id":"Ev` + strconv.Itoa(eventSeq) + `","event":` + inner + `}`
}

func commandBody(team, user string) string {
	eventSeq++
	form := url.Values{"channel_id": {"C02CHANNEL"}, "user_id": {user}, "command": {"/run-tests"}, "text": {"now"}, "trigger_id": {"trig" + strconv.Itoa(eventSeq)}}
	if team != "" {
		form.Set("team_id", team)
	}
	return form.Encode()
}

func interactiveBody(team, user, userTeam string) string {
	payload := map[string]any{
		"type": "block_actions", "team": map[string]string{"id": team},
		"user":    map[string]string{"id": user, "team_id": userTeam},
		"channel": map[string]string{"id": "C02CHANNEL"},
		"actions": []map[string]string{{"action_id": "stop", "value": "run_1"}},
	}
	raw, _ := json.Marshal(payload)
	return url.Values{"payload": {string(raw)}}.Encode()
}

func mentionBy(user, extra string) string {
	return `{"type":"app_mention","user":"` + user + `","text":"<@U08BOTID> go","ts":"1700000090.001200","channel":"C02CHANNEL"` + extra + `}`
}

// Every way a delivery asks for work is raised for the person the client
// maps, from the client's team.
func TestAMappedSenderIsRaised(t *testing.T) {
	for name, c := range map[string]struct{ path, body string }{
		"a mention":           {"", eventBody(senderTeam, mentionBy("U0ANA", ""))},
		"a direct message":    {"", eventBody(senderTeam, `{"type":"message","channel_type":"im","user":"U0ANA","text":"go","ts":"1700000091.000100","channel":"D01"}`)},
		"a reply in a thread": {"", eventBody(senderTeam, `{"type":"message","channel_type":"channel","user":"U0ANA","text":"and then?","ts":"1700000092.000100","thread_ts":"`+knownThread+`","channel":"C02CHANNEL"}`)},
		"a shared file":       {"", eventBody(senderTeam, `{"type":"message","subtype":"file_share","channel_type":"im","user":"U0ANA","text":"read this","ts":"1700000093.000100","channel":"D01"}`)},
		"a slash command":     {"/commands", commandBody(senderTeam, "U0ANA")},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGate(t)
			if w := g.post(c.path, c.body); w.Code != http.StatusOK || w.Body.Len() != 0 {
				t.Fatalf("answered %d %q", w.Code, w.Body.String())
			}
			asked, raised := g.counts()
			if len(asked) != 1 || asked[0] != senderTeam+"/U0ANA" || raised != 1 {
				t.Fatalf("asked about %v and raised %d, want one request for the mapped person", asked, raised)
			}
		})
	}
}

// A delivery from anyone else is acknowledged and raises nothing: someone
// the client does not map, a mapped user id that belongs to another team,
// and a delivery that names no team.
func TestAnyOtherSenderIsNotRaised(t *testing.T) {
	for name, c := range map[string]struct{ path, body, asked string }{
		"a mention from someone unmapped":          {"", eventBody(senderTeam, mentionBy("U0EVE", "")), senderTeam + "/U0EVE"},
		"a direct message from someone unmapped":   {"", eventBody(senderTeam, `{"type":"message","channel_type":"im","user":"U0EVE","text":"go","ts":"1700000091.000100","channel":"D01"}`), senderTeam + "/U0EVE"},
		"a thread reply from someone unmapped":     {"", eventBody(senderTeam, `{"type":"message","channel_type":"channel","user":"U0EVE","text":"and then?","ts":"1700000092.000100","thread_ts":"`+knownThread+`","channel":"C02CHANNEL"}`), senderTeam + "/U0EVE"},
		"a shared file from someone unmapped":      {"", eventBody(senderTeam, `{"type":"message","subtype":"file_share","channel_type":"im","user":"U0EVE","text":"read this","ts":"1700000093.000100","channel":"D01","files":[{"id":"F01"}]}`), senderTeam + "/U0EVE"},
		"a mention by a user of another team":      {"", eventBody(senderTeam, mentionBy("U0ANA", `,"user_team":"T09OTHER"`)), "T09OTHER/U0ANA"},
		"a mention whose event names another team": {"", eventBody(senderTeam, mentionBy("U0ANA", `,"team":"T09OTHER"`)), "T09OTHER/U0ANA"},
		"a mention delivered for another team":     {"", eventBody("T09OTHER", mentionBy("U0ANA", "")), "T09OTHER/U0ANA"},
		"a mention that names no team":             {"", eventBody("", mentionBy("U0ANA", "")), "/U0ANA"},
		"an app's user that is not mapped":         {"", eventBody(senderTeam, mentionBy("U08BOTID", "")), senderTeam + "/U08BOTID"},
		"a button from someone unmapped":           {"/interactive", interactiveBody(senderTeam, "U0EVE", senderTeam), senderTeam + "/U0EVE"},
		"a button by a user of another team":       {"/interactive", interactiveBody(senderTeam, "U0ANA", "T09OTHER"), "T09OTHER/U0ANA"},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGate(t)
			if w := g.post(c.path, c.body); w.Code != http.StatusOK || w.Body.Len() != 0 {
				t.Fatalf("answered %d %q, want an empty 200", w.Code, w.Body.String())
			}
			asked, raised := g.counts()
			if len(asked) != 1 || asked[0] != c.asked {
				t.Fatalf("asked about %v, want %s", asked, c.asked)
			}
			if raised != 0 || len(g.fake.Messages()) != 0 {
				t.Fatalf("raised %d and posted %d, want nothing", raised, len(g.fake.Messages()))
			}
		})
	}
}

// A slash command from anyone else is answered to them alone, once: the
// same person asking again is acknowledged with nothing said.
func TestASlashCommandFromAnyOtherSenderIsAnsweredOnce(t *testing.T) {
	for name, c := range map[string]struct{ team, user string }{
		"someone unmapped":     {senderTeam, "U0EVE"},
		"another team":         {"T09OTHER", "U0ANA"},
		"no team on the form":  {"", "U0ANA"},
		"an app's unmapped id": {senderTeam, "U08BOTID"},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGate(t)
			w := g.post("/commands", commandBody(c.team, c.user))
			var reply struct {
				Type string `json:"response_type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || w.Code != http.StatusOK {
				t.Fatalf("answered %d %q: %v", w.Code, w.Body.String(), err)
			}
			if reply.Type != "ephemeral" || reply.Text != slack.NotConnected {
				t.Fatalf("reply = %+v, want the ephemeral not-connected reply", reply)
			}
			if w := g.post("/commands", commandBody(c.team, c.user)); w.Code != http.StatusOK || w.Body.Len() != 0 {
				t.Fatalf("asking again answered %d %q, want an empty 200", w.Code, w.Body.String())
			}
			if _, raised := g.counts(); raised != 0 || len(g.fake.Messages()) != 0 {
				t.Fatalf("raised %d and posted %d, want nothing", raised, len(g.fake.Messages()))
			}
		})
	}
}

// What asks for nothing is not a sender's to be asked about: a bot's post,
// an edit, a message in a channel the app only sits in and a reply in a
// thread the client did not answer in go on to the transport, which raises
// none of them. Nobody is looked up for an ordinary channel message.
func TestDeliveriesThatAskForNothingRaiseNothing(t *testing.T) {
	for name, c := range map[string]struct {
		inner string
		asked int
	}{
		"a bot's post":              {`{"type":"message","channel_type":"im","bot_id":"B01","user":"U0ANA","text":"go","ts":"1700000091.000100","channel":"D01"}`, 0},
		"a bot's mention":           {`{"type":"app_mention","bot_id":"B01","text":"<@U08BOTID> go","ts":"1700000091.000100","channel":"C02CHANNEL"}`, 0},
		"a post with no user":       {`{"type":"message","channel_type":"im","text":"go","ts":"1700000091.000100","channel":"D01"}`, 0},
		"an edit":                   {`{"type":"message","subtype":"message_changed","channel_type":"im","user":"U0ANA","ts":"1700000091.000100","channel":"D01"}`, 0},
		"a plain channel message":   {`{"type":"message","channel_type":"channel","user":"U0ANA","text":"lunch?","ts":"1700000091.000100","channel":"C02CHANNEL"}`, 0},
		"a reply in another thread": {`{"type":"message","channel_type":"channel","user":"U0ANA","text":"ok","ts":"1700000091.000100","thread_ts":"1600000000.000100","channel":"C02CHANNEL"}`, 0},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGate(t)
			if w := g.post("", eventBody(senderTeam, c.inner)); w.Code != http.StatusOK {
				t.Fatalf("answered %d", w.Code)
			}
			if asked, raised := g.counts(); len(asked) != c.asked || raised != 0 {
				t.Fatalf("asked about %v and raised %d", asked, raised)
			}
		})
	}
}

// A request that is not signed is the transport's to refuse; nobody is
// asked about its sender.
func TestAnUnsignedRequestIsNotReadForItsSender(t *testing.T) {
	g := newGate(t)
	r := httptest.NewRequest(http.MethodPost, "/connectors/slack", strings.NewReader(eventBody(senderTeam, mentionBy("U0ANA", ""))))
	w := httptest.NewRecorder()
	g.c.ServeHTTP(w, r)
	if asked, raised := g.counts(); w.Code == http.StatusOK || len(asked) != 0 || raised != 0 {
		t.Fatalf("answered %d, asked about %v and raised %d", w.Code, asked, raised)
	}
}

// A stop the host refuses leaves the session as it is: nothing is said, the
// status stays, and the run can still be stopped by someone who may.
func TestARefusedStopLeavesTheSessionAndItsRun(t *testing.T) {
	g := newGate(t)
	deliver(t, g.c, target, runEvent(1, event.TypeAccepted, ""), runEvent(2, event.TypeStarted, ""))
	before := len(g.fake.Statuses())

	var mu sync.Mutex
	var presses []string
	may := map[string]bool{"U0ANA": true}
	g.c.OnStop(func(_ context.Context, run, user string) error {
		mu.Lock()
		defer mu.Unlock()
		presses = append(presses, run+" by "+user)
		if !may[user] {
			return fmt.Errorf("%w: not theirs", slack.ErrStopRefused)
		}
		return nil
	})
	stop := func(user string) string {
		return eventBody(senderTeam, `{"type":"agent_session_stopped","user":"`+user+`","channel":"C02CHANNEL","thread_ts":"`+knownThread+`"}`)
	}
	if w := g.post("", stop("U0EVE")); w.Code != http.StatusOK {
		t.Fatalf("a refused stop answered %d, want 200 so it is not delivered again", w.Code)
	}
	if n := stopsSaid(g.fake); n != 0 || len(g.fake.Statuses()) != before {
		t.Fatalf("a refused stop was confirmed %d times and set %+v", n, g.fake.Statuses()[before:])
	}

	if w := g.post("", stop("U0ANA")); w.Code != http.StatusOK {
		t.Fatalf("stop answered %d", w.Code)
	}
	mu.Lock()
	got := strings.Join(presses, ", ")
	mu.Unlock()
	if got != "run_1 by U0EVE, run_1 by U0ANA" {
		t.Fatalf("presses = %s", got)
	}
	statuses := g.fake.Statuses()
	if n := stopsSaid(g.fake); n != 1 {
		t.Fatalf("the stop was confirmed %d times, want once: %+v", n, g.fake.Messages())
	}
	if last := statuses[len(statuses)-1]; last.Status != slack.StatusActive {
		t.Fatalf("session = %+v, want active", last)
	}
}

// stopsSaid counts the stop confirmations posted.
func stopsSaid(f *slack.Fake) int {
	n := 0
	for _, m := range f.Messages() {
		if strings.HasPrefix(m.Text, "Stopped by <@") {
			n++
		}
	}
	return n
}
