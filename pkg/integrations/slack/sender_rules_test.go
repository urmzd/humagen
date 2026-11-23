package slack_test

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/urmzd/humagen/pkg/integrations/slack"
	"github.com/urmzd/saige/flow/blob"
	"github.com/urmzd/saige/flow/connectors"
	transport "github.com/urmzd/saige/flow/connectors/slack"
	"github.com/urmzd/saige/flow/event"
)

// Nothing is raised unless its sender was asked about first: every event
// type, subtype, conversation kind and thread the transport reads, from a
// person the client maps, from someone it does not, and from a mapped user
// id that belongs to another team. Only the mapped person's are raised.
func TestNothingIsRaisedBeforeItsSenderIsAskedAbout(t *testing.T) {
	senders := map[string]string{
		"mapped":             `"user":"U0ANA"`,
		"unmapped":           `"user":"U0EVE"`,
		"another user team":  `"user":"U0ANA","user_team":"T09OTHER"`,
		"another event team": `"user":"U0ANA","team":"T09OTHER"`,
	}
	for name, sender := range senders {
		t.Run(name, func(t *testing.T) {
			g := newGate(t)
			raisedByMapped := 0
			for _, kind := range []string{"app_mention", "message"} {
				for _, subtype := range []string{"", "file_share", "thread_broadcast", "me_message", "message_changed", "channel_join", "bot_message"} {
					for _, conversation := range []string{"im", "channel", "group", "mpim"} {
						for _, thread := range []string{"", knownThread, "1600000000.000100"} {
							eventSeq++
							inner := `{"type":"` + kind + `",` + sender + `,"channel_type":"` + conversation + `","text":"<@U08BOTID> go","ts":"1700000100.` + fmt.Sprintf("%06d", eventSeq) + `","channel":"C02CHANNEL"`
							if subtype != "" {
								inner += `,"subtype":"` + subtype + `"`
							}
							if thread != "" {
								inner += `,"thread_ts":"` + thread + `"`
							}
							inner += `}`
							g.reset()
							shape := kind + "/" + subtype + "/" + conversation + "/" + thread
							if w := g.post("", eventBody(senderTeam, inner)); w.Code != http.StatusOK || w.Body.Len() != 0 {
								t.Fatalf("%s answered %d %q", shape, w.Code, w.Body.String())
							}
							asked, raised := g.counts()
							if name != "mapped" && raised != 0 {
								t.Errorf("%s was raised for a sender the client does not act for", shape)
							}
							if raised > 0 && len(asked) == 0 {
								t.Errorf("%s was raised and nobody was asked about its sender", shape)
							}
							raisedByMapped += raised
						}
					}
				}
			}
			g.mu.Lock()
			unasked := g.unasked
			g.mu.Unlock()
			if len(unasked) != 0 {
				t.Errorf("raised without the sender being asked about: %v", unasked)
			}
			if len(g.fake.Messages()) != 0 {
				t.Errorf("%d messages were posted", len(g.fake.Messages()))
			}
			// Every mention, and each message that is a direct message or a
			// reply in the known thread with no subtype or a shared file's.
			if want := 7*4*3 + 2*(3+3); name == "mapped" && raisedByMapped != want {
				t.Errorf("the mapped person's events raised %d, want %d", raisedByMapped, want)
			}
		})
	}
}

// A mention that carries a subtype is a mention: its sender is asked about,
// and it is raised only for a person the client maps.
func TestAMentionWithASubtypeIsHeldToItsSender(t *testing.T) {
	for _, subtype := range []string{"thread_broadcast", "me_message", "file_share"} {
		g := newGate(t)
		if w := g.post("", eventBody(senderTeam, mentionBy("U0EVE", `,"subtype":"`+subtype+`"`))); w.Code != http.StatusOK {
			t.Fatalf("%s answered %d", subtype, w.Code)
		}
		if asked, raised := g.counts(); len(asked) != 1 || asked[0] != senderTeam+"/U0EVE" || raised != 0 {
			t.Errorf("%s from someone unmapped: asked about %v and raised %d", subtype, asked, raised)
		}
		g.reset()
		if w := g.post("", eventBody(senderTeam, mentionBy("U0ANA", `,"subtype":"`+subtype+`"`))); w.Code != http.StatusOK {
			t.Fatalf("%s answered %d", subtype, w.Code)
		}
		if asked, raised := g.counts(); len(asked) != 1 || raised != 1 {
			t.Errorf("%s from the mapped person: asked about %v and raised %d", subtype, asked, raised)
		}
	}
}

