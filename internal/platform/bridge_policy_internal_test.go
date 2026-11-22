package platform

import (
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/threads"
)

const (
	slackRoot   = "s_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	slackChild  = "s_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	consoleRoot = "s_cccccccccccccccccccccccccccccccc"
	namedRoot   = "s_dddddddddddddddddddddddddddddddd"
	plainThread = "th-eeeeeeeeeeeeeeee"
)

// Only a session the Slack thread itself started may answer in that Slack
// conversation. A private session from anywhere else is refused, even one
// that names the Slack place as its parent, and so is the Slack thread's
// session in any other channel or through any other client.
func TestOnlyTheSlackThreadsOwnPrivateSessionAnswersThere(t *testing.T) {
	p, run, _ := backgroundPlatform(t)
	c := clients.Client{ID: "app", Tenant: "acme", Surface: clients.Slack, Enabled: true}
	at := time.Now()
	owner := []threads.Participant{{Kind: threads.Person, Name: "ada", Owner: true, At: at}}
	put := func(th threads.Thread) {
		t.Helper()
		th.Project, th.Channel, th.Visibility, th.Participants, th.CreatedBy = "Field", projects.General, threads.Private, owner, "ada"
		if th.Session != nil {
			th.Default, th.Agents = "lead", []string{"lead"}
		}
		if _, err := p.Threads.Create("acme", th, at); err != nil {
			t.Fatal(err)
		}
	}
	put(threads.Thread{ID: slackRoot, Surface: &threads.Surface{Client: "app", Channel: "C0PUBLIC", TS: "1700000000.000100"}, Session: &threads.SessionIdentity{Agent: "lead", Parent: "slack://T0/C0PUBLIC/1700000000.000100"}})
	put(threads.Thread{ID: slackChild, Session: &threads.SessionIdentity{Agent: "lead", Parent: slackRoot}})
	put(threads.Thread{ID: consoleRoot, Session: &threads.SessionIdentity{Agent: "lead", Parent: "console:ada"}})
	put(threads.Thread{ID: namedRoot, Session: &threads.SessionIdentity{Agent: "lead", Parent: "slack://T0/C0PUBLIC/1700000000.000100"}})
	put(threads.Thread{ID: plainThread})
	for _, tc := range []struct {
		client          clients.Client
		thread, channel string
		want            bool
	}{
		{c, slackRoot, "C0PUBLIC", true},
		{c, slackChild, "C0PUBLIC", true},
		{c, slackRoot, "C0OTHER", false},
		{c, slackChild, "G0OTHER", false},
		{clients.Client{ID: "other", Tenant: "acme", Surface: clients.Slack, Enabled: true}, slackRoot, "C0PUBLIC", false},
		{c, consoleRoot, "C0PUBLIC", false},
		{c, namedRoot, "C0PUBLIC", false},
		{c, plainThread, "C0PUBLIC", false},
	} {
		r := run
		r.Thread = tc.thread
		if got := p.bridgeMayDeliver(t.Context(), tc.client, tc.channel, r); got != tc.want {
			t.Errorf("%s's run through %s to %s = %v, want %v", tc.thread, tc.client.ID, tc.channel, got, tc.want)
		}
	}
}
