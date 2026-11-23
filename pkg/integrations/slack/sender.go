package slack

import (
	"encoding/json"
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
}

// requestSender reads who a verified delivery asks as: a slash command's
// user and team, an interactive payload's user and their team, or a message
// or mention's user and the team they belong to (a Slack Connect user's own
// team, else the delivery's). ok is false for a delivery that asks for
// nothing: reactions, stops, bot posts, edits and the rest go on to the
// transport as before.
func requestSender(path string, body []byte) (sender, bool) {
	switch {
	case strings.HasSuffix(path, transport.CommandsPath):
		form, err := url.ParseQuery(string(body))
		if err != nil || form.Get("user_id") == "" {
			return sender{}, false
		}
		return sender{team: form.Get("team_id"), user: form.Get("user_id"), command: true}, true
	case strings.HasSuffix(path, transport.InteractivityPath):
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return sender{}, false
		}
		var p struct {
			Team struct {
				ID string `json:"id"`
			} `json:"team"`
			User struct {
				ID   string `json:"id"`
				Team string `json:"team_id"`
			} `json:"user"`
		}
		if json.Unmarshal([]byte(form.Get("payload")), &p) != nil || p.User.ID == "" {
			return sender{}, false
		}
		return sender{team: first(p.User.Team, p.Team.ID), user: p.User.ID}, true
	}
	var env struct {
		Type  string `json:"type"`
		Team  string `json:"team_id"`
		Event struct {
			Type     string `json:"type"`
			Subtype  string `json:"subtype"`
			User     string `json:"user"`
			BotID    string `json:"bot_id"`
			UserTeam string `json:"user_team"`
			Team     string `json:"team"`
		} `json:"event"`
	}
	if json.Unmarshal(body, &env) != nil || env.Type != "event_callback" {
		return sender{}, false
	}
	ev := env.Event
	if ev.Type != "app_mention" && ev.Type != "message" {
		return sender{}, false
	}
	if ev.BotID != "" || ev.User == "" || (ev.Subtype != "" && ev.Subtype != "file_share") {
		return sender{}, false
	}
	return sender{team: first(ev.UserTeam, ev.Team, env.Team), user: ev.User}, true
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

// refuse answers a verified request from a sender the client does not map
// and reports that it did. Nothing of the request is read further, no file
// is fetched and nothing is raised. A slash command is told, once in a while,
// that the sender's account is not connected; anything else is acknowledged
// and dropped, so Slack does not deliver it again.
func (c *Connector) refuse(w http.ResponseWriter, r *http.Request, body []byte) bool {
	if c.cfg.Sender == nil || c.cfg.Verifier == nil || c.cfg.Verifier.Verify(r.Header, body) != nil {
		return false
	}
	who, ok := requestSender(r.URL.Path, body)
	if !ok || c.cfg.Sender(who.team, who.user) {
		return false
	}
	if c.cfg.Log != nil {
		c.cfg.Log.Info("slack: refused a sender the client does not map", "team", who.team, "user", who.user, "command", who.command)
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