// A thanks in a thread the client answered in gets a reaction from the app
// for a person the client maps, and nothing for anyone else.
func TestAThanksIsAcknowledgedOnlyForAMappedSender(t *testing.T) {
	g := newGate(t, func(cfg *slack.Config) { cfg.StatusReactions = true })
	thanks := func(user string) slack.FakeMessage {
		t.Helper()
		ts := g.fake.Say(senderTeam, "C02CHANNEL", knownThread, user, "thanks")
		inner := `{"type":"message","channel_type":"channel","user":"` + user + `","text":"thanks","ts":"` + ts + `","thread_ts":"` + knownThread + `","channel":"C02CHANNEL"}`
		if w := g.post("", eventBody(senderTeam, inner)); w.Code != http.StatusOK {
			t.Fatalf("answered %d", w.Code)
		}
		m, _ := g.fake.Message(ts)
		return m
	}
	if m := thanks("U0EVE"); len(m.Reactions) != 0 {
		t.Fatalf("the app reacted to someone it does not act for: %v", m.Reactions)
	}
	if m := thanks("U0ANA"); len(m.Reactions) != 1 {
		t.Fatalf("the mapped person's thanks got %v, want one reaction", m.Reactions)
	}
	if _, raised := g.counts(); raised != 0 {
		t.Fatalf("a thanks raised %d events", raised)
	}
}

// fileReads counts the files the connector asks Slack about.
type fileReads struct {
	*slack.Fake
	n atomic.Int64
}

func (f *fileReads) FileInfo(ctx context.Context, workspace, id string) (transport.File, error) {
	f.n.Add(1)
	return f.Fake.FileInfo(ctx, workspace, id)
}

// A file on a message is read from Slack only for a person the client maps.
func TestAFileIsReadOnlyForAMappedSender(t *testing.T) {
	var api *fileReads
	g := newGate(t, func(cfg *slack.Config) {
		api = &fileReads{Fake: cfg.API.(*slack.Fake)}
		cfg.API, cfg.Files, cfg.FileToken = api, connectors.Files{Store: blob.NewMemory()}, "xoxb-test"
	})
	shared := func(user string) string {
		return eventBody(senderTeam, `{"type":"message","subtype":"file_share","channel_type":"im","user":"`+user+`","text":"read this","ts":"1700000093.000100","channel":"D01","files":[{"id":"F01","file_access":"check_file_info"}]}`)
	}
	if w := g.post("", shared("U0EVE")); w.Code != http.StatusOK {
		t.Fatalf("answered %d", w.Code)
	}
	if n := api.n.Load(); n != 0 {
		t.Fatalf("%d files were read for someone the client does not act for", n)
	}
	if w := g.post("", shared("U0ANA")); w.Code != http.StatusOK {
		t.Fatalf("answered %d", w.Code)
	}
	if n := api.n.Load(); n != 1 {
		t.Fatalf("%d files were read for the mapped person, want 1", n)
	}
}

// Many commands at once from people the client does not map: each person is
// told once, at most twenty are told in a minute, and nothing is raised.
func TestTheNotConnectedReplyHoldsUnderConcurrency(t *testing.T) {
	g := newGate(t)
	var same, others atomic.Int64
	var wg sync.WaitGroup
	ask := func(body string, told *atomic.Int64) {
		defer wg.Done()
		w := g.post("/commands", body)
		if w.Code != http.StatusOK {
			t.Errorf("answered %d", w.Code)
		}
		if strings.Contains(w.Body.String(), slack.NotConnected) {
			told.Add(1)
		}
	}
	for range 50 {
		wg.Add(1)
		go ask(commandBody(senderTeam, "U0SAME"), &same)
	}
	wg.Wait()
	for i := range 200 {
		wg.Add(1)
		go ask(commandBody(senderTeam, "U0X"+strconv.Itoa(i)), &others)
	}
	wg.Wait()
	if _, raised := g.counts(); same.Load() != 1 || same.Load()+others.Load() > 20 || raised != 0 || len(g.fake.Messages()) != 0 {
		t.Fatalf("one person was told %d times, %d others were told, %d raised", same.Load(), others.Load(), raised)
	}
}

