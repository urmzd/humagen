package slack

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	transport "github.com/urmzd/saige/flow/connectors/slack"
)

// SenderFunc reports whether a Slack user in a team is someone the client
// acts for. The connector asks it before a request is read any further; the
// platform decides again when it stamps what the connector raised.
type SenderFunc func(team, user string) bool

// NotConnected is the one reply a sender the client does not map gets, to a
// slash command only.
const NotConnected = "Your Slack account is not connected to Humagen, so nothing was started. Ask a workspace admin to connect your account."

// A sender the client does not map is told at most once in turnAwayEvery,
// and at most turnAwayPerMinute senders are told in a minute.
const (
	turnAwayEvery     = 10 * time.Minute
	turnAwayPerMinute = 20
	turnAwayKept      = 10000
)

// sender is who a delivery asks as.
type sender struct {
	team, user string
	command    bool
	// noTeam says the delivery named the sender's team with something that
	// is not a string, so they belong to none.
	noTeam bool
}

// requestSender reads who a verified delivery asks as: a slash command's
// user and team, an interactive payload's user and their team, or the user
// of a message or mention the transport would raise and the team they belong
// to (a Slack Connect user's own team, else the delivery's). ok is false for
// a delivery that asks for nothing: reactions, stops, bot posts, edits, a
// plain message in a channel and a reply in a thread the client did not
// answer in go on to the transport, which raises none of them.
//
// Which messages ask follows the transport's own reading (eventFrom): every
// app_mention, whatever its subtype, and a message that is a direct message
// or a reply in a thread the client answered in, with no subtype or a shared
// file's. A test holds the two together: nothing is raised that was not
// asked about here first.
//
// errUnread is the answer for a delivery this reading cannot make out: its
// form or its JSON does not parse into the fields read here. Such a delivery
// goes no further, since what cannot be read here cannot be asked about. A
// team named by something other than a string is no team: the sender is
// asked about with none, which is nobody's.
func (c *Connector) requestSender(path string, body []byte) (sender, bool, error) {
	switch {
	case strings.HasSuffix(path, transport.CommandsPath):
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return sender{}, false, errUnread
		}
		if form.Get("user_id") == "" {
			return sender{}, false, nil
		}
		return sender{team: form.Get("team_id"), user: form.Get("user_id"), command: true}, true, nil
	case strings.HasSuffix(path, transport.InteractivityPath):
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return sender{}, false, errUnread
		}
		var p struct {
			Team struct {
				ID string `json:"id"`
			} `json:"team"`
			User struct {
				ID   string          `json:"id"`
				Team json.RawMessage `json:"team_id"`
			} `json:"user"`
		}
		if json.Unmarshal([]byte(form.Get("payload")), &p) != nil {
			return sender{}, false, errUnread
		}
		if p.User.ID == "" {
			return sender{}, false, nil
		}
		own, ok := teamOf(p.User.Team)
		if !ok {
			return sender{user: p.User.ID, noTeam: true}, true, nil
		}
		return sender{team: first(own, p.Team.ID), user: p.User.ID}, true, nil
	}
	// The fields the transport reads are typed as it types them, so what it
	// can read this can. The sender's own team, which only this reads, is
	// taken as it comes.
	var env struct {
		Type  string `json:"type"`
		Team  string `json:"team_id"`
		Event struct {
			Type        string          `json:"type"`
			Subtype     string          `json:"subtype"`
			User        string          `json:"user"`
			BotID       string          `json:"bot_id"`
			UserTeam    json.RawMessage `json:"user_team"`
			Team        json.RawMessage `json:"team"`
			Channel     string          `json:"channel"`
			ChannelType string          `json:"channel_type"`
			ThreadTS    string          `json:"thread_ts"`
		} `json:"event"`
	}
	if json.Unmarshal(body, &env) != nil {
		return sender{}, false, errUnread
	}
	if env.Type != "event_callback" {
		return sender{}, false, nil
	}
	ev := env.Event
	if ev.BotID != "" || ev.User == "" {
		return sender{}, false, nil
	}
	switch ev.Type {
	case "app_mention":
	case "message":
		if ev.Subtype != "" && ev.Subtype != "file_share" {
			return sender{}, false, nil
		}
		if ev.ChannelType != "im" && (ev.ThreadTS == "" || c.cfg.ContinueThread == nil || !c.cfg.ContinueThread(env.Team, ev.Channel, ev.ThreadTS)) {
			return sender{}, false, nil
		}
	default:
		return sender{}, false, nil
	}
	userTeam, ok := teamOf(ev.UserTeam)
	eventTeam, also := teamOf(ev.Team)
	if !ok || !also {
		return sender{user: ev.User, noTeam: true}, true, nil
	}
	return sender{team: first(userTeam, eventTeam, env.Team), user: ev.User}, true, nil
}

// errUnread is requestSender's answer for a delivery it cannot make out.
var errUnread = errors.New("slack: the delivery could not be read for its sender")

// teamOf reads a team a delivery names. A field left out, or null, names
// none; ok is false for one that is not a string, which names no team.
func teamOf(raw json.RawMessage) (team string, ok bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", true
	}
	if json.Unmarshal(raw, &team) != nil {
		return "", false
	}
	return team, true
}

func first(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// turnAway decides whether a refused sender is told so.
type turnAway struct {
	mu     sync.Mutex
	last   map[string]time.Time
	minute time.Time
	told   int
}

func (t *turnAway) allow(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == nil {
		t.last = map[string]time.Time{}
	}
	if now.Sub(t.minute) >= time.Minute {
		t.minute, t.told = now, 0
	}
	if at, ok := t.last[key]; ok && now.Sub(at) < turnAwayEvery {
		return false
	}
	if t.told >= turnAwayPerMinute {
		return false
	}
	if len(t.last) >= turnAwayKept {
		for k, at := range t.last {
			if now.Sub(at) >= turnAwayEvery {
				delete(t.last, k)
			}
		}
		if len(t.last) >= turnAwayKept {
			return false
		}
	}
	t.last[key] = now
	t.told++
	return true
}

// senderKey carries a request's sender to Prepare, which writes their team
// on what the transport raises.
type senderKey struct{}

// asks reads who a request asks as, once its signature verifies. ok is
// false for a request that is not verified here, or asks for nothing; the
// error is errUnread for a verified one that cannot be read.
func (c *Connector) asks(r *http.Request, body []byte) (sender, bool, error) {
	if c.cfg.Verifier == nil || c.cfg.Verifier.Verify(r.Header, body) != nil {
		return sender{}, false, nil
	}
	return c.requestSender(r.URL.Path, body)
}

// refuse answers a verified request from a sender the client does not map
// and reports that it did. Nothing of the request is read further, no file
// is fetched, nothing is posted or reacted to and nothing is raised. A slash
// command is told, once in a while, that the sender's account is not
// connected; anything else is acknowledged and dropped, so Slack does not
// deliver it again.
func (c *Connector) refuse(w http.ResponseWriter, who sender) bool {
	if c.cfg.Sender == nil || c.cfg.Sender(who.team, who.user) {
		return false
	}
	if c.cfg.Log != nil {
		c.cfg.Log.Debug("slack: refused a sender the client does not map", "team", who.team, "user", who.user, "command", who.command)
	}
	now := time.Now()
	if c.cfg.Clock != nil {
		now = c.cfg.Clock()
	}
	if who.command && c.turned.allow(who.team+"/"+who.user, now) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"response_type": "ephemeral", "text": NotConnected})
		return true
	}
	w.WriteHeader(http.StatusOK)
	return true
}
