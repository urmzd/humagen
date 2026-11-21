package platform

import (
	"testing"

	"github.com/urmzd/humagen/pkg/threads"
)

// A conversation says where it started: the Slack thread or direct message
// it lives on, or the place its session was started from. The console and
// the API say nothing.
func TestAThreadSaysWhereItStarted(t *testing.T) {
	session := func(parent string) threads.Thread {
		return threads.Thread{Session: &threads.SessionIdentity{Agent: "helper", Parent: parent}}
	}
	for _, c := range []struct {
		name string
		th   threads.Thread
		want string
	}{
		{"a Slack channel thread", threads.Thread{Surface: &threads.Surface{Client: "acme", Channel: "C0123", TS: "1.2"}}, SourceSlack},
		{"a Slack direct message", threads.Thread{Surface: &threads.Surface{Client: "acme", Channel: "D0123", TS: "1.2"}}, SourceSlackDM},
		{"a session from a Slack thread", session("slack://T1/C0123/1.2"), SourceSlack},
		{"a session from a Slack direct message", session("slack://T1/D0123/1.2"), SourceSlackDM},
		{"a session from a pull request", session("github://acme/api/pull/482"), SourceGitHub},
		{"a scheduled session", session(ScheduleParent("nightly")), SourceScheduled},
		{"a console session", session("console://0123abcd"), ""},
		{"a thread from before sessions", threads.Thread{}, ""},
	} {
		if got := threadSource(c.th); got != c.want {
			t.Errorf("%s: source = %q, want %q", c.name, got, c.want)
		}
	}
}