// A press confirms a stop only for a run the host asked to stop. A run that
// is not the host's to stop is neither confirmed nor settled, whoever
// pressed, and the session's status stays.
func TestAStopIsConfirmedOnlyForARunThatWasAskedToStop(t *testing.T) {
	for _, user := range []string{"U0EVE", "U0ANA"} {
		g := newGate(t)
		deliver(t, g.c, target, runEvent(1, event.TypeAccepted, ""), runEvent(2, event.TypeStarted, ""))
		before := len(g.fake.Statuses())
		var presses atomic.Int64
		g.c.OnStop(func(context.Context, string, string) error {
			presses.Add(1)
			return fmt.Errorf("%w: another client's", slack.ErrNothingToStop)
		})
		body := func() string {
			return eventBody(senderTeam, `{"type":"agent_session_stopped","user":"`+user+`","channel":"C02CHANNEL","thread_ts":"`+knownThread+`"}`)
		}
		for range 2 {
			if w := g.post("", body()); w.Code != http.StatusOK {
				t.Fatalf("%s's press answered %d", user, w.Code)
			}
		}
		if presses.Load() != 2 {
			t.Errorf("%s's presses reached the host %d times, want 2: the run stays the session's", user, presses.Load())
		}
		if n := stopsSaid(g.fake); n != 0 || len(g.fake.Statuses()) != before {
			t.Errorf("%s's press on a run that did not stop was confirmed %d times and set %+v", user, n, g.fake.Statuses()[before:])
		}
	}
}

// With no way to stop a run given, a press confirms nothing.
func TestAStopWithNoHostConfirmsNothing(t *testing.T) {
	g := newGate(t)
	deliver(t, g.c, target, runEvent(1, event.TypeAccepted, ""), runEvent(2, event.TypeStarted, ""))
	before := len(g.fake.Statuses())
	if w := g.post("", eventBody(senderTeam, `{"type":"agent_session_stopped","user":"U0ANA","channel":"C02CHANNEL","thread_ts":"`+knownThread+`"}`)); w.Code != http.StatusOK {
		t.Fatalf("answered %d", w.Code)
	}
	if n := stopsSaid(g.fake); n != 0 || len(g.fake.Statuses()) != before {
		t.Fatalf("confirmed %d times and set %+v", n, g.fake.Statuses()[before:])
	}
}

// In a session with no run known, a press settles the status for a person
// the client maps and does nothing for anyone else.
func TestAStopWithNoRunKnownSettlesTheStatusOnlyForAMappedPerson(t *testing.T) {
	g := newGate(t)
	g.c.OnStop(func(context.Context, string, string) error {
		t.Error("the host was asked to stop a run in a session with none")
		return nil
	})
	press := func(user string) {
		t.Helper()
		if w := g.post("", eventBody(senderTeam, `{"type":"agent_session_stopped","user":"`+user+`","channel":"C09","thread_ts":"1700000000.000900"}`)); w.Code != http.StatusOK {
			t.Fatalf("%s's press answered %d", user, w.Code)
		}
	}
	press("U0EVE")
	if len(g.fake.Statuses()) != 0 || len(g.fake.Messages()) != 0 {
		t.Fatalf("a press by someone unmapped set %+v and posted %+v", g.fake.Statuses(), g.fake.Messages())
	}
	press("U0ANA")
	if st := g.fake.Statuses(); len(st) != 1 || st[0].Status != slack.StatusActive || len(g.fake.Messages()) != 0 {
		t.Fatalf("a mapped person's press set %+v and posted %+v", st, g.fake.Messages())
	}
}
