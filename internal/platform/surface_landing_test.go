package platform_test

import (
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
)

// An untagged message from a surface lands on the entry agent, as a console
// root does, by both routes a client's app sends one: posting it in a thread,
// and starting a turn for the person who sent it. It never waits for a
// person, with an open default or with none set anywhere.
func TestAnUntaggedSurfaceMessageLandsOnTheEntryAgent(t *testing.T) {
	for name, def := range map[string]string{"an open default": "*", "no default anywhere": ""} {
		t.Run(name, func(t *testing.T) {
			p, machine := slackRoomOn(t, withEntry(t, platform.Config{}), func(in *platform.ClientInput) { in.DefaultAgent = def })
			posted, err := p.StartThread(t.Context(), machine, fromSlack("D0ANA", "1700000000.000100", "", "<@UAPP> how do refunds work?"))
			if err != nil {
				t.Fatal(err)
			}
			if len(posted.Runs) != 1 || posted.Runs[0].Agent != platform.BuiltinEntry || posted.Runs[0].Client != "app" || strings.Contains(posted.Reason, "waits") {
				t.Fatalf("a DM to the app started %+v (%s), want the entry agent", posted.Runs, posted.Reason)
			}
			r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "api-dm", Text: "how do refunds work?", Actor: platform.PersonActor("ana"), Client: "app"})
			if err != nil || r.Agent != platform.BuiltinEntry || r.Picked != "" || r.Client != "app" {
				t.Fatalf("a turn for a person the app maps = %+v, %v; want the entry agent", r, err)
			}
		})
	}
}

// A client that may not route to the entry agent is answered by an agent it
// may route to, never the entry agent, and never left waiting.
func TestAClientThatMayNotRouteToTheEntryAgentStillLands(t *testing.T) {
	p, machine := slackRoomOn(t, withEntry(t, platform.Config{}), func(in *platform.ClientInput) {
		in.DefaultAgent, in.Agents = "*", []string{"helper"}
	})
	posted, err := p.StartThread(t.Context(), machine, fromSlack("D0ANA", "1700000000.000100", "", "<@UAPP> how do refunds work?"))
	if err != nil || len(posted.Runs) != 1 || posted.Runs[0].Agent != "helper" || posted.Runs[0].Picked == "" {
		t.Fatalf("a DM through a client that routes to helper only = %+v, %v", posted, err)
	}
	r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "api-dm", Text: "how do refunds work?", Actor: platform.PersonActor("ana"), Client: "app"})
	if err != nil || r.Agent != "helper" {
		t.Fatalf("a turn through a client that routes to helper only = %+v, %v", r, err)
	}
}
